package reports

import (
	"reflect"
	"testing"
	"time"
)

func d(s string) time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return t
}

func TestBuckets(t *testing.T) {
	got := Buckets(d("2026-02-15"), d("2026-04-10"), Month)
	want := []Bucket{
		{"2026-02", "2026-02-15", "2026-02-28"},
		{"2026-03", "2026-03-01", "2026-03-31"},
		{"2026-04", "2026-04-01", "2026-04-10"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("month buckets = %+v", got)
	}
	q := Buckets(d("2025-11-01"), d("2026-09-30"), Quarter)
	if len(q) != 4 || q[0].Key != "2025-Q4" || q[3].Key != "2026-Q3" || q[3].End != "2026-09-30" {
		t.Fatalf("quarter buckets = %+v", q)
	}
	y := Buckets(d("2025-06-01"), d("2026-01-01"), Year)
	if len(y) != 2 || y[0].Key != "2025" || y[1].Start != "2026-01-01" {
		t.Fatalf("year buckets = %+v", y)
	}
}

func TestBuild(t *testing.T) {
	cats := map[int64]Category{
		1: {ID: 1, Name: "Paychecks", GroupID: 10, GroupName: "Income"},
		2: {ID: 2, Name: "Groceries", Icon: "🛒", GroupID: 20, GroupName: "Flexible"},
		3: {ID: 3, Name: "Rent", GroupID: 30, GroupName: "Fixed"},
		4: {ID: 4, Name: "Credit Card Payment", GroupID: 40, GroupName: "Transfers"},
	}
	rows := []Row{
		{"2026-08-01", 1, "income", "Acme", 300000},
		{"2026-08-02", 3, "fixed", "Landlord", -150000},
		{"2026-08-05", 2, "flexible", "Safeway", -8000},
		{"2026-08-09", 2, "flexible", "Safeway", 1000}, // refund nets against spending
		{"2026-09-03", 2, "flexible", "Trader Joe's", -6000},
		{"2026-09-04", 4, "transfer", "Card", -50000}, // left out
		{"2026-09-05", 0, "", "Mystery", -2500},        // uncategorized spending
		{"2026-09-06", 0, "", "Venmo", 4000},           // uncategorized income
		{"2026-10-01", 2, "flexible", "Safeway", -999}, // outside buckets
	}
	b := Buckets(d("2026-08-01"), d("2026-09-30"), Month)
	r := Build(rows, cats, b, Month, ByCategory)
	if r.Income.Total != 304000 || !reflect.DeepEqual(r.Income.Values, []int64{300000, 4000}) {
		t.Errorf("income = %+v", r.Income)
	}
	if r.Spending.Total != 165500 || !reflect.DeepEqual(r.Spending.Values, []int64{157000, 8500}) {
		t.Errorf("spending = %+v", r.Spending)
	}
	names := func(ls []Line) (out []string) {
		for _, l := range ls {
			out = append(out, l.Name)
		}
		return
	}
	if got := names(r.Spending.Lines); !reflect.DeepEqual(got, []string{"Rent", "Groceries", "Uncategorized"}) {
		t.Errorf("spending lines = %v", got)
	}
	if g := r.Spending.Lines[1]; g.Icon != "🛒" || g.Total != 13000 || !reflect.DeepEqual(g.Values, []int64{7000, 6000}) {
		t.Errorf("groceries = %+v", g)
	}

	byGroup := Build(rows, cats, b, Month, ByGroup)
	if got := names(byGroup.Spending.Lines); !reflect.DeepEqual(got, []string{"Fixed", "Flexible", "Uncategorized"}) {
		t.Errorf("group lines = %v", got)
	}
	byMerchant := Build(rows, cats, b, Month, ByMerchant)
	if got := names(byMerchant.Spending.Lines); !reflect.DeepEqual(got, []string{"Landlord", "Safeway", "Trader Joe's", "Mystery"}) {
		t.Errorf("merchant lines = %v", got)
	}
}

func TestBuildTree(t *testing.T) {
	cats := map[int64]Category{
		1: {ID: 1, Name: "Groceries", GroupID: 10, GroupName: "Flexible", GroupKind: "flexible"},
		2: {ID: 2, Name: "Rent", GroupID: 11, GroupName: "Fixed", GroupKind: "fixed"},
		3: {ID: 3, Name: "Paychecks", GroupID: 12, GroupName: "Income", GroupKind: "income"},
		4: {ID: 4, Name: "Transfer", GroupID: 13, GroupName: "Transfers", GroupKind: "transfer"},
	}
	goals := map[int64]Goal{7: {ID: 7, Name: "Trip", Icon: "✈️"}}
	rows := []TreeRow{
		{CategoryID: 1, Kind: "flexible", Merchant: "Kroger", Total: -8000, Count: 2},
		{CategoryID: 1, Kind: "flexible", Merchant: "Aldi", Total: -3000, Count: 1},
		{CategoryID: 1, Kind: "flexible", Merchant: "Kroger", Total: 1000, Count: 1}, // refund, different row
		{CategoryID: 2, Kind: "fixed", Merchant: "Landlord", Total: -150000, Count: 1},
		{CategoryID: 3, Kind: "income", Merchant: "ACME", Total: 400000, Count: 2},
		{CategoryID: 4, Kind: "transfer", Merchant: "To savings", Total: -50000, Count: 1},
		{CategoryID: 4, Kind: "transfer", Merchant: "To savings", GoalID: 7, Total: 20000, Count: 1},
		{CategoryID: 0, Merchant: "Mystery", Total: -500, Count: 1},
		{CategoryID: 0, Merchant: "Venmo", Total: 2500, Count: 1},
	}
	tr := BuildTree(rows, cats, goals)
	if tr.Income.Total != 402500 || len(tr.Income.Children) != 2 {
		t.Fatalf("income = %d, %d children", tr.Income.Total, len(tr.Income.Children))
	}
	sp := tr.Spending
	if sp.Total != 150000+10000+20000+500 {
		t.Fatalf("spending total = %d", sp.Total)
	}
	names := []string{}
	for _, c := range sp.Children {
		names = append(names, c.Name)
	}
	if len(names) != 4 || names[0] != "Fixed" || names[1] != "Contributions" || names[2] != "Flexible" || names[3] != "Uncategorized" {
		t.Fatalf("sections = %v", names)
	}
	groc := sp.Children[2].Children[0]
	if groc.Name != "Groceries" || groc.Total != 10000 || groc.Count != 4 || len(groc.Children) != 2 || groc.Children[0].Name != "Kroger" || groc.Children[0].Total != 7000 {
		t.Fatalf("groceries = %+v", groc)
	}
	trip := sp.Children[1].Children[0]
	if trip.Kind != "goal" || trip.ID != 7 || trip.Children[0].Name != "To savings" {
		t.Fatalf("goal = %+v", trip)
	}
	if un := sp.Children[3]; un.Children[0].Kind != "merchant" || un.Children[0].Name != "Mystery" {
		t.Fatalf("uncategorized = %+v", un)
	}
}
