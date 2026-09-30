package server

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestMonarchImport(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	// An account that already exists (say from SimpleFIN) with one of the transactions in it.
	_, wallet := c.do("POST", "/api/accounts", `{"name":"Everyday Checking (...2080)","type":"checking","balance":"500"}`, true)
	day := func(n int) string { return time.Now().AddDate(0, 0, -n).Format(time.DateOnly) }
	_, existing := c.do("POST", "/api/transactions", fmt.Sprintf(`{"account_id":%v,"date":"%s","amount":"-55.72","description":"TST SALS PIZZA"}`, wallet["id"], day(1)), true)

	txns := "Date,Merchant,Category,Account,Original Statement,Notes,Amount,Tags,Owner,Reviewed,Id\n" +
		day(2) + ",Sal's Pizza,Restaurants & Bars,Checking (...2080),TST SALS PIZZA,,-55.72,,Shared,,1\n" +
		day(3) + ",Storage Co,Storage Unit,Checking (...2080),STORAGE,,-80,Monthly,Shared,,2\n" +
		day(40) + ",Big Store,Shopping,Card (...0428),BIG STORE,gift,-120.50,,Shared,,3\n" +
		day(41) + ",Mystery,Uncategorized,Card (...0428),X,,-9,,Shared,Needs Review,4\n"
	bals := "Date,Balance,Account\n" + day(20) + ",-300,Card (...0428)\n" + day(0) + ",-129.50,Card (...0428)\n" + day(0) + ",-9000,Honda CRV (...8141)\n"
	files := func(extra string) string {
		b, _ := json.Marshal(map[string]string{"transactions": txns, "balances": bals})
		return string(b[:len(b)-1]) + extra + "}"
	}

	code, p := c.do("POST", "/api/import/monarch/preview", files(""), true)
	if code != 200 || p["transactions"] != float64(4) || p["balances"] != float64(3) {
		t.Fatalf("preview = %d %v", code, p)
	}
	var chk map[string]any
	for _, x := range p["accounts"].([]any) {
		if x.(map[string]any)["key"] == "Checking (...2080)" {
			chk = x.(map[string]any)
		}
	}
	if chk["key"] != "Checking (...2080)" || chk["account_id"] != wallet["id"] {
		t.Fatalf("checking should map onto the account with the same last 4: %v", chk)
	}
	cats := map[string]map[string]any{}
	for _, x := range p["categories"].([]any) {
		m := x.(map[string]any)
		cats[m["name"].(string)] = m
	}
	if cats["Shopping"]["category_id"] == nil || cats["Storage Unit"]["category_id"] != nil || cats["Storage Unit"]["icon"] != "📦" || cats[""]["count"] != float64(1) {
		t.Fatalf("categories = %v", cats)
	}

	if code, out := c.do("POST", "/api/import/monarch/preview", `{"transactions":"Date,Balance,Account\n","balances":""}`, true); code != 400 {
		t.Fatalf("wrong file = %d %v", code, out)
	}

	mapping := fmt.Sprintf(`,"accounts":[{"key":"Checking (...2080)","account_id":%v},{"key":"Card (...0428)","create":{"name":"Card","type":"credit_card"}},{"key":"Honda CRV (...8141)"}],`+
		`"categories":[{"name":"Restaurants & Bars","category_id":%v},{"name":"Shopping","category_id":%v},{"name":"Storage Unit","create":{"group_id":%v,"icon":"📦"}},{"name":""}]`,
		wallet["id"], cats["Restaurants & Bars"]["category_id"], cats["Shopping"]["category_id"], cats["Storage Unit"]["group_id"])
	code, res := c.do("POST", "/api/import/monarch", files(mapping), true)
	if code != 200 {
		t.Fatalf("import = %d %v", code, res)
	}
	want := map[string]float64{"accounts_created": 1, "categories_created": 1, "imported": 3, "matched": 1, "already_imported": 0, "skipped": 0, "balance_snapshots": 2}
	for k, v := range want {
		if res[k] != v {
			t.Errorf("%s = %v, want %v (all: %v)", k, res[k], v, res)
		}
	}
	// The existing pizza transaction got Monarch's category instead of a duplicate.
	_, got := c.do("GET", fmt.Sprint("/api/transactions/", existing["id"]), "", false)
	if got["transaction"].(map[string]any)["category_name"] != "Restaurants & Bars" {
		t.Errorf("matched transaction = %v", got)
	}
	_, list := c.do("GET", "/api/transactions?q=Storage", "", false)
	rows := list["transactions"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["category_name"] != "Storage Unit" || rows[0].(map[string]any)["source"] != "import" {
		t.Fatalf("storage row = %v", rows)
	}
	_, accounts := c.do("GET", "/api/accounts", "", false)
	found := false
	for _, x := range accounts["accounts"].([]any) {
		a := x.(map[string]any)
		if a["name"] == "Card" {
			found = a["balance_cents"] == float64(-12950) && a["type"] == "credit_card" && a["mask"] == "0428"
		}
	}
	if !found {
		t.Errorf("created card account missing or wrong: %v", accounts)
	}
	// Net worth history now reaches back to the card's first transaction.
	_, hist := c.do("GET", "/api/networth/history?days=90", "", false)
	if first := hist["points"].([]any)[0].(map[string]any); first["date"] != day(41) {
		t.Errorf("history starts %v, want %s", first["date"], day(41))
	}

	// Importing the same export again adds nothing.
	_, res = c.do("POST", "/api/import/monarch", files(mapping), true)
	if res["imported"] != float64(0) || res["already_imported"] != float64(3) || res["matched"] != float64(1) || res["categories_created"] != float64(0) {
		t.Errorf("re-import = %v", res)
	}
}
