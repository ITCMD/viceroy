package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/budget"
	"viceroy/internal/db"
	"viceroy/internal/money"
)

func (s *Server) budgetRoutes(r chi.Router) {
	r.Get("/budget", s.handleGetBudget)
	r.Put("/budget/amount", s.handleSetBudgetAmount)
	r.Get("/budget/history", s.handleBudgetHistory)
	r.Put("/budget/categories/{id}/chunk", s.handleSetChunk)
	r.Get("/goals", s.handleListGoals)
	r.Post("/goals", s.handleCreateGoal)
	r.Patch("/goals/{id}", s.handleUpdateGoal)
	r.Delete("/goals/{id}", s.handleDeleteGoal)
}

// ---- household budget settings ----

const (
	setForwardDefault = "budget.forward_default"
	setWeekStart      = "budget.week_start"
	setPaySchedule    = "budget.pay_schedule"
)

type budgetSettings struct {
	ForwardDefault bool               `json:"forward_default"` // "apply to all future months" starts on
	WeekStart      int                `json:"week_start"`      // 0 = Sunday
	PaySchedule    budget.PaySchedule `json:"pay_schedule"`
}

func loadBudgetSettings(ctx context.Context, q *db.Queries, hh int64) (budgetSettings, error) {
	out := budgetSettings{PaySchedule: budget.DefaultPaySchedule}
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
		case setPaySchedule:
			var ps budget.PaySchedule
			if json.Unmarshal([]byte(r.Value), &ps) == nil && ps.Validate() == nil {
				out.PaySchedule = ps
			}
		}
	}
	return out, nil
}

// saveBudgetSettings applies the non-nil fields; it returns a user-facing error for bad input.
func saveBudgetSettings(ctx context.Context, q *db.Queries, hh int64, forward *bool, weekStart *int, pay *budget.PaySchedule) (string, error) {
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
func today() time.Time {
	n := time.Now()
	return budget.Date(n.Year(), n.Month(), n.Day())
}

// ---- budget view ----

type budgetLineDTO struct {
	ID          int64        `json:"id"`
	Name        string       `json:"name"`
	Icon        string       `json:"icon"`
	Budget      int64        `json:"budget"`       // allowance for the period
	Actual      int64        `json:"actual"`       // spent (expenses, goals) or received (income)
	Expected    int64        `json:"expected"`     // pacing: allowance through today, 0 outside the period
	MonthBudget int64        `json:"month_budget"` // the editable monthly amount (month of the period start)
	Chunk       budget.Chunk `json:"chunk"`
}

type budgetGroupDTO struct {
	ID     int64           `json:"id"`
	Name   string          `json:"name"`
	Kind   string          `json:"kind"` // income | fixed | flexible | non_monthly | goals
	Budget int64           `json:"budget"`
	Actual int64           `json:"actual"`
	Lines  []budgetLineDTO `json:"lines"`
}

type budgetSummaryDTO struct {
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

type budgetDTO struct {
	View     string           `json:"view"`
	Start    string           `json:"start"` // inclusive
	End      string           `json:"end"`   // inclusive (last day)
	Prev     string           `json:"prev"`  // a date in the previous period
	Next     string           `json:"next"`
	Month    string           `json:"month"` // YYYY-MM whose amounts the editor changes
	Today    string           `json:"today"`
	Settings budgetSettings   `json:"settings"`
	Groups   []budgetGroupDTO `json:"groups"`
	Summary  budgetSummaryDTO `json:"summary"`
}

// dailyTotals indexes signed per-day totals by owner (category or goal) id.
type dailyTotals map[int64]map[time.Time]int64

// sum returns the owner's total over [from, to).
func (d dailyTotals) sum(id int64, from, to time.Time) int64 {
	var t int64
	for day, v := range d[id] {
		if !day.Before(from) && day.Before(to) {
			t += v
		}
	}
	return t
}

func (d dailyTotals) add(id int64, date string, v int64) {
	day, err := budget.ParseDate(date)
	if err != nil {
		return
	}
	if d[id] == nil {
		d[id] = map[time.Time]int64{}
	}
	d[id][day] += v
}

func loadCategoryTotals(ctx context.Context, q *db.Queries, hh int64, from, to time.Time) (dailyTotals, error) {
	rows, err := q.DailyCategoryTotals(ctx, db.DailyCategoryTotalsParams{
		HouseholdID: hh, FromDate: budget.FormatDate(from), ToDate: budget.FormatDate(to)})
	if err != nil {
		return nil, err
	}
	out := dailyTotals{}
	for _, r := range rows {
		out.add(r.CategoryID.Int64, r.Date, r.Total)
	}
	return out, nil
}

func loadGoalTotals(ctx context.Context, q *db.Queries, hh int64, from, to time.Time) (dailyTotals, error) {
	rows, err := q.DailyGoalTotals(ctx, db.DailyGoalTotalsParams{
		HouseholdID: hh, FromDate: budget.FormatDate(from), ToDate: budget.FormatDate(to)})
	if err != nil {
		return nil, err
	}
	out := dailyTotals{}
	for _, r := range rows {
		out.add(r.GoalID.Int64, r.Date, r.Total)
	}
	return out, nil
}

// amountIndex groups stored budget amounts by category and by goal.
type amountIndex struct {
	cats, goals map[int64][]budget.AmountRow
}

func loadAmounts(ctx context.Context, q *db.Queries, hh int64) (amountIndex, error) {
	rows, err := q.ListBudgetAmounts(ctx, hh)
	if err != nil {
		return amountIndex{}, err
	}
	idx := amountIndex{cats: map[int64][]budget.AmountRow{}, goals: map[int64][]budget.AmountRow{}}
	for _, r := range rows {
		a := budget.AmountRow{Month: r.Month, Amount: r.AmountCents, Forward: r.Forward == 1}
		if r.CategoryID.Valid {
			idx.cats[r.CategoryID.Int64] = append(idx.cats[r.CategoryID.Int64], a)
		} else {
			idx.goals[r.GoalID.Int64] = append(idx.goals[r.GoalID.Int64], a)
		}
	}
	return idx, nil
}

func parseChunk(s string) budget.Chunk {
	var c budget.Chunk
	if s == "" || json.Unmarshal([]byte(s), &c) != nil || c.Validate() != nil {
		return budget.Chunk{Kind: budget.Even}
	}
	return c
}

// budgetKinds are the category group kinds shown on the budget, in order. Transfers are left out.
var budgetKinds = map[string]bool{"income": true, "fixed": true, "flexible": true, "non_monthly": true}

func (s *Server) handleGetBudget(w http.ResponseWriter, r *http.Request) {
	ctx, hh := r.Context(), HouseholdID(r)
	q := db.New(s.db)
	view := budget.View(r.URL.Query().Get("view"))
	if view != budget.ViewWeek && view != budget.ViewPaycheck {
		view = budget.ViewMonth
	}
	now := today()
	at := now
	if v := r.URL.Query().Get("date"); v != "" {
		d, err := budget.ParseDate(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
			return
		}
		at = d
	}
	st, err := loadBudgetSettings(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	p := budget.PeriodFor(view, at, time.Weekday(st.WeekStart), st.PaySchedule)
	from := budget.MonthStart(p.Start) // allowances need what was spent earlier in the month

	groups, err := q.ListCategoryGroups(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	cats, err := q.ListCategories(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	amounts, err := loadAmounts(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	catTotals, err := loadCategoryTotals(ctx, q, hh, from, p.End)
	if err != nil {
		s.internalError(w, err)
		return
	}
	goals, err := q.ListGoals(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	goalTotals, err := loadGoalTotals(ctx, q, hh, from, p.End)
	if err != nil {
		s.internalError(w, err)
		return
	}

	month := budget.MonthKey(p.Start)
	out := budgetDTO{
		View: string(view), Start: budget.FormatDate(p.Start), End: budget.FormatDate(p.End.AddDate(0, 0, -1)),
		Prev: budget.FormatDate(p.Start.AddDate(0, 0, -1)), Next: budget.FormatDate(p.End),
		Month: month, Today: budget.FormatDate(now), Settings: st, Groups: []budgetGroupDTO{},
	}
	gidx := map[int64]int{}
	for _, g := range groups {
		if !budgetKinds[g.Kind] {
			continue
		}
		gidx[g.ID] = len(out.Groups)
		out.Groups = append(out.Groups, budgetGroupDTO{ID: g.ID, Name: g.Name, Kind: g.Kind, Lines: []budgetLineDTO{}})
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
		chunk := parseChunk(c.Chunk)
		l := budget.ComputeLine(p, chunk, amounts.cats[c.ID], now, func(a, b time.Time) int64 {
			return sign * catTotals.sum(c.ID, a, b)
		})
		g.Lines = append(g.Lines, budgetLineDTO{
			ID: c.ID, Name: c.Name, Icon: c.Icon, Budget: l.Budget, Actual: l.Actual, Expected: l.Expected,
			MonthBudget: budget.Resolve(amounts.cats[c.ID], month), Chunk: chunk,
		})
		g.Budget += l.Budget
		g.Actual += l.Actual
		if g.Kind != "income" && l.Budget == 0 && l.Actual > 0 {
			out.Summary.UnbudgetedCats++
		}
	}
	gg := budgetGroupDTO{Name: "Contributions", Kind: "goals", Lines: []budgetLineDTO{}}
	for _, g := range goals {
		if g.Archived == 1 {
			continue
		}
		l := budget.ComputeLine(p, budget.Chunk{Kind: budget.Even}, amounts.goals[g.ID], now, func(a, b time.Time) int64 {
			return goalTotals.sum(g.ID, a, b)
		})
		gg.Lines = append(gg.Lines, budgetLineDTO{
			ID: g.ID, Name: g.Name, Icon: g.Icon, Budget: l.Budget, Actual: l.Actual, Expected: l.Expected,
			MonthBudget: budget.Resolve(amounts.goals[g.ID], month), Chunk: budget.Chunk{Kind: budget.Even},
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
			sm.ExpenseBudget += g.Budget
			sm.ExpenseActual += g.Actual
		}
	}
	sm.LeftToBudget = sm.IncomeBudget - sm.ExpenseBudget - sm.GoalsBudget
	sm.LeftActual = sm.IncomeActual - sm.ExpenseActual - sm.GoalsActual
	writeJSON(w, http.StatusOK, out)
}

// ---- editing ----

// budgetTarget reads which category or goal a request is about and checks it belongs to hh.
func budgetTarget(ctx context.Context, q *db.Queries, hh int64, catID, goalID *int64) (cat, goal sql.NullInt64, msg string) {
	switch {
	case catID != nil && goalID == nil:
		if _, err := q.GetCategory(ctx, db.GetCategoryParams{ID: *catID, HouseholdID: hh}); err != nil {
			return cat, goal, "Unknown category."
		}
		return sql.NullInt64{Int64: *catID, Valid: true}, goal, ""
	case goalID != nil && catID == nil:
		if _, err := q.GetGoal(ctx, db.GetGoalParams{ID: *goalID, HouseholdID: hh}); err != nil {
			return cat, goal, "Unknown goal."
		}
		return cat, sql.NullInt64{Int64: *goalID, Valid: true}, ""
	}
	return cat, goal, "Pick a category or a goal."
}

func (s *Server) handleSetBudgetAmount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CategoryID   *int64 `json:"category_id"`
		GoalID       *int64 `json:"goal_id"`
		Month        string `json:"month"`
		Amount       string `json:"amount"`
		ApplyForward bool   `json:"apply_forward"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx, hh := r.Context(), HouseholdID(r)
	bad := func(msg string) { writeError(w, http.StatusBadRequest, msg) }
	if _, err := budget.ParseMonth(in.Month); err != nil {
		bad("month must be YYYY-MM")
		return
	}
	amt := int64(0)
	if strings.TrimSpace(in.Amount) != "" {
		var err error
		if amt, err = money.ParseCents(in.Amount); err != nil || amt < 0 {
			bad("Enter an amount like 250 or 250.00.")
			return
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	q := db.New(tx)
	cat, goal, msg := budgetTarget(ctx, q, hh, in.CategoryID, in.GoalID)
	if msg != "" {
		bad(msg)
		return
	}
	idx, err := loadAmounts(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	rows := idx.cats[cat.Int64]
	if goal.Valid {
		rows = idx.goals[goal.Int64]
	}
	rows = budget.SetAmount(rows, in.Month, amt, in.ApplyForward)
	if err := q.DeleteBudgetAmountsFor(ctx, db.DeleteBudgetAmountsForParams{HouseholdID: hh, CategoryID: cat, GoalID: goal}); err != nil {
		s.internalError(w, err)
		return
	}
	for _, a := range rows {
		if err := q.InsertBudgetAmount(ctx, db.InsertBudgetAmountParams{
			HouseholdID: hh, CategoryID: cat, GoalID: goal, Month: a.Month, AmountCents: a.Amount, Forward: b2i(a.Forward),
		}); err != nil {
			s.internalError(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSetChunk(w http.ResponseWriter, r *http.Request) {
	var c budget.Chunk
	if !readJSON(w, r, &c) {
		return
	}
	if err := c.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "Spending schedule: "+err.Error()+".")
		return
	}
	ctx, hh := r.Context(), HouseholdID(r)
	q := db.New(s.db)
	id := txnID(r)
	if _, err := q.GetCategory(ctx, db.GetCategoryParams{ID: id, HouseholdID: hh}); err != nil {
		writeError(w, http.StatusNotFound, "Category not found.")
		return
	}
	v := ""
	if c.Kind != budget.Even {
		b, _ := json.Marshal(c)
		v = string(b)
	}
	if err := q.SetCategoryChunk(ctx, db.SetCategoryChunkParams{Chunk: v, ID: id, HouseholdID: hh}); err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

type historyMonthDTO struct {
	Month  string `json:"month"`
	Budget int64  `json:"budget"`
	Actual int64  `json:"actual"`
}

// handleBudgetHistory backs the edit dialog: the month's amount plus the six months before it.
func (s *Server) handleBudgetHistory(w http.ResponseWriter, r *http.Request) {
	ctx, hh := r.Context(), HouseholdID(r)
	q := db.New(s.db)
	m, err := budget.ParseMonth(r.URL.Query().Get("month"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "month must be YYYY-MM")
		return
	}
	var catID, goalID *int64
	if v, ok := queryInt(r, "category_id"); ok {
		catID = &v
	}
	if v, ok := queryInt(r, "goal_id"); ok {
		goalID = &v
	}
	cat, goal, msg := budgetTarget(ctx, q, hh, catID, goalID)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	idx, err := loadAmounts(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	first, end := m.AddDate(0, -6, 0), m.AddDate(0, 1, 0)
	var rows []budget.AmountRow
	var totals dailyTotals
	sign, id := int64(1), goal.Int64
	chunk := budget.Chunk{Kind: budget.Even}
	if cat.Valid {
		id, rows = cat.Int64, idx.cats[cat.Int64]
		c, err := q.GetCategory(ctx, db.GetCategoryParams{ID: id, HouseholdID: hh})
		if err != nil {
			s.internalError(w, err)
			return
		}
		chunk = parseChunk(c.Chunk)
		if kind, err := q.GetCategoryGroupKind(ctx, c.GroupID); err == nil && kind != "income" {
			sign = -1
		}
		totals, err = loadCategoryTotals(ctx, q, hh, first, end)
	} else {
		rows = idx.goals[goal.Int64]
		totals, err = loadGoalTotals(ctx, q, hh, first, end)
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	hist := []historyMonthDTO{}
	var sum int64
	for i := 0; i <= 6; i++ {
		ms := first.AddDate(0, i, 0)
		a := sign * totals.sum(id, ms, ms.AddDate(0, 1, 0))
		hist = append(hist, historyMonthDTO{Month: budget.MonthKey(ms), Budget: budget.Resolve(rows, budget.MonthKey(ms)), Actual: a})
		if i < 6 {
			sum += a
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"month":        budget.MonthKey(m),
		"month_budget": budget.Resolve(rows, budget.MonthKey(m)),
		"history":      hist,
		"last_month":   hist[5].Actual,
		"average":      (sum + 3) / 6,
		"chunk":        chunk,
	})
}

// ---- goals ----

type goalDTO struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Icon        string  `json:"icon"`
	Target      int64   `json:"target_cents"`
	TargetDate  *string `json:"target_date"`
	Starting    int64   `json:"starting_cents"`
	Contributed int64   `json:"contributed_cents"`
	Balance     int64   `json:"balance_cents"` // starting + contributed
	Archived    bool    `json:"archived"`
}

func (s *Server) handleListGoals(w http.ResponseWriter, r *http.Request) {
	rows, err := db.New(s.db).ListGoals(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := make([]goalDTO, len(rows))
	for i, g := range rows {
		out[i] = goalDTO{
			ID: g.ID, Name: g.Name, Icon: g.Icon, Target: g.TargetCents, Starting: g.StartingCents,
			Contributed: g.ContributedCents, Balance: g.StartingCents + g.ContributedCents, Archived: g.Archived == 1,
		}
		if g.TargetDate.Valid {
			out[i].TargetDate = &g.TargetDate.String
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"goals": out})
}

type goalIn struct {
	Name       *string `json:"name"`
	Icon       *string `json:"icon"`
	Target     *string `json:"target"`
	TargetDate *string `json:"target_date"` // "" clears it
	Starting   *string `json:"starting"`
	Archived   *bool   `json:"archived"`
}

// apply merges in into g; it returns a user-facing error message.
func (in goalIn) apply(g *db.Goal) string {
	if in.Name != nil {
		g.Name = strings.TrimSpace(*in.Name)
	}
	if g.Name == "" {
		return "Enter a goal name."
	}
	if in.Icon != nil {
		g.Icon = strings.TrimSpace(*in.Icon)
	}
	cents := func(s *string, dst *int64) bool {
		if s == nil {
			return true
		}
		if strings.TrimSpace(*s) == "" {
			*dst = 0
			return true
		}
		v, err := money.ParseCents(*s)
		if err != nil || v < 0 {
			return false
		}
		*dst = v
		return true
	}
	if !cents(in.Target, &g.TargetCents) {
		return "Enter a target like 5000."
	}
	if !cents(in.Starting, &g.StartingCents) {
		return "Enter a starting amount like 250."
	}
	if in.TargetDate != nil {
		g.TargetDate = sql.NullString{}
		if *in.TargetDate != "" {
			if _, err := budget.ParseDate(*in.TargetDate); err != nil {
				return "Enter a valid target date."
			}
			g.TargetDate = sql.NullString{String: *in.TargetDate, Valid: true}
		}
	}
	if in.Archived != nil {
		g.Archived = b2i(*in.Archived)
	}
	return ""
}

func (s *Server) handleCreateGoal(w http.ResponseWriter, r *http.Request) {
	var in goalIn
	if !readJSON(w, r, &in) {
		return
	}
	var g db.Goal
	if msg := in.apply(&g); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if g.Icon == "" {
		g.Icon = "🎯"
	}
	created, err := db.New(s.db).CreateGoal(r.Context(), db.CreateGoalParams{
		HouseholdID: HouseholdID(r), Name: g.Name, Icon: g.Icon, TargetCents: g.TargetCents,
		TargetDate: g.TargetDate, StartingCents: g.StartingCents, CreatedAt: time.Now().Unix(),
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": created.ID})
}

func (s *Server) handleUpdateGoal(w http.ResponseWriter, r *http.Request) {
	var in goalIn
	if !readJSON(w, r, &in) {
		return
	}
	ctx, hh := r.Context(), HouseholdID(r)
	q := db.New(s.db)
	g, err := q.GetGoal(ctx, db.GetGoalParams{ID: txnID(r), HouseholdID: hh})
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Goal not found.")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	if msg := in.apply(&g); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := q.UpdateGoal(ctx, db.UpdateGoalParams{
		Name: g.Name, Icon: g.Icon, TargetCents: g.TargetCents, TargetDate: g.TargetDate,
		StartingCents: g.StartingCents, Archived: g.Archived, ID: g.ID, HouseholdID: hh,
	}); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteGoal(w http.ResponseWriter, r *http.Request) {
	ctx, hh := r.Context(), HouseholdID(r)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	q := db.New(tx)
	id := txnID(r)
	if err := q.ClearGoalTransactions(ctx, db.ClearGoalTransactionsParams{GoalID: sql.NullInt64{Int64: id, Valid: true}, HouseholdID: hh}); err != nil {
		s.internalError(w, err)
		return
	}
	if err := q.DeleteGoal(ctx, db.DeleteGoalParams{ID: id, HouseholdID: hh}); err != nil {
		s.internalError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
