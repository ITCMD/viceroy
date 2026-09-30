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

	// Posted for less than the entry (not a tip), and outside the date window: no link.
	if id, _ := LinkPosted(f.ctx, f.q, f.posted("CHIPOTLE 2231", "2026-09-12", -1800), f.now); id != 0 {
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

func TestTipTolerance(t *testing.T) {
	f := setup(t)
	// An alert for $50.00 that posts as $60.00 with a tip links when the merchant agrees.
	dinner := f.pending("Bistro Nord", "2026-09-10", -5000, "")
	if id, _ := LinkPosted(f.ctx, f.q, f.posted("BISTRO NORD", "2026-09-11", -6000), f.now); id != dinner {
		t.Fatalf("tip: linked %d, want %d", id, dinner)
	}
	// More than 30% over: no link.
	f.pending("Bistro Nord", "2026-09-12", -5000, "")
	if id, _ := LinkPosted(f.ctx, f.q, f.posted("BISTRO NORD", "2026-09-12", -6600), f.now); id != 0 {
		t.Fatal("linked a posting 32% over the alert")
	}
	// A weak description match needs the exact amount.
	f.pending("Coffee", "2026-09-14", -400, "")
	if id, _ := LinkPosted(f.ctx, f.q, f.posted("SQ *BLUE BOTTLE", "2026-09-14", -450), f.now); id != 0 {
		t.Fatal("linked a different amount on a weak match")
	}
	// Money in never stretches.
	f.pending("Refund Acme", "2026-09-15", 1000, "")
	if id, _ := LinkPosted(f.ctx, f.q, f.posted("REFUND ACME", "2026-09-15", 1200), f.now); id != 0 {
		t.Fatal("linked a larger refund")
	}
	// An exact amount beats a closer-matching tip candidate.
	exact := f.pending("Gas station", "2026-09-20", -4000, "")
	f.pending("Shell Oil", "2026-09-20", -3500, "")
	if id, _ := LinkPosted(f.ctx, f.q, f.posted("SHELL OIL GAS STATION", "2026-09-20", -4000), f.now); id != exact {
		t.Fatalf("exact vs tip: linked %d, want %d", id, exact)
	}
	// The reverse direction (alert arrives after the posting) uses the same range.
	p := f.posted("PIZZA PLACE", "2026-09-22", -2600)
	id, err := LinkProvisional(f.ctx, f.q, Provisional{ID: f.pending("Pizza Place", "2026-09-22", -2200, ""), AccountID: f.acct, Date: "2026-09-22", AmountCents: -2200, Description: "Pizza Place"}, f.now)
	if err != nil || id != p.ID {
		t.Fatalf("reverse tip link = %d, %v; want %d", id, err, p.ID)
	}
}

func TestRanges(t *testing.T) {
	for _, amt := range []int64{-5000, -1875, -1, -7, -123457} {
		lo, hi := postedRange(amt)
		// Every posted amount in the range must map back to a provisional range containing amt.
		for _, posted := range []int64{lo, hi, (lo + hi) / 2} {
			plo, phi := provisionalRange(posted)
			if amt < plo || amt > phi {
				t.Errorf("prov %d → posted %d → prov range [%d, %d] excludes it", amt, posted, plo, phi)
			}
		}
	}
}
