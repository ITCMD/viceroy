// Package closeout reviews a finished budget month: where it went over or under, how much
// was left (to put toward goals or next month's budget), and how likely next month is to run
// over. It is pure; the server loads the budget views and stores the result.
package closeout

import (
	"math"
	"sort"
	"time"

	"viceroy/internal/budget"
	"viceroy/internal/budgetview"
)

// OpenDays (the month's last days) and GraceDays (the next month's first days) bound when a
// month can be closed out.
const (
	OpenDays  = 2
	GraceDays = 7
)

// Window is the month that can be closed out on today, from its last OpenDays days through
// the next month's first GraceDays days, and the last day it can be; ok is false between.
func Window(today time.Time) (month time.Time, closes time.Time, ok bool) {
	switch {
	case today.Day() <= GraceDays:
		month = budget.MonthStart(today).AddDate(0, -1, 0)
	case today.Day() > budget.DaysIn(today)-OpenDays:
		month = budget.MonthStart(today)
	default:
		return time.Time{}, time.Time{}, false
	}
	return month, month.AddDate(0, 1, GraceDays-1), true
}

// Line is one category in the review.
type Line struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Icon   string `json:"icon"`
	Group  string `json:"group"`
	Kind   string `json:"kind"`
	Budget int64  `json:"budget"`
	Actual int64  `json:"actual"`
	Diff   int64  `json:"diff"` // budget − actual: positive = under budget
	Hidden bool   `json:"hidden,omitempty"`
}

// Review is a month's budget against what was spent.
type Review struct {
	Month string `json:"month"`
	// Fixed and flexible spending. Non-monthly categories keep their own leftovers (rollover),
	// so they're listed apart and left out of the surplus.
	Budget  int64 `json:"budget"`
	Actual  int64 `json:"actual"`
	Net     int64 `json:"net"`     // Budget − Actual: positive = underspent overall
	Surplus int64 `json:"surplus"` // Net when positive: the money a close-out can put somewhere
	// Over and Under list categories most over (or under) first; OnTarget counts the rest.
	Over       []Line `json:"over"`
	Under      []Line `json:"under"`
	OnTarget   int    `json:"on_target"`
	NonMonthly []Line `json:"non_monthly"`
	// Income against plan: money "left over" from spending is only real if income arrived.
	IncomeBudget int64 `json:"income_budget"`
	IncomeActual int64 `json:"income_actual"`
	GoalsBudget  int64 `json:"goals_budget"`
	GoalsActual  int64 `json:"goals_actual"`
}

// BuildReview reviews a month budget view. Categories hidden from the budget count when they
// had spending (it's real money) and are marked Hidden.
func BuildReview(v budgetview.View) Review {
	r := Review{Month: v.Month, Over: []Line{}, Under: []Line{}, NonMonthly: []Line{}}
	for _, g := range v.Groups {
		switch g.Kind {
		case "income":
			r.IncomeBudget += g.Budget
			r.IncomeActual += g.Actual
			continue
		case "goals":
			r.GoalsBudget += g.Budget
			r.GoalsActual += g.Actual
			continue
		}
		for _, l := range g.Lines {
			if l.Budget == 0 && l.Actual == 0 {
				continue
			}
			line := Line{ID: l.ID, Name: l.Name, Icon: l.Icon, Group: g.Name, Kind: g.Kind, Budget: l.Budget, Actual: l.Actual, Diff: l.Budget - l.Actual, Hidden: l.Hidden}
			if g.Kind == "non_monthly" {
				r.NonMonthly = append(r.NonMonthly, line)
				continue
			}
			r.Budget += l.Budget
			r.Actual += l.Actual
			switch {
			case line.Diff < 0:
				r.Over = append(r.Over, line)
			case line.Diff > 0:
				r.Under = append(r.Under, line)
			default:
				r.OnTarget++
			}
		}
	}
	sort.SliceStable(r.Over, func(i, j int) bool { return r.Over[i].Diff < r.Over[j].Diff })
	sort.SliceStable(r.Under, func(i, j int) bool { return r.Under[i].Diff > r.Under[j].Diff })
	r.Net = r.Budget - r.Actual
	r.Surplus = max(0, r.Net)
	return r
}

// NextLine is one category's outlook for the month after the closed one.
type NextLine struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Icon       string `json:"icon"`
	Group      string `json:"group"`
	Budget     int64  `json:"budget"`      // next month's budget
	LastActual int64  `json:"last_actual"` // spent in the closed month
	Average    int64  `json:"average"`     // average monthly spending over the history months
	Recurring  int64  `json:"recurring"`   // recurring charges expected next month
	Expected   int64  `json:"expected"`    // the larger of Average and Recurring
	Gap        int64  `json:"gap"`         // Expected − Budget (positive = likely over)
}

// Outlook is next month's risk of overspending, judged from recent months.
type Outlook struct {
	Month    string     `json:"month"`
	Score    int        `json:"score"`
	Level    string     `json:"level"`
	Budget   int64      `json:"budget"`
	Expected int64      `json:"expected"`
	Over     int64      `json:"over"`    // sum of the positive gaps
	AtRisk   []NextLine `json:"at_risk"` // largest gap first
	// Months is how many past months the averages use.
	Months int `json:"months"`
}

// minGap ignores small gaps between expected spending and the budget.
const minGap = 10_00

// BuildOutlook compares next month's budget (next) with spending in past month views
// (history, the closed month last) and recurring charges due next month (by category). It
// counts the same categories as the dashboard's risk meter: expenses not hidden from the
// budget, without Debt Repayment split by debt.
func BuildOutlook(next budgetview.View, history []budgetview.View, recurring map[int64]int64) Outlook {
	spent := map[int64][]int64{}
	for _, h := range history {
		for _, g := range h.Groups {
			for _, l := range g.Lines {
				spent[l.ID] = append(spent[l.ID], l.Actual)
			}
		}
	}
	months := max(1, len(history))
	o := Outlook{Month: next.Month, AtRisk: []NextLine{}, Months: len(history)}
	for _, g := range next.Groups {
		if g.Kind == "income" || g.Kind == "goals" {
			continue
		}
		for _, l := range g.Lines {
			if l.Hidden || len(l.Lines) > 0 {
				continue
			}
			past := spent[l.ID]
			var total, last int64
			for _, v := range past {
				total += v
			}
			if len(past) > 0 {
				last = past[len(past)-1]
			}
			avg := int64(math.Round(float64(total) / float64(months)))
			n := NextLine{ID: l.ID, Name: l.Name, Icon: l.Icon, Group: g.Name, Budget: l.Budget, LastActual: last, Average: avg, Recurring: recurring[l.ID]}
			n.Expected = max(avg, n.Recurring)
			if g.Kind == "non_monthly" {
				// The budget includes the fund carried in; spending is lumpy, so only
				// known recurring charges count against it.
				n.Expected = n.Recurring
			}
			if n.Budget == 0 && n.Expected == 0 {
				continue
			}
			n.Gap = n.Expected - n.Budget
			o.Budget += n.Budget
			o.Expected += n.Expected
			if n.Gap >= minGap {
				o.Over += n.Gap
				o.AtRisk = append(o.AtRisk, n)
			}
		}
	}
	sort.SliceStable(o.AtRisk, func(i, j int) bool { return o.AtRisk[i].Gap > o.AtRisk[j].Gap })
	o.Score = budgetview.RiskScore(o.Over, o.Budget)
	o.Level = budgetview.RiskLevel(o.Score)
	return o
}
