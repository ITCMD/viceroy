package server

import (
	"fmt"
	"testing"
	"time"
)

func TestAIUsage(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"1000"}`, true)

	// One question with a tool round = two requests, $0.0012 each (the fake's price).
	events := c.chat(`{"content":"Which accounts do I have?"}`)
	var cost map[string]any
	var thread any
	for _, e := range events {
		if e["type"] == "cost" {
			cost = e["cost"].(map[string]any)
		}
		if e["type"] == "thread" {
			thread = e["thread"].(map[string]any)["id"]
		}
	}
	if cost == nil || cost["requests"] != float64(2) || cost["cost_micros"] != float64(2400) || cost["unpriced"] != float64(0) {
		t.Fatalf("chat cost event = %v", cost)
	}
	_, th := c.do("GET", fmt.Sprint("/api/chat/threads/", thread), "", false)
	if th["cost"].(map[string]any)["cost_micros"] != float64(2400) {
		t.Fatalf("thread cost = %v", th["cost"])
	}

	c.do("POST", "/api/settings/ai/test", `{"target":"chat"}`, true)
	code, u := c.do("GET", "/api/settings/ai/usage", "", false)
	if code != 200 {
		t.Fatalf("usage = %d %v", code, u)
	}
	m := u["month"].(map[string]any)
	if m["month"] != time.Now().Format("2006-01") || m["cost_micros"] != float64(3600) || m["requests"] != float64(3) {
		t.Fatalf("month usage = %v", m)
	}
	features := map[string]float64{}
	for _, f := range m["features"].([]any) {
		features[f.(map[string]any)["feature"].(string)] = f.(map[string]any)["cost_micros"].(float64)
	}
	if features["chat"] != 2400 || features["test"] != 1200 {
		t.Fatalf("features = %v", features)
	}
	if p := u["previous"].(map[string]any); p["cost_micros"] != float64(0) {
		t.Fatalf("previous month = %v", p)
	}
	if code, _ := c.do("GET", "/api/settings/ai/usage?month=soon", "", false); code != 400 {
		t.Fatalf("bad month = %d", code)
	}
}
