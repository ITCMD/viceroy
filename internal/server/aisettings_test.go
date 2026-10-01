package server

import "testing"

func TestAISettingsAPI(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)

	_, s := c.do("GET", "/api/settings/ai", "", false)
	if s["can_edit"] != true || s["key_source"] != "config" || s["chat_model"] != "test/model" || s["chat_ready"] != true {
		t.Fatalf("initial = %v", s)
	}
	// Without a key anywhere, nothing is ready.
	c.server.ai.Config.OpenRouterKey = ""
	if _, s = c.do("GET", "/api/settings/ai", "", false); s["key_set"] != false || s["chat_ready"] != false {
		t.Fatalf("no key = %v", s)
	}
	_, chat := c.do("GET", "/api/chat", "", false)
	if chat["configured"] != false {
		t.Fatalf("chat without key = %v", chat)
	}

	code, s := c.do("PATCH", "/api/settings/ai", `{"openrouter_key":" test-key-abcd ","email_model":"cheap/model"}`, true)
	if code != 200 || s["key_source"] != "settings" || s["key_hint"] != "abcd" || s["email_model"] != "cheap/model" || s["openrouter_key"] != nil {
		t.Fatalf("save = %d %v", code, s)
	}
	// The fake only accepts "test-key"; the saved key is wrong, so the test reports it.
	_, res := c.do("POST", "/api/settings/ai/test", `{"target":"chat"}`, true)
	if res["ok"] != false || res["error"] == nil {
		t.Fatalf("test with wrong key = %v", res)
	}
	c.do("PATCH", "/api/settings/ai", `{"openrouter_key":"test-key"}`, true)
	if _, res = c.do("POST", "/api/settings/ai/test", `{"target":"email"}`, true); res["ok"] != true || res["model"] != "cheap/model" {
		t.Fatalf("test email = %v", res)
	}
	// Chat picks the new key up without a restart.
	if _, chat = c.do("GET", "/api/chat", "", false); chat["configured"] != true {
		t.Fatalf("chat after saving key = %v", chat)
	}
	_, info := c.do("GET", "/api/email/ai", "", false)
	if info["model"] != "cheap/model" || info["local"] != false {
		t.Fatalf("email ai = %v", info)
	}

	if code, _ := c.do("PATCH", "/api/settings/ai", `{"email_base_url":"localhost:11434"}`, true); code != 400 {
		t.Fatalf("bad url = %d", code)
	}
	c.do("PATCH", "/api/settings/ai", `{"email_base_url":"http://127.0.0.1:11434/v1/"}`, true)
	if _, info = c.do("GET", "/api/email/ai", "", false); info["local"] != true {
		t.Fatalf("local email ai = %v", info)
	}
	// Removing the saved key leaves no key (the config one was cleared above).
	if _, s = c.do("PATCH", "/api/settings/ai", `{"openrouter_key":""}`, true); s["key_set"] != false || s["email_base_url"] != "http://127.0.0.1:11434/v1" {
		t.Fatalf("remove key = %v", s)
	}
}
