package syncer

import (
	"errors"
	"testing"

	"viceroy/internal/db"
	"viceroy/internal/providers/simplefin/fake"
)

// A bank shared on the Bridge after setup waits to be added; adding it fetches its history
// with a per-account request, and removing an account keeps it out of syncs.
func TestOfferedAccountsAndTracking(t *testing.T) {
	e := newEnv(t)
	c, err := e.svc.Connect(e.ctx, e.hh, fake.Token(e.url, "m1"))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(e.accounts()); n != 6 {
		t.Fatalf("first sync adds everything: %d accounts", n)
	}

	e.fake.SetScenario("initial+newbank")
	if err := e.svc.Sync(e.ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	sp := e.accounts()["Sapphire Preferred (6666)"]
	if sp.Status != "ignored" || !sp.OfferedAt.Valid {
		t.Fatalf("new bank should wait to be added: %+v", sp)
	}
	if e.txnCount(sp.ID) != 0 {
		t.Fatal("offered account imported transactions")
	}

	// Add it, and drop the savings account.
	sav := e.accounts()["360 Performance Savings (2222)"]
	before := e.fake.Requests
	if err := e.svc.SetTracked(e.ctx, c.ID, []int64{sp.ID}, []int64{sav.ID}); err != nil {
		t.Fatal(err)
	}
	if e.fake.Requests != before+1 {
		t.Fatalf("adding should cost one request, got %d", e.fake.Requests-before)
	}
	got := e.accounts()
	if a := got["Sapphire Preferred (6666)"]; a.Status != "active" || a.OfferedAt.Valid || e.txnCount(a.ID) != 1 {
		t.Fatalf("added account: %+v, %d txns", a, e.txnCount(a.ID))
	}
	if a := got["360 Performance Savings (2222)"]; a.Status != "ignored" || a.OfferedAt.Valid {
		t.Fatalf("removed account: %+v", a)
	}
	// The partial fetch must not mark the other accounts disconnected.
	if a := got["360 Checking (1111)"]; a.Status != "active" {
		t.Fatalf("checking after partial fetch: %s", a.Status)
	}

	// Next sync keeps the removed account out; turning on auto-add adds new ones right away.
	if err := e.svc.SetAutoAdd(e.ctx, e.hh, c.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Sync(e.ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if a := e.accounts()["360 Performance Savings (2222)"]; a.Status != "ignored" {
		t.Fatalf("removed account came back: %s", a.Status)
	}

	// Accounts from elsewhere are refused.
	if err := e.svc.SetTracked(e.ctx, c.ID, []int64{999}, nil); !errors.Is(err, ErrNotOnConnection) {
		t.Fatalf("foreign account: %v", err)
	}
}

// When the daily cap leaves no request for the history, the account is still added and the
// next sync fetches the full window.
func TestTrackedHistoryWaitsForCap(t *testing.T) {
	e := newEnv(t)
	c, err := e.svc.Connect(e.ctx, e.hh, fake.Token(e.url, "m2"))
	if err != nil {
		t.Fatal(err)
	}
	e.fake.SetScenario("initial+newbank")
	if err := e.svc.Sync(e.ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	q := db.New(e.svc.DB)
	if err := q.SetConnectionRequests(e.ctx, db.SetConnectionRequestsParams{RequestsDay: e.svc.Now().Format("2006-01-02"), RequestsCount: DailyRequestCap, ID: c.ID}); err != nil {
		t.Fatal(err)
	}
	sp := e.accounts()["Sapphire Preferred (6666)"]
	if err := e.svc.SetTracked(e.ctx, c.ID, []int64{sp.ID}, nil); !errors.Is(err, HistoryPending) {
		t.Fatalf("want HistoryPending, got %v", err)
	}
	if a := e.accounts()["Sapphire Preferred (6666)"]; a.Status != "active" {
		t.Fatalf("still added: %s", a.Status)
	}
	conn, _ := q.GetConnectionByID(e.ctx, c.ID)
	if conn.SyncedThrough.Valid {
		t.Fatal("synced_through should be cleared so the next sync fetches 90 days")
	}
}

// A flipped account stores the opposite of what the bank reports.
func TestInvertBalance(t *testing.T) {
	e := newEnv(t)
	c, err := e.svc.Connect(e.ctx, e.hh, fake.Token(e.url, "m3"))
	if err != nil {
		t.Fatal(err)
	}
	chk := e.accounts()["360 Checking (1111)"]
	q := db.New(e.svc.DB)
	if err := q.SetAccountInvert(e.ctx, db.SetAccountInvertParams{InvertBalance: 1, UpdatedAt: 1, ID: chk.ID, HouseholdID: e.hh}); err != nil {
		t.Fatal(err)
	}
	if a := e.accounts()["360 Checking (1111)"]; a.BalanceCents != -312044 {
		t.Fatalf("flip stored balance: %d", a.BalanceCents)
	}
	if err := e.svc.Sync(e.ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if a := e.accounts()["360 Checking (1111)"]; a.BalanceCents != -312044 {
		t.Fatalf("sync keeps it flipped: %d", a.BalanceCents)
	}
}
