package linking

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"viceroy/internal/db"
)

func TestScore(t *testing.T) {
	cases := []struct {
		prov, posted string
		ok           bool
	}{
		{"Chipotle", "CHIPOTLE 2231 AUSTIN TX", true},
		{"chipotle burrito", "CHIPOTLE 2231 AUSTIN TX", true}, // trigram carries "chipotle"
		{"Whole Foods", "WHOLEFDS MKT #10234", true},
		{"", "ANYTHING", true},
		{"Target", "CHIPOTLE 2231 AUSTIN TX", false},
		{"Blue Bottle", "SQ *BLUE BOTTLE COFFEE", true},
	}
	for _, c := range cases {
		if got := Score(c.prov, c.posted) >= MinScore; got != c.ok {
			t.Errorf("Score(%q, %q) = %.2f", c.prov, c.posted, Score(c.prov, c.posted))
		}
	}
}

type fixture struct {
	t    *testing.T
	ctx  context.Context
	q    *db.Queries
	hh   int64
	acct int64
	now  time.Time
}

func setup(t *testing.T) *fixture {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	q := db.New(conn)
	h, _ := q.CreateHousehold(ctx, db.CreateHouseholdParams{Name: "H", CreatedAt: 1})
	a, err := q.CreateAccount(ctx, db.CreateAccountParams{HouseholdID: h.ID, Name: "Chk", Type: "checking", CreatedAt: 1, UpdatedAt: 1})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, ctx: ctx, q: q, hh: h.ID, acct: a.ID, now: time.Unix(1_790_000_000, 0)}
}

func (f *fixture) pending(desc, date string, amt int64, notes string) int64 {
	id, err := f.q.InsertManualTransaction(f.ctx, db.InsertManualTransactionParams{
		HouseholdID: f.hh, AccountID: f.acct, Date: date, AmountCents: amt, Description: desc, Pending: 1, Provisional: 1, Notes: notes,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *fixture) posted(desc, date string, amt int64) Posted {
	id, err := f.q.InsertSyncedTransaction(f.ctx, db.InsertSyncedTransactionParams{
		HouseholdID: f.hh, AccountID: f.acct, Source: "simplefin", Date: date, AmountCents: amt, Description: desc,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return Posted{ID: id, AccountID: f.acct, Date: date, AmountCents: amt, Description: desc}
}

func (f *fixture) get(id int64) db.Transaction {
	tx, err := f.q.GetTransactionByID(f.ctx, id)
	if err != nil {
		f.t.Fatal(err)
	}
	return tx
}

func TestLinkUnlinkBlacklist(t *testing.T) {
	f := setup(t)
	prov := f.pending("Chipotle", "2026-09-10", -1875, "lunch with Sam")
	f.pending("Target", "2026-09-10", -1875, "") // same amount, wrong merchant
	tag, _ := f.q.UpsertTag(f.ctx, db.UpsertTagParams{HouseholdID: f.hh, Name: "work"})
	if err := f.q.AddTransactionTag(f.ctx, db.AddTransactionTagParams{TransactionID: prov, TagID: tag.ID, HouseholdID: f.hh}); err != nil {
		t.Fatal(err)
	}

	// Wrong amount, and outside the date window: no link.
	if id, _ := LinkPosted(f.ctx, f.q, f.posted("CHIPOTLE 2231", "2026-09-12", -1900), f.now); id != 0 {
		t.Fatal("linked despite amount mismatch")
	}
	if id, _ := LinkPosted(f.ctx, f.q, f.posted("CHIPOTLE 2231", "2026-09-25", -1875), f.now); id != 0 {
		t.Fatal("linked outside the date window")
	}

	p := f.posted("CHIPOTLE 2231 AUSTIN TX", "2026-09-12", -1875)
	id, err := LinkPosted(f.ctx, f.q, p, f.now)
	if err != nil || id != prov {
		t.Fatalf("LinkPosted = %d, %v; want %d", id, err, prov)
	}
	if got := f.get(p.ID); got.Notes != "lunch with Sam" {
		t.Fatalf("notes not inherited: %+v", got)
	}
	tags, _ := f.q.ListTagsForTransactions(f.ctx, []int64{p.ID})
	if len(tags) != 1 {
		t.Fatalf("tags not inherited: %v", tags)
	}

	// Unlink blacklists the pair: a re-run doesn't link it again.
	if err := Unlink(f.ctx, f.q, f.hh, prov); err != nil {
		t.Fatal(err)
	}
	if f.get(prov).LinkedTxnID.Valid {
		t.Fatal("still linked")
	}
	if id, _ := LinkPosted(f.ctx, f.q, p, f.now); id != 0 {
		t.Fatal("blacklisted pair linked again")
	}

	// A manual link still works, and a second one is refused.
	if err := Link(f.ctx, f.q, f.hh, prov, p.ID, f.now); err != nil {
		t.Fatal(err)
	}
	if f.get(prov).LinkedTxnID != (sql.NullInt64{Int64: p.ID, Valid: true}) {
		t.Fatal("manual link not stored")
	}
	if err := Link(f.ctx, f.q, f.hh, prov, p.ID, f.now); err != ErrAlreadyLinked {
		t.Fatalf("second link: %v", err)
	}
	if err := Link(f.ctx, f.q, f.hh, p.ID, prov, f.now); err != ErrNotProvisional {
		t.Fatalf("reversed link: %v", err)
	}
}

func TestDuplicates(t *testing.T) {
	f := setup(t)
	f.posted("CHIPOTLE", "2026-09-10", -1875)
	f.posted("CHIPOTLE", "2026-09-05", -1875)
	d, err := Duplicates(f.ctx, f.q, f.acct, -1875, "2026-09-11")
	if err != nil || len(d) != 1 {
		t.Fatalf("Duplicates = %v, %v", d, err)
	}
}
