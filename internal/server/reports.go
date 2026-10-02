package server

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/budgetview"
	"viceroy/internal/db"
	"viceroy/internal/reports"
)

func (s *Server) reportRoutes(r chi.Router) {
	r.Get("/reports", s.handleReport)
	r.Get("/reports/spending-pace", s.handleSpendingPace)
	r.Get("/reports/tree", s.handleReportTree)
	r.Get("/reports/debt", s.handleDebtReport)
}

func parseDate(s string) (time.Time, bool) {
	d, err := time.Parse(time.DateOnly, s)
	return d, err == nil
}

// loadReportRows reads report rows over [from, to] (inclusive).
func loadReportRows(ctx context.Context, q *db.Queries, hh int64, from, to time.Time) ([]reports.Row, error) {
	rows, err := q.ReportRows(ctx, db.ReportRowsParams{
		HouseholdID: hh, FromDate: from.Format(time.DateOnly), ToDate: to.AddDate(0, 0, 1).Format(time.DateOnly),
	})
	if err != nil {
		return nil, err
	}
	out := make([]reports.Row, len(rows))
	for i, r := range rows {
		out[i] = reports.Row{Date: r.Date, CategoryID: r.CategoryID.Int64, Kind: r.Kind, Merchant: r.Merchant, Total: r.Total}
	}
	return out, nil
}

func loadReportCategories(ctx context.Context, q *db.Queries, hh int64) (map[int64]reports.Category, error) {
	groups, err := q.ListCategoryGroups(ctx, hh)
	if err != nil {
		return nil, err
	}
	gname, gkind := map[int64]string{}, map[int64]string{}
	for _, g := range groups {
		gname[g.ID], gkind[g.ID] = g.Name, g.Kind
	}
	cats, err := q.ListCategories(ctx, hh)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]reports.Category, len(cats))
	for _, c := range cats {
		out[c.ID] = reports.Category{ID: c.ID, Name: c.Name, Icon: c.Icon, GroupID: c.GroupID, GroupName: gname[c.GroupID], GroupKind: gkind[c.GroupID]}
	}
	return out, nil
}

// GET /reports?from=&to=&interval=month|quarter|year&by=category|group|merchant
// Dates are inclusive. Defaults: the last 6 months through today, monthly, by category.
// from=all starts at the first transaction.
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	ctx, hh, q := r.Context(), HouseholdID(r), db.New(s.db)
	qs := r.URL.Query()
	iv, ok := reports.ParseInterval(qs.Get("interval"))
	if !ok {
		iv = reports.Month
	}
	by, ok := reports.ParseBy(qs.Get("by"))
	if !ok {
		by = reports.ByCategory
	}
	from, to, ok := s.reportRange(w, r)
	if !ok {
		return
	}
	rows, err := loadReportRows(ctx, q, hh, from, to)
	if err != nil {
		s.internalError(w, err)
		return
	}
	cats, err := loadReportCategories(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	rep := reports.Build(rows, cats, reports.Buckets(from, to, iv), iv, by)
	writeJSON(w, http.StatusOK, map[string]any{
		"from": from.Format(time.DateOnly), "to": to.Format(time.DateOnly), "interval": iv, "by": by,
		"buckets": rep.Buckets, "income": rep.Income, "spending": rep.Spending,
	})
}

// reportRange reads from/to (inclusive YYYY-MM-DD). Defaults: the last 6 months through today;
// from=all starts at the first transaction. Writes the error itself when it fails.
func (s *Server) reportRange(w http.ResponseWriter, r *http.Request) (from, to time.Time, ok bool) {
	qs := r.URL.Query()
	to, ok = parseDate(qs.Get("to"))
	if !ok {
		to = budgetview.Today()
	}
	from, ok = parseDate(qs.Get("from"))
	if qs.Get("from") == "all" {
		first, err := db.New(s.db).FirstTransactionDate(r.Context(), HouseholdID(r))
		if err != nil {
			s.internalError(w, err)
			return from, to, false
		}
		from, ok = parseDate(first)
	}
	if !ok {
		from = time.Date(to.Year(), to.Month()-5, 1, 0, 0, 0, 0, time.UTC)
	}
	if from.After(to) {
		writeError(w, http.StatusBadRequest, "The start date is after the end date.")
		return from, to, false
	}
	if to.Sub(from) > 20*366*24*time.Hour {
		writeError(w, http.StatusBadRequest, "That date range is too long.")
		return from, to, false
	}
	return from, to, true
}

// GET /reports/tree?from=&to=: income (category → merchant) and spending (group → category →
// merchant, plus Contributions and Uncategorized) totals over the range, for the cash flow
// diagram and the spending breakdown.
func (s *Server) handleReportTree(w http.ResponseWriter, r *http.Request) {
	from, to, ok := s.reportRange(w, r)
	if !ok {
		return
	}
	tree, err := s.reportTree(r.Context(), HouseholdID(r), from, to)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": from.Format(time.DateOnly), "to": to.Format(time.DateOnly), "income": tree.Income, "spending": tree.Spending,
	})
}

func (s *Server) reportTree(ctx context.Context, hh int64, from, to time.Time) (reports.Tree, error) {
	q := db.New(s.db)
	rows, err := q.ReportTreeRows(ctx, db.ReportTreeRowsParams{
		HouseholdID: hh, FromDate: from.Format(time.DateOnly), ToDate: to.AddDate(0, 0, 1).Format(time.DateOnly),
	})
	if err != nil {
		return reports.Tree{}, err
	}
	cats, err := loadReportCategories(ctx, q, hh)
	if err != nil {
		return reports.Tree{}, err
	}
	gl, err := q.ListGoals(ctx, hh)
	if err != nil {
		return reports.Tree{}, err
	}
	goals := make(map[int64]reports.Goal, len(gl))
	for _, g := range gl {
		goals[g.ID] = reports.Goal{ID: g.ID, Name: g.Name, Icon: g.Icon}
	}
	in := make([]reports.TreeRow, len(rows))
	for i, r := range rows {
		in[i] = reports.TreeRow{CategoryID: r.CategoryID.Int64, Kind: r.Kind, Merchant: r.Merchant, GoalID: r.GoalID, Total: r.Total, Count: r.N}
	}
	return reports.BuildTree(in, cats, goals), nil
}

// GET /reports/spending-pace?month=YYYY-MM: cumulative spending by day of month for the
// month and the one before, for the dashboard's "this month vs last month" chart.
func (s *Server) handleSpendingPace(w http.ResponseWriter, r *http.Request) {
	ctx, hh, q := r.Context(), HouseholdID(r), db.New(s.db)
	now := budgetview.Today()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if m, err := time.Parse("2006-01", r.URL.Query().Get("month")); err == nil {
		month = m
	}
	prev := month.AddDate(0, -1, 0)
	end := month.AddDate(0, 1, -1)
	rows, err := loadReportRows(ctx, q, hh, prev, end)
	if err != nil {
		s.internalError(w, err)
		return
	}
	perDay := map[string]int64{}
	for _, row := range rows {
		if income, v, counted := reports.Classify(row); counted && !income {
			perDay[row.Date] += v
		}
	}
	cumulative := func(start time.Time, through time.Time) []int64 {
		out := []int64{}
		var sum int64
		for d := start; d.Month() == start.Month() && !d.After(through); d = d.AddDate(0, 0, 1) {
			sum += perDay[d.Format(time.DateOnly)]
			out = append(out, sum)
		}
		return out
	}
	through := end
	if !now.Before(month) && now.Before(end) {
		through = now
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"month": month.Format("2006-01"), "prev_month": prev.Format("2006-01"), "today": now.Format(time.DateOnly),
		"days_in_month": end.Day(), "this": cumulative(month, through), "last": cumulative(prev, end),
	})
}

