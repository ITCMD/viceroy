package server

import (
	"fmt"
	"testing"
	"time"

	"viceroy/internal/budgetview"
)

func TestDebtRepaymentBudget(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, card := c.do("POST", "/api/accounts", `{"name":"Visa","type":"credit_card","balance":"3000"}`, true)
	_, car := c.do("POST", "/api/accounts", `{"name":"Car loan","type":"loan","balance":"8000"}`, true)
	_, bank := c.do("POST", "/api/accounts", `{"name":"Checking","type":"checking","balance":"5000"}`, true)
	today := budgetview.Today()
	month := today.Format("2006-01")

	_, b := c.do("GET", "/api/budget", "", false)
	debtLine, kind := findLine(t, b, "Debt Repayment")
	groceries, _ := findLine(t, b, "Groceries")
	if kind != "fixed" {
		t.Fatalf("Debt Repayment in %s", kind)
	}
	add := func(acct any, amount, desc string, cat any) {
		t.Helper()
		code, out := c.do("POST", "/api/transactions", fmt.Sprintf(`{"account_id":%v,"date":%q,"amount":%q,"description":%q}`, acct, today.Format(time.DateOnly), amount, desc), true)
		if code != 201 && code != 200 {
			t.Fatalf("add txn = %d %v", code, out)
		}
		if cat != nil {
			c.do("PATCH", fmt.Sprint("/api/transactions/", out["id"]), fmt.Sprintf(`{"category_id":%v}`, cat), true)
		}
	}
	add(card["id"], "-100", "Grocer", groceries["id"])       // a purchase: counts in Groceries
	add(card["id"], "500", "PAYMENT THANK YOU", nil)         // paid to the card
	add(bank["id"], "-250", "AUTO LOAN PMT", debtLine["id"]) // loan payment from checking...
	add(car["id"], "250", "PAYMENT RECEIVED", nil)           // ...and its other side
	add(bank["id"], "-75", "STUDENT LENDER", debtLine["id"]) // no debt account for it: Other

	sub := func(b map[string]any) map[string]map[string]any {
		t.Helper()
		l, _ := findLine(t, b, "Debt Repayment")
		out := map[string]map[string]any{"": l}
		lines, _ := l["lines"].([]any)
		for _, x := range lines {
			out[x.(map[string]any)["name"].(string)] = x.(map[string]any)
		}
		return out
	}
	_, b = c.do("GET", "/api/budget", "", false)
	lines := sub(b)
	if len(lines) != 4 || lines["Visa"] == nil || lines["Car loan"] == nil || lines["Other debt payments"] == nil {
		t.Fatalf("sub-lines = %v", lines[""]["lines"])
	}
	// Net paydown: the card got $500 but $100 of new charges; the loan payment isn't counted
	// twice (checking side matched to the loan side); the student lender payment is Other.
	if lines["Visa"]["actual"] != float64(40000) || lines["Car loan"]["actual"] != float64(25000) || lines["Other debt payments"]["actual"] != float64(7500) {
		t.Fatalf("actuals visa %v car %v other %v", lines["Visa"]["actual"], lines["Car loan"]["actual"], lines["Other debt payments"]["actual"])
	}
	if lines[""]["actual"] != float64(72500) {
		t.Fatalf("Debt Repayment actual = %v", lines[""]["actual"])
	}
	first := lines[""]["lines"].([]any)[0].(map[string]any)
	if first["name"] != "Car loan" || first["account_id"] != car["id"] {
		t.Fatalf("largest debt first: %v", first)
	}

	// Budget per account; the category's own amount (Other) is separate.
	if code, out := c.do("PUT", "/api/budget/amount", fmt.Sprintf(`{"account_id":%v,"month":%q,"amount":"300"}`, card["id"], month), true); code != 204 {
		t.Fatalf("set account amount = %d %v", code, out)
	}
	if code, _ := c.do("PUT", "/api/budget/amount", fmt.Sprintf(`{"account_id":%v,"month":%q,"amount":"300"}`, bank["id"], month), true); code != 400 {
		t.Fatalf("budget on a bank account = %d", code)
	}
	c.do("PUT", "/api/budget/amount", fmt.Sprintf(`{"category_id":%v,"month":%q,"amount":"50"}`, debtLine["id"], month), true)
	_, b = c.do("GET", "/api/budget", "", false)
	lines = sub(b)
	if lines["Visa"]["month_budget"] != float64(30000) || lines["Other debt payments"]["month_budget"] != float64(5000) || lines[""]["month_budget"] != float64(35000) {
		t.Fatalf("budgets visa %v other %v total %v", lines["Visa"]["month_budget"], lines["Other debt payments"]["month_budget"], lines[""]["month_budget"])
	}

	// Paid mode counts the whole payment.
	if code, out := c.do("PATCH", "/api/settings", `{"budget":{"debt_actual":"paid"}}`, true); code != 200 {
		t.Fatalf("set debt_actual = %d %v", code, out)
	}
	if code, _ := c.do("PATCH", "/api/settings", `{"budget":{"debt_actual":"gross"}}`, true); code != 400 {
		t.Fatalf("bad debt_actual = %d", code)
	}
	_, b = c.do("GET", "/api/budget", "", false)
	if v := sub(b)["Visa"]["actual"]; v != float64(50000) {
		t.Fatalf("paid-mode visa actual = %v", v)
	}
	c.do("PATCH", "/api/settings", `{"budget":{"debt_actual":"net"}}`, true)

	// The editor: history with paid/charged, terms, and the saved plan's payment.
	code, h := c.do("GET", fmt.Sprintf("/api/budget/history?account_id=%v&month=%s", card["id"], month), "", false)
	if code != 200 {
		t.Fatalf("history = %d %v", code, h)
	}
	cur := h["history"].([]any)[6].(map[string]any)
	if cur["paid"] != float64(50000) || cur["charged"] != float64(10000) || cur["actual"] != float64(40000) || h["month_budget"] != float64(30000) {
		t.Fatalf("history month = %v", cur)
	}
	d := h["debt"].(map[string]any)
	if d["ready"] != false || d["plan_payment"] != nil || d["mode"] != "net" {
		t.Fatalf("no plan without terms: %v", d)
	}
	c.do("PATCH", fmt.Sprint("/api/accounts/", card["id"]), `{"apr":"24.99","min_payment":"90"}`, true)
	c.do("PATCH", fmt.Sprint("/api/accounts/", car["id"]), `{"apr":"6.5","min_payment":"250"}`, true)
	if code, out := c.do("PUT", "/api/reports/debt/plan", `{"strategy":"avalanche","extra":"100"}`, true); code != 200 {
		t.Fatalf("save plan = %d %v", code, out)
	}
	if code, _ := c.do("PUT", "/api/reports/debt/plan", `{"strategy":"fastest","extra":"100"}`, true); code != 400 {
		t.Fatalf("bad strategy = %d", code)
	}
	_, rep := c.do("GET", "/api/reports/debt", "", false)
	if rep["strategy"] != "avalanche" || rep["extra"] != float64(10000) {
		t.Fatalf("saved plan not used: %v %v", rep["strategy"], rep["extra"])
	}
	_, h = c.do("GET", fmt.Sprintf("/api/budget/history?account_id=%v&month=%s", card["id"], month), "", false)
	d = h["debt"].(map[string]any)
	// Avalanche puts the extra $100 on the 24.99% card: $90 minimum + $100.
	if d["ready"] != true || d["plan_payment"] != float64(19000) || d["min_payment"] != float64(9000) || d["strategy"] != "avalanche" {
		t.Fatalf("plan info = %v", d)
	}
	// Snowball goes after the smaller balance too (the card), with a $300 extra.
	c.do("PUT", "/api/reports/debt/plan", `{"strategy":"snowball","extra":"300"}`, true)
	_, h = c.do("GET", fmt.Sprintf("/api/budget/history?account_id=%v&month=%s", car["id"], month), "", false)
	if d := h["debt"].(map[string]any); d["plan_payment"] != float64(25000) || d["strategy"] != "snowball" {
		t.Fatalf("car plan info = %v", d)
	}
	// A past month gets no suggestion.
	past := today.AddDate(0, -1, 0).Format("2006-01")
	if _, h := c.do("GET", fmt.Sprintf("/api/budget/history?account_id=%v&month=%s", card["id"], past), "", false); h["debt"].(map[string]any)["plan_payment"] != nil {
		t.Fatalf("past month has a plan payment")
	}
	// Other's history is only the unmatched part.
	_, h = c.do("GET", fmt.Sprintf("/api/budget/history?category_id=%v&part=other&month=%s", debtLine["id"], month), "", false)
	if v := h["history"].([]any)[6].(map[string]any)["actual"]; v != float64(7500) {
		t.Fatalf("other history = %v", v)
	}
}
