// Package fakeai is a scripted stand-in for OpenRouter's streaming chat-completions API, for
// tests and the e2e suite. It picks a tool by keyword on a user turn and summarizes the tool
// result on the next turn, streaming text a few words at a time.
package fakeai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"viceroy/internal/ai"
)

type Request struct {
	Model          string `json:"model"`
	Stream         bool   `json:"stream"`
	ResponseFormat *struct {
		Type string `json:"type"`
	} `json:"response_format"`
	Messages []ai.Message `json:"messages"`
	Tools    []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
}

type Server struct {
	mu       sync.Mutex
	Requests []Request
}

// keywords maps words in the user's message to the tool the fake calls.
var keywords = []struct{ word, tool string }{
	{"budget", "budget_status"},
	{"spend", "spending_report"},
	{"net worth", "net_worth"},
	{"recurring", "upcoming_recurring"},
	{"bill", "upcoming_recurring"},
	{"account", "list_accounts"},
	{"goal", "list_goals"},
	{"transaction", "search_transactions"},
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/chat/completions" && r.URL.Path != "/api/v1/chat/completions" {
		http.NotFound(w, r)
		return
	}
	// "test-key", or no key at all like a local Ollama; a wrong key is rejected.
	if auth := r.Header.Get("Authorization"); auth != "" && auth != "Bearer test-key" {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"No auth credentials found","code":401}}`))
		return
	}
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.Requests = append(s.Requests, req)
	s.mu.Unlock()

	if req.ResponseFormat != nil && !req.Stream {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{
			"role": "assistant", "content": classify(req.Messages[len(req.Messages)-1].Content)}}}})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	fl, _ := w.(http.Flusher)
	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, ": OPENROUTER PROCESSING\n\ndata: %s\n\n", b)
		if fl != nil {
			fl.Flush()
		}
	}
	last := req.Messages[len(req.Messages)-1]
	if offers(req, "update_transaction") {
		emailAgent(req, send)
		fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}
	if last.Role == "user" && len(req.Tools) > 0 {
		if tool := pickTool(last.Content); tool != "" {
			args := `{}`
			// Split the arguments over two chunks like real providers do.
			send(delta(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call_1", "type": "function",
				"function": map[string]any{"name": tool, "arguments": args[:1]}}}}, nil))
			send(delta(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": args[1:]}}}}, ptr("tool_calls")))
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
	}
	text := reply(req.Messages)
	words := strings.SplitAfter(text, " ")
	for i := 0; i < len(words); i += 3 {
		send(delta(map[string]any{"content": strings.Join(words[i:min(i+3, len(words))], "")}, nil))
	}
	send(delta(map[string]any{}, ptr("stop")))
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func ptr(s string) *string { return &s }

func delta(d map[string]any, finish *string) map[string]any {
	return map[string]any{"choices": []any{map[string]any{"index": 0, "delta": d, "finish_reason": finish}}}
}

func pickTool(msg string) string {
	m := strings.ToLower(msg)
	for _, k := range keywords {
		if strings.Contains(m, k.word) {
			return k.tool
		}
	}
	return ""
}

// reply summarizes the latest tool result, or echoes when there is none.
func reply(msgs []ai.Message) string {
	last := msgs[len(msgs)-1]
	if last.Role != "tool" {
		return "I can help with your budget, spending, accounts, recurring bills and goals. You said: " + last.Content
	}
	tool := "a tool"
	for i := len(msgs) - 1; i >= 0; i-- {
		if len(msgs[i].ToolCalls) > 0 {
			tool = msgs[i].ToolCalls[0].Function.Name
			break
		}
	}
	var res map[string]any
	json.Unmarshal([]byte(last.Content), &res)
	keys := make([]string, 0, len(res))
	for k := range res {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "I checked **%s**. Here is what it returned:\n\n", tool)
	for _, k := range keys {
		switch v := res[k].(type) {
		case []any:
			fmt.Fprintf(&b, "- %s: %d items\n", k, len(v))
		case map[string]any:
			fmt.Fprintf(&b, "- %s: %d fields\n", k, len(v))
		default:
			fmt.Fprintf(&b, "- %s: %v\n", k, v)
		}
	}
	return strings.TrimSpace(b.String())
}

var (
	moneyRe = regexp.MustCompile(`\$\s?([\d,]+\.\d{2})`)
	last4Re = regexp.MustCompile(`(?i)ending in (\d{4})`)
	dateRe  = regexp.MustCompile(`(?:January|February|March|April|May|June|July|August|September|October|November|December) \d{1,2}, \d{4}`)
)

// classify is a keyword stand-in for the email-reading prompt (email.LLMReader): it answers
// with the same JSON shape a model would.
func classify(text string) string {
	low := strings.ToLower(text)
	kind := "ignore"
	switch {
	case strings.Contains(low, "suspicious") || strings.Contains(low, "unusual activity"):
		kind = "security_alert"
	case strings.Contains(low, "scheduled"):
		kind = "payment_scheduled"
	case strings.Contains(low, "payment received") || strings.Contains(low, "thank you for your payment"):
		kind = "payment_received"
	case strings.Contains(low, "payment due") || strings.Contains(low, "minimum payment"):
		kind = "payment_due"
	case strings.Contains(low, "statement is ready"):
		kind = "statement_ready"
	case strings.Contains(low, "purchase") || strings.Contains(low, "transaction"):
		kind = "transaction_alert"
	}
	out := map[string]any{"kind": kind, "summary": nil, "account_last4": nil, "amount": nil, "minimum_due": nil, "date": nil}
	amounts := moneyRe.FindAllStringSubmatch(text, -1)
	if len(amounts) > 0 {
		out["amount"] = strings.ReplaceAll(amounts[0][1], ",", "")
	}
	if kind == "payment_due" && len(amounts) > 1 {
		out["minimum_due"] = strings.ReplaceAll(amounts[1][1], ",", "")
	}
	if m := last4Re.FindStringSubmatch(text); m != nil {
		out["account_last4"] = m[1]
	}
	if m := dateRe.FindString(text); m != "" {
		if d, err := time.Parse("January 2, 2006", m); err == nil {
			out["date"] = d.Format(time.DateOnly)
		}
	}
	summary := map[string]string{
		"security_alert": "Unusual activity was reported on your account.", "payment_scheduled": "A payment was scheduled.",
		"payment_received": "Your payment was received.", "payment_due": "A payment is due.", "statement_ready": "A new statement is ready.",
		"transaction_alert": "A purchase was made.", "ignore": "Marketing email.",
	}[kind]
	if a, ok := out["amount"].(string); ok {
		summary += " Amount $" + a + "."
	}
	out["summary"] = summary
	b, _ := json.Marshal(out)
	return "```json\n" + string(b) + "\n```" // models often fence JSON; the reader must cope
}

var orderRe = regexp.MustCompile(`Order #(\w+)`)

func offers(req Request, tool string) bool {
	for _, t := range req.Tools {
		if t.Function.Name == tool {
			return true
		}
	}
	return false
}

// emailAgent plays the sandboxed email reader: for an order email it finds the transaction by
// amount, notes the order number on it, then answers with the classification JSON.
func emailAgent(req Request, send func(any)) {
	email := ""
	for _, m := range req.Messages {
		if m.Role == "user" {
			email = m.Content
		}
	}
	call := func(name string, args any) {
		b, _ := json.Marshal(args)
		send(delta(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call_" + name, "type": "function",
			"function": map[string]any{"name": name, "arguments": string(b)}}}}, ptr("tool_calls")))
	}
	answer := func() {
		send(delta(map[string]any{"content": classify(email)}, nil))
		send(delta(map[string]any{}, ptr("stop")))
	}
	last := req.Messages[len(req.Messages)-1]
	order := orderRe.FindStringSubmatch(email)
	amount := moneyRe.FindStringSubmatch(email)
	switch {
	case len(req.Tools) == 0 || order == nil || amount == nil:
		answer()
	case last.Role == "user":
		call("search_transactions", map[string]string{"amount": strings.ReplaceAll(amount[1], ",", "")})
	case last.Role == "tool" && strings.Contains(last.Content, `"transactions"`):
		var res struct {
			Transactions []struct {
				ID int64 `json:"id"`
			} `json:"transactions"`
		}
		json.Unmarshal([]byte(last.Content), &res)
		if len(res.Transactions) == 0 {
			answer()
			return
		}
		call("update_transaction", map[string]any{"id": res.Transactions[0].ID, "add_note": "Order #" + order[1] + " https://evil.example/x", "add_tags": []string{"Online order"}})
	default:
		answer()
	}
}
