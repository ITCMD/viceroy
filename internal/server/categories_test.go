package server

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
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

func TestCategoryCRUDAndNoPacing(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, out := c.do("POST", "/api/accounts", `{"name":"Wallet","type":"cash","balance":"1000"}`, true)
	_, cats := c.do("GET", "/api/categories", "", false)
	var flexible map[string]any
	catID := map[string]any{}
	for _, g := range cats["groups"].([]any) {
		gm := g.(map[string]any)
		if gm["kind"] == "flexible" {
			flexible = gm
		}
		for _, cat := range gm["categories"].([]any) {
			cm := cat.(map[string]any)
			catID[cm["name"].(string)] = cm["id"]
		}
	}
	code, created := c.do("POST", "/api/categories", fmt.Sprintf(`{"name":" Pet care ","icon":"🐶","group_id":%v}`, flexible["id"]), true)
	if code != 201 || created["name"] != "Pet care" {
		t.Fatalf("create = %d %v", code, created)
	}
	id := fmt.Sprint(created["id"])
	if code, _ := c.do("POST", "/api/categories", fmt.Sprintf(`{"name":"groceries","group_id":%v}`, flexible["id"]), true); code != 400 {
		t.Fatalf("duplicate = %d", code)
	}
	if code, _ := c.do("POST", "/api/categories", `{"name":"X","group_id":999999}`, true); code != 400 {
		t.Fatalf("bad group = %d", code)
	}
	if code, r := c.do("PATCH", "/api/categories/"+id, `{"name":"Vet bills","icon":"🐱"}`, true); code != 200 || r["name"] != "Vet bills" {
		t.Fatalf("rename = %d %v", code, r)
	}
	// It's on the budget (last in Flexible); leave it out of pacing.
	month := time.Now().Format("2006-01")
	c.do("PUT", "/api/budget/amount", fmt.Sprintf(`{"category_id":%s,"month":%q,"amount":"300"}`, id, month), true)
	if code, _ := c.do("PUT", "/api/budget/categories/"+id+"/chunk", `{"kind":"even","no_pacing":true}`, true); code != 200 {
		t.Fatalf("no pacing = %d", code)
	}
	_, b := c.do("GET", "/api/budget", "", false)
	l, _ := findLine(t, b, "Vet bills")
	if l["expected"] != float64(0) || l["chunk"].(map[string]any)["no_pacing"] != true || l["budget"] != float64(30000) {
		t.Fatalf("line = %v", l)
	}

	// Delete, moving its transactions to Groceries.
	_, txn := c.do("POST", "/api/transactions", fmt.Sprintf(`{"account_id":%v,"date":"2026-09-01","amount":"-20","description":"Vet"}`, out["id"]), true)
	c.do("PATCH", fmt.Sprint("/api/transactions/", txn["id"]), `{"category_id":`+id+`}`, true)
	if _, u := c.do("GET", "/api/categories/"+id+"/usage", "", false); u["transactions"] != float64(1) {
		t.Fatalf("usage = %v", u)
	}
	if code, r := c.do("DELETE", fmt.Sprintf("/api/categories/%s?move_to=%v", id, catID["Groceries"]), "", true); code != 200 || r["moved"] != float64(1) {
		t.Fatalf("delete = %d %v", code, r)
	}
	_, d := c.do("GET", fmt.Sprint("/api/transactions/", txn["id"]), "", false)
	if d["transaction"].(map[string]any)["category_id"] != catID["Groceries"] {
		t.Fatalf("moved txn = %v", d["transaction"])
	}
	if code, _ := c.do("DELETE", "/api/categories/"+id, "", true); code != 404 {
		t.Fatalf("delete again = %d", code)
	}
}

func TestCategoryIconUpload(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, b := c.do("GET", "/api/budget", "", false)
	l, _ := findLine(t, b, "Groceries")
	id := l["id"]
	// A 1×1 PNG.
	png := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	code, out := c.do("PUT", fmt.Sprintf("/api/categories/%v/icon", id), fmt.Sprintf(`{"image":%q}`, png), true)
	icon, _ := out["icon"].(string)
	if code != 200 || !strings.HasPrefix(icon, fmt.Sprintf("/api/categories/%v/icon?v=", id)) {
		t.Fatalf("upload = %d %v", code, out)
	}
	if code, _ := c.do("PUT", fmt.Sprintf("/api/categories/%v/icon", id), `{"image":"data:image/svg+xml;base64,PHN2Zy8+"}`, true); code != 400 {
		t.Fatalf("svg upload = %d", code)
	}
	if code := iconCode(c, icon); code != 200 {
		t.Fatalf("get icon = %d", code)
	}
	// Renaming keeps the image; a made-up URL isn't an icon; an emoji replaces the image.
	if code, out := c.do("PATCH", fmt.Sprint("/api/categories/", id), fmt.Sprintf(`{"name":"Food","icon":%q}`, icon), true); code != 200 {
		t.Fatalf("rename with image = %d %v", code, out)
	}
	if code, _ := c.do("PATCH", fmt.Sprint("/api/categories/", id), `{"name":"Food","icon":"/api/categories/1/icon?v=1"}`, true); code != 400 {
		t.Fatalf("forged icon url = %d", code)
	}
	c.do("PATCH", fmt.Sprint("/api/categories/", id), `{"name":"Food","icon":"🥦"}`, true)
	if code := iconCode(c, icon); code != 404 {
		t.Fatalf("image kept after switching to an emoji: %d", code)
	}
}

func iconCode(c *client, path string) int {
	code, _ := c.do("GET", path, "", false)
	return code
}
