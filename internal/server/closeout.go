package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/ai"
	"viceroy/internal/budget"
	"viceroy/internal/budgetview"
	"viceroy/internal/closeout"
	"viceroy/internal/db"
	"viceroy/internal/money"
)

// closeoutToday is today's date for close-out windows. VICEROY_CLOSEOUT_TODAY (YYYY-MM-DD)
// pins it for the end-to-end suite; unit tests replace the function.
var closeoutToday = func() time.Time {
	if d, err := budget.ParseDate(os.Getenv("VICEROY_CLOSEOUT_TODAY")); err == nil {
		return d
	}
	return budgetview.Today()
}

// outlookMonths is how many past months next month's outlook averages.
const outlookMonths = 3

func (s *Server) closeoutRoutes(r chi.Router) {
	r.Get("/budget/risk", s.handleBudgetRisk)
	r.Get("/closeout", s.handleCloseoutStatus)
	r.Get("/closeout/{month}", s.handleGetCloseout)
	r.Post("/closeout/{month}", s.handleCloseMonth)
	r.Delete("/closeout/{month}", s.handleReopenMonth)
	r.Post("/closeout/{month}/analysis", s.handleCloseoutAnalysis)
}

// GET /budget/risk: the risk of overspending this month, from weekly pacing.
func (s *Server) handleBudgetRisk(w http.ResponseWriter, r *http.Request) {
	ctx, hh, now := r.Context(), HouseholdID(r), budgetview.Today()
	v, err := budgetview.Build(ctx, db.New(s.db), hh, budget.ViewMonth, now, now)
	if err != nil {
		s.internalError(w, err)
		return
	}
	if err := s.addUpcoming(ctx, hh, &v, now); err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, budgetview.ComputeRisk(v))
}

type closeoutStatus struct {
	Month  string `json:"month"`  // the month that can be closed out now, "" outside a window
	Closes string `json:"closes"` // last day it can be
	Closed bool   `json:"closed"`
}

// GET /closeout: which month can be closed out now, for the banners.
func (s *Server) handleCloseoutStatus(w http.ResponseWriter, r *http.Request) {
	out := closeoutStatus{}
	if m, closes, ok := closeout.Window(closeoutToday()); ok {
		out.Month, out.Closes = budget.MonthKey(m), budget.FormatDate(closes)
		_, err := db.New(s.db).GetCloseout(r.Context(), db.GetCloseoutParams{HouseholdID: HouseholdID(r), Month: out.Month})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			s.internalError(w, err)
			return
		}
		out.Closed = err == nil
	}
	writeJSON(w, http.StatusOK, out)
}

type allocationDTO struct {
	CategoryID *int64 `json:"category_id,omitempty"`
	GoalID     *int64 `json:"goal_id,omitempty"`
	Month      string `json:"month,omitempty"`
	Amount     int64  `json:"amount"`
	Name       string `json:"name"`
	Icon       string `json:"icon"`
}

type closedDTO struct {
	ClosedAt    int64           `json:"closed_at"`
	ClosedBy    string          `json:"closed_by"`
	Allocations []allocationDTO `json:"allocations"`
	Analysis    string          `json:"analysis"`
	AnalysisAt  int64           `json:"analysis_at,omitempty"`
}

type destinationDTO struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Icon  string `json:"icon"`
	Group string `json:"group"`
	Kind  string `json:"kind"` // fixed | flexible | non_monthly | goal
}

// closeoutMonth reads {month} and checks it can be shown: closed already, or in its window.
func closeoutMonth(w http.ResponseWriter, r *http.Request) (time.Time, bool) {
	m, err := budget.ParseMonth(chi.URLParam(r, "month"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "month must be YYYY-MM")
		return time.Time{}, false
	}
	return budget.Date(m.Year(), m.Month(), 1), true
}

// inWindow reports whether month can be closed out (or reopened) today.
func inWindow(month time.Time) bool {
	m, _, ok := closeout.Window(closeoutToday())
	return ok && m.Equal(month)
}

// GET /closeout/{month}: the review (stored when closed, else live), next month's outlook,
// where leftover money can go, and the close-out if done.
func (s *Server) handleGetCloseout(w http.ResponseWriter, r *http.Request) {
	month, ok := closeoutMonth(w, r)
	if !ok {
		return
	}
	ctx, hh, q := r.Context(), HouseholdID(r), db.New(s.db)
	key := budget.MonthKey(month)
	row, err := q.GetCloseout(ctx, db.GetCloseoutParams{HouseholdID: hh, Month: key})
	closed := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.internalError(w, err)
		return
	}
	if !closed && !inWindow(month) {
		writeError(w, http.StatusNotFound, "That month can't be closed out now.")
		return
	}
	var review closeout.Review
	if closed && json.Unmarshal([]byte(row.Review), &review) == nil {
		// The review as it was when closed.
	} else if review, err = s.closeoutReview(ctx, q, hh, month); err != nil {
		s.internalError(w, err)
		return
	}
	outlook, err := s.closeoutOutlook(ctx, q, hh, month)
	if err != nil {
		s.internalError(w, err)
		return
	}
	dests, err := closeoutDestinations(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	_, closes, _ := closeout.Window(month.AddDate(0, 1, 0))
	out := map[string]any{
		"month": key, "next_month": budget.MonthKey(month.AddDate(0, 1, 0)), "closes": budget.FormatDate(closes),
		"can_change": inWindow(month), "review": review, "outlook": outlook, "destinations": dests, "closed": nil,
	}
	if closed {
		c, err := s.closedDTO(ctx, q, row)
		if err != nil {
			s.internalError(w, err)
			return
		}
		out["closed"] = c
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) closeoutReview(ctx context.Context, q *db.Queries, hh int64, month time.Time) (closeout.Review, error) {
	v, err := budgetview.Build(ctx, q, hh, budget.ViewMonth, month, closeoutToday())
	if err != nil {
		return closeout.Review{}, err
	}
	return closeout.BuildReview(v), nil
}

// closeoutOutlook judges the month after month from its budget, the last outlookMonths
// months' spending (month included) and recurring charges due in it.
func (s *Server) closeoutOutlook(ctx context.Context, q *db.Queries, hh int64, month time.Time) (closeout.Outlook, error) {
	today := closeoutToday()
	next := month.AddDate(0, 1, 0)
	nv, err := budgetview.Build(ctx, q, hh, budget.ViewMonth, next, today)
	if err != nil {
		return closeout.Outlook{}, err
	}
	var history []budgetview.View
	for i := outlookMonths - 1; i >= 0; i-- {
		hv, err := budgetview.Build(ctx, q, hh, budget.ViewMonth, month.AddDate(0, -i, 0), today)
		if err != nil {
			return closeout.Outlook{}, err
		}
		history = append(history, hv)
	}
	sc, err := s.recurringSchedule(ctx, hh)
	if err != nil {
		return closeout.Outlook{}, err
	}
	return closeout.BuildOutlook(nv, history, sc.Due(next, next.AddDate(0, 1, -1))), nil
}

// closeoutDestinations lists where leftover money can go: expense categories (a raise to next
// month's budget) and goals.
func closeoutDestinations(ctx context.Context, q *db.Queries, hh int64) ([]destinationDTO, error) {
	groups, err := q.ListCategoryGroups(ctx, hh)
	if err != nil {
		return nil, err
	}
	cats, err := q.ListCategories(ctx, hh)
	if err != nil {
		return nil, err
	}
	goals, err := q.ListGoals(ctx, hh)
	if err != nil {
		return nil, err
	}
	out := []destinationDTO{}
	for _, g := range goals {
		if g.Archived == 0 {
			out = append(out, destinationDTO{ID: g.ID, Name: g.Name, Icon: g.Icon, Group: "Goals", Kind: "goal"})
		}
	}
	for _, g := range groups {
		if g.Kind != "fixed" && g.Kind != "flexible" && g.Kind != "non_monthly" {
			continue
		}
		for _, c := range cats {
			if c.GroupID == g.ID && c.Archived == 0 && c.BudgetHidden == 0 {
				out = append(out, destinationDTO{ID: c.ID, Name: c.Name, Icon: c.Icon, Group: g.Name, Kind: g.Kind})
			}
		}
	}
	return out, nil
}

func (s *Server) closedDTO(ctx context.Context, q *db.Queries, row db.BudgetCloseout) (closedDTO, error) {
	out := closedDTO{ClosedAt: row.ClosedAt, Analysis: row.Analysis, Allocations: []allocationDTO{}}
	if row.AnalysisAt.Valid {
		out.AnalysisAt = row.AnalysisAt.Int64
	}
	if row.ClosedBy.Valid {
		if u, err := q.GetUser(ctx, row.ClosedBy.Int64); err == nil {
			out.ClosedBy = u.Name
		}
	}
	rows, err := q.ListCloseoutAllocations(ctx, row.ID)
	if err != nil {
		return out, err
	}
	for _, a := range rows {
		d := allocationDTO{Month: a.Month, Amount: a.AmountCents, Name: a.Name, Icon: a.Icon}
		if a.CategoryID.Valid {
			d.CategoryID = &a.CategoryID.Int64
		} else {
			d.GoalID = &a.GoalID.Int64
		}
		out.Allocations = append(out.Allocations, d)
	}
	return out, nil
}

// raiseBudget adds delta (which may be negative) to a category's budget for one month only.
func raiseBudget(ctx context.Context, q *db.Queries, hh, catID int64, month string, delta int64) error {
	idx, err := budgetview.LoadAmounts(ctx, q, hh)
	if err != nil {
		return err
	}
	rows := idx.Cats[catID]
	rows = budget.SetAmount(rows, month, max(0, budget.Resolve(rows, month)+delta), false)
	cat := sql.NullInt64{Int64: catID, Valid: true}
	if err := q.DeleteBudgetAmountsFor(ctx, db.DeleteBudgetAmountsForParams{HouseholdID: hh, CategoryID: cat}); err != nil {
		return err
	}
	for _, a := range rows {
		if err := q.InsertBudgetAmount(ctx, db.InsertBudgetAmountParams{
			HouseholdID: hh, CategoryID: cat, Month: a.Month, AmountCents: a.Amount, Forward: b2i(a.Forward),
		}); err != nil {
			return err
		}
	}
	return nil
}

// POST /closeout/{month} {allocations: [{category_id | goal_id, amount}]}: closes the month.
// Each category allocation raises that category's budget for the next month only; a goal
// allocation adds to the goal's balance. The rest of the surplus stays unassigned.
func (s *Server) handleCloseMonth(w http.ResponseWriter, r *http.Request) {
	month, ok := closeoutMonth(w, r)
	if !ok {
		return
	}
	var in struct {
		Allocations []struct {
			CategoryID *int64 `json:"category_id"`
			GoalID     *int64 `json:"goal_id"`
			Amount     string `json:"amount"`
		} `json:"allocations"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	bad := func(msg string) { writeError(w, http.StatusBadRequest, msg) }
	if !inWindow(month) {
		bad("That month can't be closed out now.")
		return
	}
	ctx, hh := r.Context(), HouseholdID(r)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	q := db.New(tx)
	key, next := budget.MonthKey(month), budget.MonthKey(month.AddDate(0, 1, 0))
	if _, err := q.GetCloseout(ctx, db.GetCloseoutParams{HouseholdID: hh, Month: key}); err == nil {
		writeError(w, http.StatusConflict, "This month is already closed out.")
		return
	}
	review, err := s.closeoutReview(ctx, q, hh, month)
	if err != nil {
		s.internalError(w, err)
		return
	}

	type alloc struct {
		cat, goal sql.NullInt64
		amount    int64
	}
	var allocs []alloc
	var total int64
	seen := map[string]bool{}
	for _, a := range in.Allocations {
		amt, err := money.ParseCents(strings.TrimSpace(a.Amount))
		if err != nil || amt <= 0 {
			bad("Enter each amount like 50 or 50.00.")
			return
		}
		var al alloc
		al.amount = amt
		switch {
		case a.CategoryID != nil && a.GoalID == nil:
			c, err := q.GetCategory(ctx, db.GetCategoryParams{ID: *a.CategoryID, HouseholdID: hh})
			if err != nil || c.Archived == 1 {
				bad("Unknown category.")
				return
			}
			if kind, err := q.GetCategoryGroupKind(ctx, c.GroupID); err != nil || (kind != "fixed" && kind != "flexible" && kind != "non_monthly") {
				bad("Leftover money can only go to expense categories.")
				return
			}
			al.cat = sql.NullInt64{Int64: c.ID, Valid: true}
		case a.GoalID != nil && a.CategoryID == nil:
			g, err := q.GetGoal(ctx, db.GetGoalParams{ID: *a.GoalID, HouseholdID: hh})
			if err != nil || g.Archived == 1 {
				bad("Unknown goal.")
				return
			}
			al.goal = sql.NullInt64{Int64: g.ID, Valid: true}
		default:
			bad("Pick a category or a goal for each amount.")
			return
		}
		k := fmt.Sprint(al.cat.Int64, ":", al.goal.Int64)
		if seen[k] {
			bad("Each category or goal can be picked once.")
			return
		}
		seen[k] = true
		total += amt
		allocs = append(allocs, al)
	}
	if total > review.Surplus {
		bad(fmt.Sprintf("That's more than the %s left over.", money.Format(review.Surplus)))
		return
	}

	snap, _ := json.Marshal(review)
	row, err := q.CreateCloseout(ctx, db.CreateCloseoutParams{
		HouseholdID: hh, Month: key, ClosedAt: time.Now().Unix(), ClosedBy: sql.NullInt64{Int64: CurrentUser(r).ID, Valid: true},
		BudgetCents: review.Budget, ActualCents: review.Actual, SurplusCents: review.Surplus, Review: string(snap),
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	for _, a := range allocs {
		m := ""
		if a.cat.Valid {
			m = next
			if err := raiseBudget(ctx, q, hh, a.cat.Int64, next, a.amount); err != nil {
				s.internalError(w, err)
				return
			}
		}
		if err := q.InsertCloseoutAllocation(ctx, db.InsertCloseoutAllocationParams{
			CloseoutID: row.ID, HouseholdID: hh, CategoryID: a.cat, GoalID: a.goal, Month: m, AmountCents: a.amount,
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

// DELETE /closeout/{month}: reopens a month closed in the current window, taking back the
// budget raises and goal money it allocated.
func (s *Server) handleReopenMonth(w http.ResponseWriter, r *http.Request) {
	month, ok := closeoutMonth(w, r)
	if !ok {
		return
	}
	if !inWindow(month) {
		writeError(w, http.StatusBadRequest, "A close-out can only be undone while its month can still be closed out.")
		return
	}
	ctx, hh := r.Context(), HouseholdID(r)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	q := db.New(tx)
	row, err := q.GetCloseout(ctx, db.GetCloseoutParams{HouseholdID: hh, Month: budget.MonthKey(month)})
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "This month isn't closed out.")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	allocs, err := q.ListCloseoutAllocations(ctx, row.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	for _, a := range allocs {
		if a.CategoryID.Valid {
			if err := raiseBudget(ctx, q, hh, a.CategoryID.Int64, a.Month, -a.AmountCents); err != nil {
				s.internalError(w, err)
				return
			}
		}
	}
	if err := q.DeleteCloseout(ctx, db.DeleteCloseoutParams{ID: row.ID, HouseholdID: hh}); err != nil {
		s.internalError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /closeout/{month}/analysis: asks the AI how likely the next month is to run over, given
// the closed month's review, the outlook and where the leftover money went. Stored on the
// close-out.
func (s *Server) handleCloseoutAnalysis(w http.ResponseWriter, r *http.Request) {
	month, ok := closeoutMonth(w, r)
	if !ok {
		return
	}
	ctx, hh, q := r.Context(), HouseholdID(r), db.New(s.db)
	row, err := q.GetCloseout(ctx, db.GetCloseoutParams{HouseholdID: hh, Month: budget.MonthKey(month)})
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Close out the month first.")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	client, err := s.ai.ChatClient(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	if !client.Configured() {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up yet: add an OpenRouter key in Settings → AI.")
		return
	}
	client.Feature, client.Ref = "closeout", row.ID
	outlook, err := s.closeoutOutlook(ctx, q, hh, month)
	if err != nil {
		s.internalError(w, err)
		return
	}
	closed, err := s.closedDTO(ctx, q, row)
	if err != nil {
		s.internalError(w, err)
		return
	}
	data, _ := json.Marshal(map[string]any{
		"closed_month": json.RawMessage(row.Review), "next_month_outlook": outlook, "allocations": closed.Allocations,
	})
	next := month.AddDate(0, 1, 0)
	msgs := []ai.Message{
		{Role: "system", Content: closeoutPrompt(month, next)},
		{Role: "user", Content: string(data)},
	}
	reply, err := client.Stream(ctx, msgs, nil, func(string) {})
	if err != nil {
		s.log.Warn("closeout analysis", "err", err)
		writeError(w, http.StatusBadGateway, "The AI didn't answer: "+err.Error())
		return
	}
	text := strings.TrimSpace(reply.Content)
	if text == "" {
		writeError(w, http.StatusBadGateway, "The AI returned an empty answer; try again.")
		return
	}
	at := time.Now().Unix()
	if err := q.SetCloseoutAnalysis(ctx, db.SetCloseoutAnalysisParams{Analysis: text, AnalysisAt: sql.NullInt64{Int64: at, Valid: true}, ID: row.ID, HouseholdID: hh}); err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"analysis": text, "analysis_at": at})
}

func closeoutPrompt(month, next time.Time) string {
	return fmt.Sprintf(`You are the budgeting assistant inside Viceroy, a personal finance app. The household just closed out %s and is starting %s.
The user message is JSON (money in cents; "diff" is budget minus actual, negative = overspent): the closed month's review, an outlook for %s (each at-risk category's next budget against its average spending over the last %d months and its recurring charges; "gap" = how much it is likely to run over), and where they put the leftover money.
Write a short risk analysis for %s in markdown, money formatted like $1,234:
- First line: the overall risk of overspending (low, moderate, high or very high) and the main reason, in one sentence.
- Then up to 4 bullets on the categories most likely to run over and why (repeat overspending, budget below the usual, recurring charges), each with one concrete fix: raise the budget to a realistic amount, cut back, or move money from a category that usually comes in under.
- If income came in under plan, say the leftover money may not be real.
Use only the numbers given; don't invent transactions. Under 180 words. No headings.`,
		month.Format("January 2006"), next.Format("January 2006"), next.Format("January"), outlookMonths, next.Format("January"))
}
