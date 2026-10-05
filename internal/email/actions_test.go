package email

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"viceroy/internal/ai"
	"viceroy/internal/ai/fakeai"
	"viceroy/internal/db"
	"viceroy/internal/secrets"
)

func TestParseBalance(t *testing.T) {
	rcv := time.Date(2026, 10, 2, 9, 0, 0, 0, time.Local)
	for _, c := range []struct {
		text   string
		amount int64
		asOf   string
	}{
		{"Quicksilver (0428) has a balance of $14.99 as of October 1, 2026.", 1499, "2026-10-01"},
		{"Current balance:\n$1,234.56", 123456, "2026-10-02"},
		{"Your balance is -$5.00 as of 10/01/2026", -500, "2026-10-01"},
	} {
		b, err := ParseBalance("generic", "", Message{Text: c.text, Date: rcv})
		if err != nil || b.AmountCents != c.amount || b.AsOf != c.asOf {
			t.Errorf("%q > %+v %v", c.text, b, err)
		}
	}
	if _, err := ParseBalance("generic", "", Message{Text: "Your statement is ready.", Date: rcv}); err == nil {
		t.Error("found a balance in an email without one")
	}
	custom, _ := json.Marshal(CustomParser{Amount: FieldSpec{Before: "Owed", After: "today"}})
	if b, err := ParseBalance("custom", string(custom), Message{Text: "Owed $88.10 today", Date: rcv}); err != nil || b.AmountCents != 8810 {
		t.Errorf("custom = %+v %v", b, err)
	}
	if err := ValidateFilterParser(ActionBalance, "custom", string(custom)); err != nil {
		t.Errorf("a balance parser shouldn't need a merchant: %v", err)
	}
}

func TestReadingTime(t *testing.T) {
	rcv := time.Date(2026, 10, 2, 9, 0, 0, 0, time.Local)
	if got := readingTime("2026-10-02", rcv); !got.Equal(rcv) {
		t.Errorf("same day = %v", got)
	}
	if got := readingTime("2026-10-01", rcv); !got.Equal(time.Date(2026, 10, 1, 23, 59, 59, 0, time.Local)) {
		t.Errorf("day before = %v", got)
	}
}

func TestBalanceNotice(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	q := db.New(conn)
	box, _ := secrets.New(make([]byte, 32))
	svc := New(conn, box, slog.New(slog.NewTextHandler(io.Discard, nil)))
	fake := httptest.NewServer(&fakeai.Server{})
	defer fake.Close()
	client := ai.New(fake.URL, "", "local-model")
	client.Local = true
	svc.AI = LLMReader{Client: StaticClient(client)}
	var notices []Notice
	svc.Notice = func(_ context.Context, _ int64, n Notice) { notices = append(notices, n) }

	h, _ := q.CreateHousehold(ctx, db.CreateHouseholdParams{Name: "H", CreatedAt: 1})
	old := time.Now().Add(-48 * time.Hour).Unix()
	card, err := q.CreateAccount(ctx, db.CreateAccountParams{HouseholdID: h.ID, Name: "Quicksilver", Mask: "0428", Type: "credit_card",
		Currency: "USD", BalanceCents: -2000, BalanceAt: sql.NullInt64{Int64: old, Valid: true}, Status: "active", CreatedAt: 1, UpdatedAt: 1})
	if err != nil {
		t.Fatal(err)
	}
	pw, _ := box.Seal("x")
	mb, _ := q.InsertMailbox(ctx, db.InsertMailboxParams{HouseholdID: h.ID, Host: "h", Port: 1, Security: "none", Username: "u", PasswordEnc: pw, Folder: "INBOX", Enabled: 0, CreatedAt: 1})
	q.SetMailboxAI(ctx, db.SetMailboxAIParams{AiRead: 1, AiSenders: "capitalone.com", ID: mb.ID, HouseholdID: h.ID})

	today := time.Now().Format("January 2, 2006")
	body := func(amount string) string {
		return "Your requested balance summary\nQuicksilver (0428) has a balance of $" + amount + " as of " + today + ".\nThanks for being a customer."
	}
	id, err := svc.Ingest(ctx, h.ID, mb.ID, 1, rawEmail("alerts@capitalone.com", "Your requested balance summary", body("14.99")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReadPendingAI(ctx, 5); err != nil {
		t.Fatal(err)
	}
	m, _ := q.GetEmailMessage(ctx, db.GetEmailMessageParams{ID: id, HouseholdID: h.ID})
	var facts Facts
	json.Unmarshal([]byte(m.AiFacts), &facts)
	if m.AiKind != KindBalanceSummary || m.Status != StatusNoticed || facts.Problem != "" || facts.AccountID != card.ID ||
		facts.AmountCents != 1499 || facts.BalanceCents != -1499 || !facts.CanAlways || facts.AccountText != "Quicksilver (0428)" {
		t.Fatalf("message %s/%s facts %+v", m.AiKind, m.Status, facts)
	}
	if len(notices) != 1 || notices[0].Title != "Balance · Quicksilver" || !strings.Contains(notices[0].URL, "email=") {
		t.Fatalf("notices = %+v", notices)
	}

	// Applying sets the synced card's balance as a newer reading...
	if _, err := ApplyBalance(ctx, q, h.ID, card.ID, facts.AmountCents, facts.AsOf, time.Unix(m.ReceivedAt, 0), time.Now()); err != nil {
		t.Fatal(err)
	}
	bal := func() int64 {
		a, _ := q.GetAccount(ctx, db.GetAccountParams{ID: card.ID, HouseholdID: h.ID})
		return a.BalanceCents
	}
	if bal() != -1499 {
		t.Fatalf("balance = %d", bal())
	}
	// ...that a sync with an older bank balance doesn't undo, and a newer one replaces.
	sync := func(cents, at int64) int64 {
		kept, err := q.UpdateAccountFromSync(ctx, db.UpdateAccountFromSyncParams{Currency: "USD", BalanceCents: cents,
			BalanceAt: sql.NullInt64{Int64: at, Valid: true}, UpdatedAt: at, ID: card.ID})
		if err != nil {
			t.Fatal(err)
		}
		return kept
	}
	if kept := sync(-2000, old); kept != -1499 {
		t.Errorf("older sync replaced the email balance: %d", kept)
	}
	if kept := sync(-3000, time.Now().Add(time.Hour).Unix()); kept != -3000 {
		t.Errorf("newer sync kept %d", kept)
	}
	// Now the bank's balance is newer than the email's: applying it again is refused.
	if _, err := ApplyBalance(ctx, q, h.ID, card.ID, 1499, facts.AsOf, time.Unix(m.ReceivedAt, 0), time.Now()); err != ErrStaleBalance {
		t.Errorf("stale apply = %v", err)
	}

	// A balance filter does it for the next email without AI.
	q.UpdateAccountFromSync(ctx, db.UpdateAccountFromSyncParams{Currency: "USD", BalanceCents: -3000, BalanceAt: sql.NullInt64{Int64: old, Valid: true}, ID: card.ID})
	conn.ExecContext(ctx, "UPDATE accounts SET balance_at = ? WHERE id = ?", old, card.ID)
	if _, err := q.InsertEmailFilter(ctx, db.InsertEmailFilterParams{HouseholdID: h.ID, Name: "bal", Priority: 1, Enabled: 1,
		Sender: "alerts@capitalone.com", SubjectMatch: "balance summary", BodyMatch: "Quicksilver (0428)", AccountID: card.ID,
		Parser: "generic", Sign: "debit", Action: ActionBalance, CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	raw := rawEmail("alerts@capitalone.com", "Your requested balance summary", body("1,020.00"))
	raw = []byte(strings.Replace(string(raw), "Message-ID: <", "Message-ID: <2-", 1))
	id2, err := svc.Ingest(ctx, h.ID, mb.ID, 2, raw)
	if err != nil {
		t.Fatal(err)
	}
	m2, _ := q.GetEmailMessage(ctx, db.GetEmailMessageParams{ID: id2, HouseholdID: h.ID})
	if m2.Status != StatusApplied || m2.AiStatus != "" || bal() != -102000 || !strings.Contains(m2.Applied, "$1020.00") {
		t.Fatalf("filtered email %s %q ai=%q, balance %d", m2.Status, m2.Applied, m2.AiStatus, bal())
	}
}

func TestCheckBalanceProblems(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	q := db.New(conn)
	h, _ := q.CreateHousehold(ctx, db.CreateHouseholdParams{Name: "H", CreatedAt: 1})
	for _, name := range []string{"Card A", "Card B"} {
		q.CreateAccount(ctx, db.CreateAccountParams{HouseholdID: h.ID, Name: name, Mask: "0428", Type: "credit_card", Currency: "USD", Status: "active", CreatedAt: 1, UpdatedAt: 1})
	}
	q.CreateAccount(ctx, db.CreateAccountParams{HouseholdID: h.ID, Name: "Checking", Mask: "1111", Type: "checking", Currency: "USD", Status: "active", CreatedAt: 1, UpdatedAt: 1})
	m := Message{Subject: "Balance", Text: "Card (0428) has a balance of $14.99. Checking (1111) has a balance of $500.00.", Date: time.Now()}
	res := func(amount int64, text string) AIResult {
		return AIResult{Kind: KindBalanceSummary, Amount: sql.NullInt64{Int64: amount, Valid: true}, AccountText: text}
	}
	for _, c := range []struct {
		res  AIResult
		want string
	}{
		{res(1500, "Card (0428)"), "isn't in the email"},
		{res(1499, "Card (0428)"), "2 accounts end in 0428"},
		{res(1499, "Card (9999)"), "couldn't tell which account"},
		{res(50000, "Checking (1111)"), ""},
	} {
		f, err := CheckBalance(ctx, q, h.ID, m, c.res)
		if err != nil {
			t.Fatal(err)
		}
		if (c.want == "") != (f.Problem == "") || !strings.Contains(f.Problem, c.want) {
			t.Errorf("%+v > problem %q, want %q", c.res, f.Problem, c.want)
		}
		if c.want == "" && f.BalanceCents != 50000 {
			t.Errorf("checking balance = %d", f.BalanceCents)
		}
	}
}
