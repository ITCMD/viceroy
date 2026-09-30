package secrets

import (
	"path/filepath"
	"testing"
)

func TestRoundTripAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	b1, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := b1.Seal("https://user:pass@bridge.example/simplefin")
	if err != nil {
		t.Fatal(err)
	}
	b2, err := LoadOrCreate(path) // same key reloaded from disk
	if err != nil {
		t.Fatal(err)
	}
	got, err := b2.Open(sealed)
	if err != nil || got != "https://user:pass@bridge.example/simplefin" {
		t.Fatalf("open = %q, %v", got, err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := b2.Open(sealed); err == nil {
		t.Fatal("tampered ciphertext opened")
	}
}
