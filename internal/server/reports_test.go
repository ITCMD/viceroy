package server

import (
	"fmt"
	"strconv"
	"testing"
	"time"
)

func TestReportsAPI(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, out := c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"1000"}`, true)
	acct := strconv.FormatInt(int64(out["id"].(float64)), 10)
	_, b := c.do("GET", "/api/budget?date=2026-02-10", "", false)
	rest, _ := findLine(t, b, "Restaurants & Bars")
	pay, _ := findLine(t, b, "Paychecks")

	add := func(date, amount, desc string, cat any) {
		t.Helper()
		code, out := c.do("POST", "/api/transactions", `{"account_id":`+acct+`,"date":"`+date+`","amount":"`+amount+`","description":"`+desc+`"}`, true)
		if code != 201 && code != 200 {
			t.Fatalf("add txn = %d %v", code, out)
		}
		c.do("PATCH", fmt.Sprint("/api/transactions/", out["id"]), fmt.Sprintf(`{"category_id":%v}`, cat), true)
	}
	add("2026-01-05", "-40", "Bistro", rest["id"])
	add("2026-02-07", "-60", "Bistro", rest["id"])
	add("2026-02-01", "2000", "Acme Payroll", pay["id"])

	code, r := c.do("GET", "/api/reports?from=2026-01-01&to=2026-02-28&interval=month&by=category", "", false)
	if code != 200 {
		t.Fatalf("report = %d %v", code, r)
	}
	sp := r["spending"].(map[string]any)
	in := r["income"].(map[string]any)
	if sp["total"] != float64(10000) || fmt.Sprint(sp["values"]) != "[4000 6000]" || in["total"] != float64(200000) {
		t.Fatalf("report totals: spending %v income %v", sp, in)
	}
	line := sp["lines"].([]any)[0].(map[string]any)
	if line["name"] != "Restaurants & Bars" || line["total"] != float64(10000) {
		t.Fatalf("spending line = %v", line)
	}
	if code, r = c.do("GET", "/api/reports?from=all&interval=year&by=merchant", "", false); code != 200 || r["from"] != "2026-01-05" {
		t.Fatalf("all-time report = %d %v", code, r["from"])
	}
	if code, _ = c.do("GET", "/api/reports?from=2026-03-01&to=2026-02-01", "", false); code != 400 {
		t.Fatalf("reversed range = %d", code)
	}
	if code, r = c.do("GET", "/api/reports/spending-pace?month=2026-02", "", false); code != 200 || r["days_in_month"] != float64(28) {
		t.Fatalf("pace = %d %v", code, r)
	}
	if this := r["this"].([]any); len(this) != 28 || this[6] != float64(6000) || r["last"].([]any)[30] != float64(4000) {
		t.Fatalf("pace series = %v / %v", r["this"], r["last"])
	}

	// A monthly charge that is still active shows up as recurring and can be dismissed.
	now := time.Now()
	for _, back := range []int{62, 31, 1} {
		add(now.AddDate(0, 0, -back).Format(time.DateOnly), "-15.99", "Streamflix", rest["id"])
	}
	_, r = c.do("GET", "/api/recurring", "", false)
	var key string
	for _, s := range r["suggestions"].([]any) {
		if sm := s.(map[string]any); sm["name"] == "Streamflix" {
			key = sm["key"].(string)
			if sm["cadence"] != "monthly" || sm["amount"] != float64(-1599) || sm["strong"] != true {
				t.Fatalf("series = %v", sm)
			}
		}
	}
	if key == "" {
		t.Fatalf("Streamflix not detected: %v", r)
	}
	if code, _ := c.do("PUT", "/api/recurring/dismissed", `{"key":"`+key+`","dismissed":true}`, true); code != 204 {
		t.Fatalf("dismiss = %d", code)
	}
	_, r = c.do("GET", "/api/recurring", "", false)
	if s := r["dismissed"].([]any); len(s) != 1 || s[0].(map[string]any)["dismissed"] != true || len(r["suggestions"].([]any)) != 0 {
		t.Fatalf("after dismiss = %v", r)
	}

	_, nw := c.do("GET", "/api/networth/history?days=7", "", false)
	pts := nw["points"].([]any)
	if g := pts[len(pts)-1].(map[string]any)["groups"].(map[string]any); g["cash"] == nil {
		t.Fatalf("net worth groups = %v", g)
	}
}
