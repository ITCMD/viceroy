package server

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func previewRows(t *testing.T, out map[string]any) map[string]map[string]any {
	t.Helper()
	rows := map[string]map[string]any{}
	for _, r := range out["rows"].([]any) {
		m := r.(map[string]any)
		rows[m["category"].(string)] = m
	}
	return rows
}

func TestBudgetImportExport(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)
	_, b := c.do("GET", "/api/budget?date=2026-03-01", "", false)
	rent, _ := findLine(t, b, "Rent")
	rentID := fmt.Sprint(rent["id"])
	c.do("PUT", "/api/budget/amount", `{"category_id":`+rentID+`,"month":"2026-03","amount":"1500","apply_forward":true}`, true)
	c.do("PUT", "/api/budget/categories/"+rentID+"/chunk", `{"kind":"day","day":1}`, true)
	c.do("POST", "/api/goals", `{"name":"Emergency fund"}`, true)

	// Export: a CSV with every budget line, rent with its amount and schedule.
	resp, err := c.http.Get(c.base + "/api/budget/export?month=2026-03")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	csv := string(raw)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") ||
		!strings.HasPrefix(csv, "Group,Category,Amount,Timing,Icon\n") || !strings.Contains(csv, "Fixed,Rent,1500.00,Day 1,🏠") ||
		!strings.Contains(csv, "Contributions,Emergency fund,0.00,,🎯") || strings.Contains(csv, "Transfer") {
		t.Fatalf("export = %d %s", resp.StatusCode, csv)
	}

	// CSV preview: changes, a new category, a new goal, a transfer, a bad line, a duplicate.
	in := "Group,Category,Amount,Timing\n" +
		"Fixed,Rent,1600,Day 3\n" +
		"Flexible,groceries,650,Every 2 weeks\n" +
		"Flexible,Hobbies,80,\n" +
		"Goals,Vacation fund,200,\n" +
		"Transfers,Transfer,100,\n" +
		"Flexible,Coffee Shops,abc,\n" +
		"Mystery,Gadgets,40,\n" +
		"Flexible,Hobbies,90,\n"
	body, _ := json.Marshal(map[string]any{"month": "2026-03", "csv": in})
	code, out := c.do("POST", "/api/budget/import/preview", string(body), true)
	if code != 200 || out["source"] != "csv" {
		t.Fatalf("preview = %d %v", code, out)
	}
	rows := previewRows(t, out)
	if r := rows["Rent"]; r["target"] != "cat:"+rentID || r["status"] != "changed" || r["old_amount"] != float64(150000) || r["amount"] != float64(160000) {
		t.Fatalf("rent %v", r)
	}
	if r := rows["groceries"]; !strings.HasPrefix(r["target"].(string), "cat:") || r["timing"].(map[string]any)["anchor"] != "2026-03-01" {
		t.Fatalf("groceries %v", r)
	}
	if r := rows["Vacation fund"]; r["target"] != "new:goals" || r["status"] != "new" {
		t.Fatalf("goal %v", r)
	}
	if r := rows["Transfer"]; r["target"] != "skip" {
		t.Fatalf("transfer %v", r)
	}
	if r := rows["Gadgets"]; !strings.HasPrefix(r["target"].(string), "new:") || !strings.Contains(r["note"].(string), "Mystery") {
		t.Fatalf("unknown group %v", r)
	}
	if r := rows["Hobbies"]; r["amount"] != float64(9000) || r["status"] != "new" {
		t.Fatalf("duplicate keeps the last: %v", r)
	}
	problems := fmt.Sprint(out["problems"])
	if !strings.Contains(problems, "Coffee Shops") || !strings.Contains(problems, "Hobbies is listed twice") {
		t.Fatalf("problems %v", problems)
	}

	// Apply what the preview proposed.
	var apply []map[string]any
	for _, r := range out["rows"].([]any) {
		m := r.(map[string]any)
		apply = append(apply, map[string]any{"target": m["target"], "category": m["category"], "icon": m["icon"], "amount": m["amount"], "timing": m["timing"]})
	}
	body, _ = json.Marshal(map[string]any{"month": "2026-03", "rows": apply})
	code, res := c.do("POST", "/api/budget/import", string(body), true)
	if code != 200 || res["created"] != float64(3) || res["updated"] != float64(2) {
		t.Fatalf("apply = %d %v", code, res)
	}
	// Importing the same thing again creates nothing.
	if _, res = c.do("POST", "/api/budget/import", string(body), true); res["created"] != float64(0) || res["updated"] != float64(0) {
		t.Fatalf("re-apply = %v", res)
	}
	_, b = c.do("GET", "/api/budget?date=2026-05-01", "", false) // amounts apply forward
	rent, _ = findLine(t, b, "Rent")
	hobbies, kind := findLine(t, b, "Hobbies")
	vac, gkind := findLine(t, b, "Vacation fund")
	groc, _ := findLine(t, b, "Groceries")
	if rent["month_budget"] != float64(160000) || rent["chunk"].(map[string]any)["day"] != float64(3) || hobbies["month_budget"] != float64(9000) ||
		kind != "flexible" || gkind != "goals" || vac["month_budget"] != float64(20000) || groc["chunk"].(map[string]any)["kind"] != "every_n_weeks" {
		t.Fatalf("after import: rent %v hobbies %v (%s) vacation %v (%s) groceries %v", rent, hobbies, kind, vac, gkind, groc)
	}
	_, b = c.do("GET", "/api/budget?date=2026-02-01", "", false) // and not backward
	if rent, _ = findLine(t, b, "Rent"); rent["month_budget"] != float64(0) {
		t.Fatalf("february rent %v", rent)
	}

	// Bad targets are refused.
	if code, _ := c.do("POST", "/api/budget/import", `{"month":"2026-03","rows":[{"target":"cat:99999","category":"x","amount":1}]}`, true); code != 400 {
		t.Fatalf("unknown category = %d", code)
	}
	if code, _ := c.do("POST", "/api/budget/import/preview", `{"month":"2026-03"}`, true); code != 400 {
		t.Fatalf("empty preview = %d", code)
	}

	// AI: pasted text and a screenshot, read by the multimodal model (the fake).
	body, _ = json.Marshal(map[string]any{"month": "2026-03", "text": "Dining out: $300\nVacation 2400/year\nTotal 2700"})
	code, out = c.do("POST", "/api/budget/import/preview", string(body), true)
	rows = previewRows(t, out)
	if code != 200 || out["source"] != "ai" || out["model"] != "test/model" || rows["Restaurants & Bars"]["status"] != "changed" || rows["Vacation"]["amount"] != float64(20000) {
		t.Fatalf("ai text = %d %v", code, out)
	}
	c.do("PATCH", "/api/settings/ai", `{"vision_model":"vision/model"}`, true)
	png := "data:image/png;base64,iVBORw0KGgo="
	body, _ = json.Marshal(map[string]any{"month": "2026-03", "images": []string{png}})
	code, out = c.do("POST", "/api/budget/import/preview", string(body), true)
	rows = previewRows(t, out)
	if code != 200 || out["model"] != "vision/model" || rows["Rent"]["target"] != "cat:"+rentID || rows["Hobbies"]["old_amount"] != float64(9000) {
		t.Fatalf("ai image = %d %v", code, out)
	}
	if code, _ := c.do("POST", "/api/budget/import/preview", `{"images":["data:text/html;base64,AAAA"]}`, true); code != 400 {
		t.Fatalf("bad image = %d", code)
	}
	if _, s := c.do("GET", "/api/settings/ai", "", false); s["vision_model"] != "vision/model" || s["vision_ready"] != true {
		t.Fatalf("ai settings %v", s)
	}
	if _, res := c.do("POST", "/api/settings/ai/test", `{"target":"vision"}`, true); res["ok"] != true || res["model"] != "vision/model" {
		t.Fatalf("vision test %v", res)
	}
}
