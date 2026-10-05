package budgetview

import (
	"testing"

	"viceroy/internal/budget"
)

func TestComputeRisk(t *testing.T) {
	l := func(id int64, b, a, week int64) Line {
		return Line{ID: id, Name: "Cat", Budget: b, Actual: a, WeekExpected: week, Chunk: budget.Chunk{Kind: budget.Even}}
	}
	noPace := l(5, 100_00, 150_00, 0)
	noPace.Chunk.NoPacing = true
	hidden := l(6, 0, 500_00, 0)
	hidden.Hidden = true
	bills := l(7, 200_00, 120_00, 150_00)
	bills.Upcoming = 100_00
	v := View{Month: "2026-10", PaceThrough: "2026-10-10", Groups: []Group{
		{Kind: "income", Lines: []Line{l(1, 5000_00, 0, 0)}},
		{Kind: "flexible", Lines: []Line{
			l(2, 400_00, 200_00, 100_00), // $100 ahead of this week's plan > ends $100 over
			l(3, 400_00, 50_00, 100_00),  // behind: fine
			l(4, 300_00, 90_00, 70_00),   // $20 ahead
			noPace,                       // already $50 over
			hidden,                       // excluded from the budget
			bills,                        // $120 + $100 of bills = $20 over
		}},
	}}
	r := ComputeRisk(v)
	if r.Counted != 5 || r.Budget != 1400_00 || r.ProjectedOver != 190_00 || len(r.Drivers) != 4 {
		t.Fatalf("risk = %+v", r)
	}
	if d := r.Drivers[0]; d.ID != 2 || d.Over != 100_00 || d.Reason != "pace" || d.Planned != 100_00 {
		t.Fatalf("top driver = %+v", d)
	}
	reasons := map[int64]string{}
	for _, d := range r.Drivers {
		reasons[d.ID] = d.Reason
	}
	if reasons[5] != "over" || reasons[7] != "upcoming" {
		t.Fatalf("reasons = %v", reasons)
	}
	// 190 / 1400 = 13.6% of the budget; 15% scores 100.
	if r.Score != 90 || r.Level != "very_high" {
		t.Fatalf("score = %d %s", r.Score, r.Level)
	}

	// After the month, projections are just what was spent.
	v.PaceThrough = ""
	if r := ComputeRisk(v); r.ProjectedOver != 70_00 {
		t.Fatalf("closed month over = %d", r.ProjectedOver)
	}
	if RiskLevel(0) != "low" || RiskLevel(30) != "moderate" || RiskLevel(60) != "high" || RiskScore(0, 0) != 0 || RiskScore(5, 0) != 100 {
		t.Fatal("levels")
	}
}
