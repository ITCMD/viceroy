package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"viceroy/internal/config"
	"viceroy/internal/db"
	"viceroy/internal/email"
	"viceroy/internal/email/fakeimap"
	"viceroy/internal/secrets"
	"viceroy/internal/syncer"
)

type client struct {
	t      *testing.T
	base   string
	http   *http.Client
	server *Server
}

func newTestServer(t *testing.T) *client {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	cfg := config.Default()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	box, _ := secrets.New(make([]byte, 32))
	web := fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}}
	mail := email.New(conn, box, slog.New(slog.DiscardHandler))
	mail.Poll = 100 * time.Millisecond
	aiset := testAI(t, conn, box)
	mail.AI = email.LLMReader{Client: aiset.EmailClient}
	go mail.Run(t.Context())
	s := New(cfg, conn, web, slog.New(slog.DiscardHandler), syncer.New(conn, box, slog.New(slog.DiscardHandler)), mail, testNotifier(conn), aiset)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: srv.URL, http: &http.Client{Jar: jar}, server: s}
}

func (c *client) do(method, path, body string, csrf bool) (int, map[string]any) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if csrf {
		req.Header.Set("X-Viceroy-CSRF", "1")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func TestSetupLoginFlow(t *testing.T) {
	c := newTestServer(t)

	code, body := c.do("GET", "/api/session", "", false)
	if code != 200 || body["needs_setup"] != true || body["user"] != nil {
		t.Fatalf("fresh session = %d %v", code, body)
	}

	setup := `{"name":"Lucas","email":"lucas@example.com","password":"correct horse battery","household_name":"Home"}`
	if code, _ := c.do("POST", "/api/setup", setup, false); code != 403 {
		t.Fatalf("setup without CSRF header = %d, want 403", code)
	}
	if code, body := c.do("POST", "/api/setup", setup, true); code != 200 {
		t.Fatalf("setup = %d %v", code, body)
	}
	code, body = c.do("GET", "/api/session", "", false)
	if body["needs_setup"] != false || body["user"] == nil || body["household"].(map[string]any)["name"] != "Home" {
		t.Fatalf("after setup session = %v", body)
	}
	if code, _ := c.do("POST", "/api/setup", setup, true); code != 409 {
		t.Fatalf("second setup = %d, want 409", code)
	}

	if code, _ := c.do("POST", "/api/auth/logout", "", true); code != 204 {
		t.Fatalf("logout = %d", code)
	}
	if _, body := c.do("GET", "/api/session", "", false); body["user"] != nil {
		t.Fatalf("still signed in after logout: %v", body)
	}

	if code, _ := c.do("POST", "/api/auth/login", `{"email":"lucas@example.com","password":"nope nope nope"}`, true); code != 401 {
		t.Fatalf("bad login = %d", code)
	}
	if code, _ := c.do("POST", "/api/auth/login", `{"email":"lucas@example.com","password":"correct horse battery"}`, true); code != 200 {
		t.Fatalf("login = %d", code)
	}
	if _, body := c.do("GET", "/api/session", "", false); body["user"] == nil {
		t.Fatal("not signed in after login")
	}
}

func TestLoginRateLimit(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"L","email":"l@example.com","password":"correct horse battery"}`, true)
	var code int
	for i := 0; i < 11; i++ {
		code, _ = c.do("POST", "/api/auth/login", `{"email":"l@example.com","password":"wrong wrong wrong"}`, true)
	}
	if code != 429 {
		t.Fatalf("11th failed login = %d, want 429", code)
	}
}

func TestSPAFallback(t *testing.T) {
	c := newTestServer(t)
	resp, err := c.http.Get(c.base + "/budget/2026-09")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(b), "app") {
		t.Fatalf("client route = %d %q", resp.StatusCode, b)
	}
	resp, _ = c.http.Get(c.base + "/assets/missing.js")
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("missing asset = %d, want 404", resp.StatusCode)
	}
	if code, _ := c.do("GET", "/api/nope", "", false); code != 404 {
		t.Fatalf("unknown api route = %d", code)
	}
}

func TestManifestContentType(t *testing.T) {
	web := fstest.MapFS{"index.html": {Data: []byte("x")}, "manifest.webmanifest": {Data: []byte("{}")}}
	rec := httptest.NewRecorder()
	spaHandler(web).ServeHTTP(rec, httptest.NewRequest("GET", "/manifest.webmanifest", nil))
	if ct := rec.Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Fatalf("content type = %q", ct)
	}
}

func TestAccountsAPI(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)

	if code, _ := c.do("GET", "/api/accounts", "", false); code != 200 {
		t.Fatalf("list accounts = %d", code)
	}
	code, out := c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"$40.25"}`, true)
	if code != 201 {
		t.Fatalf("create = %d %v", code, out)
	}
	id := int64(out["id"].(float64))
	code, _ = c.do("POST", "/api/accounts", `{"name":"Car loan","type":"loan","balance":"1,000"}`, true)
	if code != 201 {
		t.Fatalf("create loan = %d", code)
	}
	if code, _ := c.do("PATCH", "/api/accounts/"+strconv.FormatInt(id, 10), `{"balance":"50"}`, true); code != 204 {
		t.Fatalf("patch = %d", code)
	}
	_, hist := c.do("GET", "/api/networth/history?days=7", "", false)
	pts := hist["points"].([]any)
	last := pts[len(pts)-1].(map[string]any)
	if last["assets"].(float64) != 5000 || last["liabilities"].(float64) != 100000 || last["net"].(float64) != -95000 {
		t.Fatalf("net worth = %v", last)
	}
	if code, _ := c.do("POST", "/api/connections", `{"setup_token":"nope!"}`, true); code != 400 {
		t.Fatalf("bad token = %d", code)
	}
	if code, _ := c.do("PATCH", "/api/accounts/999", `{"name":"x"}`, true); code != 404 {
		t.Fatalf("missing account = %d", code)
	}
	_, ah := c.do("GET", "/api/accounts/"+strconv.FormatInt(id, 10)+"/history?days=365", "", false)
	if apts := ah["points"].([]any); len(apts) == 0 || apts[len(apts)-1].(map[string]any)["balance"].(float64) != 5000 {
		t.Fatalf("account history = %v", ah)
	}
	if code, _ := c.do("GET", "/api/accounts/999/history", "", false); code != 404 {
		t.Fatalf("missing account history = %d", code)
	}
	if code, _ := c.do("PATCH", "/api/accounts/"+strconv.FormatInt(id, 10), `{"invert_balance":true}`, true); code != 400 {
		t.Fatalf("flip a manual balance = %d", code)
	}
}

func TestTransactionsAPI(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, out := c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"40"}`, true)
	acct := strconv.FormatInt(int64(out["id"].(float64)), 10)

	_, cats := c.do("GET", "/api/categories", "", false)
	groups := cats["groups"].([]any)
	if len(groups) != 5 {
		t.Fatalf("seeded groups = %d", len(groups))
	}
	var coffee float64
	for _, g := range groups {
		for _, ct := range g.(map[string]any)["categories"].([]any) {
			if m := ct.(map[string]any); m["name"] == "Coffee Shops" {
				coffee = m["id"].(float64)
			}
		}
	}

	code, out := c.do("POST", "/api/transactions", `{"account_id":`+acct+`,"date":"2026-09-10","amount":"-4.50","description":"Blue Bottle","tags":["treat"]}`, true)
	if code != 201 || out["merchant"] != "Blue Bottle" || out["needs_review"] != true || len(out["tags"].([]any)) != 1 {
		t.Fatalf("create = %d %v", code, out)
	}
	id := strconv.FormatInt(int64(out["id"].(float64)), 10)
	code, out = c.do("PATCH", "/api/transactions/"+id, fmt.Sprintf(`{"category_id":%v,"notes":"latte"}`, coffee), true)
	if code != 200 || out["category_name"] != "Coffee Shops" || out["needs_review"] != false || out["notes"] != "latte" {
		t.Fatalf("patch = %d %v", code, out)
	}
	if code, _ := c.do("PATCH", "/api/transactions/"+id, `{"category_id":99999}`, true); code != 400 {
		t.Fatalf("bad category = %d", code)
	}

	// A pending entry that matches a transaction posted the day before gets a warning.
	body := `{"account_id":` + acct + `,"date":"2026-09-11","amount":"-4.50","description":"Blue Bottle","pending":true}`
	if code, out := c.do("POST", "/api/transactions", body, true); code != 409 || len(out["duplicates"].([]any)) != 1 {
		t.Fatalf("duplicate warning = %d %v", code, out)
	}
	body = strings.Replace(body, `"pending":true`, `"pending":true,"force":true`, 1)
	code, out = c.do("POST", "/api/transactions", body, true)
	if code != 201 || out["provisional"] != true || out["category_name"] != "Coffee Shops" {
		t.Fatalf("forced pending = %d %v", code, out)
	}
	prov := strconv.FormatInt(int64(out["id"].(float64)), 10)

	// Link by hand, then unlink.
	if code, out := c.do("POST", "/api/transactions/"+prov+"/link", `{"posted_id":`+id+`}`, true); code != 200 || out["linked_txn_id"] == nil {
		t.Fatalf("link = %d %v", code, out)
	}
	_, list := c.do("GET", "/api/transactions?account="+acct, "", false)
	txns := list["transactions"].([]any)
	if len(txns) != 1 || txns[0].(map[string]any)["has_linked"] != true {
		t.Fatalf("list after link = %v", txns)
	}
	_, detail := c.do("GET", "/api/transactions/"+id, "", false)
	if len(detail["linked"].([]any)) != 1 {
		t.Fatalf("detail linked = %v", detail["linked"])
	}
	if code, _ := c.do("POST", "/api/transactions/"+prov+"/unlink", "", true); code != 200 {
		t.Fatalf("unlink = %d", code)
	}
	if _, list := c.do("GET", "/api/transactions?q=bottle", "", false); len(list["transactions"].([]any)) != 2 {
		t.Fatalf("search after unlink = %v", list)
	}

	// Rules: create, apply to existing.
	code, out = c.do("POST", "/api/rules", `{"match_field":"merchant","match_op":"contains","match_value":"bottle","set_merchant":"Blue Bottle Coffee","add_tag":"coffee"}`, true)
	if code != 201 {
		t.Fatalf("create rule = %d %v", code, out)
	}
	rule := strconv.FormatInt(int64(out["id"].(float64)), 10)
	if code, out := c.do("POST", "/api/rules/"+rule+"/apply", "", true); code != 200 || out["updated"].(float64) != 2 {
		t.Fatalf("apply rule = %d %v", code, out)
	}
	if code, _ := c.do("POST", "/api/rules", `{"match_field":"merchant","match_op":"contains","match_value":"x"}`, true); code != 400 {
		t.Fatalf("rule without action = %d", code)
	}
	if code, _ := c.do("DELETE", "/api/transactions/"+prov, "", true); code != 204 {
		t.Fatalf("delete manual = %d", code)
	}
}

func TestPaperCash(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, out := c.do("GET", "/api/accounts", "", false)
	var id string
	for _, a := range out["accounts"].([]any) {
		if m := a.(map[string]any); m["builtin"] == "paper_cash" {
			id = strconv.FormatInt(int64(m["id"].(float64)), 10)
			if m["name"] != "Paper Cash" || m["type"] != "cash" || m["status"] != "active" {
				t.Fatalf("paper cash = %v", m)
			}
		}
	}
	if id == "" {
		t.Fatal("no Paper Cash account after setup")
	}
	if code, _ := c.do("DELETE", "/api/accounts/"+id, "", true); code != 400 {
		t.Fatalf("delete = %d", code)
	}
	if code, _ := c.do("PATCH", "/api/accounts/"+id, `{"type":"checking"}`, true); code != 400 {
		t.Fatalf("retype = %d", code)
	}
	balance := func() float64 {
		_, out := c.do("GET", "/api/accounts", "", false)
		for _, a := range out["accounts"].([]any) {
			if m := a.(map[string]any); m["builtin"] == "paper_cash" {
				return m["balance_cents"].(float64)
			}
		}
		return -1
	}
	c.do("PATCH", "/api/accounts/"+id, `{"balance":"40"}`, true)
	code, out := c.do("POST", "/api/transactions", `{"account_id":`+id+`,"date":"2026-09-10","amount":"-3","description":"Farmers market"}`, true)
	if code != 201 {
		t.Fatalf("cash txn = %d %v", code, out)
	}
	txn := strconv.FormatInt(int64(out["id"].(float64)), 10)
	if b := balance(); b != 3700 {
		t.Fatalf("balance after spending = %v", b)
	}
	c.do("PATCH", "/api/transactions/"+txn, `{"amount":"-5"}`, true)
	if b := balance(); b != 3500 {
		t.Fatalf("balance after edit = %v", b)
	}
	c.do("POST", "/api/transactions", `{"account_id":`+id+`,"date":"2026-09-11","amount":"20","description":"ATM withdrawal"}`, true)
	if b := balance(); b != 5500 {
		t.Fatalf("balance after withdrawal = %v", b)
	}
	if code, _ := c.do("DELETE", "/api/transactions/"+txn, "", true); code != 204 {
		t.Fatalf("delete = %d", code)
	}
	if b := balance(); b != 6000 {
		t.Fatalf("balance after delete = %v", b)
	}

	if code, out := c.do("PATCH", "/api/settings", `{"paper_cash_enabled":false}`, true); code != 200 || out["paper_cash_enabled"] != false {
		t.Fatalf("disable = %d %v", code, out)
	}
	_, out = c.do("GET", "/api/accounts", "", false)
	for _, a := range out["accounts"].([]any) {
		if m := a.(map[string]any); m["builtin"] == "paper_cash" && (m["status"] != "closed" || m["hidden"] != true) {
			t.Fatalf("disabled paper cash = %v", m)
		}
	}
	if _, list := c.do("GET", "/api/transactions?q=atm", "", false); len(list["transactions"].([]any)) != 1 {
		t.Fatal("history lost when disabling")
	}
	if _, out := c.do("PATCH", "/api/settings", `{"paper_cash_enabled":true}`, true); out["paper_cash_enabled"] != true {
		t.Fatalf("enable = %v", out)
	}
}

func TestEmailAPI(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, acct := c.do("POST", "/api/accounts", `{"name":"CU Checking","type":"checking","balance":"100"}`, true)
	acctID := strconv.FormatInt(int64(acct["id"].(float64)), 10)

	srv, err := fakeimap.Start("127.0.0.1:0", "me@example.com", "app-pass")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	for _, f := range []string{"labeled.eml", "unknown.eml"} {
		raw, _ := os.ReadFile("../email/testdata/" + f)
		srv.Deliver(raw)
	}
	host, port, _ := net.SplitHostPort(srv.Addr)
	mbox := `{"host":"` + host + `","port":` + port + `,"security":"none","username":"me@example.com","password":"%s"}`
	if code, out := c.do("POST", "/api/email/mailboxes", fmt.Sprintf(mbox, "wrong"), true); code != 400 {
		t.Fatalf("bad password = %d %v", code, out)
	}
	code, mb := c.do("POST", "/api/email/mailboxes", fmt.Sprintf(mbox, "app-pass"), true)
	if code != 201 || mb["password_enc"] != nil || mb["folder"] != "INBOX" {
		t.Fatalf("create mailbox = %d %v", code, mb)
	}
	var open []any
	for deadline := time.Now().Add(5 * time.Second); len(open) < 2 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		_, out := c.do("GET", "/api/email/messages?status=open", "", false)
		open = out["messages"].([]any)
	}
	if len(open) != 2 {
		t.Fatalf("open messages = %v", open)
	}
	var cuMsg string
	for _, m := range open {
		if m := m.(map[string]any); m["from_addr"] == "alerts@mycu.org" {
			cuMsg = strconv.FormatInt(int64(m["id"].(float64)), 10)
		}
	}
	_, detail := c.do("GET", "/api/email/messages/"+cuMsg, "", false)
	if !strings.Contains(detail["body_text"].(string), "SHELL OIL") {
		t.Fatalf("detail = %v", detail)
	}

	// Build a custom parser against the sample.
	draft := `{"sender":"mycu.org","account_id":` + acctID + `,"parser":"custom"` +
		`,"custom_parser":{"amount":{"before":"Amount:"},"merchant":{"before":"Merchant:"}}}`
	_, pv := c.do("POST", "/api/email/filters/preview", strings.Replace(draft, "{", `{"message_id":`+cuMsg+`,`, 1), true)
	if ms := pv["matches"].([]any); len(ms) != 1 || pv["sample"].(map[string]any)["parsed"].(map[string]any)["merchant"] != "SHELL OIL 57442" {
		t.Fatalf("preview = %v", pv)
	}
	if code, _ := c.do("POST", "/api/email/filters", `{"account_id":`+acctID+`}`, true); code != 400 {
		t.Fatalf("empty filter = %d", code)
	}
	code, res := c.do("POST", "/api/email/filters", draft, true)
	if code != 201 || res["routed"].(float64) != 1 {
		t.Fatalf("create filter = %d %v", code, res)
	}
	_, detail = c.do("GET", "/api/email/messages/"+cuMsg, "", false)
	txn := strconv.FormatInt(int64(detail["transaction_id"].(float64)), 10)
	_, td := c.do("GET", "/api/transactions/"+txn, "", false)
	if tx := td["transaction"].(map[string]any); tx["source"] != "email" || tx["amount_cents"].(float64) != -4810 || td["email"].(map[string]any)["subject"] != "Debit card purchase alert" {
		t.Fatalf("transaction = %v", td)
	}
	_, out := c.do("GET", "/api/email/messages?status=open", "", false)
	news := out["messages"].([]any)[0].(map[string]any)
	if len(out["messages"].([]any)) != 1 || out["counts"].(map[string]any)["unrouted"].(float64) != 1 {
		t.Fatalf("after filter = %v", out)
	}
	newsID := strconv.FormatInt(int64(news["id"].(float64)), 10)
	if code, _ := c.do("POST", "/api/email/messages/"+newsID+"/ignore", "", true); code != 204 {
		t.Fatalf("ignore = %d", code)
	}
	if code, _ := c.do("POST", "/api/email/messages/"+cuMsg+"/ignore", "", true); code != 409 {
		t.Fatalf("ignore parsed = %d", code)
	}
}
