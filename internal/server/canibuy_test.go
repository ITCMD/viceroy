package server

import (
	"fmt"
	"testing"

	"viceroy/internal/budgetview"
)

func TestCanIBuy(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, out := c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"1000"}`, true)
	acct := fmt.Sprint(out["id"])
	_, b := c.do("GET", "/api/budget", "", false)
	coffee, _ := findLine(t, b, "Coffee Shops")
	coffeeID := fmt.Sprint(coffee["id"])
	month := budgetview.Today().Format("2006-01")
	today := budgetview.Today().Format("2006-01-02")
	c.do("PUT", "/api/budget/amount", `{"category_id":`+coffeeID+`,"month":"`+month+`","amount":"10"}`, true)

	// The AI picks Coffee Shops and guesses $5.50; nothing spent yet, but $5.50 may run ahead of
	// this week's pace early in the month, so either yes or careful.
	code, r := c.do("POST", "/api/can-i-buy", `{"text":"a coffee"}`, true)
	v, _ := r["verdict"].(map[string]any)
	if code != 200 || r["amount_cents"] != float64(550) || r["amount_source"] != "estimate" || r["category"].(map[string]any)["name"] != "Coffee Shops" ||
		(v["answer"] != "yes" && v["answer"] != "careful") {
		t.Fatalf("estimate = %d %v", code, r)
	}

	// Two past coffees: their median beats the AI's guess. $9 + $7 spent → $8 more is over $10.
	for _, a := range []string{"-9", "-7"} {
		_, tx := c.do("POST", "/api/transactions", `{"account_id":`+acct+`,"date":"`+today+`","amount":"`+a+`","description":"Corner Coffee"}`, true)
		c.do("PATCH", fmt.Sprintf("/api/transactions/%v", tx["id"]), `{"category_id":`+coffeeID+`}`, true)
	}
	code, r = c.do("POST", "/api/can-i-buy", `{"text":"coffee at the corner"}`, true)
	v, _ = r["verdict"].(map[string]any)
	if code != 200 || r["amount_cents"] != float64(800) || r["amount_source"] != "history" || r["history_count"] != float64(2) || v["answer"] != "no" || v["left"] != float64(-1400) {
		t.Fatalf("history = %d %v", code, r)
	}

	// Category and amount given: the AI isn't asked.
	code, r = c.do("POST", "/api/can-i-buy", `{"text":"","amount":"3","category_id":`+coffeeID+`}`, true)
	if code != 200 || r["amount_source"] != "you" || r["verdict"].(map[string]any)["answer"] != "no" {
		t.Fatalf("given = %d %v", code, r)
	}
	if code, _ := c.do("POST", "/api/can-i-buy", `{"text":"x","amount":"-3"}`, true); code != 400 {
		t.Fatalf("negative amount = %d", code)
	}
}
