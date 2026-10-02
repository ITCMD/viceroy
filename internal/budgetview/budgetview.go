// Package budgetview computes the budget screen (per-category allowances, actuals and pacing)
// from the database. The HTTP API, the notifier and the AI chat tools all use it.
package budgetview

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"viceroy/internal/budget"
	"viceroy/internal/db"
)

// ---- household budget settings ----

const (
	setForwardDefault = "budget.forward_default"
	setWeekStart      = "budget.week_start"
	setPaySchedule    = "budget.pay_schedule"
	setUpcomingWindow = "budget.upcoming_window"
)

type Settings struct {
	ForwardDefault bool               `json:"forward_default"` // "apply to all future months" starts on
	WeekStart      int                `json:"week_start"`      // 0 = Sunday
	PaySchedule    budget.PaySchedule `json:"pay_schedule"`
	// UpcomingWindow is how far ahead the budget marks recurring charges still to come:
	// "week" (the next 7 days) or "paycheck" (until the next payday).
	UpcomingWindow string `json:"upcoming_window"`
}

func LoadSettings(ctx context.Context, q *db.Queries, hh int64) (Settings, error) {
	out := Settings{PaySchedule: budget.DefaultPaySchedule, UpcomingWindow: "week"}
	rows, err := q.ListHouseholdSettings(ctx, hh)
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		switch r.Key {
		case setForwardDefault:
			out.ForwardDefault = r.Value == "1"
		case setWeekStart:
			if n, err := strconv.Atoi(r.Value); err == nil && n >= 0 && n <= 6 {
				out.WeekStart = n
			}
		case setUpcomingWindow:
			if r.Value == "week" || r.Value == "paycheck" {
				out.UpcomingWindow = r.Value
			}
		case setPaySchedule:
			var ps budget.PaySchedule
			if json.Unmarshal([]byte(r.Value), &ps) == nil && ps.Validate() == nil {
				out.PaySchedule = ps
			}
		}
	}
	return out, nil
}

// SaveSettings applies the non-nil fields; it returns a user-facing error for bad input.
func SaveSettings(ctx context.Context, q *db.Queries, hh int64, forward *bool, weekStart *int, pay *budget.PaySchedule, upcoming *string) (string, error) {
	set := func(k, v string) error {
		return q.SetHouseholdSetting(ctx, db.SetHouseholdSettingParams{HouseholdID: hh, Key: k, Value: v})
	}
	if weekStart != nil && (*weekStart < 0 || *weekStart > 6) {
		return "Week start must be a weekday.", nil
	}
	if pay != nil {
		if err := pay.Validate(); err != nil {
			return "Pay schedule: " + err.Error() + ".", nil
		}
	}
	if upcoming != nil {
		if *upcoming != "week" && *upcoming != "paycheck" {
			return "Upcoming window must be week or paycheck.", nil
		}
		if err := set(setUpcomingWindow, *upcoming); err != nil {
			return "", err
		}
	}
	if forward != nil {
		v := "0"
		if *forward {
			v = "1"
		}
		if err := set(setForwardDefault, v); err != nil {
			return "", err
		}
	}
	if weekStart != nil {
		if err := set(setWeekStart, strconv.Itoa(*weekStart)); err != nil {
			return "", err
		}
	}
	if pay != nil {
		b, _ := json.Marshal(pay)
		if err := set(setPaySchedule, string(b)); err != nil {
			return "", err
		}
	}
	return "", nil
}

// today is the server's local date; transaction dates are local dates too.
func Today() time.Time {
	n := time.Now()
	return budget.Date(n.Year(), n.Month(), n.Day())
}

// ---- budget view ----

type Line struct {
	ID          int64        `json:"id"`
	Name        string       `json:"name"`
	Icon        string       `json:"icon"`
	Budget      int64        `json:"budget"`       // allowance for the period
	Actual      int64        `json:"actual"`       // spent (expenses, goals) or received (income)
	Expected    int64        `json:"expected"`     // pacing: allowance through today, 0 outside the period
	MonthBudget int64        `json:"month_budget"` // the editable monthly amount (month of the period start)
	Chunk       budget.Chunk `json:"chunk"`
	Hidden      bool         `json:"hidden"` // hidden from the budget (still counted in totals)
	Upcoming    int64        `json:"upcoming"` // recurring charges still to come in the upcoming window (set by the API)
	// Rollover is what a non-monthly category carries into this month from earlier ones
	// (unspent budget, or overspending when negative). Budget already includes it.
	Rollover int64 `json:"rollover"`
}

type Group struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"` // income | fixed | flexible | non_monthly | goals
	Budget int64  `json:"budget"`
	Actual int64  `json:"actual"`
	Lines  []Line `json:"lines"`
	// The part of Budget that is rollover rather than this period's plan.
	Rollover int64 `json:"rollover"`
}

type Summary struct {
	IncomeBudget   int64 `json:"income_budget"`
	IncomeActual   int64 `json:"income_actual"`
	ExpenseBudget  int64 `json:"expense_budget"`
	ExpenseActual  int64 `json:"expense_actual"`
	GoalsBudget    int64 `json:"goals_budget"`
	GoalsActual    int64 `json:"goals_actual"`
	LeftToBudget   int64 `json:"left_to_budget"` // income − expenses − goals, budgeted
	LeftActual     int64 `json:"left_actual"`    // the same, actual
	UnbudgetedCats int   `json:"unbudgeted_spend_count"`
}

type View struct {
	View     string   `json:"view"`
	Start    string   `json:"start"` // inclusive
	End      string   `json:"end"`   // inclusive (last day)
	Prev     string   `json:"prev"`  // a date in the previous period
	Next     string   `json:"next"`
	Month    string   `json:"month"` // YYYY-MM whose amounts the editor changes
	Today    string   `json:"today"`
	Settings Settings `json:"settings"`
	Groups   []Group  `json:"groups"`
	Summary  Summary  `json:"summary"`
}

// Totals indexes signed per-day totals by owner (category or goal) id.
type Totals map[int64]map[time.Time]int64

// sum returns the owner's total over [from, to).
func (d Totals) Sum(id int64, from, to time.Time) int64 {
	var t int64
	for day, v := range d[id] {
		if !day.Before(from) && day.Before(to) {
			t += v
		}
	}
	return t
}

func (d Totals) add(id int64, date string, v int64) {
	day, err := budget.ParseDate(date)
	if err != nil {
		return
	}
	if d[id] == nil {
		d[id] = map[time.Time]int64{}
	}
	d[id][day] += v
}

func LoadCategoryTotals(ctx context.Context, q *db.Queries, hh int64, from, to time.Time) (Totals, error) {
	rows, err := q.DailyCategoryTotals(ctx, db.DailyCategoryTotalsParams{
		HouseholdID: hh, FromDate: budget.FormatDate(from), ToDate: budget.FormatDate(to)})
	if err != nil {
		return nil, err
	}
	out := Totals{}
	for _, r := range rows {
		out.add(r.CategoryID.Int64, r.Date, r.Total)
	}
	return out, nil
}

func LoadGoalTotals(ctx context.Context, q *db.Queries, hh int64, from, to time.Time) (Totals, error) {
	rows, err := q.DailyGoalTotals(ctx, db.DailyGoalTotalsParams{
		HouseholdID: hh, FromDate: budget.FormatDate(from), ToDate: budget.FormatDate(to)})
	if err != nil {
		return nil, err
	}
	out := Totals{}
	for _, r := range rows {
		out.add(r.GoalID.Int64, r.Date, r.Total)
	}
	return out, nil
}

// Amounts groups stored budget amounts by category and by goal.
type Amounts struct {
	Cats, Goals map[int64][]budget.AmountRow
}

func LoadAmounts(ctx context.Context, q *db.Queries, hh int64) (Amounts, error) {
	rows, err := q.ListBudgetAmounts(ctx, hh)
	if err != nil {
		return Amounts{}, err
	}
	idx := Amounts{Cats: map[int64][]budget.AmountRow{}, Goals: map[int64][]budget.AmountRow{}}
	for _, r := range rows {
		a := budget.AmountRow{Month: r.Month, Amount: r.AmountCents, Forward: r.Forward == 1}
		if r.CategoryID.Valid {
			idx.Cats[r.CategoryID.Int64] = append(idx.Cats[r.CategoryID.Int64], a)
		} else {
			idx.Goals[r.GoalID.Int64] = append(idx.Goals[r.GoalID.Int64], a)
		}
	}
	return idx, nil
}

func ParseChunk(s string) budget.Chunk {
	var c budget.Chunk
	if s == "" || json.Unmarshal([]byte(s), &c) != nil || c.Validate() != nil {
		return budget.Chunk{Kind: budget.Even}
	}
	return c
}

// Kinds are the category group kinds shown on the budget, in order. Transfers are left out.
var Kinds = map[string]bool{"income": true, "fixed": true, "flexible": true, "non_monthly": true}

// Rollover reports whether categories of a group kind carry unspent budget and overspending
// into the next month.
func Rollover(kind string) bool { return kind == "non_monthly" }

// Build computes the budget for the period of the given view containing at. now is today's
// date (it decides pacing).
func Build(ctx context.Context, q *db.Queries, hh int64, view budget.View, at, now time.Time) (View, error) {
	st, err := LoadSettings(ctx, q, hh)
	if err != nil {
		return View{}, err
	}
	p := budget.PeriodFor(view, at, time.Weekday(st.WeekStart), st.PaySchedule)
	from := budget.MonthStart(p.Start) // allowances need what was spent earlier in the month

	groups, err := q.ListCategoryGroups(ctx, hh)
	if err != nil {
		return View{}, err
	}
	cats, err := q.ListCategories(ctx, hh)
	if err != nil {
		return View{}, err
	}
	amounts, err := LoadAmounts(ctx, q, hh)
	if err != nil {
		return View{}, err
	}
	// Rollover categories need their spending back to the month they were first budgeted.
	kindOf := map[int64]string{}
	for _, g := range groups {
		kindOf[g.ID] = g.Kind
	}
	totalsFrom := from
	for _, c := range cats {
		if first := budget.FirstMonth(amounts.Cats[c.ID]); Rollover(kindOf[c.GroupID]) && !first.IsZero() && first.Before(totalsFrom) {
			totalsFrom = first
		}
	}
	catTotals, err := LoadCategoryTotals(ctx, q, hh, totalsFrom, p.End)
	if err != nil {
		return View{}, err
	}
	goals, err := q.ListGoals(ctx, hh)
	if err != nil {
		return View{}, err
	}
	goalTotals, err := LoadGoalTotals(ctx, q, hh, from, p.End)
	if err != nil {
		return View{}, err
	}

	month := budget.MonthKey(p.Start)
	out := View{
		View: string(view), Start: budget.FormatDate(p.Start), End: budget.FormatDate(p.End.AddDate(0, 0, -1)),
		Prev: budget.FormatDate(p.Start.AddDate(0, 0, -1)), Next: budget.FormatDate(p.End),
		Month: month, Today: budget.FormatDate(now), Settings: st, Groups: []Group{},
	}
	gidx := map[int64]int{}
	for _, g := range groups {
		if !Kinds[g.Kind] {
			continue
		}
		gidx[g.ID] = len(out.Groups)
		out.Groups = append(out.Groups, Group{ID: g.ID, Name: g.Name, Kind: g.Kind, Lines: []Line{}})
	}
	for _, c := range cats {
		i, ok := gidx[c.GroupID]
		if !ok || c.Archived == 1 {
			continue
		}
		g := &out.Groups[i]
		sign := int64(-1) // expenses are negative amounts; show spending as positive
		if g.Kind == "income" {
			sign = 1
		}
		chunk := ParseChunk(c.Chunk)
		spent := func(a, b time.Time) int64 { return sign * catTotals.Sum(c.ID, a, b) }
		rows := amounts.Cats[c.ID]
		l := budget.ComputeLine(p, chunk, rows, now, spent, nil)
		line := Line{
			ID: c.ID, Name: c.Name, Icon: c.Icon, Budget: l.Budget, Actual: l.Actual, Expected: l.Expected,
			MonthBudget: budget.Resolve(rows, month), Chunk: chunk, Hidden: c.BudgetHidden == 1,
		}
		if Rollover(g.Kind) {
			carry := func(m time.Time) int64 { return budget.Carryover(rows, m, spent) }
			withCarry := budget.ComputeLine(p, chunk, rows, now, spent, carry)
			g.Rollover += withCarry.Budget - l.Budget
			line.Budget, line.Expected, line.Rollover = withCarry.Budget, withCarry.Expected, carry(p.Start)
		}
		if chunk.NoPacing {
			line.Expected = 0
		}
		g.Lines = append(g.Lines, line)
		g.Budget += line.Budget
		g.Actual += line.Actual
		if g.Kind != "income" && line.Budget == 0 && line.Actual > 0 {
			out.Summary.UnbudgetedCats++
		}
	}
	gg := Group{Name: "Contributions", Kind: "goals", Lines: []Line{}}
	for _, g := range goals {
		if g.Archived == 1 {
			continue
		}
		l := budget.ComputeLine(p, budget.Chunk{Kind: budget.Even}, amounts.Goals[g.ID], now, func(a, b time.Time) int64 {
			return goalTotals.Sum(g.ID, a, b)
		}, nil)
		gg.Lines = append(gg.Lines, Line{
			ID: g.ID, Name: g.Name, Icon: g.Icon, Budget: l.Budget, Actual: l.Actual, Expected: l.Expected,
			MonthBudget: budget.Resolve(amounts.Goals[g.ID], month), Chunk: budget.Chunk{Kind: budget.Even},
		})
		gg.Budget += l.Budget
		gg.Actual += l.Actual
	}
	out.Groups = append(out.Groups, gg)

	sm := &out.Summary
	for _, g := range out.Groups {
		switch g.Kind {
		case "income":
			sm.IncomeBudget += g.Budget
			sm.IncomeActual += g.Actual
		case "goals":
			sm.GoalsBudget += g.Budget
			sm.GoalsActual += g.Actual
		default:
			// Rollover is money budgeted (or overspent) in earlier months, not this period's plan.
			sm.ExpenseBudget += g.Budget - g.Rollover
			sm.ExpenseActual += g.Actual
		}
	}
	sm.LeftToBudget = sm.IncomeBudget - sm.ExpenseBudget - sm.GoalsBudget
	sm.LeftActual = sm.IncomeActual - sm.ExpenseActual - sm.GoalsActual
	return out, nil
}
