package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"viceroy/internal/db"
)

func newService(t *testing.T) *Service {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return New(conn)
}

var validSetup = SetupInput{Name: "Lucas", Email: "lucas@example.com", Password: "correct horse battery"}

func TestSetupOnlyOnce(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	if need, _ := s.NeedsSetup(ctx); !need {
		t.Fatal("fresh db should need setup")
	}
	u, err := s.Setup(ctx, validSetup)
	if err != nil {
		t.Fatal(err)
	}
	if u.IsAdmin != 1 {
		t.Error("first user should be admin")
	}
	h, err := db.New(s.DB).GetUserHousehold(ctx, u.ID)
	if err != nil || h.Name != "Lucas's household" {
		t.Errorf("household = %+v, %v", h, err)
	}
	if _, err := s.Setup(ctx, validSetup); !errors.Is(err, ErrAlreadySetUp) {
		t.Fatalf("second setup err = %v", err)
	}
}

func TestSetupValidation(t *testing.T) {
	s := newService(t)
	bad := validSetup
	bad.Password = "short"
	var ve ValidationError
	if _, err := s.Setup(context.Background(), bad); !errors.As(err, &ve) {
		t.Fatalf("err = %v, want ValidationError", err)
	}
}

func TestLoginAndSessions(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	now := time.Unix(1_800_000_000, 0)
	s.Now = func() time.Time { return now }
	if _, err := s.Setup(ctx, validSetup); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, "lucas@example.com", "wrong password!"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("bad password err = %v", err)
	}
	if _, err := s.Login(ctx, "nobody@example.com", "whatever12345"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown user err = %v", err)
	}
	u, err := s.Login(ctx, "LUCAS@example.com", validSetup.Password)
	if err != nil {
		t.Fatalf("case-insensitive login: %v", err)
	}
	tok, _, err := s.CreateSession(ctx, u.ID, "test", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Authenticate(ctx, tok); err != nil || got.ID != u.ID {
		t.Fatalf("authenticate = %+v, %v", got, err)
	}
	if _, err := s.Authenticate(ctx, tok+"x"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("tampered token err = %v", err)
	}
	now = now.Add(SessionTTL + time.Second)
	if _, err := s.Authenticate(ctx, tok); !errors.Is(err, ErrNoSession) {
		t.Fatalf("expired session err = %v", err)
	}
	now = time.Unix(1_800_000_000, 0)
	if err := s.Logout(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, tok); !errors.Is(err, ErrNoSession) {
		t.Fatalf("after logout err = %v", err)
	}
}

func TestResetLinkFor(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	u, err := s.Setup(ctx, validSetup)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ResetLinkFor(ctx, "nobody@example.com"); err == nil {
		t.Error("made a link for an unknown email")
	}
	inv, got, err := s.ResetLinkFor(ctx, " lucas@example.com ")
	if err != nil || got.ID != u.ID {
		t.Fatalf("ResetLinkFor = %v, %v", got.ID, err)
	}
	if _, err := s.AcceptInvite(ctx, inv.Token, AcceptInput{Password: "a brand new password"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, validSetup.Email, "a brand new password"); err != nil {
		t.Errorf("login with the new password: %v", err)
	}
}
