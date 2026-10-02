package backup

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"viceroy/internal/db"
)

func setup(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, DBFile))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := db.New(conn).CreateHousehold(context.Background(), db.CreateHouseholdParams{Name: "Before", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "secret.key"), []byte("old-key"), 0o600)
	os.WriteFile(filepath.Join(dir, "vapid.json"), []byte(`{"v":1}`), 0o600)
	cfg := filepath.Join(t.TempDir(), "viceroy.toml")
	os.WriteFile(cfg, []byte("listen = \"x\"\n"), 0o600)
	return &Service{DB: conn, DataDir: dir, Dir: filepath.Join(dir, "backups"), Config: cfg, Keep: 3,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, conn
}

func households(t *testing.T, path string) []string {
	t.Helper()
	c, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	rows, err := c.Query("SELECT name FROM households ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		out = append(out, n)
	}
	return out
}

func TestCreateAndRestore(t *testing.T) {
	ctx := context.Background()
	s, conn := setup(t)
	b, err := s.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(b.Path); st.Mode().Perm() != 0o600 {
		t.Errorf("backup mode = %v, want 0600", st.Mode().Perm())
	}

	// Changes after the backup are rolled back by a restore.
	db.New(conn).CreateHousehold(ctx, db.CreateHouseholdParams{Name: "After", CreatedAt: 2})
	os.WriteFile(filepath.Join(s.DataDir, "secret.key"), []byte("new-key"), 0o600)
	conn.Close() // the server is stopped for a restore

	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	files, err := Restore(ctx, b.Path, s.DataDir, now)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(files, ",") != "viceroy.db,secret.key,vapid.json" {
		t.Errorf("restored %v", files)
	}
	if got := households(t, filepath.Join(s.DataDir, DBFile)); strings.Join(got, ",") != "Before" {
		t.Errorf("households after restore = %v", got)
	}
	if k, _ := os.ReadFile(filepath.Join(s.DataDir, "secret.key")); string(k) != "old-key" {
		t.Errorf("secret.key = %q", k)
	}
	aside := ".before-restore-20261002-120000"
	if k, _ := os.ReadFile(filepath.Join(s.DataDir, "secret.key"+aside)); string(k) != "new-key" {
		t.Errorf("old secret.key not kept aside: %q", k)
	}
	if got := households(t, filepath.Join(s.DataDir, DBFile+aside)); len(got) != 2 {
		t.Errorf("old database not kept aside intact: %v", got)
	}
	if _, err := os.Stat(filepath.Join(s.DataDir, "viceroy.toml")); err == nil {
		t.Error("restore wrote the config into data_dir")
	}
}

func TestRestoreRejectsBadArchives(t *testing.T) {
	ctx := context.Background()
	s, _ := setup(t)
	junk := filepath.Join(t.TempDir(), "junk.tar.gz")
	os.WriteFile(junk, []byte("not gzip"), 0o600)
	if _, err := Restore(ctx, junk, s.DataDir, time.Now()); err == nil {
		t.Error("restored a non-archive")
	}

	os.Remove(filepath.Join(s.DataDir, "secret.key"))
	b, err := s.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(ctx, b.Path, s.DataDir, time.Now()); err == nil || !strings.Contains(err.Error(), "secret.key") {
		t.Errorf("restore without secret.key: err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.DataDir, DBFile)); err != nil {
		t.Error("a refused restore touched the database")
	}
}

func TestScheduleAndPrune(t *testing.T) {
	ctx := context.Background()
	s, _ := setup(t)
	now := time.Date(2026, 10, 1, 3, 0, 0, 0, time.Local)
	s.Now = func() time.Time { return now }

	for day := 0; day < 5; day++ {
		s.tick(ctx)
		s.tick(ctx) // a second check the same hour does nothing
		now = now.Add(24 * time.Hour)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("kept %d backups, want 3", len(list))
	}
	if list[0].Name != "viceroy-20261005-030000.tar.gz" || list[2].Name != "viceroy-20261003-030000.tar.gz" || !list[0].At.Equal(now.Add(-24*time.Hour)) {
		t.Errorf("kept %v", list)
	}
	if due, _ := s.Due(); !due {
		t.Error("not due a day after the last backup")
	}
	now = now.Add(-23 * time.Hour)
	if due, _ := s.Due(); due {
		t.Error("due an hour after the last backup")
	}
}
