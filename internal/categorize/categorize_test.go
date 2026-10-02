package categorize

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"viceroy/internal/db"
)

func TestMerchant(t *testing.T) {
	cases := []struct{ payee, desc, name, key string }{
		{"", "WHOLEFDS MKT #10234", "Wholefds Mkt", "wholefdsmkt"},
		{"", "SQ *BLUE BOTTLE COFFEE", "Blue Bottle Coffee", "bluebottlecoffee"},
		{"", "CHIPOTLE 2231 AUSTIN TX", "Chipotle", "chipotle"},
		{"", "TRADER JOE'S #552", "Trader Joe's", "traderjoes"},
		{"", "AMAZON.COM*2K4", "Amazon.com", "amazoncom"},
		{"Netflix", "NETFLIX.COM 866-579-7172 CA", "Netflix", "netflix"},
		{"", "UBER *TRIP", "Uber", "uber"},
		{"", "12345", "", ""},
	}
	for _, c := range cases {
		name, key := Merchant(c.payee, c.desc)
		if name != c.name || key != c.key {
			t.Errorf("Merchant(%q, %q) = %q, %q; want %q, %q", c.payee, c.desc, name, key, c.name, c.key)
		}
	}
}

func TestRuleMatches(t *testing.T) {
	txn := Txn{AccountID: 1, AmountCents: -4500, Date: "2026-10-03", Description: "SHELL OIL 5774"}
	cases := []struct {
		rule db.Rule
		want bool
	}{
		{db.Rule{MatchField: "merchant", MatchOp: "contains", MatchValue: "shell"}, true},
		{db.Rule{MatchField: "merchant", MatchOp: "equals", MatchValue: "shell oil"}, true},
		{db.Rule{MatchField: "merchant", MatchOp: "equals", MatchValue: "shell"}, false},
		{db.Rule{MatchField: "description", MatchOp: "starts_with", MatchValue: "SHELL OIL 57"}, true},
		{db.Rule{MatchField: "merchant", MatchOp: "contains", MatchValue: "shell", AccountID: sql.NullInt64{Int64: 2, Valid: true}}, false},
		{db.Rule{MatchField: "merchant", MatchOp: "contains", MatchValue: "shell", AmountMin: sql.NullInt64{Int64: 5000, Valid: true}}, false},
		{db.Rule{MatchField: "merchant", MatchOp: "contains", MatchValue: "shell", AmountMax: sql.NullInt64{Int64: 5000, Valid: true}}, true},
		{db.Rule{MatchField: "merchant", MatchOp: "contains", MatchValue: " "}, false},
		{db.Rule{MatchField: "merchant", MatchOp: "equals", MatchValue: "SHELL-OIL"}, true}, // letters and digits only
		{db.Rule{MatchField: "merchant", MatchOp: "contains", MatchValue: "shell", Direction: "in"}, false},
		{db.Rule{MatchField: "merchant", MatchOp: "contains", MatchValue: "shell", Direction: "out"}, true},
		{db.Rule{MatchField: "merchant", MatchOp: "contains", MatchValue: "shell", DayMin: sql.NullInt64{Int64: 1, Valid: true}, DayMax: sql.NullInt64{Int64: 7, Valid: true}}, true},
		{db.Rule{MatchField: "merchant", MatchOp: "contains", MatchValue: "shell", DayMin: sql.NullInt64{Int64: 10, Valid: true}, DayMax: sql.NullInt64{Int64: 20, Valid: true}}, false},
		{db.Rule{MatchField: "merchant", MatchOp: "contains", MatchValue: "shell", DayMin: sql.NullInt64{Int64: 28, Valid: true}, DayMax: sql.NullInt64{Int64: 5, Valid: true}}, true}, // wraps
		{db.Rule{MatchField: "merchant", MatchOp: "contains", AccountID: sql.NullInt64{Int64: 1, Valid: true}}, true},                                                                  // account only
	}
	for i, c := range cases {
		if got := RuleMatches(c.rule, txn, "Shell Oil"); got != c.want {
			t.Errorf("case %d: got %v", i, got)
		}
	}
}

func TestPipeline(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	q := db.New(conn)
	h, _ := q.CreateHousehold(ctx, db.CreateHouseholdParams{Name: "H", CreatedAt: 1})
	if err := SeedDefaults(ctx, q, h.ID); err != nil {
		t.Fatal(err)
	}
	if err := SeedDefaults(ctx, q, h.ID); err != nil { // idempotent
		t.Fatal(err)
	}
	cats, _ := q.ListCategories(ctx, h.ID)
	byName := map[string]int64{}
	for _, c := range cats {
		if _, dup := byName[c.Name]; dup {
			t.Fatalf("category %q seeded twice", c.Name)
		}
		byName[c.Name] = c.ID
	}
	acct, err := q.CreateAccount(ctx, db.CreateAccountParams{HouseholdID: h.ID, Name: "Chk", Type: "checking", IsManual: 1, CreatedAt: 1, UpdatedAt: 1})
	if err != nil {
		t.Fatal(err)
	}
	insert := func(desc string, amt int64) int64 {
		id, err := q.InsertSyncedTransaction(ctx, db.InsertSyncedTransactionParams{
			HouseholdID: h.ID, AccountID: acct.ID, Source: "simplefin", Date: "2026-09-01", AmountCents: amt, Description: desc,
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	if _, err := q.CreateRule(ctx, db.CreateRuleParams{
		HouseholdID: h.ID, MatchField: "merchant", MatchOp: "contains", MatchValue: "shell",
		SetCategoryID: sql.NullInt64{Int64: byName["Gas"], Valid: true}, SetMerchant: "Shell",
	}); err != nil {
		t.Fatal(err)
	}
	c, err := New(ctx, q, h.ID)
	if err != nil {
		t.Fatal(err)
	}
	get := func(id int64) db.Transaction {
		tx, err := q.GetTransactionByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return tx
	}

	// Rule: category and merchant rename.
	id := insert("SHELL OIL 5774 AUSTIN TX", -4500)
	res, err := c.Apply(ctx, Txn{ID: id, HouseholdID: h.ID, AccountID: acct.ID, AmountCents: -4500, Description: "SHELL OIL 5774 AUSTIN TX"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Merchant != "Shell" || get(id).CategoryID.Int64 != byName["Gas"] || get(id).CategorySource != SourceRule {
		t.Fatalf("rule not applied: %+v %+v", res, get(id))
	}

	// Unknown merchant: uncategorized, needs review.
	id = insert("BLUE BOTTLE 12", -650)
	if _, err := c.Apply(ctx, Txn{ID: id, AccountID: acct.ID, AmountCents: -650, Description: "BLUE BOTTLE 12"}); err != nil {
		t.Fatal(err)
	}
	if tx := get(id); tx.CategoryID.Valid || tx.NeedsReview != 1 {
		t.Fatalf("expected uncategorized + review: %+v", tx)
	}

	// The user categorizes it; the next one from that merchant follows history.
	tx := get(id)
	if err := q.UpdateTransactionUser(ctx, db.UpdateTransactionUserParams{
		Date: tx.Date, AmountCents: tx.AmountCents, Description: tx.Description, MerchantID: tx.MerchantID,
		CategoryID: sql.NullInt64{Int64: byName["Coffee Shops"], Valid: true}, CategorySource: SourceUser, ID: id, HouseholdID: h.ID,
	}); err != nil {
		t.Fatal(err)
	}
	id2 := insert("BLUE BOTTLE 98", -700)
	if _, err := c.Apply(ctx, Txn{ID: id2, AccountID: acct.ID, AmountCents: -700, Description: "BLUE BOTTLE 98"}); err != nil {
		t.Fatal(err)
	}
	if tx := get(id2); tx.CategoryID.Int64 != byName["Coffee Shops"] || tx.CategorySource != SourceHistory || tx.NeedsReview != 0 || tx.MerchantID != get(id).MerchantID {
		t.Fatalf("history not applied: %+v", tx)
	}
}
