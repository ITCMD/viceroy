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
	"math/big"
	"net/http"
	"slices"
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
	// Parts, when set, is sent as the content instead of Content (text and images for
	// multimodal models). Decoding an array content fills Parts and joins its text into Content.
	Parts []Part `json:"-"`
}

// Part is one piece of a multimodal message: {type: text, text} or {type: image_url, image_url}.
type Part struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

type ImageURL struct {
	URL string `json:"url"` // https URL or data:image/...;base64,...
}

func TextPart(s string) Part    { return Part{Type: "text", Text: s} }
func ImagePart(url string) Part { return Part{Type: "image_url", ImageURL: &ImageURL{URL: url}} }
func (m Message) HasImages() bool {
	return slices.ContainsFunc(m.Parts, func(p Part) bool { return p.ImageURL != nil })
}

type plainMessage Message

func (m Message) MarshalJSON() ([]byte, error) {
	if len(m.Parts) == 0 {
		return json.Marshal(plainMessage(m))
	}
	return json.Marshal(struct {
		plainMessage
		Content []Part `json:"content"`
	}{plainMessage(m), m.Parts})
}

func (m *Message) UnmarshalJSON(b []byte) error {
	var raw struct {
		plainMessage
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*m = Message(raw.plainMessage)
	c := bytes.TrimSpace(raw.Content)
	switch {
	case len(c) > 0 && c[0] == '[':
		if err := json.Unmarshal(c, &m.Parts); err != nil {
			return err
		}
		var texts []string
		for _, p := range m.Parts {
			if p.Text != "" {
				texts = append(texts, p.Text)
			}
		}
		m.Content = strings.Join(texts, "\n")
	case len(c) > 0 && c[0] == '"':
		return json.Unmarshal(c, &m.Content)
	}
	return nil
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
	// Local marks a self-hosted endpoint (Ollama, llama.cpp) that needs no API key.
	Local bool
}

func New(baseURL, key, model string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Key: key, Model: model, HTTP: &http.Client{Timeout: 3 * time.Minute}}
}

func (c *Client) Configured() bool { return c != nil && c.Model != "" && (c.Key != "" || c.Local) }

func (c *Client) post(ctx context.Context, body map[string]any) (*http.Response, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Title", "Viceroy")
	if c.Referer != "" {
		req.Header.Set("HTTP-Referer", c.Referer)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var e struct {
			Error apiError `json:"error"`
		}
		if json.Unmarshal(b, &e) == nil && e.Error.Message != "" {
			return nil, fmt.Errorf("%s: %s", c.name(), e.Error.Message)
		}
		return nil, fmt.Errorf("%s returned %s", c.name(), resp.Status)
	}
	return resp, nil
}

func (c *Client) name() string {
	if c.Local {
		return "AI endpoint"
	}
	return "OpenRouter"
}

// CompleteJSON sends one non-streaming request asking for a JSON object and returns the
// reply text (the caller validates it).
func (c *Client) CompleteJSON(ctx context.Context, msgs []Message) (string, error) {
	if !c.Configured() {
		return "", ErrNotConfigured
	}
	resp, err := c.post(ctx, map[string]any{
		"model": c.Model, "messages": msgs, "temperature": 0,
		"response_format": map[string]string{"type": "json_object"},
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *apiError `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return "", fmt.Errorf("%s: unreadable reply: %w", c.name(), err)
	}
	if out.Error != nil {
		return "", fmt.Errorf("%s: %s", c.name(), out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("%s: empty reply", c.name())
	}
	return out.Choices[0].Message.Content, nil
}

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
	resp, err := c.post(ctx, body)
	if err != nil {
		return Message{}, err
	}
	defer resp.Body.Close()

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

// ModelInfo is one model an endpoint offers. Prices are US dollars per million tokens as
// decimal strings ("" when the endpoint doesn't say, like Ollama).
type ModelInfo struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Context         int64  `json:"context"`
	PromptPrice     string `json:"prompt_price"`
	CompletionPrice string `json:"completion_price"`
	Images          bool   `json:"images"` // accepts image input
	Tools           bool   `json:"tools"`  // supports tool calling
}

// ListModels reads GET {base}/models (OpenRouter's catalog, or an OpenAI-compatible list).
func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", c.name(), resp.Status)
	}
	var out struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ContextLength int64  `json:"context_length"`
			Pricing       struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
			Architecture struct {
				InputModalities []string `json:"input_modalities"`
			} `json:"architecture"`
			SupportedParameters []string `json:"supported_parameters"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("%s: unreadable model list: %w", c.name(), err)
	}
	models := make([]ModelInfo, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID == "" {
			continue
		}
		name := m.Name
		if name == "" {
			name = m.ID
		}
		models = append(models, ModelInfo{
			ID: m.ID, Name: name, Context: m.ContextLength,
			PromptPrice: perMillion(m.Pricing.Prompt), CompletionPrice: perMillion(m.Pricing.Completion),
			Images: slices.Contains(m.Architecture.InputModalities, "image"),
			Tools:  slices.Contains(m.SupportedParameters, "tools"),
		})
	}
	return models, nil
}

// perMillion turns a per-token price ("0.000003") into dollars per million tokens ("3"),
// exactly. Negative prices (OpenRouter uses -1 for "varies") come back as "".
func perMillion(s string) string {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(s))
	if !ok || r.Sign() < 0 {
		return ""
	}
	v := strings.TrimRight(r.Mul(r, big.NewRat(1_000_000, 1)).FloatString(4), "0")
	return strings.TrimSuffix(v, ".")
}
