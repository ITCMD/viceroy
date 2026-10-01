package server

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestAIChangesAndUndo(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, acct := c.do("POST", "/api/accounts", `{"name":"Card","type":"credit_card","balance":"0"}`, true)
	today := time.Now().Format(time.DateOnly)
	_, txn := c.do("POST", "/api/transactions", fmt.Sprintf(`{"account_id":%v,"date":"%s","amount":"-45.99","description":"AMZN MKTP","notes":"birthday gift"}`, acct["id"], today), true)

	srv := startFakeIMAP(t)
	host, port, _ := net.SplitHostPort(srv.Addr)
	_, mb := c.do("POST", "/api/email/mailboxes", `{"host":"`+host+`","port":`+port+`,"security":"none","username":"me@example.com","password":"app-pass","enabled":false,"ai_read":true,"ai_senders":"shop.example"}`, true)

	raw := strings.Join([]string{"From: Shop <orders@shop.example>", "Subject: Your order", "Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"Message-ID: <o1@shop.example>", "Content-Type: text/plain", "", "Order #ZX9 for $45.99 has shipped.", ""}, "\r\n")
	if _, err := c.server.mail.Ingest(t.Context(), 1, int64(mb["id"].(float64)), 1, []byte(raw)); err != nil {
		t.Fatal(err)
	}
	if n, err := c.server.mail.ReadPendingAI(t.Context(), 5); err != nil || n != 1 {
		t.Fatalf("read = %d %v", n, err)
	}

	path := fmt.Sprint("/api/transactions/", txn["id"])
	_, d := c.do("GET", path, "", false)
	tx := d["transaction"].(map[string]any)
	changes := d["ai_changes"].([]any)
	// It was already flagged for review (uncategorized), so only the note and tag are the AI's.
	if tx["notes"] != "birthday gift\nAI: Order #ZX9" || tx["needs_review"] != true || len(changes) != 2 {
		t.Fatalf("after AI: notes %q review %v changes %v", tx["notes"], tx["needs_review"], changes)
	}
	if ch := changes[0].(map[string]any); ch["email_subject"] != "Your order" || ch["description"] != "Added a note" {
		t.Errorf("change = %v", ch)
	}

	code, after := c.do("POST", path+"/ai-undo", "", true)
	// Undo reverts only what the AI did: the review flag it found stays.
	if code != 200 || after["notes"] != "birthday gift" || after["needs_review"] != true || len(after["tags"].([]any)) != 0 {
		t.Fatalf("undo = %d %v", code, after)
	}
	if _, d = c.do("GET", path, "", false); len(d["ai_changes"].([]any)) != 0 {
		t.Fatalf("changes after undo = %v", d["ai_changes"])
	}
}
