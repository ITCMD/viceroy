package server

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"viceroy/internal/ai/fakeai"
)

func TestWishlistAPI(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, out := c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"1000"}`, true)
	acct := fmt.Sprint(out["id"])
	shop := httptest.NewServer(&fakeai.Server{ShopDir: "../wishlist/testdata"})
	defer shop.Close()

	// The product page server is on 127.0.0.1, which the guard refuses by default.
	if code, out := c.do("POST", "/api/wishlist/preview", `{"url":"`+shop.URL+`/shop/jsonld"}`, true); code != 400 {
		t.Fatalf("private preview = %d %v", code, out)
	}
	c.server.wish.AllowPrivate = true
	code, p := c.do("POST", "/api/wishlist/preview", `{"url":"`+shop.URL+`/shop/short/jsonld"}`, true)
	if code != 200 || p["title"] != "Brightside Reading Lamp" || p["price_cents"] != float64(4999) || p["url"] != shop.URL+"/shop/jsonld" || p["image_url"] != shop.URL+"/shop/lamp.png" {
		t.Fatalf("preview = %d %v", code, p)
	}
	// No price in the markup: the AI reads it from the page text.
	if _, p := c.do("POST", "/api/wishlist/preview", `{"url":"`+shop.URL+`/shop/textonly"}`, true); p["price_cents"] != float64(21900) || p["blocked"] != false {
		t.Fatalf("ai preview = %v", p)
	}
	if _, p := c.do("POST", "/api/wishlist/preview", `{"url":"`+shop.URL+`/shop/captcha"}`, true); p["blocked"] != true {
		t.Fatalf("captcha preview = %v", p)
	}

	// The first visit creates the Wishlist goal.
	code, wl := c.do("GET", "/api/wishlist", "", false)
	afford := wl["afford"].(map[string]any)
	goal := fmt.Sprint(afford["goal_id"])
	if code != 200 || afford["goal_name"] != "Wishlist" || len(wl["items"].([]any)) != 0 || len(wl["members"].([]any)) != 1 {
		t.Fatalf("empty wishlist = %d %v", code, wl)
	}

	code, out = c.do("POST", "/api/wishlist", `{"title":"Lamp","url":"`+shop.URL+`/shop/jsonld?utm_source=x","price":"49.99","price_fetched":true,"stars":4,"image_url":"`+shop.URL+`/shop/lamp.png"}`, true)
	if code != 201 || out["image_problem"] != nil {
		t.Fatalf("create = %d %v", code, out)
	}
	lamp := fmt.Sprint(out["id"])
	_, out = c.do("POST", "/api/wishlist", `{"title":"Bench","price":"219","stars":5,"saves_money":true}`, true)
	bench := fmt.Sprint(out["id"])
	_, out = c.do("POST", "/api/wishlist", `{"title":"Mystery","stars":5}`, true)
	if code, out := c.do("POST", "/api/wishlist", `{"title":"","stars":9}`, true); code != 400 {
		t.Fatalf("bad item = %d %v", code, out)
	}
	if code, out := c.do("PATCH", "/api/wishlist/"+lamp, `{"stars":6}`, true); code != 400 {
		t.Fatalf("bad stars = %d %v", code, out)
	}

	items := func(sort string) []map[string]any {
		_, wl := c.do("GET", "/api/wishlist?sort="+sort, "", false)
		var out []map[string]any
		for _, it := range wl["items"].([]any) {
			out = append(out, it.(map[string]any))
		}
		return out
	}
	byPrice := items("price")
	if byPrice[0]["title"] != "Lamp" || byPrice[1]["title"] != "Bench" || byPrice[2]["title"] != "Mystery" {
		t.Fatalf("price order = %v", byPrice)
	}
	l := byPrice[0]
	if l["url"] != shop.URL+"/shop/jsonld" || l["store"] != "127.0.0.1" || l["price_source"] != "fetched" || l["image_url"] == "" {
		t.Fatalf("lamp = %v", l)
	}
	// Lamp 4×1÷49.99 ≈ 0.08; bench 5×1÷219×1.5 ≈ 0.034.
	if s := items("score"); s[0]["title"] != "Lamp" || s[1]["score_text"] != "5★ × 1 person ÷ $219 × 1.5" {
		t.Fatalf("score order = %v", s)
	}
	if code, _ := c.do("GET", "/api"+fmt.Sprint(l["image_url"])[len("/api"):], "", false); code != 200 {
		t.Fatalf("image = %d", code)
	}

	// Budget $100/month, $60 put in so far, $200 already saved → $260 now, $300 by month end.
	today := time.Now().Format(time.DateOnly)
	month := today[:7]
	c.do("PATCH", "/api/goals/"+goal, `{"starting":"200"}`, true)
	c.do("PUT", "/api/budget/amount", `{"goal_id":`+goal+`,"month":"`+month+`","amount":"100"}`, true)
	_, out = c.do("POST", "/api/transactions", `{"account_id":`+acct+`,"date":"`+today+`","amount":"-60","description":"To savings"}`, true)
	c.do("PATCH", "/api/transactions/"+fmt.Sprint(out["id"]), `{"goal_id":`+goal+`}`, true)
	_, wl = c.do("GET", "/api/wishlist?sort=price", "", false)
	afford = wl["afford"].(map[string]any)
	if afford["saved_now"] != float64(26000) || afford["month_end"] != float64(30000) || afford["on_budget"] != true {
		t.Fatalf("afford = %v", afford)
	}
	got := wl["items"].([]any)
	if got[0].(map[string]any)["afford"] != "now" || got[1].(map[string]any)["afford"] != "month_end" || got[2].(map[string]any)["afford"] != "" {
		t.Fatalf("afford marks = %v", got)
	}

	// Buying the lamp with a transaction spends it from the goal; it isn't a contribution.
	_, out = c.do("POST", "/api/transactions", `{"account_id":`+acct+`,"date":"`+today+`","amount":"-49.99","description":"Brightside"}`, true)
	buy := fmt.Sprint(out["id"])
	if code, out := c.do("POST", "/api/wishlist/"+lamp+"/bought", `{"transaction_id":`+buy+`}`, true); code != 204 {
		t.Fatalf("bought = %d %v", code, out)
	}
	_, tx := c.do("GET", "/api/transactions/"+buy, "", false)
	if tt := tx["transaction"].(map[string]any); fmt.Sprint(tt["goal_id"]) != goal || tt["goal_withdrawal"] != true {
		t.Fatalf("purchase txn = %v", tt)
	}
	_, wl = c.do("GET", "/api/wishlist", "", false)
	if a := wl["afford"].(map[string]any); a["saved_now"] != float64(26000-4999) || len(wl["bought"].([]any)) != 1 || len(wl["items"].([]any)) != 2 {
		t.Fatalf("after buying = %v", wl)
	}
	_, b := c.do("GET", "/api/budget", "", false)
	if w, _ := findLine(t, b, "Wishlist"); w["actual"] != float64(6000) {
		t.Fatalf("wishlist contributions = %v", w)
	}
	_, gl := c.do("GET", "/api/goals", "", false)
	for _, g := range gl["goals"].([]any) {
		if g := g.(map[string]any); g["builtin"] == "wishlist" && (g["withdrawn_cents"] != float64(4999) || g["balance_cents"] != float64(26000-4999)) {
			t.Fatalf("goal = %v", g)
		}
	}
	// The Wishlist goal can't be deleted while items remain.
	if code, _ := c.do("DELETE", "/api/goals/"+goal, "", true); code != 409 {
		t.Fatalf("delete wishlist goal = %d", code)
	}

	// Undo puts it back and the purchase leaves the goal.
	c.do("DELETE", "/api/wishlist/"+lamp+"/bought", "", true)
	_, tx = c.do("GET", "/api/transactions/"+buy, "", false)
	if tt := tx["transaction"].(map[string]any); tt["goal_id"] != nil || tt["goal_withdrawal"] != false {
		t.Fatalf("after undo = %v", tt)
	}
	// Bought on a date only: the goal doesn't change.
	c.do("POST", "/api/wishlist/"+bench+"/bought", `{"date":"`+today+`"}`, true)
	_, wl = c.do("GET", "/api/wishlist", "", false)
	if a := wl["afford"].(map[string]any); a["saved_now"] != float64(26000) || len(wl["bought"].([]any)) != 1 {
		t.Fatalf("date bought = %v", wl)
	}
	if code, _ := c.do("DELETE", "/api/wishlist/"+lamp, "", true); code != 204 {
		t.Fatalf("delete = %d", code)
	}
	// Filter by person: nobody else exists, so an unknown id shows nothing.
	if _, wl := c.do("GET", "/api/wishlist?person=999", "", false); len(wl["items"].([]any)) != 0 {
		t.Fatalf("person filter = %v", wl["items"])
	}
}
