package server

import (
	"fmt"
	"net"
	"testing"

	"viceroy/internal/email/fakeimap"
)

func TestMailboxAISettings(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, info := c.do("GET", "/api/email/ai", "", false)
	if info["configured"] != true || info["model"] != "test/model" || info["local"] != false {
		t.Fatalf("ai info = %v", info)
	}

	srv := startFakeIMAP(t)
	host, port, _ := net.SplitHostPort(srv.Addr)
	mbox := `{"host":"` + host + `","port":` + port + `,"security":"none","username":"me@example.com","password":"app-pass"%s}`
	if code, out := c.do("POST", "/api/email/mailboxes", fmt.Sprintf(mbox, `,"ai_read":true,"ai_senders":"bad sender"`), true); code != 400 {
		t.Fatalf("bad sender list = %d %v", code, out)
	}
	code, mb := c.do("POST", "/api/email/mailboxes", fmt.Sprintf(mbox, `,"ai_read":true,"ai_senders":" Chase.com \n\nalerts@mybank.com"`), true)
	if code != 201 || mb["ai_read"] != true || mb["ai_senders"] != "chase.com\nalerts@mybank.com" {
		t.Fatalf("create = %d %v", code, mb)
	}
	// Turning it off keeps the list.
	code, mb = c.do("PATCH", fmt.Sprint("/api/email/mailboxes/", mb["id"]), fmt.Sprintf(mbox, `,"ai_read":false`), true)
	if code != 200 || mb["ai_read"] != false || mb["ai_senders"] != "chase.com\nalerts@mybank.com" {
		t.Fatalf("turn off = %d %v", code, mb)
	}

	// Without a key, turning it on is refused with a hint.
	c.server.ai.Config.OpenRouterKey = ""
	code, out := c.do("PATCH", fmt.Sprint("/api/email/mailboxes/", mb["id"]), fmt.Sprintf(mbox, `,"ai_read":true`), true)
	if code != 400 || out["error"] == nil {
		t.Fatalf("no AI = %d %v", code, out)
	}
}

func startFakeIMAP(t *testing.T) *fakeimap.Server {
	t.Helper()
	srv, err := fakeimap.Start("127.0.0.1:0", "me@example.com", "app-pass")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	return srv
}
