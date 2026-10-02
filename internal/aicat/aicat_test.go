package aicat

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"viceroy/internal/ai"
	"viceroy/internal/categorize"
	"viceroy/internal/db"
)

type fakeClient struct {
	calls int
	reply func(user string, cats []Category) string
	cats  []Category
}

func (f *fakeClient) Configured() bool { return true }
func (f *fakeClient) CompleteJSON(ctx context.Context, msgs []ai.Message) (string, error) {
	f.calls++
	return f.reply(msgs[1].Content, f.cats), nil
}

func TestRunAndSoftRule(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	q := db.New(conn)
	h, _ := q.CreateHousehold(ctx, db.CreateHouseholdParams{Name: "H", CreatedAt: 1})
	if err := categorize.SeedDefaults(ctx, q, h.ID); err != nil {
		t.Fatal(err)
	}
	cats, _ := Categories(ctx, q, h.ID)
	byName := map[string]int64{}
	for _, c := range cats {
		byName[c.Name] = c.ID
	}
	acct, _ := q.CreateAccount(ctx, db.CreateAccountParams{HouseholdID: h.ID, Name: "Chk", Type: "checking", CreatedAt: 1, UpdatedAt: 1})
	cat, _ := categorize.New(ctx, q, h.ID)
	today := time.Now().Format(time.DateOnly)
	add := func(desc string, amt int64) int64 {
		id, err := q.InsertSyncedTransaction(ctx, db.InsertSyncedTransactionParams{
			HouseholdID: h.ID, AccountID: acct.ID, Source: "simplefin", Date: today, AmountCents: amt, Description: desc,
			CreatedAt: time.Now().Unix(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cat.Apply(ctx, categorize.Txn{ID: id, HouseholdID: h.ID, AccountID: acct.ID, AmountCents: amt, Date: today, Description: desc}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	a1 := add("BLUE BOTTLE COFFEE 12", -650)
	a2 := add("BLUE BOTTLE COFFEE 99", -700)
	b := add("ZELLE TO J SMITH", -2000)

	fc := &fakeClient{cats: cats, reply: func(user string, cats []Category) string {
		if !strings.Contains(user, "=====") || strings.Count(user, "\n") < 3 {
			t.Errorf("prompt not fenced: %q", user)
		}
		// Both coffee rows share one merchant line; for Zelle, null and a made-up id are ignored.
		lines := strings.Split(user, "\n")
		out := `{"items":[`
		for _, l := range lines {
			switch {
			case strings.Contains(l, "Blue Bottle"):
				out += `{"n":` + l[:1] + `,"category_id":` + itoa(byName["Coffee Shops"]) + `},`
			case strings.Contains(l, "Zelle"):
				out += `{"n":` + l[:1] + `,"category_id":null},{"n":` + l[:1] + `,"category_id":999999},`
			}
		}
		return strings.TrimSuffix(out, ",") + `]}`
	}}
	res, err := Run(ctx, conn, fc, h.ID, Options{Since: today})
	if err != nil {
		t.Fatal(err)
	}
	if res.Merchants != 2 || res.Categorized != 2 || res.Skipped != 1 || fc.calls != 1 {
		t.Fatalf("result %+v calls %d", res, fc.calls)
	}
	for _, id := range []int64{a1, a2} {
		tx, _ := q.GetTransactionByID(ctx, id)
		if tx.CategoryID.Int64 != byName["Coffee Shops"] || tx.CategorySource != "ai" || tx.NeedsReview != 1 {
			t.Fatalf("not categorized: %+v", tx)
		}
	}
	if tx, _ := q.GetTransactionByID(ctx, b); tx.CategoryID.Valid || tx.AiCatTried != 1 {
		t.Fatalf("zelle: %+v", tx)
	}

	// The automatic pass doesn't ask about the one it passed on.
	if res, err := Run(ctx, conn, fc, h.ID, Options{Since: today}); err != nil || res.Merchants != 0 || fc.calls != 1 {
		t.Fatalf("second run %+v %v calls %d", res, err, fc.calls)
	}
	// The manual one does.
	if res, _ := Run(ctx, conn, fc, h.ID, Options{Since: today, IncludeTried: true}); res.Merchants != 1 {
		t.Fatalf("include tried %+v", res)
	}

	// Soft rule: the next Blue Bottle comes from history, no AI needed.
	c := add("BLUE BOTTLE COFFEE 7", -500)
	if tx, _ := q.GetTransactionByID(ctx, c); tx.CategoryID.Int64 != byName["Coffee Shops"] || tx.CategorySource != categorize.SourceHistory {
		t.Fatalf("history: %+v", tx)
	}
}

func TestParseReply(t *testing.T) {
	cats := []Category{{ID: 5}, {ID: 6}}
	got := ParseReply("```json\n{\"items\":[{\"n\":1,\"category_id\":5},{\"n\":2,\"category_id\":7},{\"n\":3,\"category_id\":6},{\"n\":9,\"category_id\":6}]}\n```", 3, cats)
	if len(got) != 2 || got[0] != 5 || got[2] != 6 {
		t.Fatalf("%v", got)
	}
	if len(ParseReply("not json", 3, cats)) != 0 {
		t.Fatal("garbage accepted")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
