package server

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/db"
	"viceroy/internal/recurring"
	"viceroy/internal/reports"
)

func (s *Server) reportRoutes(r chi.Router) {
	r.Get("/reports", s.handleReport)
	r.Get("/reports/spending-pace", s.handleSpendingPace)
	r.Get("/recurring", s.handleListRecurring)
	r.Put("/recurring/dismissed", s.handleDismissRecurring)
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
	gname := map[int64]string{}
	for _, g := range groups {
		gname[g.ID] = g.Name
	}
	cats, err := q.ListCategories(ctx, hh)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]reports.Category, len(cats))
	for _, c := range cats {
		out[c.ID] = reports.Category{ID: c.ID, Name: c.Name, Icon: c.Icon, GroupID: c.GroupID, GroupName: gname[c.GroupID]}
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
	to, ok := parseDate(qs.Get("to"))
	if !ok {
		to = today()
	}
	from, ok := parseDate(qs.Get("from"))
	if qs.Get("from") == "all" {
		first, err := q.FirstTransactionDate(ctx, hh)
		if err != nil {
			s.internalError(w, err)
			return
		}
		from, ok = parseDate(first)
	}
	if !ok {
		from = time.Date(to.Year(), to.Month()-5, 1, 0, 0, 0, 0, time.UTC)
	}
	if from.After(to) {
		writeError(w, http.StatusBadRequest, "The start date is after the end date.")
		return
	}
	if to.Sub(from) > 20*366*24*time.Hour {
		writeError(w, http.StatusBadRequest, "That date range is too long.")
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

// GET /reports/spending-pace?month=YYYY-MM: cumulative spending by day of month for the
// month and the one before, for the dashboard's "this month vs last month" chart.
func (s *Server) handleSpendingPace(w http.ResponseWriter, r *http.Request) {
	ctx, hh, q := r.Context(), HouseholdID(r), db.New(s.db)
	now := today()
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

type recurringDTO struct {
	recurring.Series
	Dismissed bool `json:"dismissed"`
}

func (s *Server) recurringSeries(ctx context.Context, hh int64) ([]recurringDTO, error) {
	q := db.New(s.db)
	now := today()
	rows, err := q.RecurringCandidates(ctx, db.RecurringCandidatesParams{
		HouseholdID: hh, FromDate: now.AddDate(0, 0, -recurring.HistoryDays).Format(time.DateOnly),
	})
	if err != nil {
		return nil, err
	}
	txns := make([]recurring.Txn, len(rows))
	for i, r := range rows {
		txns[i] = recurring.Txn{
			ID: r.ID, Date: r.Date, Amount: r.AmountCents, MerchantID: r.MerchantID.Int64, Merchant: r.Merchant,
			CategoryID: r.CategoryID.Int64, CategoryName: r.CategoryName, CategoryIcon: r.CategoryIcon,
			AccountID: r.AccountID, AccountName: r.AccountName,
		}
	}
	dismissed, err := q.ListRecurringDismissed(ctx, hh)
	if err != nil {
		return nil, err
	}
	isDismissed := map[string]bool{}
	for _, k := range dismissed {
		isDismissed[k] = true
	}
	out := []recurringDTO{}
	for _, sr := range recurring.Detect(txns, now) {
		out = append(out, recurringDTO{sr, isDismissed[sr.Key]})
	}
	return out, nil
}

// GET /recurring: detected series sorted by next date, dismissed ones flagged.
func (s *Server) handleListRecurring(w http.ResponseWriter, r *http.Request) {
	list, err := s.recurringSeries(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	sort.SliceStable(list, func(i, j int) bool { return !list[i].Dismissed && list[j].Dismissed })
	writeJSON(w, http.StatusOK, map[string]any{"series": list, "today": today().Format(time.DateOnly)})
}

// PUT /recurring/dismissed {key, dismissed}
func (s *Server) handleDismissRecurring(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key       string `json:"key"`
		Dismissed bool   `json:"dismissed"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	body.Key = strings.TrimSpace(body.Key)
	if body.Key == "" || len(body.Key) > 300 {
		writeError(w, http.StatusBadRequest, "Missing series key.")
		return
	}
	q, hh := db.New(s.db), HouseholdID(r)
	var err error
	if body.Dismissed {
		err = q.DismissRecurring(r.Context(), db.DismissRecurringParams{HouseholdID: hh, Key: body.Key})
	} else {
		err = q.RestoreRecurring(r.Context(), db.RestoreRecurringParams{HouseholdID: hh, Key: body.Key})
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
