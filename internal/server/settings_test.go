package server

import "testing"

func TestLogoSetting(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	if _, s := c.do("GET", "/api/settings", "", false); s["logo"] != "butterfly" {
		t.Fatalf("default logo = %v", s["logo"])
	}
	if code, s := c.do("PATCH", "/api/settings", `{"logo":"classic"}`, true); code != 200 || s["logo"] != "classic" {
		t.Fatalf("set classic = %d %v", code, s)
	}
	if code, _ := c.do("PATCH", "/api/settings", `{"logo":"dragon"}`, true); code != 400 {
		t.Fatalf("unknown logo = %d", code)
	}
	if _, s := c.do("GET", "/api/settings", "", false); s["logo"] != "classic" {
		t.Fatalf("logo after bad update = %v", s["logo"])
	}
}
