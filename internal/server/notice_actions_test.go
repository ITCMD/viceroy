package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"viceroy/internal/db"
	"viceroy/internal/email"
)

func TestEmailNoticeActions(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, acct := c.do("POST", "/api/accounts", `{"name":"Visa (0428)","type":"credit_card","balance":"20"}`, true)
	acctID := int64(acct["id"].(float64))
	ctx, q := t.Context(), db.New(c.server.db)
	hh := int64(1)

	n := 0
	ingest := func(subject, body string) int64 {
		t.Helper()
		n++
		raw := strings.Join([]string{"From: Bank <alerts@bank.example>", "Subject: " + subject, "Date: " + time.Now().UTC().Format(time.RFC1123Z),
			fmt.Sprintf("Message-ID: <m%d@bank.example>", n), "Content-Type: text/plain", "", body, ""}, "\r\n")
		id, err := c.server.mail.Ingest(ctx, hh, 0, uint32(n), []byte(raw))
		if err != nil || id == 0 {
			t.Fatal(id, err)
		}
		return id
	}
	balanceBody := func(amount string) string {
		return "Visa (0428) has a balance of $" + amount + " as of " + time.Now().Format("January 2, 2006") + "."
	}
	// What the AI reading would have stored for a balance summary.
	id := ingest("Your requested balance summary", balanceBody("14.99"))
	m, _ := q.GetEmailMessage(ctx, db.GetEmailMessageParams{ID: id, HouseholdID: hh})
	facts, err := email.CheckBalance(ctx, q, hh, email.FromRow(m), email.AIResult{Kind: email.KindBalanceSummary,
		Amount: sql.NullInt64{Int64: 1499, Valid: true}, AccountText: "Visa (0428)"})
	if err != nil || facts.Problem != "" || !facts.CanAlways {
		t.Fatalf("facts = %+v %v", facts, err)
	}
	b, _ := json.Marshal(facts)
	q.SetEmailAIFacts(ctx, db.SetEmailAIFactsParams{AiFacts: string(b), ID: id})

	path := fmt.Sprintf("/api/email/messages/%d", id)
	_, detail := c.do("GET", path, "", false)
	if f, _ := detail["ai_facts"].(map[string]any); f == nil || f["balance_cents"].(float64) != -1499 || detail["suggested_account_id"].(float64) != float64(acctID) {
		t.Fatalf("detail = %v", detail)
	}
	if code, out := c.do("POST", path+"/action", `{"action":"balance","always":true,"subject_match":"balance summary"}`, true); code != 200 ||
		out["applied"] != "Set Visa (0428) balance to $14.99, and will for every email like this" || out["filter_id"] == nil {
		t.Fatalf("balance = %d %v", code, out)
	}
	balance := func() int64 {
		a, _ := q.GetAccount(ctx, db.GetAccountParams{ID: acctID, HouseholdID: hh})
		return a.BalanceCents
	}
	if got := balance(); got != -1499 {
		t.Fatalf("balance = %v", got)
	}
	_, detail = c.do("GET", path, "", false)
	if detail["status"] != "applied" || !strings.HasPrefix(detail["applied"].(string), "Set Visa") {
		t.Fatalf("after = %v", detail)
	}

	// The filter handles the next one by itself.
	next := ingest("Your requested balance summary", balanceBody("30.00"))
	m, _ = q.GetEmailMessage(ctx, db.GetEmailMessageParams{ID: next, HouseholdID: hh})
	if m.Status != email.StatusApplied || balance() != -3000 {
		t.Fatalf("next = %s %q, balance %v", m.Status, m.Applied, balance())
	}
	if fs, _ := q.ListEmailFilters(ctx, hh); len(fs) != 1 || fs[0].Action != email.ActionBalance || fs[0].BodyMatch != "Visa (0428)" {
		t.Fatalf("filters = %+v", fs)
	}

	// Mark a bill paid.
	due := ingest("Payment due", "Your payment is due.")
	if code, out := c.do("POST", fmt.Sprintf("/api/email/messages/%d/action", due), fmt.Sprintf(`{"action":"bill_paid","account_id":%d}`, acctID), true); code != 200 || out["applied"] != "Marked Visa (0428) paid" {
		t.Fatalf("bill paid = %d %v", code, out)
	}
	rows, _ := q.ListRecentBills(ctx, db.ListRecentBillsParams{HouseholdID: hh, CreatedAt: 0})
	if len(rows) != 1 || rows[0].Kind != "paid" || rows[0].AccountID.Int64 != acctID {
		t.Fatalf("bills = %+v", rows)
	}

	// Ignore emails like this: needs part of the subject.
	promo := ingest("Rewards news for you", "Earn more points.")
	ppath := fmt.Sprintf("/api/email/messages/%d/action", promo)
	if code, _ := c.do("POST", ppath, `{"action":"ignore"}`, true); code != 400 {
		t.Fatalf("ignore without subject = %d", code)
	}
	if code, out := c.do("POST", ppath, `{"action":"ignore","subject_match":"Rewards news"}`, true); code != 200 || out["filter_id"] == nil {
		t.Fatalf("ignore = %d %v", code, out)
	}
	again := ingest("Rewards news for you", "Even more points.")
	m, _ = q.GetEmailMessage(ctx, db.GetEmailMessageParams{ID: again, HouseholdID: hh})
	if m.Status != email.StatusIgnored {
		t.Fatalf("next promo = %s", m.Status)
	}
	// A balance action on an email without a verified balance is refused.
	if code, _ := c.do("POST", ppath, `{"action":"balance"}`, true); code != 400 {
		t.Fatalf("balance without facts = %d", code)
	}
}
