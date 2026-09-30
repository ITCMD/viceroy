// Command viceroy is the Viceroy budgeting server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"viceroy/internal/auth"
	"viceroy/internal/categorize"
	"viceroy/internal/config"
	"viceroy/internal/db"
	"viceroy/internal/secrets"
	"viceroy/internal/server"
	"viceroy/internal/syncer"
	"viceroy/web"
)

const usage = `Usage: viceroy [-config path] <command>

Commands:
  init    write a default viceroy.toml
  serve   run the server (default)
`

func main() {
	fl := flag.NewFlagSet("viceroy", flag.ExitOnError)
	fl.Usage = func() { fmt.Fprint(os.Stderr, usage); fl.PrintDefaults() }
	cfgPath := fl.String("config", "viceroy.toml", "path to config file")
	fl.Parse(os.Args[1:])

	cmd := fl.Arg(0)
	if cmd == "" {
		cmd = "serve"
	}
	var err error
	switch cmd {
	case "init":
		err = runInit(*cfgPath)
	case "serve":
		err = runServe(*cfgPath)
	default:
		fl.Usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "viceroy:", err)
		os.Exit(1)
	}
}

func runInit(path string) error {
	if err := config.WriteTemplate(path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s already exists; not overwriting", path)
		}
		return err
	}
	fmt.Printf("Wrote %s. Edit it if needed, then run: viceroy serve\n", path)
	return nil
}

func runServe(path string) error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg, err := config.Load(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s not found; run `viceroy init` first", path)
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	conn, err := db.Open(filepath.Join(cfg.DataDir, "viceroy.db"))
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := categorize.SeedAll(context.Background(), db.New(conn)); err != nil {
		return fmt.Errorf("seeding categories: %w", err)
	}

	box, err := secrets.LoadOrCreate(filepath.Join(cfg.DataDir, "secret.key"))
	if err != nil {
		return err
	}
	sync := syncer.New(conn, box, log)

	webFS, err := web.Dist()
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           server.New(cfg, conn, webFS, log, sync).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go pruneSessions(ctx, auth.New(conn), log)
	go sync.Run(ctx)

	errc := make(chan error, 1)
	go func() {
		scheme := "http"
		if cfg.TLS.Cert != "" {
			scheme = "https"
		}
		log.Info("viceroy listening", "url", fmt.Sprintf("%s://%s", scheme, cfg.Listen), "allowed", cfg.AllowedCIDRs)
		if cfg.TLS.Cert != "" {
			errc <- srv.ListenAndServeTLS(cfg.TLS.Cert, cfg.TLS.Key)
		} else {
			errc <- srv.ListenAndServe()
		}
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func pruneSessions(ctx context.Context, a *auth.Service, log *slog.Logger) {
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		if err := a.PruneSessions(ctx); err != nil && ctx.Err() == nil {
			log.Warn("pruning sessions", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
