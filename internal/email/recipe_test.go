package email

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"viceroy/internal/ai"
	"viceroy/internal/ai/fakeai"
	"viceroy/internal/db"
)

// withdrawal is the Capital One "Withdrawal notice" the AI should learn a filter from.
func withdrawal(t *testing.T) Message {
	m := load(t, "samples/capital_one_withdrawal.eml")
	m.Date = time.Date(2026, 10, 1, 18, 2, 0, 0, time.UTC)
	return m
}

func goodRecipe() Recipe {
	return Recipe{
		Direction: "out", Amount: "17.08", Merchant: "MOUNT WASHINGTON", Date: "2026-10-01",
		AccountText: "ending in 2080", SubjectContains: "Withdrawal notice",
		AmountRule: FieldSpec{Before: "Amount:"}, MerchantRule: FieldSpec{Regex: `^(.+?) has initiated the following`},
		DateRule: &FieldSpec{Before: "Submitted on:"},
	}
}

func TestCheckRecipe(t *testing.T) {
	e := newSBEnv(t)
	acct := func(name, mask, typ string) int64 {
		a, err := e.q.CreateAccount(e.ctx, db.CreateAccountParams{HouseholdID: e.hh, Name: name, Mask: mask, Type: typ, Currency: "USD", Status: "active", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	// Renamed by the user: the name has no digits, the stored mask still does.
	checking := acct("Lucas Checking", "2080", "checking")
	m := withdrawal(t)

	chk, err := CheckRecipe(e.ctx, e.q, e.hh, m, goodRecipe())
	if err != nil || chk.Problem != "" {
		t.Fatalf("good recipe: %+v %v", chk, err)
	}
	if chk.AccountID != checking || chk.Sign != "debit" || chk.Parser != "custom" || chk.Sender != "capitalone@notification.capitalone.com" ||
		chk.SubjectMatch != "Withdrawal notice" || chk.BodyMatch != "ending in 2080" {
		t.Fatalf("filter = %+v", chk)
	}

	// Rules that don't reproduce the values fall back to a built-in parser that does.
	r := goodRecipe()
	r.MerchantRule = FieldSpec{Before: "From:"}
	if chk, _ = CheckRecipe(e.ctx, e.q, e.hh, m, r); chk.Problem != "" || chk.Parser != "generic" {
		t.Fatalf("template fallback: %+v", chk)
	}

	bad := map[string]func(*Recipe){
		"the amount 99.00 isn't in the email":                              func(r *Recipe) { r.Amount = "99.00" },
		"no clear merchant":                                                func(r *Recipe) { r.Merchant = "EVIL CORP" },
		"couldn't tell which account it's for":                             func(r *Recipe) { r.AccountText = "card 1234 is not in the email" },
		"it isn't clear whether money went out or came in":                 func(r *Recipe) { r.Direction = "sideways" },
		"its reading rules didn't give the same amount, merchant and date": func(r *Recipe) { r.Merchant = "Lucas Checking" },
	}
	for want, mod := range bad {
		r := goodRecipe()
		mod(&r)
		if chk, err := CheckRecipe(e.ctx, e.q, e.hh, m, r); err != nil || chk.Problem != want {
			t.Errorf("%s: got %q %v", want, chk.Problem, err)
		}
	}
	// Digits in the email that no account has.
	other := m
	other.Text += "\nCard ending in 9999"
	r = goodRecipe()
	r.AccountText = "ending in 9999"
	if chk, _ = CheckRecipe(e.ctx, e.q, e.hh, other, r); chk.Problem != "no account ending in 9999" {
		t.Errorf("unknown account: %q", chk.Problem)
	}
	// A subject phrase that isn't in the subject is dropped, not trusted.
	r = goodRecipe()
	r.SubjectContains = "Anything at all"
	if chk, _ = CheckRecipe(e.ctx, e.q, e.hh, m, r); chk.Problem != "" || chk.SubjectMatch != "" {
		t.Fatalf("subject: %+v", chk)
	}
	// Two accounts ending in 2080: refuse to guess.
	acct("Debit card", "2080", "checking")
	if chk, _ = CheckRecipe(e.ctx, e.q, e.hh, m, goodRecipe()); chk.Problem != "2 accounts end in 2080" {
		t.Fatalf("ambiguous: %q", chk.Problem)
	}
}

func TestSuggestAccount(t *testing.T) {
	e := newSBEnv(t)
	a, _ := e.q.CreateAccount(e.ctx, db.CreateAccountParams{HouseholdID: e.hh, Name: "My checking", Mask: "2080", Type: "checking", Currency: "USD", Status: "active", CreatedAt: 1, UpdatedAt: 1})
	id, phrase, err := SuggestAccount(e.ctx, e.q, e.hh, withdrawal(t))
	if err != nil || id != a.ID || phrase != "...2080" {
		t.Fatalf("suggest = %d %q %v", id, phrase, err)
	}
}

// The whole loop with the fake model: an unmatched withdrawal notice becomes an AI filter and a
// transaction flagged for review; the next one is read without AI.
func TestAILearnsFilter(t *testing.T) {
	e := newSBEnv(t)
	enableFakeAI(t, e)
	checking, _ := e.q.CreateAccount(e.ctx, db.CreateAccountParams{HouseholdID: e.hh, Name: "360 Checking", Mask: "2080", Type: "checking", Currency: "USD", Status: "active", IsManual: 1, CreatedAt: 1, UpdatedAt: 1})
	if err := e.q.SetMailboxAI(e.ctx, db.SetMailboxAIParams{AiRead: 1, ID: e.mb.ID, HouseholdID: e.hh}); err != nil {
		t.Fatal(err)
	}
	var notices []Notice
	e.svc.Notice = func(_ context.Context, _ int64, n Notice) { notices = append(notices, n) }

	raw := strings.ReplaceAll(string(eml(t, "samples/capital_one_withdrawal.eml")), "Thu, 01 Oct 2026 14:02:00 -0400", time.Now().Format(time.RFC1123Z))
	raw = strings.ReplaceAll(raw, "October 1, 2026", time.Now().Format("January 2, 2006"))
	id, err := e.svc.Ingest(e.ctx, e.hh, e.mb.ID, 1, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	drainAI(t, e)
	msg, _ := e.q.GetEmailMessage(e.ctx, db.GetEmailMessageParams{ID: id, HouseholdID: e.hh})
	if msg.Status != StatusParsed || !msg.TransactionID.Valid || msg.AiKind != KindTransactionAlert {
		t.Fatalf("message = %s %v %s (%s)", msg.Status, msg.TransactionID, msg.AiKind, msg.AiRecipe)
	}
	var chk RecipeCheck
	json.Unmarshal([]byte(msg.AiRecipe), &chk)
	if chk.AccountID != checking.ID || chk.Problem != "" {
		t.Fatalf("recipe = %s", msg.AiRecipe)
	}
	filters, _ := e.q.ListEmailFilters(e.ctx, e.hh)
	if len(filters) != 1 || filters[0].Source != "ai" || filters[0].AccountID != checking.ID || filters[0].Sign != "debit" {
		t.Fatalf("filters = %+v", filters)
	}
	txn, _ := e.q.GetTransaction(e.ctx, db.GetTransactionParams{ID: msg.TransactionID.Int64, HouseholdID: e.hh})
	if txn.AmountCents != -1708 || txn.Payee != "MOUNT WASHINGTON" || txn.NeedsReview != 1 || txn.AccountID != checking.ID {
		t.Fatalf("txn = %+v", txn)
	}
	if len(notices) != 1 || !strings.Contains(notices[0].Title, "New email filter") {
		t.Fatalf("notices = %+v", notices)
	}

	// The next notice is read by the filter alone.
	raw2 := strings.NewReplacer("withdrawal-2080@", "withdrawal-2@", "MOUNT WASHINGTON", "CITY WATER", "$17.08", "$42.00").Replace(raw)
	id2, _ := e.svc.Ingest(e.ctx, e.hh, e.mb.ID, 2, []byte(raw2))
	msg2, _ := e.q.GetEmailMessage(e.ctx, db.GetEmailMessageParams{ID: id2, HouseholdID: e.hh})
	if msg2.Status != StatusParsed || msg2.AiStatus != "" {
		t.Fatalf("second = %s ai=%q", msg2.Status, msg2.AiStatus)
	}

	// Editing the filter makes it the user's: the AI can't rewrite it any more.
	f := filters[0]
	if err := e.q.UpdateEmailFilter(e.ctx, db.UpdateEmailFilterParams{Name: f.Name, Priority: f.Priority, Enabled: 1, Sender: f.Sender, SubjectMatch: f.SubjectMatch,
		BodyMatch: f.BodyMatch, AccountID: f.AccountID, Parser: f.Parser, CustomParser: f.CustomParser, Sign: f.Sign, ID: f.ID, HouseholdID: e.hh}); err != nil {
		t.Fatal(err)
	}
	e.q.UpdateAIEmailFilterParser(e.ctx, db.UpdateAIEmailFilterParserParams{Parser: "generic", ID: f.ID, HouseholdID: e.hh})
	if g, _ := e.q.GetEmailFilter(e.ctx, db.GetEmailFilterParams{ID: f.ID, HouseholdID: e.hh}); g.Source != "user" || g.Parser != f.Parser {
		t.Fatalf("user filter changed: %+v", g)
	}
}

func enableFakeAI(t *testing.T, e *sbEnv) {
	fake := httptest.NewServer(&fakeai.Server{})
	t.Cleanup(fake.Close)
	client := ai.New(fake.URL, "", "local-model")
	client.Local = true
	e.svc.AI = LLMReader{Client: StaticClient(client)}
}

func drainAI(t *testing.T, e *sbEnv) {
	for {
		n, err := e.svc.ReadPendingAI(e.ctx, 5)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
}
