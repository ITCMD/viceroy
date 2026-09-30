package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// chat posts a message and returns the decoded SSE events.
func (c *client) chat(body string) []map[string]any {
	c.t.Helper()
	req, _ := http.NewRequest("POST", c.base+"/api/chat/messages", strings.NewReader(body))
	req.Header.Set("X-Viceroy-CSRF", "1")
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		c.t.Fatalf("chat = %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var out []map[string]any
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			var e map[string]any
			json.Unmarshal([]byte(data), &e)
			out = append(out, e)
		}
	}
	return out
}

func TestChatAPI(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"1000"}`, true)

	_, info := c.do("GET", "/api/chat", "", false)
	if info["configured"] != true || info["model"] != "test/model" || len(info["threads"].([]any)) != 0 {
		t.Fatalf("info = %v", info)
	}
	if code, _ := c.do("POST", "/api/chat/messages", `{"content":"  "}`, true); code != 400 {
		t.Fatalf("empty message = %d", code)
	}

	events := c.chat(`{"content":"Which accounts do I have?"}`)
	types, text := []string{}, ""
	for _, e := range events {
		types = append(types, e["type"].(string))
		if e["type"] == "text" {
			text += e["text"].(string)
		}
	}
	if types[0] != "thread" || types[1] != "tool" || events[1]["tool"] != "list_accounts" || types[len(types)-1] != "done" {
		t.Fatalf("events = %v", types)
	}
	if !strings.Contains(text, "I checked **list_accounts**") || !strings.Contains(text, "accounts: 2 items") {
		t.Fatalf("text = %q", text)
	}
	thread := events[0]["thread"].(map[string]any)
	id := thread["id"]
	if thread["title"] != "Which accounts do I have?" {
		t.Errorf("title = %v", thread["title"])
	}

	// A follow-up in the same thread; the fake answers without tools.
	events = c.chat(fmt.Sprintf(`{"thread_id":%v,"content":"thanks"}`, id))
	if events[len(events)-1]["type"] != "done" {
		t.Fatalf("follow-up events = %v", events)
	}

	code, th := c.do("GET", fmt.Sprint("/api/chat/threads/", id), "", false)
	msgs := th["messages"].([]any)
	if code != 200 || len(msgs) != 4 {
		t.Fatalf("thread = %d %v", code, th)
	}
	a1 := msgs[1].(map[string]any)
	if a1["role"] != "assistant" || fmt.Sprint(a1["tools"]) != "[list_accounts]" || msgs[3].(map[string]any)["tools"] != nil {
		t.Fatalf("messages = %v", msgs)
	}
	if _, info = c.do("GET", "/api/chat", "", false); len(info["threads"].([]any)) != 1 {
		t.Fatalf("threads = %v", info["threads"])
	}
	if code, _ := c.do("GET", "/api/chat/threads/999", "", false); code != 404 {
		t.Fatalf("missing thread = %d", code)
	}
	c.do("DELETE", fmt.Sprint("/api/chat/threads/", id), "", true)
	if _, info = c.do("GET", "/api/chat", "", false); len(info["threads"].([]any)) != 0 {
		t.Fatalf("threads after delete = %v", info["threads"])
	}
}

func TestChatTools(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, out := c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"1000"}`, true)
	acct := out["id"]
	c.do("POST", "/api/transactions", fmt.Sprintf(`{"account_id":%v,"date":"2026-09-02","amount":"-42.50","description":"Corner Cafe"}`, acct), true)
	c.do("POST", "/api/transactions", fmt.Sprintf(`{"account_id":%v,"date":"2026-08-02","amount":"-8","description":"Corner Cafe"}`, acct), true)

	srv := c.server
	tools := map[string]func(string) map[string]any{}
	for _, tl := range srv.chatTools(1) {
		tl := tl
		tools[tl.Name] = func(args string) map[string]any {
			v, err := tl.Run(t.Context(), json.RawMessage(args))
			if err != nil {
				return map[string]any{"error": err.Error()}
			}
			b, _ := json.Marshal(v)
			var m map[string]any
			json.Unmarshal(b, &m)
			return m
		}
	}
	if len(tools) != 7 {
		t.Fatalf("tools = %d", len(tools))
	}
	r := tools["search_transactions"](`{"query":"corner","from":"2026-09-01","to":"2026-09-30"}`)
	if r["matched"] != float64(1) || r["sum_of_matched"] != "-42.50" {
		t.Fatalf("search = %v", r)
	}
	r = tools["search_transactions"](`{"min_abs_amount":10}`)
	if r["matched"] != float64(1) {
		t.Fatalf("search by amount = %v", r)
	}
	if r = tools["search_transactions"](`{"category":"Nope"}`); !strings.Contains(fmt.Sprint(r["error"]), "Groceries") {
		t.Fatalf("unknown category = %v", r)
	}
	r = tools["spending_report"](`{"from":"2026-08-01","to":"2026-09-30"}`)
	if r["spending"].(map[string]any)["total"] != "50.50" || fmt.Sprint(r["periods"]) != "[2026-08 2026-09]" {
		t.Fatalf("report = %v", r)
	}
	r = tools["budget_status"](`{"view":"week","date":"2026-09-02"}`)
	if r["view"] != "week" || r["summary"] == nil {
		t.Fatalf("budget = %v", r)
	}
	if r = tools["list_accounts"](`{}`); len(r["accounts"].([]any)) != 2 {
		t.Fatalf("accounts = %v", r)
	}
	for _, name := range []string{"net_worth", "upcoming_recurring", "list_goals"} {
		if r := tools[name](`{}`); r["error"] != nil {
			t.Fatalf("%s = %v", name, r)
		}
	}
}
