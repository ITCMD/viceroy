// Package fakeai is a scripted stand-in for OpenRouter's streaming chat-completions API, for
// tests and the e2e suite. It picks a tool by keyword on a user turn and summarizes the tool
// result on the next turn, streaming text a few words at a time.
package fakeai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"viceroy/internal/ai"
)

type Request struct {
	Model    string       `json:"model"`
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
	if r.Header.Get("Authorization") != "Bearer test-key" {
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
