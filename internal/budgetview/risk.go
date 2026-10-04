package budgetview

import (
	"math"
	"sort"
)

// RiskDriver is one category that is on track to end the period over budget.
type RiskDriver struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Icon      string `json:"icon"`
	Budget    int64  `json:"budget"`
	Actual    int64  `json:"actual"`
	Planned   int64  `json:"planned"`   // what the plan allows through PaceThrough (budget for unpaced lines)
	Upcoming  int64  `json:"upcoming"`  // recurring charges still to come
	Projected int64  `json:"projected"` // expected spending by the end of the period
	Over      int64  `json:"over"`      // Projected − Budget
	// Why: "over" (already over budget), "upcoming" (recurring charges will push it over),
	// "pace" (spending ahead of the plan through this week).
	Reason string `json:"reason"`
}

// Risk is the chance of ending the period over budget, from weekly pacing.
type Risk struct {
	Score       int    `json:"score"` // 0..100
	Level       string `json:"level"` // low | moderate | high | very_high
	Month       string `json:"month"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Today       string `json:"today"`
	PaceThrough string `json:"pace_through"`
	Budget      int64  `json:"budget"`    // counted lines
	Actual      int64  `json:"actual"`    // counted lines
	Projected   int64  `json:"projected"` // counted lines, by the end of the period
	// ProjectedOver sums each category's projected overspending; underspending elsewhere
	// doesn't offset it (Projected − Budget is the net).
	ProjectedOver int64        `json:"projected_over"`
	Drivers       []RiskDriver `json:"drivers"`
	Counted       int          `json:"counted"` // categories counted
}

// RiskFullShare is the projected overspending, as a share of the budget, that scores 100.
const RiskFullShare = 0.15

// minRiskOver ignores tiny projected overages.
const minRiskOver = 5_00

// riskLine projects one expense line to the end of the period: what is spent plus the rest of
// the plan, starting from the end of this week (so spending ahead of the week's plan carries
// through), and at least what is spent plus recurring charges still to come.
func riskLine(l Line, inPeriod bool) (projected, planned int64, reason string) {
	projected, planned = l.Actual, l.Budget
	if inPeriod {
		if !l.Chunk.NoPacing {
			planned = l.WeekExpected
		}
		projected = l.Actual + max(0, l.Budget-planned)
	}
	reason = "pace"
	if l.Actual > l.Budget {
		reason = "over"
	}
	if withBills := l.Actual + l.Upcoming; withBills > projected {
		projected = withBills
		if reason != "over" {
			reason = "upcoming"
		}
	}
	return projected, planned, reason
}

// ComputeRisk scores the risk of overspending the view's period. It counts expense categories
// (fixed, flexible, non-monthly) except ones hidden from the budget and Debt Repayment split by
// debt (paying debt ahead of plan isn't overspending).
func ComputeRisk(v View) Risk {
	out := Risk{Month: v.Month, Start: v.Start, End: v.End, Today: v.Today, PaceThrough: v.PaceThrough, Drivers: []RiskDriver{}}
	inPeriod := v.PaceThrough != ""
	for _, g := range v.Groups {
		if g.Kind == "income" || g.Kind == "goals" {
			continue
		}
		for _, l := range g.Lines {
			if l.Hidden || len(l.Lines) > 0 || (l.Budget <= 0 && l.Actual <= 0) {
				continue
			}
			projected, planned, reason := riskLine(l, inPeriod)
			out.Counted++
			out.Budget += l.Budget
			out.Actual += l.Actual
			out.Projected += projected
			if over := projected - l.Budget; over >= minRiskOver {
				out.ProjectedOver += over
				out.Drivers = append(out.Drivers, RiskDriver{
					ID: l.ID, Name: l.Name, Icon: l.Icon, Budget: l.Budget, Actual: l.Actual, Planned: planned,
					Upcoming: l.Upcoming, Projected: projected, Over: over, Reason: reason,
				})
			}
		}
	}
	sort.SliceStable(out.Drivers, func(i, j int) bool { return out.Drivers[i].Over > out.Drivers[j].Over })
	out.Score = RiskScore(out.ProjectedOver, out.Budget)
	out.Level = RiskLevel(out.Score)
	return out
}

// RiskScore turns projected overspending against a budget into a 0..100 score.
func RiskScore(over, budget int64) int {
	switch {
	case over <= 0:
		return 0
	case budget <= 0:
		return 100
	}
	return min(100, int(math.Round(float64(over)/float64(budget)/RiskFullShare*100)))
}

// RiskLevel names a 0..100 risk score.
func RiskLevel(score int) string {
	switch {
	case score < 25:
		return "low"
	case score < 50:
		return "moderate"
	case score < 75:
		return "high"
	}
	return "very_high"
}
