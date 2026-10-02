package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/budget"
	"viceroy/internal/budgetview"
	"viceroy/internal/db"
	"viceroy/internal/money"
)

func (s *Server) budgetRoutes(r chi.Router) {
	r.Get("/budget", s.handleGetBudget)
	r.Put("/budget/amount", s.handleSetBudgetAmount)
	r.Get("/budget/history", s.handleBudgetHistory)
	r.Put("/budget/categories/{id}/chunk", s.handleSetChunk)
	r.Put("/budget/categories/{id}/hidden", s.handleSetBudgetHidden)
	r.Get("/goals", s.handleListGoals)
	r.Post("/goals", s.handleCreateGoal)
	r.Patch("/goals/{id}", s.handleUpdateGoal)
	r.Delete("/goals/{id}", s.handleDeleteGoal)
}

func (s *Server) handleGetBudget(w http.ResponseWriter, r *http.Request) {
	view := budget.View(r.URL.Query().Get("view"))
	if view != budget.ViewWeek && view != budget.ViewPaycheck {
		view = budget.ViewMonth
	}
	now := budgetview.Today()
	at := now
	if v := r.URL.Query().Get("date"); v != "" {
		d, err := budget.ParseDate(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
			return
		}
		at = d
	}
	out, err := budgetview.Build(r.Context(), db.New(s.db), HouseholdID(r), view, at, now)
	if err != nil {
		s.internalError(w, err)
		return
	}
	if err := s.addUpcoming(r.Context(), HouseholdID(r), &out, now); err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// addUpcoming fills Line.Upcoming for expense categories: unpaid recurring charges from today
// through the upcoming window (a week, or until the next payday), within the period shown.
func (s *Server) addUpcoming(ctx context.Context, hh int64, v *budgetview.View, today time.Time) error {
	start, err1 := budget.ParseDate(v.Start)
	end, err2 := budget.ParseDate(v.End)
	if err1 != nil || err2 != nil || today.Before(start) || today.After(end) {
		return nil
	}
	until := today.AddDate(0, 0, 6)
	if v.Settings.UpcomingWindow == "paycheck" {
		until = v.Settings.PaySchedule.PeriodAt(today).End.AddDate(0, 0, -1)
	}
	if until.After(end) {
		until = end
	}
	sc, err := s.recurringSchedule(ctx, hh)
	if err != nil {
		return err
	}
	// Late charges (due before today, not yet seen) still count: they're about to land.
	due := sc.Due(today.AddDate(0, 0, -7), until)
	for gi := range v.Groups {
		g := &v.Groups[gi]
		if g.Kind == "income" || g.Kind == "goals" {
			continue
		}
		for li := range g.Lines {
			g.Lines[li].Upcoming = due[g.Lines[li].ID]
		}
	}
	return nil
}

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
	idx, err := budgetview.LoadAmounts(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	rows := idx.Cats[cat.Int64]
	if goal.Valid {
		rows = idx.Goals[goal.Int64]
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
	if c.Kind != budget.Even || c.NoPacing {
		b, _ := json.Marshal(c)
		v = string(b)
	}
	if err := q.SetCategoryChunk(ctx, db.SetCategoryChunkParams{Chunk: v, ID: id, HouseholdID: hh}); err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// PUT /budget/categories/{id}/hidden {hidden}: hide a category the household doesn't budget for.
func (s *Server) handleSetBudgetHidden(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Hidden bool `json:"hidden"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx, hh, id := r.Context(), HouseholdID(r), txnID(r)
	q := db.New(s.db)
	if _, err := q.GetCategory(ctx, db.GetCategoryParams{ID: id, HouseholdID: hh}); err != nil {
		writeError(w, http.StatusNotFound, "Category not found.")
		return
	}
	if err := q.SetCategoryBudgetHidden(ctx, db.SetCategoryBudgetHiddenParams{BudgetHidden: b2i(in.Hidden), ID: id, HouseholdID: hh}); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	idx, err := budgetview.LoadAmounts(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	first, end := m.AddDate(0, -6, 0), m.AddDate(0, 1, 0)
	var rows []budget.AmountRow
	var totals budgetview.Totals
	sign, id := int64(1), goal.Int64
	chunk := budget.Chunk{Kind: budget.Even}
	if cat.Valid {
		id, rows = cat.Int64, idx.Cats[cat.Int64]
		c, err := q.GetCategory(ctx, db.GetCategoryParams{ID: id, HouseholdID: hh})
		if err != nil {
			s.internalError(w, err)
			return
		}
		chunk = budgetview.ParseChunk(c.Chunk)
		if kind, err := q.GetCategoryGroupKind(ctx, c.GroupID); err == nil && kind != "income" {
			sign = -1
		}
		totals, err = budgetview.LoadCategoryTotals(ctx, q, hh, first, end)
	} else {
		rows = idx.Goals[goal.Int64]
		totals, err = budgetview.LoadGoalTotals(ctx, q, hh, first, end)
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	hist := []historyMonthDTO{}
	var sum int64
	for i := 0; i <= 6; i++ {
		ms := first.AddDate(0, i, 0)
		a := sign * totals.Sum(id, ms, ms.AddDate(0, 1, 0))
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
