package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/ai"
	"viceroy/internal/budget"
	"viceroy/internal/budgetview"
	"viceroy/internal/db"
)

func (s *Server) chatRoutes(r chi.Router) {
	r.Get("/chat", s.handleChatInfo)
	r.Get("/chat/threads/{id}", s.handleGetChatThread)
	r.Delete("/chat/threads/{id}", s.handleDeleteChatThread)
	r.Post("/chat/messages", s.handleChatMessage)
}

type chatThreadDTO struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	UpdatedAt int64  `json:"updated_at"`
}

type chatMessageDTO struct {
	ID      int64    `json:"id"`
	Role    string   `json:"role"` // user | assistant
	Content string   `json:"content"`
	Tools   []string `json:"tools,omitempty"` // tools the assistant used before this answer
}

// GET /chat: whether chat is configured, plus the user's recent threads.
func (s *Server) handleChatInfo(w http.ResponseWriter, r *http.Request) {
	rows, err := db.New(s.db).ListChatThreads(r.Context(), CurrentUser(r).ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	threads := make([]chatThreadDTO, len(rows))
	for i, t := range rows {
		threads[i] = chatThreadDTO{t.ID, t.Title, t.UpdatedAt}
	}
	client, err := s.ai.ChatClient(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := map[string]any{"configured": client.Configured(), "threads": threads}
	if client.Configured() {
		out["model"] = client.Model
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetChatThread(w http.ResponseWriter, r *http.Request) {
	ctx, q := r.Context(), db.New(s.db)
	t, err := q.GetChatThread(ctx, db.GetChatThreadParams{ID: txnID(r), UserID: CurrentUser(r).ID})
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Conversation not found.")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	rows, err := q.ListChatMessages(ctx, t.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	// Show user messages and assistant text; fold tool rounds into the answer that follows.
	msgs := []chatMessageDTO{}
	var tools []string
	for _, m := range rows {
		switch m.Role {
		case "user":
			tools = nil
			msgs = append(msgs, chatMessageDTO{ID: m.ID, Role: "user", Content: m.Content})
		case "assistant":
			var calls []ai.ToolCall
			if m.ToolCalls != "" {
				json.Unmarshal([]byte(m.ToolCalls), &calls)
			}
			for _, c := range calls {
				tools = append(tools, c.Function.Name)
			}
			if strings.TrimSpace(m.Content) != "" && len(calls) == 0 {
				msgs = append(msgs, chatMessageDTO{ID: m.ID, Role: "assistant", Content: m.Content, Tools: tools})
				tools = nil
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"thread": chatThreadDTO{t.ID, t.Title, t.UpdatedAt}, "messages": msgs, "cost": s.threadCost(ctx, HouseholdID(r), t.ID)})
}

func (s *Server) handleDeleteChatThread(w http.ResponseWriter, r *http.Request) {
	if err := db.New(s.db).DeleteChatThread(r.Context(), db.DeleteChatThreadParams{ID: txnID(r), UserID: CurrentUser(r).ID}); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// maxContext caps the page data a Discuss chat starts with.
const maxContext = 120_000

// pageContext tells the model what page the chat was started from and what it showed.
func pageContext(c string) string {
	if c == "" {
		return ""
	}
	return "\n\nThe user started this chat with the Discuss button on a page of the app. Here is what that page showed (JSON; money in dollars):\n" + c +
		"\nAnswer about this data first. Use the tools for anything it doesn't include (for example search_transactions for individual transactions in a category or from a merchant, with the same dates)."
}

// maxHistory caps the stored messages sent back to the model.
const maxHistory = 60

func (s *Server) systemPrompt(ctx context.Context, r *http.Request) string {
	name := "your household"
	if h, err := db.New(s.db).GetUserHousehold(ctx, CurrentUser(r).ID); err == nil {
		name = h.Name
	}
	today := budgetview.Today()
	return fmt.Sprintf(`You are the budgeting assistant inside Viceroy, a personal finance app, talking with %s about the household "%s".
Today is %s (%s).
You can only read data, through the tools. You cannot change anything: if asked to recategorize, edit a budget or similar, say where in the app to do it (Transactions, Budget, Accounts, Goals, Settings).
Tool amounts are dollar strings; negative transaction amounts are money out. Budget and report spending are positive numbers.
Always call a tool for numbers; never guess. Keep answers short and concrete: lead with the answer, then a few bullets or a small markdown table. Format money like $1,234.56.
For advice, apply common personal-finance practice to their actual numbers and say when something is a rule of thumb: an emergency fund of 3-6 months of expenses; roughly 50/30/20 needs/wants/savings as a starting point, not a rule; budgets set from what past months really cost (budget_status or spending_report for earlier dates), not wishes; irregular costs (car repairs, gifts, annual bills) saved for monthly in Non-monthly; high-interest debt paid down before extra saving or investing beyond an employer match; savings and goals budgeted like a bill. Don't recommend specific investments.`,
		CurrentUser(r).Name, name, budget.FormatDate(today), today.Weekday())
}

// POST /chat/messages {thread_id?, content}: stores the message and streams the reply as
// server-sent events (ai.Event JSON, plus a first {"type":"thread"} event with the thread).
func (s *Server) handleChatMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ThreadID int64  `json:"thread_id"`
		Content  string `json:"content"`
		// What was on screen when the chat started from a page's Discuss button (JSON,
		// used on the first message of a new thread only).
		Context string `json:"context"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Content = strings.TrimSpace(in.Content)
	if in.Content == "" || len(in.Content) > 8000 {
		writeError(w, http.StatusBadRequest, "Write a message (up to 8,000 characters).")
		return
	}
	if len(in.Context) > maxContext {
		writeError(w, http.StatusBadRequest, "That page has too much data to discuss; try a shorter date range.")
		return
	}
	client, err := s.ai.ChatClient(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	if !client.Configured() {
		writeError(w, http.StatusServiceUnavailable, "Chat isn't set up yet: add an OpenRouter key in Settings → AI.")
		return
	}
	ctx, u, q := r.Context(), CurrentUser(r), db.New(s.db)
	now := time.Now().Unix()
	var thread db.ChatThread
	if in.ThreadID != 0 {
		thread, err = q.GetChatThread(ctx, db.GetChatThreadParams{ID: in.ThreadID, UserID: u.ID})
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "Conversation not found.")
			return
		}
	} else {
		thread, err = q.CreateChatThread(ctx, db.CreateChatThreadParams{UserID: u.ID, Title: chatTitle(in.Content), Context: in.Context, CreatedAt: now, UpdatedAt: now})
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	rows, err := q.ListChatMessages(ctx, thread.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	save := func(m ai.Message) error {
		calls := ""
		if len(m.ToolCalls) > 0 {
			b, _ := json.Marshal(m.ToolCalls)
			calls = string(b)
		}
		// Saved with a background context so a closed tab doesn't lose a finished answer.
		_, err := q.InsertChatMessage(context.WithoutCancel(ctx), db.InsertChatMessageParams{
			ThreadID: thread.ID, Role: m.Role, Content: m.Content, ToolCalls: calls, ToolCallID: m.ToolCallID, CreatedAt: time.Now().Unix(),
		})
		return err
	}
	if err := save(ai.Message{Role: "user", Content: in.Content}); err != nil {
		s.internalError(w, err)
		return
	}
	q.TouchChatThread(ctx, db.TouchChatThreadParams{UpdatedAt: now, ID: thread.ID})

	history := []ai.Message{{Role: "system", Content: s.systemPrompt(ctx, r) + pageContext(thread.Context)}}
	history = append(history, trimHistory(rows)...)
	history = append(history, ai.Message{Role: "user", Content: in.Content})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}
	send(map[string]any{"type": "thread", "thread": chatThreadDTO{thread.ID, thread.Title, now}})
	client.Ref = thread.ID
	err = client.Run(ctx, history, s.chatTools(HouseholdID(r)), func(e ai.Event) { send(e) }, save)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Warn("chat failed", "err", err)
		}
		send(ai.Event{Type: "error", Error: err.Error()})
		return
	}
	send(map[string]any{"type": "cost", "cost": s.threadCost(context.WithoutCancel(ctx), HouseholdID(r), thread.ID)})
	send(ai.Event{Type: "done"})
}

// threadCost is what a chat thread's requests cost so far.
type threadCost struct {
	Requests   int64 `json:"requests"`
	CostMicros int64 `json:"cost_micros"`
	Unpriced   int64 `json:"unpriced"` // requests whose cost the endpoint didn't report
}

func (s *Server) threadCost(ctx context.Context, hh, id int64) threadCost {
	u, err := db.New(s.db).AIUsageForRef(ctx, db.AIUsageForRefParams{HouseholdID: hh, Feature: "chat", RefID: sql.NullInt64{Int64: id, Valid: true}})
	if err != nil {
		return threadCost{}
	}
	return threadCost{u.Requests, u.CostMicros, u.Unpriced}
}

// trimHistory converts stored rows to model messages, keeping at most maxHistory and starting
// at a user message so tool results never lose their call.
func trimHistory(rows []db.ChatMessage) []ai.Message {
	start := 0
	if len(rows) > maxHistory {
		start = len(rows) - maxHistory
		for start < len(rows) && rows[start].Role != "user" {
			start++
		}
	}
	out := []ai.Message{}
	for _, m := range rows[start:] {
		msg := ai.Message{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		if m.ToolCalls != "" {
			json.Unmarshal([]byte(m.ToolCalls), &msg.ToolCalls)
		}
		out = append(out, msg)
	}
	return out
}

// chatTitle is the first line of the first message, shortened.
func chatTitle(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	if utf8.RuneCountInString(s) > 60 {
		s = string([]rune(s)[:57]) + "…"
	}
	return s
}
