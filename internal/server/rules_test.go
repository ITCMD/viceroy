package server

import (
	"fmt"
	"strconv"
	"testing"
)

// A rule made from an edit: merchant + account + direction + day range, several tags, a
// category; the dry run counts, hand-picked categories need override_user, and new
// transactions follow the rule.
func TestRuleFromEdit(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, out := c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"40"}`, true)
	acct := strconv.FormatInt(int64(out["id"].(float64)), 10)
	_, out = c.do("POST", "/api/accounts", `{"name":"Other","type":"cash","balance":"40"}`, true)
	other := strconv.FormatInt(int64(out["id"].(float64)), 10)
	_, cats := c.do("GET", "/api/categories", "", false)
	ids := map[string]float64{}
	for _, g := range cats["groups"].([]any) {
		for _, ct := range g.(map[string]any)["categories"].([]any) {
			m := ct.(map[string]any)
			ids[m["name"].(string)] = m["id"].(float64)
		}
	}
	add := func(account, date, amount, desc string, pending bool) string {
		code, out := c.do("POST", "/api/transactions", fmt.Sprintf(`{"account_id":%s,"date":%q,"amount":%q,"description":%q,"pending":%v,"force":true}`, account, date, amount, desc, pending), true)
		if code != 201 {
			t.Fatalf("add = %d %v", code, out)
		}
		return strconv.FormatInt(int64(out["id"].(float64)), 10)
	}
	src := add(acct, "2026-09-02", "-5.00", "SQ *BLUE BOTTLE 12", false)
	add(acct, "2026-08-03", "-6.00", "BLUE BOTTLE 99", true)       // pending entry: included
	hand := add(acct, "2026-07-04", "-7.00", "Blue Bottle", false) // hand-picked category
	add(acct, "2026-09-20", "-5.00", "BLUE BOTTLE 12", false)      // outside the day range
	add(other, "2026-09-02", "-5.00", "BLUE BOTTLE 12", false)     // other account
	add(acct, "2026-09-02", "5.00", "BLUE BOTTLE REFUND", false)   // money in
	c.do("PATCH", "/api/transactions/"+hand, fmt.Sprintf(`{"category_id":%v}`, ids["Restaurants & Bars"]), true)
	c.do("PATCH", "/api/transactions/"+src, fmt.Sprintf(`{"category_id":%v,"tags":["coffee","treat"],"merchant":"Blue Bottle Coffee"}`, ids["Coffee Shops"]), true)

	body := fmt.Sprintf(`{"match_field":"merchant","match_op":"contains","match_value":"Blue Bottle","account_id":%s,"direction":"out","day_min":1,"day_max":10,"set_category_id":%v,"set_merchant":"Blue Bottle Coffee","tags":["coffee","treat"]}`, acct, ids["Coffee Shops"])
	code, out := c.do("POST", "/api/rules", body, true)
	if code != 201 || len(out["tags"].([]any)) != 2 || out["direction"] != "out" || out["day_min"].(float64) != 1 {
		t.Fatalf("create = %d %v", code, out)
	}
	rule := strconv.FormatInt(int64(out["id"].(float64)), 10)
	// The source row already has everything; the pending one changes; the hand-picked one only
	// gets the merchant and tags (its category is counted as hand-picked).
	if code, out := c.do("POST", "/api/rules/"+rule+"/apply", `{"dry_run":true}`, true); code != 200 || out["matches"].(float64) != 2 || out["hand_picked"].(float64) != 1 || out["with_hand_picked"].(float64) != 2 {
		t.Fatalf("dry run = %d %v", code, out)
	}
	if code, out := c.do("POST", "/api/rules/"+rule+"/apply", `{"override_user":true}`, true); code != 200 || out["updated"].(float64) != 2 {
		t.Fatalf("apply = %d %v", code, out)
	}
	_, d := c.do("GET", "/api/transactions/"+hand, "", false)
	if tx := d["transaction"].(map[string]any); tx["category_name"] != "Coffee Shops" || tx["merchant"] != "Blue Bottle Coffee" || len(tx["tags"].([]any)) != 2 {
		t.Fatalf("hand-picked after override = %v", tx)
	}
	if code, out := c.do("POST", "/api/rules/"+rule+"/apply", `{"dry_run":true}`, true); out["matches"].(float64) != 0 {
		t.Fatalf("second dry run = %d %v", code, out)
	}

	// New transactions follow it, from any source going through the pipeline.
	id := add(acct, "2026-10-05", "-4.00", "BLUE BOTTLE 3", true)
	_, d = c.do("GET", "/api/transactions/"+id, "", false)
	if tx := d["transaction"].(map[string]any); tx["category_name"] != "Coffee Shops" || len(tx["tags"].([]any)) != 2 {
		t.Fatalf("new pending = %v", tx)
	}

	if code, _ := c.do("POST", "/api/rules", `{"match_field":"merchant","match_op":"contains","match_value":"x","day_min":40,"set_hidden":true}`, true); code != 400 {
		t.Fatalf("bad day = %d", code)
	}
	if code, _ := c.do("POST", "/api/rules", `{"match_field":"merchant","match_op":"contains","match_value":"","set_hidden":true}`, true); code != 400 {
		t.Fatalf("no condition = %d", code)
	}
}
