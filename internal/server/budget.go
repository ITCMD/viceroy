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

	"viceroy/internal/accounts"
	"viceroy/internal/budget"
	"viceroy/internal/budgetview"
	"viceroy/internal/db"
	"viceroy/internal/debt"
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

// debtTarget checks that accountID is one of hh's debt accounts and finds the household's
// Debt Repayment category, which its budget amounts belong to.
func debtTarget(ctx context.Context, q *db.Queries, hh, accountID int64) (cat, acct sql.NullInt64, msg string) {
	a, err := q.GetAccount(ctx, db.GetAccountParams{ID: accountID, HouseholdID: hh})
	if err != nil || !accounts.IsLiability(a.Type) {
		return cat, acct, "Unknown debt account."
	}
	c, err := q.GetBuiltinCategory(ctx, db.GetBuiltinCategoryParams{HouseholdID: hh, Builtin: budgetview.DebtRepaymentKey})
	if err != nil {
		return cat, acct, "There's no Debt Repayment category."
	}
	return sql.NullInt64{Int64: c.ID, Valid: true}, sql.NullInt64{Int64: a.ID, Valid: true}, ""
}

func (s *Server) handleSetBudgetAmount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CategoryID   *int64 `json:"category_id"`
		GoalID       *int64 `json:"goal_id"`
		AccountID    *int64 `json:"account_id"` // a debt account's Debt Repayment line
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
	var cat, goal, acct sql.NullInt64
	var msg string
	if in.AccountID != nil {
		cat, acct, msg = debtTarget(ctx, q, hh, *in.AccountID)
	} else {
		cat, goal, msg = budgetTarget(ctx, q, hh, in.CategoryID, in.GoalID)
	}
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
	} else if acct.Valid {
		rows = idx.Accounts[acct.Int64]
	}
	rows = budget.SetAmount(rows, in.Month, amt, in.ApplyForward)
	if err := q.DeleteBudgetAmountsFor(ctx, db.DeleteBudgetAmountsForParams{HouseholdID: hh, CategoryID: cat, GoalID: goal, AccountID: acct}); err != nil {
		s.internalError(w, err)
		return
	}
	for _, a := range rows {
		if err := q.InsertBudgetAmount(ctx, db.InsertBudgetAmountParams{
			HouseholdID: hh, CategoryID: cat, GoalID: goal, AccountID: acct, Month: a.Month, AmountCents: a.Amount, Forward: b2i(a.Forward),
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
	// Debt account lines: money paid in and charged that month (Actual is their difference in
	// net mode, Paid in paid mode).
	Paid    *int64 `json:"paid,omitempty"`
	Charged *int64 `json:"charged,omitempty"`
}

// debtLineInfo backs a debt account's budget editor: its terms and what the saved payoff plan
// pays on it in the month being edited.
type debtLineInfo struct {
	Balance          int64         `json:"balance"`
	APRBps           int64         `json:"apr_bps"`
	APRSource        string        `json:"apr_source"`
	MinPayment       int64         `json:"min_payment"`
	MinPaymentSource string        `json:"min_payment_source"`
	Ready            bool          `json:"ready"` // every debt has its terms, so a plan exists
	Strategy         debt.Strategy `json:"strategy"`
	Extra            int64         `json:"extra"`
	PlanPayment      *int64        `json:"plan_payment"` // nil: no plan, or a month before this one
	PlanMonths       int           `json:"plan_months"`  // when the plan pays this debt off (1 = this month, 0 = never)
	Mode             string        `json:"mode"`         // budget.debt_actual: net | paid
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
	if v, ok := queryInt(r, "account_id"); ok {
		s.debtLineHistory(w, r, v, m)
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
		if c.Builtin == budgetview.DebtRepaymentKey && r.URL.Query().Get("part") == "other" {
			// Other debt payments: Debt Repayment spending no debt account line explains.
			var split *budgetview.DebtSplit
			if split, err = budgetview.LoadDebtSplit(ctx, q, hh, id, first, end, budgetview.DebtActualNet); err == nil {
				totals, sign = budgetview.Totals{id: split.Other[0]}, 1
			}
		} else {
			totals, err = budgetview.LoadCategoryTotals(ctx, q, hh, first, end)
		}
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
	Withdrawn   int64   `json:"withdrawn_cents"` // spent from the goal (e.g. wishlist purchases)
	Balance     int64   `json:"balance_cents"`   // starting + contributed − withdrawn
	Archived    bool    `json:"archived"`
	Builtin     string  `json:"builtin"` // "wishlist" for the Wishlist goal
}

func toGoalDTO(g db.ListGoalsRow) goalDTO {
	d := goalDTO{
		ID: g.ID, Name: g.Name, Icon: g.Icon, Target: g.TargetCents, Starting: g.StartingCents,
		Contributed: g.ContributedCents, Withdrawn: g.WithdrawnCents,
		Balance: g.StartingCents + g.ContributedCents - g.WithdrawnCents, Archived: g.Archived == 1, Builtin: g.Builtin,
	}
	if g.TargetDate.Valid {
		d.TargetDate = &g.TargetDate.String
	}
	return d
}

func (s *Server) handleListGoals(w http.ResponseWriter, r *http.Request) {
	rows, err := db.New(s.db).ListGoals(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := make([]goalDTO, len(rows))
	for i, g := range rows {
		out[i] = toGoalDTO(g)
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
	if g, err := q.GetGoal(ctx, db.GetGoalParams{ID: id, HouseholdID: hh}); err == nil && g.Builtin == wishlistGoal {
		if n, err := q.CountOpenWishlistItems(ctx, hh); err != nil || n > 0 {
			writeError(w, http.StatusConflict, "The Wishlist goal can't be deleted while the wishlist has items.")
			return
		}
	}
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

// debtLineHistory is handleBudgetHistory for a debt account's Debt Repayment line.
func (s *Server) debtLineHistory(w http.ResponseWriter, r *http.Request, accountID int64, m time.Time) {
	ctx, hh := r.Context(), HouseholdID(r)
	q := db.New(s.db)
	cat, acct, msg := debtTarget(ctx, q, hh, accountID)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	st, err := budgetview.LoadSettings(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	c, err := q.GetCategory(ctx, db.GetCategoryParams{ID: cat.Int64, HouseholdID: hh})
	if err != nil {
		s.internalError(w, err)
		return
	}
	idx, err := budgetview.LoadAmounts(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	first, end := m.AddDate(0, -6, 0), m.AddDate(0, 1, 0)
	split, err := budgetview.LoadDebtSplit(ctx, q, hh, cat.Int64, first, end, st.DebtActual)
	if err != nil {
		s.internalError(w, err)
		return
	}
	rows := idx.Accounts[acct.Int64]
	spent := split.Spent(acct.Int64)
	hist := []historyMonthDTO{}
	var sum int64
	for i := 0; i <= 6; i++ {
		ms := first.AddDate(0, i, 0)
		me := ms.AddDate(0, 1, 0)
		paid, charged := split.Paid.Sum(acct.Int64, ms, me), split.Charged.Sum(acct.Int64, ms, me)
		a := spent(ms, me)
		hist = append(hist, historyMonthDTO{Month: budget.MonthKey(ms), Budget: budget.Resolve(rows, budget.MonthKey(ms)), Actual: a, Paid: &paid, Charged: &charged})
		if i < 6 {
			sum += a
		}
	}

	info := debtLineInfo{Mode: st.DebtActual, APRSource: "missing", MinPaymentSource: "missing"}
	plan, err := s.loadDebtPlan(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	info.Strategy, info.Extra = plan.Strategy, plan.Extra
	rep, err := s.debtReport(ctx, hh, plan.Extra)
	if err != nil {
		s.internalError(w, err)
		return
	}
	info.Ready = rep.Ready && len(rep.Plans) > 0
	for _, d := range rep.Debts {
		if d.AccountID == acct.Int64 {
			info.Balance, info.APRBps, info.APRSource, info.MinPayment, info.MinPaymentSource = d.Balance, d.APRBps, d.APRSource, d.MinPayment, d.MinPaymentSource
		}
	}
	// Plan month 1 is this month; earlier months have no suggestion.
	if p, ok := rep.Plans[string(plan.Strategy)]; ok && info.Ready {
		today := budgetview.Today()
		k := (m.Year()-today.Year())*12 + int(m.Month()-today.Month()) + 1
		for _, d := range p.Debts {
			if d.ID == acct.Int64 {
				info.PlanMonths = d.Months
				if k >= 1 {
					v := p.PaymentIn(d.ID, k)
					info.PlanPayment = &v
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"month":        budget.MonthKey(m),
		"month_budget": budget.Resolve(rows, budget.MonthKey(m)),
		"history":      hist,
		"last_month":   hist[5].Actual,
		"average":      (sum + 3) / 6,
		"chunk":        budgetview.ParseChunk(c.Chunk),
		"debt":         info,
	})
}
