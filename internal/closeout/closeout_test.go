package closeout

import (
	"testing"

	"viceroy/internal/budget"
	"viceroy/internal/budgetview"
)

func TestWindow(t *testing.T) {
	for today, want := range map[string]string{
		"2026-09-28": "",        // too early
		"2026-09-29": "2026-09", // last two days of September
		"2026-09-30": "2026-09",
		"2026-10-01": "2026-09",
		"2026-10-07": "2026-09", // last grace day
		"2026-10-08": "",
		"2026-02-27": "2026-02", // 28-day February
		"2026-01-03": "2025-12",
	} {
		d, _ := budget.ParseDate(today)
		m, closes, ok := Window(d)
		got := ""
		if ok {
			got = budget.MonthKey(m)
			if wantCloses := m.AddDate(0, 1, 6); !closes.Equal(wantCloses) {
				t.Errorf("%s closes %v", today, closes)
			}
		}
		if got != want {
			t.Errorf("Window(%s) = %q, want %q", today, got, want)
		}
	}
}

func line(id int64, name string, b, a int64) budgetview.Line {
	return budgetview.Line{ID: id, Name: name, Budget: b, Actual: a}
}

func TestBuildReview(t *testing.T) {
	hidden := line(6, "Water", 0, 30_00)
	hidden.Hidden = true
	v := budgetview.View{Month: "2026-09", Groups: []budgetview.Group{
		{Kind: "income", Budget: 5000_00, Actual: 4800_00, Lines: []budgetview.Line{line(1, "Pay", 5000_00, 4800_00)}},
		{Kind: "fixed", Name: "Fixed", Lines: []budgetview.Line{line(2, "Rent", 1500_00, 1500_00), hidden}},
		{Kind: "flexible", Name: "Flexible", Lines: []budgetview.Line{
			line(3, "Dining", 300_00, 380_00), line(4, "Groceries", 600_00, 450_00), line(5, "Fun", 100_00, 90_00), line(7, "Unused", 0, 0),
		}},
		{Kind: "non_monthly", Name: "Non-monthly", Lines: []budgetview.Line{line(8, "Car", 900_00, 0)}},
		{Kind: "goals", Budget: 200_00, Actual: 200_00},
	}}
	r := BuildReview(v)
	// 2500 budget − 2450 spent (water counts) = 50 left.
	if r.Budget != 2500_00 || r.Actual != 2450_00 || r.Net != 50_00 || r.Surplus != 50_00 || r.OnTarget != 1 {
		t.Fatalf("review = %+v", r)
	}
	if len(r.Over) != 2 || r.Over[0].Name != "Dining" || r.Over[1].Name != "Water" || !r.Over[1].Hidden {
		t.Fatalf("over = %+v", r.Over)
	}
	if len(r.Under) != 2 || r.Under[0].Name != "Groceries" || r.Under[0].Diff != 150_00 {
		t.Fatalf("under = %+v", r.Under)
	}
	if len(r.NonMonthly) != 1 || r.IncomeActual != 4800_00 || r.GoalsActual != 200_00 {
		t.Fatalf("review = %+v", r)
	}
	v.Groups[2].Lines[0].Actual = 600_00 // now 170 over overall
	if r := BuildReview(v); r.Net != -170_00 || r.Surplus != 0 {
		t.Fatalf("overspent month = %+v", r)
	}
}

func TestBuildOutlook(t *testing.T) {
	month := func(dining, groc int64) budgetview.View {
		return budgetview.View{Groups: []budgetview.Group{{Kind: "flexible", Lines: []budgetview.Line{line(3, "Dining", 0, dining), line(4, "Groceries", 0, groc)}}}}
	}
	hidden := line(6, "Water", 0, 0)
	hidden.Hidden = true
	next := budgetview.View{Month: "2026-10", Groups: []budgetview.Group{
		{Kind: "fixed", Lines: []budgetview.Line{line(2, "Phone", 50_00, 0), hidden}},
		{Kind: "flexible", Lines: []budgetview.Line{line(3, "Dining", 300_00, 0), line(4, "Groceries", 600_00, 0)}},
		{Kind: "non_monthly", Lines: []budgetview.Line{line(8, "Car", 900_00, 0)}},
	}}
	o := BuildOutlook(next, []budgetview.View{month(350_00, 500_00), month(400_00, 550_00), month(450_00, 600_00)},
		map[int64]int64{2: 80_00, 6: 40_00, 8: 100_00})
	// Dining averages 400 vs 300 (gap 100); Phone's recurring 80 vs 50 (gap 30); Car's lumpy
	// average is ignored and its bill fits; hidden Water doesn't count.
	if len(o.AtRisk) != 2 || o.AtRisk[0].Name != "Dining" || o.AtRisk[0].Gap != 100_00 || o.AtRisk[1].Gap != 30_00 || o.Over != 130_00 {
		t.Fatalf("outlook = %+v", o)
	}
	if o.Budget != 1850_00 || o.Score != budgetview.RiskScore(130_00, 1850_00) || o.Level != "moderate" {
		t.Fatalf("score = %+v", o)
	}
}
