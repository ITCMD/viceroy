package ai_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"viceroy/internal/ai"
	"viceroy/internal/ai/fakeai"
)

func TestRunWithTool(t *testing.T) {
	fake := &fakeai.Server{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := ai.New(srv.URL, "test-key", "test/model")

	var gotArgs string
	tools := []ai.Tool{{
		Name: "budget_status", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`),
		Run: func(_ context.Context, args json.RawMessage) (any, error) {
			gotArgs = string(args)
			return map[string]any{"left_to_budget": "120.00", "categories": []int{1, 2}}, nil
		},
	}}
	var text strings.Builder
	var events []string
	var saved []ai.Message
	err := c.Run(context.Background(), []ai.Message{{Role: "system", Content: "s"}, {Role: "user", Content: "How is my budget?"}}, tools,
		func(e ai.Event) {
			events = append(events, e.Type)
			text.WriteString(e.Text)
		},
		func(m ai.Message) error { saved = append(saved, m); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if gotArgs != "{}" {
		t.Errorf("tool args = %q", gotArgs)
	}
	if len(saved) != 3 || saved[0].ToolCalls[0].Function.Name != "budget_status" || saved[1].Role != "tool" || saved[1].ToolCallID != "call_1" || saved[2].Role != "assistant" {
		t.Fatalf("saved = %+v", saved)
	}
	want := "I checked **budget_status**. Here is what it returned:\n\n- categories: 2 items\n- left_to_budget: 120.00"
	if text.String() != want || saved[2].Content != want {
		t.Fatalf("text = %q", text.String())
	}
	if events[0] != "tool" || events[len(events)-1] != "text" || len(events) < 4 {
		t.Errorf("events = %v (want tool then several text chunks)", events)
	}
	// The second request carried the tool call and its result back.
	if n := len(fake.Requests); n != 2 {
		t.Fatalf("requests = %d", n)
	}
	msgs := fake.Requests[1].Messages
	if msgs[len(msgs)-1].Role != "tool" || !strings.Contains(msgs[len(msgs)-1].Content, "120.00") {
		t.Errorf("second request messages = %+v", msgs)
	}
}

func TestErrors(t *testing.T) {
	srv := httptest.NewServer(&fakeai.Server{})
	defer srv.Close()
	_, err := ai.New(srv.URL, "wrong", "m").Stream(context.Background(), []ai.Message{{Role: "user", Content: "hi"}}, nil, nil)
	if err == nil || err.Error() != "OpenRouter: No auth credentials found" {
		t.Errorf("bad key error = %v", err)
	}
	if _, err := ai.New(srv.URL, "", "m").Stream(context.Background(), nil, nil, nil); err != ai.ErrNotConfigured {
		t.Errorf("no key error = %v", err)
	}
}

func TestMicros(t *testing.T) {
	for in, want := range map[string]int64{"0.0012": 1200, "0.00000049": 0, "0.0000005": 1, "1.5": 1500000, "0": 0} {
		if got, ok := ai.Micros(in); !ok || got != want {
			t.Errorf("Micros(%q) = %d %v, want %d", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "-1", "abc"} {
		if _, ok := ai.Micros(in); ok {
			t.Errorf("Micros(%q) accepted", in)
		}
	}
}
