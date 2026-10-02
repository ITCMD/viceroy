// Package fakeai is a scripted stand-in for OpenRouter's streaming chat-completions API, for
// tests and the e2e suite. It picks a tool by keyword on a user turn and summarizes the tool
// result on the next turn, streaming text a few words at a time.
package fakeai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
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

// models is a small slice of OpenRouter's catalog shape.
const models = `{"data":[
{"id":"anthropic/claude-sonnet-5.5","name":"Anthropic: Claude Sonnet 5.5","context_length":1000000,"pricing":{"prompt":"0.000003","completion":"0.000015"},"architecture":{"input_modalities":["text","image"]},"supported_parameters":["tools","response_format"]},
{"id":"google/gemini-3-pro","name":"Google: Gemini 3 Pro","context_length":1048576,"pricing":{"prompt":"0.00000125","completion":"0.00001"},"architecture":{"input_modalities":["text","image","file"]},"supported_parameters":["tools"]},
{"id":"deepseek/deepseek-v4-flash","name":"DeepSeek: V4 Flash","context_length":163840,"pricing":{"prompt":"0.00000007","completion":"0.00000028"},"architecture":{"input_modalities":["text"]},"supported_parameters":["tools","response_format"]},
{"id":"meta/llama-free","name":"Meta: Llama (free)","context_length":131072,"pricing":{"prompt":"0","completion":"0"},"architecture":{"input_modalities":["text"]},"supported_parameters":[]}
]}`

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/models" || r.URL.Path == "/api/v1/models" {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(models))
		return
	}
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
		last := req.Messages[len(req.Messages)-1]
		content := classify(last.Content)
		if strings.Contains(req.Messages[0].Content, "imported into Viceroy") {
			content = budgetRows(last)
		}
		if strings.Contains(req.Messages[0].Content, "You categorize bank transactions") {
			content = categorizeReply(req.Messages[0].Content, last.Content)
		}
		if strings.Contains(req.Messages[0].Content, "brand colors") {
			content = brandColors(last.Content)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{
			"role": "assistant", "content": content}}}})
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
	// "Quicksilver (0428)"
	acctParenRe = regexp.MustCompile(`([A-Z][A-Za-z]+) \((\d{4})\)`)
	dateRe      = regexp.MustCompile(`(?:January|February|March|April|May|June|July|August|September|October|November|December) \d{1,2}, \d{4}`)
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
	case strings.Contains(low, "balance summary") || strings.Contains(low, "has a balance of"):
		kind = "balance_summary"
	case strings.Contains(low, "purchase") || strings.Contains(low, "transaction") || strings.Contains(low, "withdrawal") || strings.Contains(low, "deposit"):
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
	if m := acctParenRe.FindStringSubmatch(text); m != nil && kind == "balance_summary" {
		out["account_last4"], out["account_text"] = m[2], m[0]
	}
	if m := dateRe.FindString(text); m != "" {
		if d, err := time.Parse("January 2, 2006", m); err == nil {
			out["date"] = d.Format(time.DateOnly)
		}
	}
	summary := map[string]string{
		"security_alert": "Unusual activity was reported on your account.", "payment_scheduled": "A payment was scheduled.",
		"payment_received": "Your payment was received.", "payment_due": "A payment is due.", "statement_ready": "A new statement is ready.", "balance_summary": "Your balance was reported.",
		"transaction_alert": "A purchase was made.", "ignore": "Marketing email.",
	}[kind]
	if a, ok := out["amount"].(string); ok {
		summary += " Amount $" + a + "."
	}
	out["summary"] = summary
	if kind == "transaction_alert" {
		out["transaction"] = recipe(text)
	}
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

var budgetLineRe = regexp.MustCompile(`(?i)^\s*([A-Za-z][A-Za-z &']*?)\s*[:\-]?\s*\$?([\d,]+(?:\.\d{1,2})?)\s*(?:/\s*(year|yr|week|wk|month|mo))?\s*$`)

// budgetRows plays the budget-import reader (budgetio.Messages): screenshots get a fixed
// budget; text lines like "Dining out: $300" or "Vacation 2400/year" become rows.
func budgetRows(m ai.Message) string {
	type row struct {
		Group    string  `json:"group"`
		Category string  `json:"category"`
		Amount   string  `json:"amount"`
		Timing   *string `json:"timing"`
		Icon     *string `json:"icon"`
	}
	var rows []row
	notes := ""
	if slices.ContainsFunc(m.Parts, func(p ai.Part) bool { return p.ImageURL != nil }) {
		rows = []row{
			{Group: "Fixed", Category: "Rent", Amount: "1850", Timing: ptr("day 1")},
			{Group: "Flexible", Category: "Groceries", Amount: "620"},
			{Group: "Flexible", Category: "Restaurants & Bars", Amount: "275"},
			{Group: "Flexible", Category: "Hobbies", Amount: "80", Icon: ptr("🎨")},
		}
		notes = "Read from the screenshot."
	}
	for _, line := range strings.Split(m.Content, "\n") {
		g := budgetLineRe.FindStringSubmatch(line)
		if g == nil {
			continue
		}
		name, low := strings.TrimSpace(g[1]), strings.ToLower(g[1])
		cents, _ := strconv.ParseFloat(strings.ReplaceAll(g[2], ",", ""), 64)
		switch strings.ToLower(g[3]) {
		case "year", "yr":
			cents /= 12
		case "week", "wk":
			cents = cents * 52 / 12
		}
		r := row{Group: "Flexible", Category: name, Amount: strconv.FormatFloat(cents, 'f', 2, 64)}
		switch {
		case strings.Contains(low, "dining") || strings.Contains(low, "eating out"):
			r.Category = "Restaurants & Bars"
		case strings.Contains(low, "rent"):
			r.Group, r.Timing = "Fixed", ptr("day 1")
		case strings.Contains(low, "salary") || strings.Contains(low, "paycheck"):
			r.Group, r.Category = "Income", "Paychecks"
		case strings.Contains(low, "vacation") || strings.Contains(low, "travel"):
			r.Group = "Non-monthly"
		case strings.Contains(low, "total"):
			continue
		}
		rows = append(rows, r)
	}
	b, _ := json.Marshal(map[string]any{"rows": rows, "notes": notes})
	return string(b)
}

var (
	initiatedRe = regexp.MustCompile(`(?m)^(.+?) has initiated the following (withdrawal|deposit)`)
	labeledAmt  = regexp.MustCompile(`Amount: \$([\d,]+\.\d{2})`)
	submittedRe = regexp.MustCompile(`Submitted on: ([A-Z][a-z]+ \d{1,2}, \d{4})`)
	subjectRe   = regexp.MustCompile(`Subject: ([^\n]+)`)
)

// recipe plays the AI's "transaction" reading for withdrawal/deposit notices; other alerts get
// one without a merchant (which Viceroy then refuses to build a filter from).
func recipe(text string) map[string]any {
	r := map[string]any{"direction": "out", "amount": nil, "merchant": nil, "date": nil, "account_text": nil, "subject_contains": "",
		"amount_rule": map[string]string{"before": "Amount:"}, "merchant_rule": map[string]string{"regex": `^(.+?) has initiated the following`},
		"date_rule": map[string]string{"before": "Submitted on:"}}
	if m := initiatedRe.FindStringSubmatch(text); m != nil {
		r["merchant"] = strings.TrimSpace(m[1])
		if m[2] == "deposit" {
			r["direction"] = "in"
		}
	}
	if m := labeledAmt.FindStringSubmatch(text); m != nil {
		r["amount"] = strings.ReplaceAll(m[1], ",", "")
	} else if m := moneyRe.FindStringSubmatch(text); m != nil {
		r["amount"] = strings.ReplaceAll(m[1], ",", "")
	}
	if m := submittedRe.FindStringSubmatch(text); m != nil {
		if d, err := time.Parse("January 2, 2006", m[1]); err == nil {
			r["date"] = d.Format(time.DateOnly)
		}
	}
	if m := last4Re.FindStringSubmatch(text); m != nil {
		r["account_text"] = m[0]
	}
	if m := subjectRe.FindStringSubmatch(text); m != nil {
		r["subject_contains"] = strings.TrimSpace(m[1])
	}
	return r
}

// categorizeReply plays the transaction categorizer (aicat.Messages): known merchant words map
// to seeded category names; anything else gets null.
func categorizeReply(system, user string) string {
	ids := map[string]int64{}
	for _, line := range strings.Split(system, "\n") {
		var id int64
		var rest string
		if i := strings.Index(line, ": "); i > 0 {
			if _, err := fmt.Sscanf(line[:i], "%d", &id); err == nil {
				rest = line[i+2:]
				if j := strings.LastIndex(rest, " ("); j > 0 {
					rest = rest[:j]
				}
				ids[strings.ToLower(rest)] = id
			}
		}
	}
	words := []struct{ word, cat string }{
		{"coffee", "coffee shops"}, {"starbucks", "coffee shops"}, {"blue bottle", "coffee shops"},
		{"chipotle", "restaurants & bars"}, {"trattoria", "restaurants & bars"}, {"pizza", "restaurants & bars"},
		{"trader joe", "groceries"}, {"whole foods", "groceries"}, {"market", "groceries"},
		{"shell", "gas"}, {"chevron", "gas"},
	}
	type item struct {
		N          int    `json:"n"`
		CategoryID *int64 `json:"category_id"`
	}
	var items []item
	for _, line := range strings.Split(user, "\n") {
		var n int
		if _, err := fmt.Sscanf(line, "%d.", &n); err != nil || n == 0 {
			continue
		}
		low := strings.ToLower(line)
		it := item{N: n}
		for _, w := range words {
			if strings.Contains(low, w.word) {
				if id, ok := ids[w.cat]; ok {
					it.CategoryID = &id
				}
				break
			}
		}
		items = append(items, it)
	}
	b, _ := json.Marshal(map[string]any{"items": items})
	return string(b)
}

// brandColors plays the bank color picker (branding.Messages).
func brandColors(user string) string {
	known := map[string]string{"capital one": "#004977", "chase": "#117aca", "dcu": "#00703c", "first platypus": "#6b4fbb"}
	type item struct {
		N     int    `json:"n"`
		Color string `json:"color"`
	}
	var items []item
	for _, line := range strings.Split(user, "\n") {
		var n int
		if _, err := fmt.Sscanf(line, "%d.", &n); err != nil || n == 0 {
			continue
		}
		low := strings.ToLower(line)
		c := ""
		for k, v := range known {
			if strings.Contains(low, k) {
				c = v
			}
		}
		items = append(items, item{N: n, Color: c})
	}
	b, _ := json.Marshal(map[string]any{"items": items})
	return string(b)
}
