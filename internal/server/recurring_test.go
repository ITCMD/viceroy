package server

import (
	"fmt"
	"strconv"
	"testing"
	"time"
)

func TestRecurringItemsAPI(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, out := c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"1000"}`, true)
	acct := strconv.FormatInt(int64(out["id"].(float64)), 10)
	_, b := c.do("GET", "/api/budget", "", false)
	rest, _ := findLine(t, b, "Restaurants & Bars")
	today := time.Now()
	var ids []any
	for _, back := range []int{61, 31} {
		_, o := c.do("POST", "/api/transactions", `{"account_id":`+acct+`,"date":"`+today.AddDate(0, 0, -back).Format(time.DateOnly)+`","amount":"-20","description":"CLOUD BOX 123"}`, true)
		ids = append(ids, o["id"])
		c.do("PATCH", fmt.Sprint("/api/transactions/", o["id"]), fmt.Sprintf(`{"category_id":%v}`, rest["id"]), true)
	}

	// Two similar charges a month apart: a weak suggestion, not counted as upcoming.
	_, r := c.do("GET", "/api/recurring", "", false)
	sug := r["suggestions"].([]any)
	if len(sug) != 1 || sug[0].(map[string]any)["strong"] != false || len(r["upcoming"].([]any)) != 0 {
		t.Fatalf("suggestions = %v", r)
	}
	key := sug[0].(map[string]any)["key"].(string)

	// Confirm it, due today; it's then tracked, upcoming and on the budget line.
	body := fmt.Sprintf(`{"series_key":%q,"transaction_id":%v,"anchor_date":%q,"cadence":"monthly"}`, key, ids[1], today.Format(time.DateOnly))
	code, item := c.do("POST", "/api/recurring/items", body, true)
	if code != 201 || item["name"] == "" || item["amount_cents"] != float64(-2000) {
		t.Fatalf("create = %d %v", code, item)
	}
	id := fmt.Sprint(item["id"])
	_, r = c.do("GET", "/api/recurring", "", false)
	tr := r["tracked"].([]any)
	if len(tr) != 1 || len(r["suggestions"].([]any)) != 0 || tr[0].(map[string]any)["count"] != float64(2) || tr[0].(map[string]any)["next_date"] != today.Format(time.DateOnly) {
		t.Fatalf("after confirm = %v", r)
	}
	_, b = c.do("GET", "/api/budget", "", false)
	if l, _ := findLine(t, b, "Restaurants & Bars"); l["upcoming"] != float64(2000) {
		t.Fatalf("budget upcoming = %v", l)
	}
	_, d := c.do("GET", fmt.Sprint("/api/transactions/", ids[0]), "", false)
	if rec, _ := d["recurring"].(map[string]any); rec == nil || fmt.Sprint(rec["id"]) != id {
		t.Fatalf("txn recurring = %v", d["recurring"])
	}

	// Edit: bad input is refused; a new amount out of range no longer matches the payments.
	if code, _ := c.do("PATCH", "/api/recurring/items/"+id, `{"cadence":"hourly"}`, true); code != 400 {
		t.Fatalf("bad cadence = %d", code)
	}
	if code, _ := c.do("PATCH", "/api/recurring/items/"+id, `{"name":"Cloud storage","amount":-5000}`, true); code != 200 {
		t.Fatalf("edit = %d", code)
	}
	_, r = c.do("GET", "/api/recurring", "", false)
	if tr := r["tracked"].([]any)[0].(map[string]any); tr["name"] != "Cloud storage" || tr["count"] != float64(0) {
		t.Fatalf("after edit = %v", tr)
	}
	if code, _ := c.do("DELETE", "/api/recurring/items/"+id, "", true); code != 204 {
		t.Fatalf("delete = %d", code)
	}
	if code, _ := c.do("POST", "/api/recurring/items", `{"name":"x","amount":-100,"cadence":"monthly","anchor_date":"2026-10-01"}`, true); code != 400 {
		t.Fatalf("no match text = %d", code)
	}

	// Budget setting for the window.
	if code, _ := c.do("PATCH", "/api/settings", `{"budget":{"upcoming_window":"paycheck"}}`, true); code != 200 {
		t.Fatalf("upcoming window = %d", code)
	}
	if code, _ := c.do("PATCH", "/api/settings", `{"budget":{"upcoming_window":"year"}}`, true); code != 400 {
		t.Fatalf("bad window = %d", code)
	}
}

func TestNetworthAnnotations(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, out := c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"1000"}`, true)
	_, txn := c.do("POST", "/api/transactions", fmt.Sprintf(`{"account_id":%v,"date":"2026-09-01","amount":"-900","description":"Couch"}`, out["id"]), true)
	code, a := c.do("POST", "/api/networth/annotations", fmt.Sprintf(`{"date":"2026-09-01","label":"New couch","icon":"🛋️","transaction_id":%v}`, txn["id"]), true)
	if code != 201 {
		t.Fatalf("create = %d %v", code, a)
	}
	id := fmt.Sprint(a["id"])
	if code, _ := c.do("POST", "/api/networth/annotations", `{"date":"2026-09-01","label":"x","icon":"too long for an icon"}`, true); code != 400 {
		t.Fatalf("long icon = %d", code)
	}
	if code, _ := c.do("PATCH", "/api/networth/annotations/"+id, `{"date":"2026-09-02","label":"Couch","icon":""}`, true); code != 204 {
		t.Fatalf("edit = %d", code)
	}
	_, l := c.do("GET", "/api/networth/annotations", "", false)
	list := l["annotations"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["label"] != "Couch" || list[0].(map[string]any)["transaction_id"] != float64(0) {
		t.Fatalf("list = %v", l)
	}
	if code, _ := c.do("DELETE", "/api/networth/annotations/"+id, "", true); code != 204 {
		t.Fatalf("delete = %d", code)
	}
}
