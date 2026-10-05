package server

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"viceroy/internal/budget"
	"viceroy/internal/budgetview"
)

func TestCloseoutAPI(t *testing.T) {
	c := newTestServer(t)
	day := budget.Date(2026, 10, 3)
	closeoutToday = func() time.Time { return day }
	t.Cleanup(func() { closeoutToday = budgetview.Today })

	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, out := c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"5000"}`, true)
	acct := strconv.FormatInt(int64(out["id"].(float64)), 10)
	_, b := c.do("GET", "/api/budget?date=2026-09-10", "", false)
	rest, _ := findLine(t, b, "Restaurants & Bars")
	groc, _ := findLine(t, b, "Groceries")
	restID, grocID := fmt.Sprint(rest["id"]), fmt.Sprint(groc["id"])
	c.do("PUT", "/api/budget/amount", `{"category_id":`+restID+`,"month":"2026-09","amount":"400","apply_forward":true}`, true)
	c.do("PUT", "/api/budget/amount", `{"category_id":`+grocID+`,"month":"2026-09","amount":"500","apply_forward":true}`, true)
	spend := func(date, amount, catID string) {
		_, out := c.do("POST", "/api/transactions", `{"account_id":`+acct+`,"date":"`+date+`","amount":"`+amount+`","description":"x"}`, true)
		c.do("PATCH", fmt.Sprint("/api/transactions/", out["id"]), `{"category_id":`+catID+`}`, true)
	}
	spend("2026-09-05", "-450", restID) // $50 over
	spend("2026-09-06", "-300", grocID) // $200 under > $150 left overall

	if code, st := c.do("GET", "/api/closeout", "", false); code != 200 || st["month"] != "2026-09" || st["closes"] != "2026-10-07" || st["closed"] != false {
		t.Fatalf("status = %d %v", code, st)
	}
	code, co := c.do("GET", "/api/closeout/2026-09", "", false)
	review := co["review"].(map[string]any)
	if code != 200 || review["surplus"] != float64(150_00) || co["closed"] != nil || co["can_change"] != true {
		t.Fatalf("closeout = %d %v", code, co)
	}
	if over := review["over"].([]any); len(over) != 1 || over[0].(map[string]any)["diff"] != float64(-50_00) {
		t.Fatalf("over = %v", review["over"])
	}
	if o := co["outlook"].(map[string]any); o["month"] != "2026-10" || o["level"] == nil {
		t.Fatalf("outlook = %v", o)
	}

	_, g := c.do("POST", "/api/goals", `{"name":"Trip","target":"1000"}`, true)
	goalID := fmt.Sprint(g["id"])
	for body, want := range map[string]int{
		`{"allocations":[{"category_id":` + restID + `,"amount":"200"}]}`:                                             400, // more than left
		`{"allocations":[{"goal_id":999,"amount":"10"}]}`:                                                             400,
		`{"allocations":[{"category_id":` + restID + `,"amount":"10"},{"category_id":` + restID + `,"amount":"10"}]}`: 400,
		`{"allocations":[{"goal_id":` + goalID + `,"amount":"-5"}]}`:                                                  400,
	} {
		if code, out := c.do("POST", "/api/closeout/2026-09", body, true); code != want {
			t.Fatalf("POST %s = %d %v", body, code, out)
		}
	}
	body := `{"allocations":[{"category_id":` + restID + `,"amount":"50"},{"goal_id":` + goalID + `,"amount":"100"}]}`
	if code, out := c.do("POST", "/api/closeout/2026-09", body, true); code != 204 {
		t.Fatalf("close = %d %v", code, out)
	}
	if code, _ := c.do("POST", "/api/closeout/2026-09", `{"allocations":[]}`, true); code != 409 {
		t.Fatalf("close twice = %d", code)
	}
	monthBudget := func(date string) any {
		_, b := c.do("GET", "/api/budget?date="+date, "", false)
		l, _ := findLine(t, b, "Restaurants & Bars")
		return l["month_budget"]
	}
	goalContributed := func() any {
		_, gl := c.do("GET", "/api/goals", "", false)
		for _, x := range gl["goals"].([]any) {
			if gm := x.(map[string]any); fmt.Sprint(gm["id"]) == goalID {
				return gm["contributed_cents"]
			}
		}
		return nil
	}
	if oct, nov := monthBudget("2026-10-10"), monthBudget("2026-11-10"); oct != float64(450_00) || nov != float64(400_00) {
		t.Fatalf("restaurants Oct %v Nov %v, want 450 then 400", oct, nov)
	}
	if v := goalContributed(); v != float64(100_00) {
		t.Fatalf("goal contributed = %v", v)
	}
	_, st := c.do("GET", "/api/closeout", "", false)
	_, co = c.do("GET", "/api/closeout/2026-09", "", false)
	closed, _ := co["closed"].(map[string]any)
	if st["closed"] != true || closed == nil || len(closed["allocations"].([]any)) != 2 || closed["closed_by"] != "A" {
		t.Fatalf("after close: status %v closeout %v", st, co)
	}

	if code, out := c.do("POST", "/api/closeout/2026-09/analysis", "", true); code != 200 || out["analysis"] == "" {
		t.Fatalf("analysis = %d %v", code, out)
	}
	_, co = c.do("GET", "/api/closeout/2026-09", "", false)
	if co["closed"].(map[string]any)["analysis"] == "" {
		t.Fatal("analysis not stored")
	}

	// Reopening takes the money back.
	if code, _ := c.do("DELETE", "/api/closeout/2026-09", "", true); code != 204 {
		t.Fatalf("reopen = %d", code)
	}
	if oct := monthBudget("2026-10-10"); oct != float64(400_00) {
		t.Fatalf("restaurants Oct after reopen = %v", oct)
	}
	if v := goalContributed(); v != float64(0) {
		t.Fatalf("goal contributed after reopen = %v", v)
	}

	// Outside the window nothing can be closed.
	day = budget.Date(2026, 10, 15)
	if _, st := c.do("GET", "/api/closeout", "", false); st["month"] != "" {
		t.Fatalf("status mid-month = %v", st)
	}
	if code, _ := c.do("GET", "/api/closeout/2026-09", "", false); code != 404 {
		t.Fatalf("closeout mid-month = %d", code)
	}
	if code, _ := c.do("POST", "/api/closeout/2026-09", `{"allocations":[]}`, true); code != 400 {
		t.Fatalf("close mid-month = %d", code)
	}

	if code, r := c.do("GET", "/api/budget/risk", "", false); code != 200 || r["level"] == nil || r["drivers"] == nil {
		t.Fatalf("risk = %d %v", code, r)
	}
}
