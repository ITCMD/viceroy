// Package ai talks to OpenRouter's chat-completions API (OpenAI format) with streaming and
// runs the tool-calling loop for "chat with your budget".
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultBaseURL = "https://openrouter.ai/api/v1"

var ErrNotConfigured = errors.New("AI chat is not configured: set [ai] openrouter_key in viceroy.toml")

type Message struct {
	Role       string     `json:"role"` // system | user | assistant | tool
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON text
}

// Tool is a function the model may call. Run gets the raw JSON arguments and returns a value
// that is sent back as JSON.
type Tool struct {
	Name        string
	Description string
	Parameters  json.RawMessage // JSON Schema object
	Run         func(ctx context.Context, args json.RawMessage) (any, error)
}

type Client struct {
	BaseURL string
	Key     string
	Model   string
	HTTP    *http.Client
	// Referer and Title identify the app to OpenRouter (optional headers).
	Referer string
}

func New(baseURL, key, model string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Key: key, Model: model, HTTP: &http.Client{Timeout: 3 * time.Minute}}
}

func (c *Client) Configured() bool { return c != nil && c.Key != "" }

type toolSpec struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type chunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Error *apiError `json:"error"`
}

type apiError struct {
	Message string `json:"message"`
	Code    any    `json:"code"`
}

// Stream sends one completion request and streams the reply's text to onText. It returns the
// assembled assistant message (text and any tool calls).
func (c *Client) Stream(ctx context.Context, msgs []Message, tools []Tool, onText func(string)) (Message, error) {
	if !c.Configured() {
		return Message{}, ErrNotConfigured
	}
	body := map[string]any{"model": c.Model, "messages": msgs, "stream": true}
	if len(tools) > 0 {
		specs := make([]toolSpec, len(tools))
		for i, t := range tools {
			specs[i].Type = "function"
			specs[i].Function.Name, specs[i].Function.Description, specs[i].Function.Parameters = t.Name, t.Description, t.Parameters
		}
		body["tools"] = specs
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Title", "Viceroy")
	if c.Referer != "" {
		req.Header.Set("HTTP-Referer", c.Referer)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Message{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var e struct {
			Error apiError `json:"error"`
		}
		if json.Unmarshal(b, &e) == nil && e.Error.Message != "" {
			return Message{}, fmt.Errorf("OpenRouter: %s", e.Error.Message)
		}
		return Message{}, fmt.Errorf("OpenRouter returned %s", resp.Status)
	}

	out := Message{Role: "assistant"}
	var text strings.Builder
	calls := map[int]*ToolCall{}
	order := []int{}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // blank separators and ": keep-alive" comments
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var ch chunk
		if err := json.Unmarshal([]byte(data), &ch); err != nil {
			continue
		}
		if ch.Error != nil {
			return Message{}, fmt.Errorf("OpenRouter: %s", ch.Error.Message)
		}
		for _, choice := range ch.Choices {
			if d := choice.Delta.Content; d != "" {
				text.WriteString(d)
				if onText != nil {
					onText(d)
				}
			}
			for _, tc := range choice.Delta.ToolCalls {
				call, ok := calls[tc.Index]
				if !ok {
					call = &ToolCall{Type: "function"}
					calls[tc.Index] = call
					order = append(order, tc.Index)
				}
				if tc.ID != "" {
					call.ID = tc.ID
				}
				if tc.Function.Name != "" {
					call.Function.Name += tc.Function.Name
				}
				call.Function.Arguments += tc.Function.Arguments
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Message{}, err
	}
	out.Content = text.String()
	for i, idx := range order {
		call := calls[idx]
		if call.ID == "" {
			call.ID = fmt.Sprintf("call_%d", i)
		}
		out.ToolCalls = append(out.ToolCalls, *call)
	}
	return out, nil
}

// Event is streamed to the browser while a reply is produced.
type Event struct {
	Type  string `json:"type"` // text | tool | done | error
	Text  string `json:"text,omitempty"`
	Tool  string `json:"tool,omitempty"`
	Error string `json:"error,omitempty"`
}

// MaxRounds caps model calls per user message (each tool round is one call).
const MaxRounds = 8

// maxToolResult keeps a runaway tool result from blowing up the context.
const maxToolResult = 24 * 1024

// Run answers the last user message in history: it streams text, runs requested tools and
// repeats until the model stops calling tools. Every new assistant and tool message is passed
// to save (in order) so the thread can be stored.
func (c *Client) Run(ctx context.Context, history []Message, tools []Tool, emit func(Event), save func(Message) error) error {
	byName := map[string]Tool{}
	for _, t := range tools {
		byName[t.Name] = t
	}
	msgs := append([]Message(nil), history...)
	for round := 0; round < MaxRounds; round++ {
		offer := tools
		if round == MaxRounds-1 {
			offer = nil // last round: force a text answer
		}
		reply, err := c.Stream(ctx, msgs, offer, func(s string) { emit(Event{Type: "text", Text: s}) })
		if err != nil {
			return err
		}
		if err := save(reply); err != nil {
			return err
		}
		msgs = append(msgs, reply)
		if len(reply.ToolCalls) == 0 {
			return nil
		}
		for _, call := range reply.ToolCalls {
			emit(Event{Type: "tool", Tool: call.Function.Name})
			result := runTool(ctx, byName, call)
			m := Message{Role: "tool", ToolCallID: call.ID, Content: result}
			if err := save(m); err != nil {
				return err
			}
			msgs = append(msgs, m)
		}
	}
	return nil
}

func runTool(ctx context.Context, tools map[string]Tool, call ToolCall) string {
	t, ok := tools[call.Function.Name]
	if !ok {
		return `{"error":"unknown tool"}`
	}
	args := json.RawMessage(call.Function.Arguments)
	if strings.TrimSpace(call.Function.Arguments) == "" {
		args = json.RawMessage("{}")
	}
	v, err := t.Run(ctx, args)
	if err != nil {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(b)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"could not encode result"}`
	}
	if len(b) > maxToolResult {
		b, _ = json.Marshal(map[string]string{"error": "result too large; narrow the request (shorter range or a filter)"})
	}
	return string(b)
}
