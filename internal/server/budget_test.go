package server

import (
	"fmt"
	"strconv"
	"testing"
)

// findLine returns the budget line named name, and its group kind.
func findLine(t *testing.T, b map[string]any, name string) (map[string]any, string) {
	t.Helper()
	for _, g := range b["groups"].([]any) {
		gm := g.(map[string]any)
		for _, l := range gm["lines"].([]any) {
			if lm := l.(map[string]any); lm["name"] == name {
				return lm, gm["kind"].(string)
			}
		}
	}
	t.Fatalf("no budget line %q", name)
	return nil, ""
}

func TestBudgetAPI(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, out := c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"1000"}`, true)
	acct := strconv.FormatInt(int64(out["id"].(float64)), 10)

	code, b := c.do("GET", "/api/budget?date=2026-02-10", "", false)
	if code != 200 || b["start"] != "2026-02-01" || b["end"] != "2026-02-28" || b["prev"] != "2026-01-31" || b["next"] != "2026-03-01" {
		t.Fatalf("month view = %d %v", code, b)
	}
	rest, kind := findLine(t, b, "Restaurants & Bars")
	rent, _ := findLine(t, b, "Rent")
	if kind != "flexible" {
		t.Fatalf("kind %s", kind)
	}
	restID, rentID := fmt.Sprint(rest["id"]), fmt.Sprint(rent["id"])

	// $400/month for restaurants from January on; rent $1500 in week 1 from February only.
	if code, _ := c.do("PUT", "/api/budget/amount", `{"category_id":`+restID+`,"month":"2026-01","amount":"400","apply_forward":true}`, true); code != 204 {
		t.Fatalf("set amount = %d", code)
	}
	c.do("PUT", "/api/budget/amount", `{"category_id":`+rentID+`,"month":"2026-02","amount":"1500"}`, true)
	if code, out := c.do("PUT", "/api/budget/categories/"+rentID+"/chunk", `{"kind":"week","week":1}`, true); code != 200 || out["week"] != float64(1) {
		t.Fatalf("chunk = %d %v", code, out)
	}
	if code, _ := c.do("PUT", "/api/budget/categories/"+rentID+"/chunk", `{"kind":"week","week":9}`, true); code != 400 {
		t.Fatalf("bad chunk = %d", code)
	}
	if code, _ := c.do("PUT", "/api/budget/amount", `{"category_id":`+restID+`,"month":"2026-02","amount":"-5"}`, true); code != 400 {
		t.Fatalf("negative amount = %d", code)
	}

	// $100 at a restaurant on Feb 3.
	_, out = c.do("POST", "/api/transactions", `{"account_id":`+acct+`,"date":"2026-02-03","amount":"-100","description":"Bistro"}`, true)
	txn := fmt.Sprint(out["id"])
	c.do("PATCH", "/api/transactions/"+txn, `{"category_id":`+restID+`}`, true)

	_, b = c.do("GET", "/api/budget?date=2026-02-10", "", false)
	rest, _ = findLine(t, b, "Restaurants & Bars")
	rent, _ = findLine(t, b, "Rent")
	if rest["budget"] != float64(40000) || rest["actual"] != float64(10000) || rent["budget"] != float64(150000) {
		t.Fatalf("month lines: %v / %v", rest, rent)
	}

	// Weekly view (weeks start Sunday): Feb 8-14 gets $300 × 7/21 = $100; rent is all in week 1.
	c.do("PATCH", "/api/settings", `{"budget":{"week_start":0}}`, true)
	_, b = c.do("GET", "/api/budget?view=week&date=2026-02-10", "", false)
	rest, _ = findLine(t, b, "Restaurants & Bars")
	rent, _ = findLine(t, b, "Rent")
	if b["start"] != "2026-02-08" || rest["budget"] != float64(10000) || rent["budget"] != float64(0) || rest["month_budget"] != float64(40000) {
		t.Fatalf("week view: %v %v %v", b["start"], rest, rent)
	}
	_, b = c.do("GET", "/api/budget?view=week&date=2026-02-02", "", false)
	rent, _ = findLine(t, b, "Rent")
	if rent["budget"] != float64(150000) {
		t.Fatalf("rent week 1: %v", rent)
	}

	// Paycheck view with a bad schedule is rejected; a good one is used.
	if code, _ := c.do("PATCH", "/api/settings", `{"budget":{"pay_schedule":{"kind":"biweekly"}}}`, true); code != 400 {
		t.Fatalf("bad pay schedule = %d", code)
	}
	code, s := c.do("PATCH", "/api/settings", `{"budget":{"pay_schedule":{"kind":"biweekly","anchor":"2026-01-02"},"forward_default":true}}`, true)
	if code != 200 || s["budget"].(map[string]any)["forward_default"] != true {
		t.Fatalf("settings = %d %v", code, s)
	}
	_, b = c.do("GET", "/api/budget?view=paycheck&date=2026-02-10", "", false)
	if b["start"] != "2026-01-30" || b["end"] != "2026-02-12" {
		t.Fatalf("paycheck period %v - %v", b["start"], b["end"])
	}

	// History for the edit dialog.
	code, h := c.do("GET", "/api/budget/history?category_id="+restID+"&month=2026-03", "", false)
	hist := h["history"].([]any)
	if code != 200 || len(hist) != 7 || h["last_month"] != float64(10000) || h["month_budget"] != float64(40000) || h["average"] != float64(1667) {
		t.Fatalf("history = %d %v", code, h)
	}

	// Goals: create, budget, contribute a transaction, then delete clears the link.
	code, out = c.do("POST", "/api/goals", `{"name":"Vacation","target":"3000","starting":"200"}`, true)
	if code != 201 {
		t.Fatalf("create goal = %d %v", code, out)
	}
	goal := fmt.Sprint(out["id"])
	c.do("PUT", "/api/budget/amount", `{"goal_id":`+goal+`,"month":"2026-02","amount":"250"}`, true)
	_, out = c.do("POST", "/api/transactions", `{"account_id":`+acct+`,"date":"2026-02-05","amount":"-250","description":"To savings"}`, true)
	save := fmt.Sprint(out["id"])
	if code, out := c.do("PATCH", "/api/transactions/"+save, `{"goal_id":`+goal+`}`, true); code != 200 || out["goal_id"] != float64(out["goal_id"].(float64)) {
		t.Fatalf("assign goal = %d %v", code, out)
	}
	_, b = c.do("GET", "/api/budget?date=2026-02-10", "", false)
	vac, kind := findLine(t, b, "Vacation")
	sum := b["summary"].(map[string]any)
	if kind != "goals" || vac["budget"] != float64(25000) || vac["actual"] != float64(25000) || sum["goals_actual"] != float64(25000) {
		t.Fatalf("goal line %s %v %v", kind, vac, sum)
	}
	_, gl := c.do("GET", "/api/goals", "", false)
	g0 := gl["goals"].([]any)[0].(map[string]any)
	if g0["balance_cents"] != float64(45000) || g0["target_cents"] != float64(300000) {
		t.Fatalf("goals = %v", g0)
	}
	if code, _ := c.do("DELETE", "/api/goals/"+goal, "", true); code != 204 {
		t.Fatalf("delete goal = %d", code)
	}
	if _, out := c.do("GET", "/api/transactions/"+save, "", false); out["goal_id"] != nil {
		t.Fatalf("goal link kept: %v", out["goal_id"])
	}
}
