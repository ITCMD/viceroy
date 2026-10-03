package server

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"viceroy/internal/budgetview"
)

func TestReportTreeAndTxnFilters(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, out := c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"1000"}`, true)
	acct := strconv.FormatInt(int64(out["id"].(float64)), 10)
	_, b := c.do("GET", "/api/budget?date=2026-02-10", "", false)
	rest, _ := findLine(t, b, "Restaurants & Bars")
	pay, _ := findLine(t, b, "Paychecks")
	_, g := c.do("POST", "/api/goals", `{"name":"Trip","icon":"✈️","target":"1000"}`, true)

	add := func(date, amount, desc string, patch string) float64 {
		t.Helper()
		code, out := c.do("POST", "/api/transactions", `{"account_id":`+acct+`,"date":"`+date+`","amount":"`+amount+`","description":"`+desc+`"}`, true)
		if code != 201 && code != 200 {
			t.Fatalf("add txn = %d %v", code, out)
		}
		if patch != "" {
			c.do("PATCH", fmt.Sprint("/api/transactions/", out["id"]), patch, true)
		}
		return out["id"].(float64)
	}
	add("2026-02-03", "-40", "Bistro", fmt.Sprintf(`{"category_id":%v}`, rest["id"]))
	add("2026-02-07", "-60", "Bistro", fmt.Sprintf(`{"category_id":%v}`, rest["id"]))
	add("2026-02-09", "-25", "Taqueria", fmt.Sprintf(`{"category_id":%v}`, rest["id"]))
	add("2026-02-01", "2000", "Acme Payroll", fmt.Sprintf(`{"category_id":%v}`, pay["id"]))
	add("2026-02-12", "-200", "To savings", fmt.Sprintf(`{"goal_id":%v}`, g["id"]))
	add("2026-02-15", "-12", "Mystery shop", "")

	code, tr := c.do("GET", "/api/reports/tree?from=2026-02-01&to=2026-02-28", "", false)
	if code != 200 {
		t.Fatalf("tree = %d %v", code, tr)
	}
	sp := tr["spending"].(map[string]any)
	if sp["total"] != float64(12500+20000+1200) {
		t.Fatalf("spending total = %v", sp["total"])
	}
	secs := sp["children"].([]any)
	first := secs[0].(map[string]any)
	if first["kind"] != "contributions" || first["total"] != float64(20000) {
		t.Fatalf("first section = %v", first)
	}
	var flex map[string]any
	for _, s := range secs {
		if m := s.(map[string]any); m["group"] == "flexible" {
			flex = m
		}
	}
	cat := flex["children"].([]any)[0].(map[string]any)
	bistro := cat["children"].([]any)[0].(map[string]any)
	if cat["name"] != "Restaurants & Bars" || bistro["name"] != "Bistro" || bistro["total"] != float64(10000) || bistro["count"] != float64(2) {
		t.Fatalf("category = %v", cat)
	}
	if tr["income"].(map[string]any)["total"] != float64(200000) {
		t.Fatalf("income = %v", tr["income"])
	}

	count := func(qs string) int {
		t.Helper()
		code, out := c.do("GET", "/api/transactions?"+qs, "", false)
		if code != 200 {
			t.Fatalf("list %s = %d %v", qs, code, out)
		}
		return len(out["transactions"].([]any))
	}
	cases := map[string]int{
		"merchant=Bistro": 2,
		fmt.Sprintf("category=%v&merchant=Bistro", rest["id"]): 2,
		"direction=in":                            1,
		"direction=out":                           5,
		"min=30&max=100":                          2,
		"min=100":                                 2,
		"direction=out&min=100":                   1,
		"from=2026-02-05&to=2026-02-10":           2,
		fmt.Sprintf("group=%v", flex["id"]):       3,
		"uncategorized=1&merchant=Mystery%20shop": 1,
	}
	for qs, want := range cases {
		if got := count(qs); got != want {
			t.Errorf("%s: %d transactions, want %d", qs, got, want)
		}
	}
	if code, _ := c.do("GET", "/api/transactions?direction=sideways", "", false); code != 400 {
		t.Errorf("bad direction = %d", code)
	}
	if code, _ := c.do("GET", "/api/transactions?min=abc", "", false); code != 400 {
		t.Errorf("bad min = %d", code)
	}
}

func TestDebtReport(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, card := c.do("POST", "/api/accounts", `{"name":"Visa","type":"credit_card","balance":"3000"}`, true)
	_, car := c.do("POST", "/api/accounts", `{"name":"Car loan","type":"loan","balance":"8000"}`, true)
	c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"100"}`, true)
	c.do("POST", "/api/transactions", fmt.Sprintf(`{"account_id":%v,"date":"%s","amount":"-45.10","description":"INTEREST CHARGE ON PURCHASES"}`, card["id"], budgetview.Today().Format(time.DateOnly)), true)

	_, r := c.do("GET", "/api/reports/debt", "", false)
	debts := r["debts"].([]any)
	if len(debts) != 2 {
		t.Fatalf("debts = %v", debts)
	}
	visa := debts[1].(map[string]any) // largest first: the car loan, then the card
	// Nothing is guessed: no plans until every debt has an APR and a minimum.
	if visa["name"] != "Visa" || visa["apr_source"] != "missing" || visa["min_payment_source"] != "missing" || visa["interest_paid_12m"] != float64(4510) {
		t.Fatalf("visa = %v", visa)
	}
	if loan := debts[0].(map[string]any); loan["apr_source"] != "missing" || loan["monthly_interest"] != float64(0) {
		t.Fatalf("loan = %v", loan)
	}
	if r["ready"] != false || len(r["plans"].(map[string]any)) != 0 {
		t.Fatalf("plans without terms: ready %v, plans %v", r["ready"], r["plans"])
	}

	if code, out := c.do("PATCH", fmt.Sprint("/api/accounts/", car["id"]), `{"apr":"6.5%","min_payment":"250"}`, true); code != 204 {
		t.Fatalf("set terms = %d %v", code, out)
	}
	if code, _ := c.do("PATCH", fmt.Sprint("/api/accounts/", car["id"]), `{"apr":"lots"}`, true); code != 400 {
		t.Fatalf("bad apr = %d", code)
	}
	if _, r := c.do("GET", "/api/reports/debt", "", false); r["ready"] != false {
		t.Fatalf("ready with the card's terms missing")
	}
	if code, out := c.do("PATCH", fmt.Sprint("/api/accounts/", card["id"]), `{"apr":"24.99","min_payment":"90"}`, true); code != 204 {
		t.Fatalf("set card terms = %d %v", code, out)
	}
	code, r := c.do("GET", "/api/reports/debt?extra=200", "", false)
	if code != 200 || r["extra"] != float64(20000) {
		t.Fatalf("debt = %d %v", code, r)
	}
	loan := r["debts"].([]any)[0].(map[string]any)
	if loan["apr_bps"] != float64(650) || loan["apr_source"] != "user" || loan["min_payment"] != float64(25000) || loan["monthly_interest"] != float64(4333) {
		t.Fatalf("loan with terms = %v", loan)
	}
	if r["ready"] != true {
		t.Fatalf("not ready with all terms set")
	}
	plans := r["plans"].(map[string]any)
	min, snow := plans["minimum"].(map[string]any), plans["snowball"].(map[string]any)
	if snow["months"].(float64) >= min["months"].(float64) || snow["interest"].(float64) >= min["interest"].(float64) || snow["never"] != false {
		t.Fatalf("plans: minimum %v / snowball %v", min["months"], snow["months"])
	}
	// A 0% intro rate: no interest until it ends, and a sooner plan than without it.
	if code, _ := c.do("PATCH", fmt.Sprint("/api/accounts/", card["id"]), `{"promo_until":"next spring"}`, true); code != 400 {
		t.Fatalf("bad promo date = %d", code)
	}
	until := budgetview.Today().AddDate(1, 0, 0).Format(time.DateOnly)
	if code, out := c.do("PATCH", fmt.Sprint("/api/accounts/", card["id"]), fmt.Sprintf(`{"promo_until":%q}`, until), true); code != 204 {
		t.Fatalf("set promo = %d %v", code, out)
	}
	_, rp := c.do("GET", "/api/reports/debt?extra=200", "", false)
	visa = rp["debts"].([]any)[1].(map[string]any)
	if visa["promo_until"] != until || visa["monthly_interest"] != float64(0) || visa["apr_bps"] != float64(2499) {
		t.Fatalf("visa with promo = %v", visa)
	}
	if pi := rp["plans"].(map[string]any)["avalanche"].(map[string]any)["interest"].(float64); pi >= plans["avalanche"].(map[string]any)["interest"].(float64) {
		t.Fatalf("promo plan interest %v not below %v", pi, plans["avalanche"].(map[string]any)["interest"])
	}
	c.do("PATCH", fmt.Sprint("/api/accounts/", card["id"]), `{"promo_until":""}`, true)
	if _, rp := c.do("GET", "/api/reports/debt", "", false); rp["debts"].([]any)[1].(map[string]any)["promo_until"] != nil {
		t.Fatalf("promo not cleared")
	}

	if h := r["history"].([]any); len(h) == 0 || h[len(h)-1].(map[string]any)["total"] != float64(1100000+4510) {
		t.Fatalf("history = %v", h)
	}
	if code, _ := c.do("GET", "/api/reports/debt?extra=-5", "", false); code != 400 {
		t.Fatalf("negative extra = %d", code)
	}

	// The chat tool and Discuss context.
	events := c.chat(`{"content":"How fast can I pay off my debt?"}`)
	if events[1]["tool"] != "debt_payoff" {
		t.Fatalf("events = %v", events)
	}
	events = c.chat(`{"content":"what stands out?","context":"{\"page\":\"Debt Free Future\"}"}`)
	text := ""
	for _, e := range events {
		if e["type"] == "text" {
			text += e["text"].(string)
		}
	}
	if !strings.Contains(text, "Looking at your Debt Free Future page") {
		t.Fatalf("text = %q", text)
	}
	// Follow-ups in that thread keep the page context.
	events = c.chat(fmt.Sprintf(`{"thread_id":%v,"content":"and?"}`, events[0]["thread"].(map[string]any)["id"]))
	text = ""
	for _, e := range events {
		if e["type"] == "text" {
			text += e["text"].(string)
		}
	}
	if !strings.Contains(text, "Looking at your Debt Free Future page") {
		t.Fatalf("follow-up = %v", events)
	}
}
