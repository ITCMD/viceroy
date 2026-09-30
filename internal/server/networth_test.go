package server

import (
	"fmt"
	"testing"
	"time"
)

func TestBackfill(t *testing.T) {
	f := backfill("2026-09-10", 1000, map[string]int64{"2026-09-01": -100, "2026-09-05": 50, "2026-09-10": -30, "2026-09-12": 999})
	for date, want := range map[string]int64{
		"2026-08-01": 1080, // before every transaction: flat
		"2026-09-01": 980,
		"2026-09-04": 980,
		"2026-09-05": 1030,
		"2026-09-09": 1030,
		"2026-09-10": 1000, // the anchor day already includes its own transactions
	} {
		if got := f(date); got != want {
			t.Errorf("balance on %s = %d, want %d", date, got, want)
		}
	}
}

func TestNetWorthBackfillsFromTransactions(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, acct := c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"1000"}`, true)
	day := func(n int) string { return time.Now().AddDate(0, 0, -n).Format(time.DateOnly) }
	for _, tx := range []struct{ date, amount string }{{day(10), "-100"}, {day(5), "50"}} {
		if code, out := c.do("POST", "/api/transactions", fmt.Sprintf(`{"account_id":%v,"date":"%s","amount":"%s","description":"x"}`, acct["id"], tx.date, tx.amount), true); code >= 300 {
			t.Fatalf("add = %d %v", code, out)
		}
	}
	_, hist := c.do("GET", "/api/networth/history?days=30", "", false)
	pts := hist["points"].([]any)
	if len(pts) != 11 {
		t.Fatalf("points = %d, want 11 (from the first transaction to today)", len(pts))
	}
	net := func(i int) float64 { return pts[i].(map[string]any)["net"].(float64) }
	if pts[0].(map[string]any)["date"] != day(10) || net(0) != 90000 || net(4) != 90000 || net(5) != 95000 || net(10) != 95000 {
		t.Fatalf("history = %v", pts)
	}
}
