package email

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"viceroy/internal/ai"
	"viceroy/internal/ai/fakeai"
	"viceroy/internal/bills"
	"viceroy/internal/db"
	"viceroy/internal/secrets"
)

func TestParseAIResult(t *testing.T) {
	rcv := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	r, err := ParseAIResult("Sure!\n```json\n"+`{"kind":"Payment_Due","summary":"Your Quicksilver payment of $245.10 is due Oct 12.","account_last4":"xx3333","amount":245.1,"minimum_due":"$35.00","date":"2026-10-12"}`+"\n```", rcv)
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != KindPaymentDue || r.Last4 != "3333" || r.Amount.Int64 != 24510 || r.Minimum.Int64 != 3500 || r.Date != "2026-10-12" {
		t.Errorf("result = %+v", r)
	}
	// Unknown kind fails; nonsense fields are dropped, not guessed.
	if _, err := ParseAIResult(`{"kind":"pay_now"}`, rcv); err == nil {
		t.Error("unknown kind accepted")
	}
	r, err = ParseAIResult(`{"kind":"payment_due","account_last4":"12","amount":"lots","date":"2031-01-01"}`, rcv)
	if err != nil || r.Last4 != "" || r.Amount.Valid || r.Date != "" {
		t.Errorf("bad fields = %+v %v", r, err)
	}
	if _, err := ParseAIResult("I can't help with that.", rcv); err == nil {
		t.Error("non-JSON accepted")
	}
}

func TestSenderAllowed(t *testing.T) {
	list := "capitalone.com\nalerts@mybank.com"
	for addr, want := range map[string]bool{
		"service@notification.capitalone.com": true, "alerts@mybank.com": true, "other@mybank.com": false, "friend@gmail.com": false,
	} {
		if got := SenderAllowed(list, addr); got != want {
			t.Errorf("SenderAllowed(%q) = %v", addr, got)
		}
	}
	if !SenderAllowed("", "anyone@x.com") {
		t.Error("an empty list should allow everyone")
	}
}

func rawEmail(from, subject, body string) []byte {
	return []byte(strings.Join([]string{
		"From: Bank <" + from + ">", "Subject: " + subject, "Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"Message-ID: <" + strings.ReplaceAll(subject, " ", "-") + "@" + strings.SplitN(from, "@", 2)[1] + ">",
		"Content-Type: text/plain", "", body, "",
	}, "\r\n"))
}

func TestAIReading(t *testing.T) {
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
	svc.AI = LLMReader{Client: client}
	var notices []Notice
	svc.Notice = func(_ context.Context, _ int64, n Notice) { notices = append(notices, n) }

	h, _ := q.CreateHousehold(ctx, db.CreateHouseholdParams{Name: "H", CreatedAt: 1})
	card, err := q.CreateAccount(ctx, db.CreateAccountParams{HouseholdID: h.ID, Name: "Quicksilver Card (3333)", Mask: "3333", Type: "credit_card",
		Currency: "USD", Status: "active", CreatedAt: 1, UpdatedAt: 1})
	if err != nil {
		t.Fatal(err)
	}
	pw, _ := box.Seal("x")
	mb, _ := q.InsertMailbox(ctx, db.InsertMailboxParams{HouseholdID: h.ID, Host: "h", Port: 1, Security: "none", Username: "u", PasswordEnc: pw, Folder: "INBOX", Enabled: 0, CreatedAt: 1})

	ingest := func(from, subject, body string) int64 {
		t.Helper()
		id, err := svc.Ingest(ctx, h.ID, mb.ID, 1, rawEmail(from, subject, body))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	msg := func(id int64) db.EmailMessage {
		m, _ := q.GetEmailMessage(ctx, db.GetEmailMessageParams{ID: id, HouseholdID: h.ID})
		return m
	}
	due := time.Now().AddDate(0, 0, 10).Format("January 2, 2006")

	// AI reading off: nothing is queued.
	off := ingest("alerts@bank.example", "Payment due soon", "Your payment due is $245.10 on "+due+" for your card ending in 3333. Minimum payment $35.00.")
	if msg(off).AiStatus != "" {
		t.Fatal("queued with AI reading off")
	}

	if err := q.SetMailboxAI(ctx, db.SetMailboxAIParams{AiRead: 1, AiSenders: "bank.example", ID: mb.ID, HouseholdID: h.ID}); err != nil {
		t.Fatal(err)
	}
	if n, _ := svc.QueueRecentForAI(ctx, h.ID); n != 1 {
		t.Fatalf("queued recent = %d, want 1", n)
	}
	friend := ingest("pal@gmail.com", "Lunch?", "Want to grab lunch?")
	security := ingest("alerts@bank.example", "Unusual activity", "We noticed suspicious activity on your card ending in 3333.")
	purchase := ingest("alerts@bank.example", "Purchase alert", "A purchase of $12.00 was made with your card ending in 3333.")
	scheduled := ingest("alerts@bank.example", "Payment scheduled", "Your payment of $245.10 is scheduled for "+due+" from checking, card ending in 3333.")
	if msg(friend).AiStatus != "" {
		t.Fatal("a sender outside the list was queued")
	}

	for {
		n, err := svc.ReadPendingAI(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	if m := msg(off); m.AiKind != KindPaymentDue || m.Status != StatusNoticed {
		t.Errorf("due email = %s / %s", m.AiKind, m.Status)
	}
	if m := msg(scheduled); m.AiKind != KindPaymentScheduled {
		t.Errorf("scheduled email = %s", m.AiKind)
	}
	if m := msg(security); m.AiKind != KindSecurityAlert || m.Status != StatusNoticed {
		t.Errorf("security email = %s / %s", m.AiKind, m.Status)
	}
	// A purchase alert with no filter stays in "Emails to review", labelled.
	if m := msg(purchase); m.AiKind != KindTransactionAlert || m.Status != StatusUnrouted || m.AiSummary == "" {
		t.Errorf("purchase email = %+v", m)
	}

	titles := []string{}
	for _, n := range notices {
		titles = append(titles, n.Title)
	}
	wantDue := "Payment due " + time.Now().AddDate(0, 0, 10).Format("Jan 2") + " · Quicksilver Card (3333)"
	got := strings.Join(titles, " | ")
	if !strings.Contains(got, wantDue) || !strings.Contains(got, "Security alert · Quicksilver Card (3333)") || len(notices) != 3 {
		t.Fatalf("notices = %q", got)
	}

	rows, _ := q.ListRecentBills(ctx, db.ListRecentBillsParams{HouseholdID: h.ID, CreatedAt: 0})
	st := bills.ByAccount(rows, time.Now().Format(time.DateOnly))[card.ID]
	if st.Due == nil || st.Due.AmountCents != (sql.NullInt64{Int64: 24510, Valid: true}) || st.Scheduled == nil {
		t.Fatalf("bill state = %+v", st)
	}
}
