// Command viceroy is the Viceroy budgeting server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // TZ works in minimal containers

	"viceroy/internal/accounts"
	"viceroy/internal/aicat"
	"viceroy/internal/aisettings"
	"viceroy/internal/auth"
	"viceroy/internal/backup"
	"viceroy/internal/branding"
	"viceroy/internal/categorize"
	"viceroy/internal/config"
	"viceroy/internal/db"
	"viceroy/internal/email"
	"viceroy/internal/notify"
	"viceroy/internal/secrets"
	"viceroy/internal/server"
	"viceroy/internal/syncer"
	"viceroy/web"
)

const usage = `Usage: viceroy [-config path] <command>

Commands:
  init               write a default viceroy.toml
  serve [-init]      run the server (default); -init writes viceroy.toml first if it's missing
  backup [-o file]   write a backup now (default: into the backup dir from the config)
  restore <file>     restore a backup into data_dir (stop the server first)
  reset-password <email>
                     print a one-time link to set a new password for that user
  version            print the version

Environment overrides (useful in containers): ` + "VICEROY_LISTEN, VICEROY_ALLOWED_CIDRS (comma-separated),\n" +
	"VICEROY_TRUSTED_PROXIES, VICEROY_PUBLIC_URL, VICEROY_DATA_DIR, VICEROY_BACKUP_KEEP.\n\n"

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	fl := flag.NewFlagSet("viceroy", flag.ExitOnError)
	fl.Usage = func() { fmt.Fprint(os.Stderr, usage); fl.PrintDefaults() }
	cfgPath := fl.String("config", "viceroy.toml", "path to config file")
	fl.Parse(os.Args[1:])

	cmd, args := fl.Arg(0), fl.Args()
	if cmd == "" {
		cmd = "serve"
	} else {
		args = args[1:]
	}
	var err error
	switch cmd {
	case "init":
		err = runInit(*cfgPath)
	case "serve":
		sf := flag.NewFlagSet("serve", flag.ExitOnError)
		initFirst := sf.Bool("init", false, "write a default config first if it doesn't exist")
		sf.Parse(args)
		if *initFirst {
			if err = config.WriteTemplate(*cfgPath); errors.Is(err, fs.ErrExist) {
				err = nil
			} else if err == nil {
				fmt.Fprintf(os.Stderr, "viceroy: wrote default config to %s\n", *cfgPath)
			}
		}
		if err == nil {
			err = runServe(*cfgPath)
		}
	case "backup":
		bf := flag.NewFlagSet("backup", flag.ExitOnError)
		out := bf.String("o", "", "write the backup to this file instead of the backup dir")
		bf.Parse(args)
		err = runBackup(*cfgPath, *out)
	case "restore":
		if len(args) != 1 {
			fl.Usage()
			os.Exit(2)
		}
		err = runRestore(*cfgPath, args[0])
	case "reset-password":
		if len(args) != 1 {
			fl.Usage()
			os.Exit(2)
		}
		err = runResetPassword(*cfgPath, args[0])
	case "version":
		fmt.Println("viceroy", version)
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

func loadConfig(path string) (config.Config, error) {
	cfg, err := config.Load(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, fmt.Errorf("%s not found; run `viceroy init` first", path)
	}
	return cfg, err
}

func runBackup(cfgPath, out string) error {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	conn, err := db.OpenExisting(filepath.Join(cfg.DataDir, backup.DBFile))
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("no database in %s yet; nothing to back up", cfg.DataDir)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	b := &backup.Service{DB: conn, DataDir: cfg.DataDir, Dir: cfg.Backup.Dir, Config: cfgPath}
	ctx := context.Background()
	if out != "" {
		if err := b.WriteTo(ctx, out); err != nil {
			return err
		}
		fmt.Println("Wrote", out)
		return nil
	}
	info, err := b.Create(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Wrote %s (%d KB)\n", info.Path, (info.Size+1023)/1024)
	return nil
}

func runRestore(cfgPath, archive string) error {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	// A running server would keep writing to the old database file.
	if ln, err := net.Listen("tcp", cfg.Listen); err != nil {
		return fmt.Errorf("can't bind %s (%v); stop the server before restoring", cfg.Listen, err)
	} else {
		ln.Close()
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	files, err := backup.Restore(context.Background(), archive, cfg.DataDir, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("Restored %s into %s. The previous files were kept with a .before-restore suffix.\n", strings.Join(files, ", "), cfg.DataDir)
	fmt.Println("Start the server again: viceroy serve")
	return nil
}

func runResetPassword(cfgPath, email string) error {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	conn, err := db.OpenExisting(filepath.Join(cfg.DataDir, backup.DBFile))
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("no database in %s yet", cfg.DataDir)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	inv, u, err := auth.New(conn).ResetLinkFor(context.Background(), email)
	if err != nil {
		return err
	}
	base := strings.TrimRight(cfg.PublicURL, "/")
	if base == "" {
		base = "http://" + cfg.Listen
	}
	fmt.Printf("Password reset link for %s (valid %d days, works once):\n%s/join/%s\n", u.Name, int(auth.InviteTTL.Hours()/24), base, inv.Token)
	return nil
}

func runServe(path string) error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}
	if len(cfg.EnvSet) > 0 {
		log.Info("environment overrides config file", "vars", strings.Join(cfg.EnvSet, ","))
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
	if err := accounts.EnsurePaperCashAll(context.Background(), db.New(conn)); err != nil {
		return fmt.Errorf("creating Paper Cash accounts: %w", err)
	}

	box, err := secrets.LoadOrCreate(filepath.Join(cfg.DataDir, "secret.key"))
	if err != nil {
		return err
	}
	keys, err := notify.LoadOrCreateKeys(filepath.Join(cfg.DataDir, "vapid.json"))
	if err != nil {
		return err
	}
	subject := ""
	if strings.HasPrefix(cfg.PublicURL, "https://") {
		subject = cfg.PublicURL
	}
	notifier := notify.New(conn, log, keys, subject)
	aiset := &aisettings.Store{DB: conn, Box: box, Config: cfg.AI, Referer: cfg.PublicURL, Log: log}
	cat := &aicat.Service{DB: conn, Log: log, Client: func(ctx context.Context, hh int64) (aicat.Client, aicat.Auto, error) {
		st, err := aiset.Load(ctx, hh)
		return aiset.Track(st.Email(cfg.AI.BaseURL, cfg.PublicURL), hh, "categorize"), aicat.Auto{On: st.Categorize, Review: st.CatReview}, err
	}}
	colors := &branding.Service{DB: conn, Log: log, Client: func(ctx context.Context, hh int64) (branding.Client, error) {
		c, err := aiset.EmailClient(ctx, hh)
		if c != nil {
			c.Feature = "colors"
		}
		return c, err
	}}
	changed := func(hh int64) {
		notifier.Changed(hh)
		cat.Changed(hh)
		colors.Changed(hh)
	}
	sync := syncer.New(conn, box, log)
	sync.Changed = changed
	mail := email.New(conn, box, log)
	mail.Changed = changed
	mail.AI = email.LLMReader{Client: aiset.EmailClient}
	mail.Notice = func(ctx context.Context, hh int64, n email.Notice) {
		a := notify.Alert{Kind: n.Kind, Key: n.Key, Title: n.Title, Body: n.Body, URL: n.URL}
		if err := notifier.NotifyHousehold(ctx, hh, a, func(p notify.Prefs) bool { return p.BankNotices }); err != nil {
			log.Warn("bank notice", "err", err)
		}
		notifier.Changed(hh) // a new bill may need a "payment due" reminder
	}

	webFS, err := web.Dist()
	if err != nil {
		return err
	}
	api := server.New(cfg, conn, webFS, log, sync, mail, notifier, aiset)
	api.ConfigPath = path
	api.Changed = colors.Changed // new manual accounts; synced ones come through sync.Changed
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute, // drop idle keep-alive connections
		// No Read/WriteTimeout: chat answers stream for minutes, and imports upload screenshots.
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go pruneSessions(ctx, auth.New(conn), log)
	go colors.Sweep(ctx) // accounts from before colors existed
	go sync.Run(ctx)
	go mail.Run(ctx)
	go notifier.Run(ctx)
	go mail.RunAI(ctx)
	go (&backup.Service{DB: conn, DataDir: cfg.DataDir, Dir: cfg.Backup.Dir, Config: path, Keep: cfg.Backup.Keep, Log: log}).Run(ctx)

	errc := make(chan error, 1)
	go func() {
		scheme := "http"
		if cfg.TLS.Cert != "" {
			scheme = "https"
		}
		log.Info("viceroy listening", "version", version, "url", fmt.Sprintf("%s://%s", scheme, cfg.Listen), "allowed", cfg.AllowedCIDRs)
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
