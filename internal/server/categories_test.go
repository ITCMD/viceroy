package server

import (
	"fmt"
	"strconv"
	"testing"
)

func TestCategoryLayoutAndRollover(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, out := c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"1000"}`, true)
	acct := strconv.FormatInt(int64(out["id"].(float64)), 10)

	_, cats := c.do("GET", "/api/categories", "", false)
	group := map[string]map[string]any{}
	catID := map[string]float64{}
	for _, g := range cats["groups"].([]any) {
		gm := g.(map[string]any)
		group[gm["kind"].(string)] = gm
		for _, cat := range gm["categories"].([]any) {
			cm := cat.(map[string]any)
			catID[cm["name"].(string)] = cm["id"].(float64)
		}
	}
	// Gifts moves from Flexible to the top of Non-monthly.
	nonMonthly := []any{catID["Gifts"]}
	nonMonthly = append(nonMonthly, ids(group["non_monthly"])...)
	body := fmt.Sprintf(`{"groups":[{"id":%v,"category_ids":%v}]}`, group["non_monthly"]["id"], jsonList(nonMonthly))
	code, res := c.do("PUT", "/api/categories/layout", body, true)
	if code != 200 {
		t.Fatalf("layout = %d %v", code, res)
	}
	for _, g := range res["groups"].([]any) {
		gm := g.(map[string]any)
		first := gm["categories"].([]any)[0].(map[string]any)["name"]
		if gm["kind"] == "non_monthly" && first != "Gifts" {
			t.Fatalf("non-monthly starts with %v", first)
		}
		if gm["kind"] == "flexible" {
			for _, cat := range gm["categories"].([]any) {
				if cat.(map[string]any)["name"] == "Gifts" {
					t.Fatal("Gifts is still flexible")
				}
			}
		}
	}
	// Income categories stay income.
	bad := fmt.Sprintf(`{"groups":[{"id":%v,"category_ids":[%v]}]}`, group["fixed"]["id"], catID["Paychecks"])
	if code, _ := c.do("PUT", "/api/categories/layout", bad, true); code != 400 {
		t.Fatalf("income into fixed = %d", code)
	}

	// Non-monthly rolls over: $100/month from January, $30 spent in January, $200 in February.
	gifts := fmt.Sprint(catID["Gifts"])
	c.do("PUT", "/api/budget/amount", `{"category_id":`+gifts+`,"month":"2026-01","amount":"100","apply_forward":true}`, true)
	for _, t2 := range []struct{ date, amt string }{{"2026-01-10", "-30"}, {"2026-02-05", "-200"}} {
		_, tx := c.do("POST", "/api/transactions", `{"account_id":`+acct+`,"date":"`+t2.date+`","amount":"`+t2.amt+`","description":"Gift shop"}`, true)
		c.do("PATCH", fmt.Sprintf("/api/transactions/%v", tx["id"]), `{"category_id":`+gifts+`}`, true)
	}
	for date, want := range map[string][2]float64{ // rollover, budget
		"2026-01-15": {0, 10000},
		"2026-02-15": {7000, 17000},
		"2026-03-15": {-3000, 7000}, // $70 carried in, $100 overspent
	} {
		_, b := c.do("GET", "/api/budget?date="+date, "", false)
		l, kind := findLine(t, b, "Gifts")
		if kind != "non_monthly" || l["rollover"].(float64) != want[0] || l["budget"].(float64) != want[1] {
			t.Errorf("%s: %s rollover %v budget %v, want %v", date, kind, l["rollover"], l["budget"], want)
		}
		// "Left to budget" counts this month's plan, not money carried in.
		if sm := b["summary"].(map[string]any); date == "2026-02-15" && sm["expense_budget"].(float64) != 10000 {
			t.Errorf("expense budget = %v", sm["expense_budget"])
		}
	}
}

func ids(g map[string]any) []any {
	var out []any
	for _, c := range g["categories"].([]any) {
		out = append(out, c.(map[string]any)["id"])
	}
	return out
}

func jsonList(xs []any) string {
	s := "["
	for i, x := range xs {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprint(x)
	}
	return s + "]"
}
