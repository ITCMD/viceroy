package server

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"testing"
)

// newSession is another browser against the same server.
func (c *client) newSession() *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: c.t, base: c.base, http: &http.Client{Jar: jar}, server: c.server}
}

func TestHouseholdInvites(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"Lucas","email":"lucas@example.com","password":"correct horse battery","household_name":"Home"}`, true)

	_, h := c.do("GET", "/api/household", "", false)
	if h["name"] != "Home" || h["can_manage"] != true || len(h["members"].([]any)) != 1 {
		t.Fatalf("household = %v", h)
	}
	if code, h := c.do("PATCH", "/api/household", `{"name":"The Elliotts"}`, true); code != 200 || h["name"] != "The Elliotts" {
		t.Fatalf("rename = %d %v", code, h)
	}

	// Join link: anyone with it can see what it's for and join once.
	code, inv := c.do("POST", "/api/household/invites", `{"label":"Sam"}`, true)
	token, _ := inv["token"].(string)
	if code != 201 || token == "" || inv["kind"] != "join" {
		t.Fatalf("invite = %d %v", code, inv)
	}
	_, h = c.do("GET", "/api/household", "", false)
	if open := h["invites"].([]any); len(open) != 1 || open[0].(map[string]any)["label"] != "Sam" {
		t.Fatalf("open invites = %v", h["invites"])
	}
	sam := c.newSession()
	if code, info := sam.do("GET", "/api/invites/"+token, "", false); code != 200 || info["household_name"] != "The Elliotts" || info["invited_by"] != "Lucas" {
		t.Fatalf("invite info = %d %v", code, info)
	}
	if code, _ := sam.do("POST", "/api/invites/"+token+"/accept", `{"name":"Sam","email":"lucas@example.com","password":"another long password"}`, true); code != 409 {
		t.Fatalf("taken email = %d", code)
	}
	if code, _ := sam.do("POST", "/api/invites/"+token+"/accept", `{"name":"Sam","email":"sam@example.com","password":"short"}`, true); code != 400 {
		t.Fatalf("short password = %d", code)
	}
	if code, body := sam.do("POST", "/api/invites/"+token+"/accept", `{"name":"Sam","email":"sam@example.com","password":"another long password"}`, true); code != 200 {
		t.Fatalf("accept = %d %v", code, body)
	}
	if code, _ := c.newSession().do("GET", "/api/invites/"+token, "", false); code != 404 {
		t.Fatalf("used link = %d", code)
	}

	// Sam shares the household's data but can't manage it.
	_, sess := sam.do("GET", "/api/session", "", false)
	if sess["household"].(map[string]any)["name"] != "The Elliotts" || sess["user"].(map[string]any)["is_admin"] != false {
		t.Fatalf("sam session = %v", sess)
	}
	_, mine := c.do("GET", "/api/accounts", "", false)
	_, theirs := sam.do("GET", "/api/accounts", "", false)
	if len(mine["accounts"].([]any)) != len(theirs["accounts"].([]any)) {
		t.Fatalf("accounts differ: %v vs %v", mine, theirs)
	}
	_, h = sam.do("GET", "/api/household", "", false)
	if h["can_manage"] != false || len(h["members"].([]any)) != 2 || len(h["invites"].([]any)) != 0 {
		t.Fatalf("sam household = %v", h)
	}
	if code, _ := sam.do("POST", "/api/household/invites", `{}`, true); code != 403 {
		t.Fatalf("member invite = %d", code)
	}
	var samID, lucasID int64
	for _, m := range h["members"].([]any) {
		m := m.(map[string]any)
		if m["name"] == "Sam" {
			samID = int64(m["id"].(float64))
		} else {
			lucasID = int64(m["id"].(float64))
		}
	}

	// Owners: accounts and transactions, with a filter.
	acct := int64(mine["accounts"].([]any)[0].(map[string]any)["id"].(float64))
	if code, a := c.do("PATCH", fmt.Sprintf("/api/accounts/%d", acct), fmt.Sprintf(`{"owner_id":%d}`, samID), true); code != 204 {
		t.Fatalf("set owner = %d %v", code, a)
	}
	if code, _ := c.do("PATCH", fmt.Sprintf("/api/accounts/%d", acct), `{"owner_id":999}`, true); code != 400 {
		t.Fatalf("stranger owner = %d", code)
	}
	_, tx := c.do("POST", "/api/transactions", fmt.Sprintf(`{"account_id":%d,"date":"2026-09-01","amount":"-5.00","description":"Coffee"}`, acct), true)
	txID := int64(tx["id"].(float64))
	// Manual entries belong to whoever added them.
	if tx["owner_id"].(float64) != float64(lucasID) || tx["owner_set"] != true {
		t.Fatalf("new txn owner = %v", tx)
	}
	c.do("PATCH", fmt.Sprintf("/api/transactions/%d", txID), `{"owner_id":null}`, true)
	_, list := c.do("GET", fmt.Sprintf("/api/transactions?owner=%d", samID), "", false)
	if txns := list["transactions"].([]any); len(txns) != 1 || txns[0].(map[string]any)["owner_set"] != false {
		t.Fatalf("sam's transactions = %v", list)
	}
	_, list = c.do("GET", fmt.Sprintf("/api/transactions?owner=%d", lucasID), "", false)
	if len(list["transactions"].([]any)) != 0 {
		t.Fatalf("lucas's transactions = %v", list)
	}

	// Admin rules: the last admin can't step down; promote, then demote works.
	if code, _ := c.do("PATCH", fmt.Sprintf("/api/household/members/%d", lucasID), `{"is_admin":false}`, true); code != 409 {
		t.Fatalf("last admin demote = %d", code)
	}
	if code, _ := c.do("PATCH", fmt.Sprintf("/api/household/members/%d", samID), `{"is_admin":true}`, true); code != 200 {
		t.Fatalf("promote = %d", code)
	}
	if code, _ := sam.do("PATCH", fmt.Sprintf("/api/household/members/%d", lucasID), `{"is_admin":false}`, true); code != 200 {
		t.Fatalf("demote lucas = %d", code)
	}
	c.do("PATCH", "/api/household", `{"name":"x"}`, true)
	if code, _ := c.do("PATCH", "/api/household", `{"name":"x"}`, true); code != 403 {
		t.Fatalf("demoted rename = %d", code)
	}

	// Password reset link for Lucas: old sessions end, the new password works.
	_, reset := sam.do("POST", "/api/household/invites", fmt.Sprintf(`{"user_id":%d}`, lucasID), true)
	rtoken := reset["token"].(string)
	anon := c.newSession()
	if _, info := anon.do("GET", "/api/invites/"+rtoken, "", false); info["kind"] != "reset" || info["email"] != "lucas@example.com" {
		t.Fatalf("reset info = %v", info)
	}
	if code, _ := anon.do("POST", "/api/invites/"+rtoken+"/accept", `{"password":"brand new password"}`, true); code != 200 {
		t.Fatalf("reset = %d", code)
	}
	if code, _ := c.do("GET", "/api/household", "", false); code != 401 {
		t.Fatalf("old session after reset = %d", code)
	}
	if code, _ := c.do("POST", "/api/auth/login", `{"email":"lucas@example.com","password":"brand new password"}`, true); code != 200 {
		t.Fatalf("login with new password = %d", code)
	}

	// Revoke an unused link; remove a member (their transactions stay, unowned).
	_, inv = sam.do("POST", "/api/household/invites", `{}`, true)
	if code, _ := sam.do("DELETE", fmt.Sprintf("/api/household/invites/%d", int64(inv["id"].(float64))), "", true); code != 204 {
		t.Fatalf("revoke = %d", code)
	}
	if code, _ := anon.do("GET", "/api/invites/"+inv["token"].(string), "", false); code != 404 {
		t.Fatalf("revoked link = %d", code)
	}
	if code, _ := sam.do("DELETE", fmt.Sprintf("/api/household/members/%d", samID), "", true); code != 400 {
		t.Fatalf("remove self = %d", code)
	}
	if code, _ := sam.do("DELETE", fmt.Sprintf("/api/household/members/%d", lucasID), "", true); code != 200 {
		t.Fatalf("remove lucas = %d", code)
	}
	if code, _ := c.do("POST", "/api/auth/login", `{"email":"lucas@example.com","password":"brand new password"}`, true); code != 401 {
		t.Fatalf("removed member login = %d", code)
	}
	_, list = sam.do("GET", "/api/transactions?q=Coffee", "", false)
	if len(list["transactions"].([]any)) != 1 {
		t.Fatalf("transactions after removal = %v", list)
	}
}

func TestChangePassword(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"Lucas","email":"lucas@example.com","password":"correct horse battery"}`, true)
	other := c.newSession()
	other.do("POST", "/api/auth/login", `{"email":"lucas@example.com","password":"correct horse battery"}`, true)

	if code, body := c.do("POST", "/api/me/password", `{"current_password":"wrong password!","new_password":"a whole new password"}`, true); code != 400 || body["error"] != "Your current password isn't right." {
		t.Fatalf("wrong current = %d %v", code, body)
	}
	if code, _ := c.do("POST", "/api/me/password", `{"current_password":"correct horse battery","new_password":"short"}`, true); code != 400 {
		t.Fatalf("short = %d", code)
	}
	if code, _ := c.do("POST", "/api/me/password", `{"current_password":"correct horse battery","new_password":"a whole new password"}`, true); code != 204 {
		t.Fatalf("change = %d", code)
	}
	// This session stays, the other one is signed out, and the new password works.
	if code, _ := c.do("GET", "/api/household", "", false); code != 200 {
		t.Fatalf("own session after change = %d", code)
	}
	if code, _ := other.do("GET", "/api/household", "", false); code != 401 {
		t.Fatalf("other session after change = %d", code)
	}
	if code, _ := c.newSession().do("POST", "/api/auth/login", `{"email":"lucas@example.com","password":"a whole new password"}`, true); code != 200 {
		t.Fatalf("login with new password = %d", code)
	}

	if code, body := c.do("PATCH", "/api/me", `{"name":"Lucas E"}`, true); code != 200 || body["user"].(map[string]any)["name"] != "Lucas E" {
		t.Fatalf("rename = %d %v", code, body)
	}
	if _, s := c.do("GET", "/api/session", "", false); s["user"].(map[string]any)["name"] != "Lucas E" {
		t.Fatalf("session after rename = %v", s)
	}
}

func TestChangeEmail(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"Lucas","email":"lucas@example.com","password":"correct horse battery"}`, true)
	_, inv := c.do("POST", "/api/household/invites", `{}`, true)
	if code, _ := c.newSession().do("POST", "/api/invites/"+inv["token"].(string)+"/accept", `{"name":"Sam","email":"sam@example.com","password":"sam's password!"}`, true); code != 200 {
		t.Fatalf("join = %d", code)
	}

	cases := []struct {
		body string
		code int
	}{
		{`{"current_password":"wrong password!","email":"new@example.com"}`, 400},
		{`{"current_password":"correct horse battery","email":"not an email"}`, 400},
		{`{"current_password":"correct horse battery","email":"SAM@example.com"}`, 409},
		{`{"current_password":"correct horse battery","email":"Lucas@Example.com"}`, 200}, // own email, new case
		{`{"current_password":"correct horse battery","email":" new@example.com "}`, 200},
	}
	for _, tc := range cases {
		if code, body := c.do("POST", "/api/me/email", tc.body, true); code != tc.code {
			t.Fatalf("%s = %d %v", tc.body, code, body)
		}
	}
	// Still signed in; the new email signs in and the old one doesn't.
	if _, s := c.do("GET", "/api/session", "", false); s["user"].(map[string]any)["email"] != "new@example.com" {
		t.Fatalf("session after change = %v", s)
	}
	if code, _ := c.newSession().do("POST", "/api/auth/login", `{"email":"new@example.com","password":"correct horse battery"}`, true); code != 200 {
		t.Fatalf("login with new email = %d", code)
	}
	if code, _ := c.newSession().do("POST", "/api/auth/login", `{"email":"lucas@example.com","password":"correct horse battery"}`, true); code != 401 {
		t.Fatalf("login with old email = %d", code)
	}
}
