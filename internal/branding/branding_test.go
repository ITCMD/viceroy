package branding

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"viceroy/internal/accounts"
	"viceroy/internal/ai"
	"viceroy/internal/db"
)

type fakeClient struct{ asked []string }

func (f *fakeClient) Configured() bool { return true }
func (f *fakeClient) CompleteJSON(ctx context.Context, msgs []ai.Message) (string, error) {
	f.asked = append(f.asked, msgs[1].Content)
	var items []string
	for _, l := range strings.Split(msgs[1].Content, "\n") {
		switch {
		case strings.Contains(l, "DCU"):
			items = append(items, `{"n":`+l[:1]+`,"color":"#00703C"}`)
		case strings.Contains(l, "Mystery"):
			items = append(items, `{"n":`+l[:1]+`,"color":"green"}`) // not a hex color
		}
	}
	return `{"items":[` + strings.Join(items, ",") + `]}`, nil
}

func TestFill(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	q := db.New(conn)
	h, _ := q.CreateHousehold(ctx, db.CreateHouseholdParams{Name: "H", CreatedAt: 1})
	mk := func(name, inst string) int64 {
		a, err := q.CreateAccount(ctx, db.CreateAccountParams{HouseholdID: h.ID, Name: name, InstitutionName: inst, Type: "checking", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	chk, sav, odd := mk("Checking", "DCU"), mk("Savings", "DCU"), mk("Wallet", "Mystery Bank")
	if err := accounts.EnsurePaperCash(ctx, q, h.ID); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{}
	if n, err := Fill(ctx, conn, fc, h.ID); err != nil || n != 4 {
		t.Fatalf("filled %d, %v", n, err)
	}
	if len(fc.asked) != 1 || strings.Count(fc.asked[0], "DCU") != 1 {
		t.Fatalf("asked %q", fc.asked) // one request, each bank once
	}
	get := func(id int64) db.Account {
		a, _ := q.GetAccount(ctx, db.GetAccountParams{ID: id, HouseholdID: h.ID})
		return a
	}
	for _, id := range []int64{chk, sav} {
		if a := get(id); a.Color != "#00703c" || a.ColorSource != SourceAI {
			t.Fatalf("dcu: %q %q", a.Color, a.ColorSource)
		}
	}
	if a := get(odd); a.Color != Fallback("Mystery Bank") || a.ColorSource != SourceAuto {
		t.Fatalf("fallback: %q %q", a.Color, a.ColorSource)
	}
	// A new DCU account reuses the color without asking.
	loan := mk("Car loan", "DCU")
	if _, err := Fill(ctx, conn, fc, h.ID); err != nil || len(fc.asked) != 1 || get(loan).Color != "#00703c" {
		t.Fatalf("reuse: %v asked %d color %q", err, len(fc.asked), get(loan).Color)
	}
	// User colors stay.
	q.SetAccountColor(ctx, db.SetAccountColorParams{Color: "#ffffff", ColorSource: SourceUser, ID: chk, HouseholdID: h.ID})
	if _, err := Fill(ctx, conn, fc, h.ID); err != nil || get(chk).Color != "#ffffff" {
		t.Fatal("user color changed")
	}
}
