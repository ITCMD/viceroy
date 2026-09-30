package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTemplateLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "viceroy.toml")
	if err := WriteTemplate(path); err != nil {
		t.Fatal(err)
	}
	if err := WriteTemplate(path); err == nil {
		t.Fatal("WriteTemplate overwrote an existing file")
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:8420" {
		t.Errorf("listen = %q", cfg.Listen)
	}
	if cfg.DataDir != filepath.Join(dir, "data") {
		t.Errorf("data_dir = %q, want resolved relative to config", cfg.DataDir)
	}
	if len(cfg.Allowed) != 2 {
		t.Errorf("allowed = %v", cfg.Allowed)
	}
}

func TestParsePrefixes(t *testing.T) {
	cases := []struct {
		in      []string
		want    int
		wantErr bool
	}{
		{[]string{"127.0.0.1"}, 1, false},
		{[]string{"192.168.0.0/24", "::1"}, 2, false},
		{[]string{"0.0.0.0"}, 2, false},
		{[]string{"192.168.1.5/24"}, 1, false},
		{[]string{"not-an-ip"}, 0, true},
	}
	for _, c := range cases {
		got, err := parsePrefixes(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("%v: err = %v", c.in, err)
			continue
		}
		if len(got) != c.want {
			t.Errorf("%v: got %v", c.in, got)
		}
	}
}

func TestUnknownKeysRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "viceroy.toml")
	os.WriteFile(path, []byte("listn = \"x\"\n"), 0o600)
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for misspelled key")
	}
}
