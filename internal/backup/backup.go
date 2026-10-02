// Package backup makes consistent backups of a running Viceroy instance.
//
// A backup is a .tar.gz holding a snapshot of the database (SQLite VACUUM INTO,
// safe while the server is writing) plus the generated keys in data_dir that the
// database is useless without: secret.key decrypts stored bank tokens and mailbox
// passwords, vapid.json keeps existing push subscriptions working. The config file
// is included too, for reference; restore does not overwrite it.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	DBFile  = "viceroy.db"
	prefix  = "viceroy-"
	suffix  = ".tar.gz"
	stamp   = "20060102-150405"
	cfgName = "viceroy.toml"
)

// KeyFiles are the files in data_dir that belong with the database.
var KeyFiles = []string{"secret.key", "vapid.json"}

// Service creates backups of one instance.
type Service struct {
	DB      *sql.DB
	DataDir string
	Dir     string // where backups go
	Config  string // path of viceroy.toml, included for reference ("" = skip)
	Keep    int    // scheduled backups to keep; 0 = no scheduled backups
	Log     *slog.Logger
	Now     func() time.Time
}

// Info describes one backup file.
type Info struct {
	Name string    `json:"name"`
	Path string    `json:"-"`
	Size int64     `json:"size"`
	At   time.Time `json:"at"`
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Create writes a new backup into s.Dir and returns it.
func (s *Service) Create(ctx context.Context) (Info, error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return Info{}, err
	}
	at := s.now().In(time.Local)
	path := filepath.Join(s.Dir, prefix+at.Format(stamp)+suffix)
	for i := 2; ; i++ { // two in the same second
		if _, err := os.Stat(path); err != nil {
			break
		}
		path = filepath.Join(s.Dir, prefix+at.Format(stamp)+fmt.Sprintf("-%d", i)+suffix)
	}
	if err := s.WriteTo(ctx, path); err != nil {
		return Info{}, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return Info{}, err
	}
	return Info{Name: filepath.Base(path), Path: path, Size: st.Size(), At: at}, nil
}

// WriteTo writes a backup archive to path (replacing it only once it's complete).
func (s *Service) WriteTo(ctx context.Context, path string) error {
	dir := filepath.Dir(path)
	snap, err := os.CreateTemp(dir, ".viceroy-snapshot-*.db")
	if err != nil {
		return err
	}
	snapPath := snap.Name()
	snap.Close()
	os.Remove(snapPath) // VACUUM INTO wants to create the file itself
	defer os.Remove(snapPath)
	if err := Snapshot(ctx, s.DB, snapPath); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".viceroy-backup-*"+suffix)
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := s.writeArchive(tmp, snapPath); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (s *Service) writeArchive(w io.Writer, snapPath string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	if err := addFile(tw, snapPath, DBFile); err != nil {
		return err
	}
	for _, name := range KeyFiles {
		err := addFile(tw, filepath.Join(s.DataDir, name), name)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if s.Config != "" {
		if err := addFile(tw, s.Config, cfgName); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func addFile(tw *tar.Writer, src, name string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	hdr := &tar.Header{Name: name, Mode: 0o600, Size: st.Size(), ModTime: st.ModTime(), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

// Snapshot copies the live database to dst (which must not exist) and checks the copy.
func Snapshot(ctx context.Context, conn *sql.DB, dst string) error {
	if _, err := conn.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		return fmt.Errorf("snapshotting database: %w", err)
	}
	return Check(ctx, dst)
}

// Check runs SQLite's integrity check on the database file at path.
func Check(ctx context.Context, path string) error {
	c, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err
	}
	defer c.Close()
	var res string
	if err := c.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&res); err != nil {
		return fmt.Errorf("checking %s: %w", filepath.Base(path), err)
	}
	if res != "ok" {
		return fmt.Errorf("database integrity check failed: %s", res)
	}
	return nil
}

// List returns the backups in s.Dir, newest first.
func (s *Service) List() ([]Info, error) {
	ents, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		// The name carries the time (mtimes change when backups are copied around).
		at := st.ModTime()
		if ts := strings.TrimPrefix(name, prefix); len(ts) >= len(stamp) {
			if t, err := time.ParseInLocation(stamp, ts[:len(stamp)], time.Local); err == nil {
				at = t
			}
		}
		out = append(out, Info{Name: name, Path: filepath.Join(s.Dir, name), Size: st.Size(), At: at})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.After(out[j].At)
		}
		return out[i].Name > out[j].Name
	})
	return out, nil
}

// Prune deletes all but the newest keep backups.
func (s *Service) Prune(keep int) error {
	list, err := s.List()
	if err != nil || len(list) <= keep {
		return err
	}
	var errs []error
	for _, b := range list[keep:] {
		if err := os.Remove(b.Path); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Due reports whether the newest backup is a day old or missing.
func (s *Service) Due() (bool, error) {
	list, err := s.List()
	if err != nil {
		return false, err
	}
	return len(list) == 0 || s.now().Sub(list[0].At) >= 24*time.Hour-time.Minute, nil
}

// Run makes a backup whenever the last one is a day old, keeping s.Keep of them.
// It checks at start and then hourly, so a server that's often restarted still
// gets daily backups. Keep 0 disables it.
func (s *Service) Run(ctx context.Context) {
	if s.Keep <= 0 {
		return
	}
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		s.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	due, err := s.Due()
	if err != nil {
		s.Log.Warn("backup", "err", err)
		return
	}
	if !due {
		return
	}
	b, err := s.Create(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.Log.Error("scheduled backup failed", "err", err)
		}
		return
	}
	s.Log.Info("backup written", "file", b.Path, "bytes", b.Size)
	if err := s.Prune(s.Keep); err != nil {
		s.Log.Warn("pruning backups", "err", err)
	}
}

// Restore unpacks archive into dataDir. The current database and keys are kept
// next to it with a ".before-restore-<time>" suffix. The server must be stopped.
// It returns the names of the files it restored.
func Restore(ctx context.Context, archive, dataDir string, now time.Time) ([]string, error) {
	tmpDir, err := os.MkdirTemp(dataDir, ".restore-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	files, err := extract(archive, tmpDir)
	if err != nil {
		return nil, err
	}
	if !files[DBFile] {
		return nil, fmt.Errorf("%s has no %s; is it a Viceroy backup?", filepath.Base(archive), DBFile)
	}
	if err := Check(ctx, filepath.Join(tmpDir, DBFile)); err != nil {
		return nil, err
	}
	if !files["secret.key"] {
		return nil, errors.New("backup has no secret.key; refusing to restore a database whose bank and mailbox credentials can't be decrypted")
	}

	aside := ".before-restore-" + now.Format(stamp)
	var restored []string
	for _, name := range append([]string{DBFile}, KeyFiles...) {
		if !files[name] {
			continue
		}
		dst := filepath.Join(dataDir, name)
		if err := os.Rename(dst, dst+aside); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return restored, err
		}
		if name == DBFile { // stale WAL from the old database must not be replayed on the new one
			for _, ext := range []string{"-wal", "-shm"} {
				if err := os.Rename(dst+ext, dst+ext+aside); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return restored, err
				}
			}
		}
		if err := os.Rename(filepath.Join(tmpDir, name), dst); err != nil {
			return restored, err
		}
		restored = append(restored, name)
	}
	return restored, nil
}

func extract(archive, dir string) (map[string]bool, error) {
	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(archive), err)
	}
	tr := tar.NewReader(gz)
	wanted := map[string]bool{DBFile: true}
	for _, k := range KeyFiles {
		wanted[k] = true
	}
	got := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(archive), err)
		}
		if hdr.Typeflag != tar.TypeReg || !wanted[hdr.Name] {
			continue // only known flat names; nothing can escape dir
		}
		out, err := os.OpenFile(filepath.Join(dir, hdr.Name), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, err
		}
		_, err = io.Copy(out, tr)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, err
		}
		got[hdr.Name] = true
	}
	return got, nil
}
