package server

import (
	"fmt"
	"testing"
)

func TestBackupsAPI(t *testing.T) {
	c := newTestServer(t)
	c.server.cfg.DataDir = t.TempDir()
	c.server.cfg.Backup.Dir = t.TempDir()
	c.do("POST", "/api/setup", `{"name":"Lucas","email":"lucas@example.com","password":"correct horse battery","household_name":"Home"}`, true)

	_, l := c.do("GET", "/api/settings/backups", "", false)
	if l["keep"] != float64(14) || len(l["backups"].([]any)) != 0 {
		t.Fatalf("backups = %v", l)
	}
	code, l := c.do("POST", "/api/settings/backups", "", true)
	if code != 200 || len(l["backups"].([]any)) != 1 {
		t.Fatalf("create = %d %v", code, l)
	}
	if b := l["backups"].([]any)[0].(map[string]any); b["size"].(float64) < 1000 {
		t.Errorf("backup = %v", b)
	}

	// Members can't see or make them.
	_, inv := c.do("POST", "/api/household/invites", `{"label":"Sam"}`, true)
	sam := c.newSession()
	sam.do("POST", fmt.Sprintf("/api/invites/%s/accept", inv["token"]), `{"name":"Sam","email":"sam@example.com","password":"another long password"}`, true)
	if code, _ := sam.do("GET", "/api/settings/backups", "", false); code != 403 {
		t.Errorf("member list = %d", code)
	}
	if code, _ := sam.do("POST", "/api/settings/backups", "", true); code != 403 {
		t.Errorf("member create = %d", code)
	}
}
