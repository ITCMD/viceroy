package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"testing"

	"viceroy/internal/aisettings"
	"viceroy/internal/config"
	"viceroy/internal/secrets"
	"viceroy/internal/ai/fakeai"
	"viceroy/internal/db"
	"viceroy/internal/notify"
)

func testNotifier(conn *sql.DB) *notify.Service {
	n := notify.New(conn, slog.New(slog.DiscardHandler), notify.Keys{Public: "BPUBLIC", Private: "x"}, "")
	n.Push = func(context.Context, db.PushSubscription, string, []byte) (int, error) { return 201, nil }
	return n
}

// testAI points AI settings at the fake OpenRouter, with the key coming from "viceroy.toml".
func testAI(t *testing.T, conn *sql.DB, box *secrets.Box) *aisettings.Store {
	srv := httptest.NewServer(&fakeai.Server{})
	t.Cleanup(srv.Close)
	return &aisettings.Store{DB: conn, Box: box, Config: config.AIConfig{BaseURL: srv.URL, OpenRouterKey: "test-key", ChatModel: "test/model"}}
}

func TestNotificationsAPI(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)

	code, s := c.do("GET", "/api/notifications/settings", "", false)
	prefs := s["prefs"].(map[string]any)
	if code != 200 || s["public_key"] != "BPUBLIC" || prefs["large_txn_cents"] != float64(50000) || prefs["pacing_pct"] != float64(25) {
		t.Fatalf("settings = %d %v", code, s)
	}
	if code, out := c.do("PUT", "/api/notifications/settings", `{"over_budget":true,"pacing":true,"pacing_pct":2,"large_txn":true,"large_txn_cents":100,"disconnected":true}`, true); code != 400 {
		t.Fatalf("bad pacing = %d %v", code, out)
	}
	code, s = c.do("PUT", "/api/notifications/settings", `{"over_budget":false,"pacing":true,"pacing_pct":40,"large_txn":true,"large_txn_cents":25000,"disconnected":true}`, true)
	if code != 200 || s["prefs"].(map[string]any)["large_txn_cents"] != float64(25000) {
		t.Fatalf("save prefs = %d %v", code, s)
	}

	if code, _ := c.do("POST", "/api/notifications/subscriptions", `{"endpoint":"http://insecure","keys":{"p256dh":"a","auth":"b"}}`, true); code != 400 {
		t.Fatalf("http endpoint accepted: %d", code)
	}
	sub := `{"endpoint":"https://push.example/abc","expirationTime":null,"keys":{"p256dh":"BK","auth":"AU"}}`
	code, d := c.do("POST", "/api/notifications/subscriptions", sub, true)
	if code != 200 {
		t.Fatalf("subscribe = %d %v", code, d)
	}
	c.do("POST", "/api/notifications/subscriptions", sub, true) // same device again: no duplicate
	_, s = c.do("GET", "/api/notifications/settings", "", false)
	if n := len(s["devices"].([]any)); n != 1 {
		t.Fatalf("devices = %d", n)
	}

	code, out := c.do("POST", "/api/notifications/test", "", true)
	if code != 200 || out["sent"] != float64(1) {
		t.Fatalf("test = %d %v", code, out)
	}
	_, list := c.do("GET", "/api/notifications", "", false)
	items := list["notifications"].([]any)
	if list["unread"] != float64(1) || len(items) != 1 || items[0].(map[string]any)["title"] != "Test notification" {
		t.Fatalf("list = %v", list)
	}
	c.do("POST", "/api/notifications/read", "", true)
	if _, list = c.do("GET", "/api/notifications", "", false); list["unread"] != float64(0) {
		t.Fatalf("unread after read = %v", list["unread"])
	}

	if code, _ := c.do("DELETE", fmt.Sprint("/api/notifications/subscriptions/", d["id"]), "", true); code != 204 {
		t.Fatalf("unsubscribe = %d", code)
	}
	if _, s = c.do("GET", "/api/notifications/settings", "", false); len(s["devices"].([]any)) != 0 {
		t.Fatalf("devices after delete = %v", s["devices"])
	}
}
