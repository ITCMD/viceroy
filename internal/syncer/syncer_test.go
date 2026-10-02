package syncer

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"viceroy/internal/db"
	"viceroy/internal/providers/simplefin/fake"
	"viceroy/internal/secrets"
)

type env struct {
	t    *testing.T
	svc  *Service
	fake *fake.Server
	url  string
	hh   int64
	ctx  context.Context
}

func newEnv(t *testing.T) *env {
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	box, _ := secrets.New(make([]byte, 32))
	f := fake.New()
	ts := httptest.NewServer(f)
	t.Cleanup(ts.Close)
	ctx := context.Background()
	h, err := db.New(conn).CreateHousehold(ctx, db.CreateHouseholdParams{Name: "H", CreatedAt: 1})
	if err != nil {
		t.Fatal(err)
	}
	return &env{t: t, svc: New(conn, box, slog.New(slog.NewTextHandler(io.Discard, nil))), fake: f, url: ts.URL, hh: h.ID, ctx: ctx}
}

func (e *env) accounts() map[string]db.Account {
	e.t.Helper()
	list, err := db.New(e.svc.DB).ListAccounts(e.ctx, e.hh)
	if err != nil {
		e.t.Fatal(err)
	}
	out := map[string]db.Account{}
	for _, a := range list {
		k := a.Name
		for i := 2; ; i++ {
			if _, dup := out[k]; !dup {
				break
			}
			k = fmt.Sprintf("%s#%d", a.Name, i)
		}
		out[k] = a
	}
	return out
}

func (e *env) txnCount(accountID int64) int64 {
	n, err := db.New(e.svc.DB).CountAccountTransactions(e.ctx, accountID)
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) pendingCount(accountID int64) int {
	rows, err := db.New(e.svc.DB).ListPendingSynced(e.ctx, db.ListPendingSyncedParams{AccountID: accountID, Source: source, Date: "0000"})
	if err != nil {
		e.t.Fatal(err)
	}
	return len(rows)
}

func TestConnectRelinkAndMerge(t *testing.T) {
	e := newEnv(t)
	c, err := e.svc.Connect(e.ctx, e.hh, fake.Token(e.url, "a"))
	if err != nil {
		t.Fatal(err)
	}
	accts := e.accounts()
	if len(accts) != 6 {
		t.Fatalf("got %d accounts after first sync", len(accts))
	}
	chk := accts["360 Checking (1111)"]
	if chk.Type != "checking" || chk.Mask != "1111" || chk.BalanceCents != 312044 || chk.InstitutionName != "Capital One" {
		t.Fatalf("checking = %+v", chk)
	}
	if accts["Quicksilver Card (3333)"].Type != "credit_card" {
		t.Fatal("card type not inferred")
	}
	if e.txnCount(chk.ID) != 6 || e.pendingCount(chk.ID) != 1 {
		t.Fatalf("checking txns = %d pending = %d", e.txnCount(chk.ID), e.pendingCount(chk.ID))
	}

	// Re-syncing the same data is idempotent.
	if err := e.svc.Sync(e.ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(e.accounts()); n != 6 || e.txnCount(chk.ID) != 6 {
		t.Fatalf("resync changed data: %d accounts, %d txns", n, e.txnCount(chk.ID))
	}

	// The bank is re-linked with new ids and only some accounts shared.
	e.fake.SetScenario("relinked")
	if err := e.svc.Sync(e.ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	accts = e.accounts()
	chk2 := accts["360 Checking (1111)"]
	if chk2.ID != chk.ID || chk2.ExternalID.String != "ACT-c2-chk" || chk2.Status != "active" {
		t.Fatalf("checking not relinked in place: %+v", chk2)
	}
	if n := e.txnCount(chk.ID); n != 6 || e.pendingCount(chk.ID) != 0 {
		t.Fatalf("after relink checking has %d txns, %d pending (want 6, 0)", n, e.pendingCount(chk.ID))
	}
	if qs := accts["Quicksilver Card (3333)"]; e.txnCount(qs.ID) != 2 || e.pendingCount(qs.ID) != 0 {
		t.Fatalf("card txns = %d pending %d", e.txnCount(qs.ID), e.pendingCount(qs.ID))
	}
	if s := accts["360 Performance Savings (2222)"].Status; s != "disconnected" {
		t.Fatalf("unshared savings status = %s", s)
	}
	// A card the bank never shared before waits for the user to add it.
	if v := accts["Venture Card (5555)"]; v.Status != "ignored" || !v.OfferedAt.Valid {
		t.Fatalf("new card = %s, offered %v", v.Status, v.OfferedAt.Valid)
	}
	var review db.Account
	for _, a := range accts {
		if a.Status == "review" {
			review = a
		}
	}
	if review.Name != "Savor Card" || !review.ReviewCandidateID.Valid {
		t.Fatalf("expected Savor Card in review, got %+v", review)
	}
	if len(accts) != 8 {
		t.Fatalf("got %d accounts, want 8 (6 + Venture + review)", len(accts))
	}

	// User links the review account to the first Savor Card.
	target := review.ReviewCandidateID.Int64
	if err := e.svc.Merge(e.ctx, e.hh, review.ID, target); err != nil {
		t.Fatal(err)
	}
	got, err := db.New(e.svc.DB).GetAccount(e.ctx, db.GetAccountParams{ID: target, HouseholdID: e.hh})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "active" || got.ExternalID.String != "ACT-c2-sv" || got.BalanceCents != -13000 {
		t.Fatalf("merge target = %+v", got)
	}
	// A further sync updates the merged account instead of recreating it.
	if err := e.svc.Sync(e.ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(e.accounts()); n != 7 {
		t.Fatalf("after merge+sync got %d accounts, want 7", n)
	}
}

func TestReauthKeepsAccounts(t *testing.T) {
	e := newEnv(t)
	c, err := e.svc.Connect(e.ctx, e.hh, fake.Token(e.url, "a"))
	if err != nil {
		t.Fatal(err)
	}
	e.fake.SetScenario("reauth")
	if err := e.svc.Sync(e.ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	for name, a := range e.accounts() {
		if a.Status != "active" {
			t.Errorf("%s status = %s during reauth", name, a.Status)
		}
	}
	insts, _ := db.New(e.svc.DB).ListInstitutions(e.ctx, e.hh)
	var reauth int
	for _, i := range insts {
		if i.Status == "reauth" {
			reauth++
		}
	}
	if reauth != 1 {
		t.Fatalf("institutions needing reauth = %d", reauth)
	}
}

func TestTokenReuseAndDailyCap(t *testing.T) {
	e := newEnv(t)
	c, err := e.svc.Connect(e.ctx, e.hh, fake.Token(e.url, "a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Connect(e.ctx, e.hh, fake.Token(e.url, "a")); err == nil {
		t.Fatal("reusing a setup token should fail")
	}
	for i := 1; i < DailyRequestCap; i++ {
		if err := e.svc.Sync(e.ctx, c.ID); err != nil {
			t.Fatalf("sync %d: %v", i, err)
		}
	}
	if err := e.svc.Sync(e.ctx, c.ID); err != ErrDailyCap {
		t.Fatalf("want ErrDailyCap, got %v", err)
	}
	if e.fake.Requests != DailyRequestCap {
		t.Fatalf("bridge saw %d requests", e.fake.Requests)
	}
	// Next day the budget resets.
	e.svc.Now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	if err := e.svc.Sync(e.ctx, c.ID); err != nil {
		t.Fatal(err)
	}
}

var _ = sql.ErrNoRows

func TestSyncCategorizesAndLinksPending(t *testing.T) {
	e := newEnv(t)
	c, err := e.svc.Connect(e.ctx, e.hh, fake.Token(e.url, "a"))
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(e.svc.DB)
	chk := e.accounts()["360 Checking (1111)"]
	g, _ := q.CreateCategoryGroup(e.ctx, db.CreateCategoryGroupParams{HouseholdID: e.hh, Name: "Flexible", Kind: "flexible"})
	cat, _ := q.CreateCategory(e.ctx, db.CreateCategoryParams{HouseholdID: e.hh, GroupID: g.ID, Name: "Restaurants"})
	prov, err := q.InsertManualTransaction(e.ctx, db.InsertManualTransactionParams{
		HouseholdID: e.hh, AccountID: chk.ID, Date: time.Now().Format(time.DateOnly), AmountCents: -1875,
		Description: "Chipotle", Pending: 1, Provisional: 1,
		CategoryID: sql.NullInt64{Int64: cat.ID, Valid: true}, CategorySource: "user",
	})
	if err != nil {
		t.Fatal(err)
	}

	e.fake.SetScenario("initial+posted")
	if err := e.svc.Sync(e.ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	p, err := q.GetTransactionByID(e.ctx, prov)
	if err != nil || !p.LinkedTxnID.Valid {
		t.Fatalf("pending entry not linked: %+v %v", p, err)
	}
	posted, _ := q.GetTransactionByID(e.ctx, p.LinkedTxnID.Int64)
	if posted.Description != "CHIPOTLE 2231 AUSTIN TX" || posted.CategoryID.Int64 != cat.ID || posted.CategorySource != "linked" || !posted.MerchantID.Valid {
		t.Fatalf("posted row = %+v", posted)
	}
	m, _ := q.GetMerchant(e.ctx, db.GetMerchantParams{ID: posted.MerchantID.Int64, HouseholdID: e.hh})
	if m.Name != "Chipotle" {
		t.Fatalf("merchant = %q", m.Name)
	}
}
