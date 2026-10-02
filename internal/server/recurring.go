package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/budget"
	"viceroy/internal/budgetview"
	"viceroy/internal/db"
	"viceroy/internal/recurring"
)

func (s *Server) recurringRoutes(r chi.Router) {
	r.Get("/recurring", s.handleListRecurring)
	r.Put("/recurring/dismissed", s.handleDismissRecurring)
	r.Post("/recurring/items", s.handleCreateRecurring)
	r.Patch("/recurring/items/{id}", s.handleUpdateRecurring)
	r.Delete("/recurring/items/{id}", s.handleDeleteRecurring)
}

func (s *Server) recurringSchedule(ctx context.Context, hh int64) (recurring.Schedule, error) {
	return recurring.Load(ctx, db.New(s.db), hh, budgetview.Today())
}

// nextPayday is the first payday after today on the household's pay schedule.
func nextPayday(ctx context.Context, q *db.Queries, hh int64, today time.Time) (time.Time, error) {
	st, err := budgetview.LoadSettings(ctx, q, hh)
	if err != nil {
		return today, err
	}
	return st.PaySchedule.PeriodAt(today).End, nil
}

// GET /recurring?from=&to=: tracked items, suggestions, dismissed suggestions, what's due
// before the next payday, and due dates in [from, to] (default: this month) for the calendar.
func (s *Server) handleListRecurring(w http.ResponseWriter, r *http.Request) {
	ctx, hh := r.Context(), HouseholdID(r)
	q := db.New(s.db)
	today := budgetview.Today()
	from, to := budget.MonthStart(today), budget.MonthStart(today).AddDate(0, 1, -1)
	if v, ok := parseDate(r.URL.Query().Get("from")); ok {
		from = v
	}
	if v, ok := parseDate(r.URL.Query().Get("to")); ok && !v.Before(from) && v.Sub(from) < 400*24*time.Hour {
		to = v
	}
	sc, err := s.recurringSchedule(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	payday, err := nextPayday(ctx, q, hh, today)
	if err != nil {
		s.internalError(w, err)
		return
	}
	beforePay := []recurring.Occurrence{}
	for _, o := range sc.Occurrences(today.AddDate(0, 0, -7), payday.AddDate(0, 0, -1), false) {
		if o.Status == "upcoming" || o.Status == "due" {
			beforePay = append(beforePay, o)
		}
	}
	nonNil := func(v []recurring.Series) []recurring.Series {
		if v == nil {
			return []recurring.Series{}
		}
		return v
	}
	occ := sc.Occurrences(from, to, false)
	if occ == nil {
		occ = []recurring.Occurrence{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"today": budget.FormatDate(today), "next_payday": budget.FormatDate(payday),
		"tracked": nonNil(sc.Tracked), "suggestions": nonNil(sc.Suggestions), "dismissed": nonNil(sc.Dismissed),
		"upcoming": nonNil(sc.Upcoming()), "before_payday": beforePay,
		"from": budget.FormatDate(from), "to": budget.FormatDate(to), "occurrences": occ,
	})
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

type recurringIn struct {
	Name          *string `json:"name"`
	MerchantID    *int64  `json:"merchant_id"` // 0 = none
	MatchText     *string `json:"match_text"`
	AccountID     *int64  `json:"account_id"`  // 0 = any
	CategoryID    *int64  `json:"category_id"` // 0 = none
	Amount        *int64  `json:"amount"`      // signed cents
	AmountVaries  *bool   `json:"amount_varies"`
	Cadence       *string `json:"cadence"`
	AnchorDate    *string `json:"anchor_date"`
	Day2          *int64  `json:"day2"`
	SeriesKey     string  `json:"series_key"`     // create: the suggestion it confirms
	TransactionID int64   `json:"transaction_id"` // create: fill the blanks from this transaction
}

func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id > 0} }

// apply copies the set fields onto it and checks the result.
func (in recurringIn) apply(ctx context.Context, q *db.Queries, hh int64, it *db.RecurringItem) string {
	if in.Name != nil {
		it.Name = strings.TrimSpace(*in.Name)
	}
	if in.MatchText != nil {
		it.MatchText = strings.TrimSpace(*in.MatchText)
	}
	if in.MerchantID != nil {
		it.MerchantID = nullID(*in.MerchantID)
	}
	if in.AccountID != nil {
		it.AccountID = nullID(*in.AccountID)
	}
	if in.CategoryID != nil {
		it.CategoryID = nullID(*in.CategoryID)
	}
	if in.Amount != nil {
		it.AmountCents = *in.Amount
	}
	if in.AmountVaries != nil {
		it.AmountVaries = b2i(*in.AmountVaries)
	}
	if in.Cadence != nil {
		it.Cadence = *in.Cadence
	}
	if in.AnchorDate != nil {
		it.AnchorDate = *in.AnchorDate
	}
	if in.Day2 != nil {
		it.Day2 = *in.Day2
	}
	switch {
	case it.Name == "" || len(it.Name) > 100:
		return "Give it a name (up to 100 characters)."
	case len(it.MatchText) > 100:
		return "The match text is too long."
	case !it.MerchantID.Valid && len(recurring.MatchKey(it.MatchText)) < 3:
		return "Say which transactions it matches: a merchant, or at least 3 letters of the bank's text."
	case it.AmountCents == 0:
		return "Enter an amount."
	case !recurring.ValidCadence(recurring.Cadence(it.Cadence)):
		return "Pick how often it repeats."
	case it.Cadence == string(recurring.Semimonthly) && (it.Day2 < 1 || it.Day2 > 31):
		return "Pick the second day of the month."
	}
	if _, err := budget.ParseDate(it.AnchorDate); err != nil {
		return "Pick the next due date."
	}
	if it.Cadence != string(recurring.Semimonthly) {
		it.Day2 = 0
	}
	if it.MerchantID.Valid {
		if _, err := q.GetMerchant(ctx, db.GetMerchantParams{ID: it.MerchantID.Int64, HouseholdID: hh}); err != nil {
			return "Unknown merchant."
		}
	}
	if it.AccountID.Valid {
		if _, err := q.GetAccount(ctx, db.GetAccountParams{ID: it.AccountID.Int64, HouseholdID: hh}); err != nil {
			return "Unknown account."
		}
	}
	if it.CategoryID.Valid {
		if _, err := q.GetCategory(ctx, db.GetCategoryParams{ID: it.CategoryID.Int64, HouseholdID: hh}); err != nil {
			return "Unknown category."
		}
	}
	return ""
}

// POST /recurring/items: track a recurring transaction. With transaction_id, blanks are filled
// from that transaction (merchant, account, category, amount, monthly from its date).
func (s *Server) handleCreateRecurring(w http.ResponseWriter, r *http.Request) {
	var in recurringIn
	if !readJSON(w, r, &in) {
		return
	}
	ctx, hh := r.Context(), HouseholdID(r)
	q := db.New(s.db)
	it := db.RecurringItem{HouseholdID: hh, Cadence: string(recurring.Monthly), SeriesKey: strings.TrimSpace(in.SeriesKey)}
	if len(it.SeriesKey) > 300 {
		writeError(w, http.StatusBadRequest, "Bad series key.")
		return
	}
	if in.TransactionID != 0 {
		t, err := q.GetTransactionByID(ctx, in.TransactionID)
		if err != nil || t.HouseholdID != hh {
			writeError(w, http.StatusBadRequest, "Unknown transaction.")
			return
		}
		it.MerchantID, it.AccountID, it.CategoryID = t.MerchantID, sql.NullInt64{Int64: t.AccountID, Valid: true}, t.CategoryID
		it.AmountCents, it.AnchorDate = t.AmountCents, t.Date
		it.Name, it.MatchText = t.Payee, t.Description
		if t.MerchantID.Valid {
			if m, err := q.GetMerchant(ctx, db.GetMerchantParams{ID: t.MerchantID.Int64, HouseholdID: hh}); err == nil {
				it.Name = m.Name
			}
		}
		if it.Name == "" {
			it.Name = t.Description
		}
		if len(it.Name) > 100 {
			it.Name = it.Name[:100]
		}
		if len(it.MatchText) > 100 {
			it.MatchText = it.MatchText[:100]
		}
	}
	if msg := in.apply(ctx, q, hh, &it); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	now := time.Now().Unix()
	created, err := q.CreateRecurringItem(ctx, db.CreateRecurringItemParams{
		HouseholdID: hh, Name: it.Name, MerchantID: it.MerchantID, MatchText: it.MatchText, AccountID: it.AccountID,
		CategoryID: it.CategoryID, AmountCents: it.AmountCents, AmountVaries: it.AmountVaries, Cadence: it.Cadence,
		AnchorDate: it.AnchorDate, Day2: it.Day2, SeriesKey: it.SeriesKey, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// PATCH /recurring/items/{id}
func (s *Server) handleUpdateRecurring(w http.ResponseWriter, r *http.Request) {
	var in recurringIn
	if !readJSON(w, r, &in) {
		return
	}
	ctx, hh := r.Context(), HouseholdID(r)
	q := db.New(s.db)
	it, err := q.GetRecurringItem(ctx, db.GetRecurringItemParams{ID: txnID(r), HouseholdID: hh})
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Recurring item not found.")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	if msg := in.apply(ctx, q, hh, &it); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := q.UpdateRecurringItem(ctx, db.UpdateRecurringItemParams{
		Name: it.Name, MerchantID: it.MerchantID, MatchText: it.MatchText, AccountID: it.AccountID, CategoryID: it.CategoryID,
		AmountCents: it.AmountCents, AmountVaries: it.AmountVaries, Cadence: it.Cadence, AnchorDate: it.AnchorDate,
		Day2: it.Day2, UpdatedAt: time.Now().Unix(), ID: it.ID, HouseholdID: hh,
	}); err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

// DELETE /recurring/items/{id}: stop tracking. A suggestion it came from shows up again
// unless it was dismissed.
func (s *Server) handleDeleteRecurring(w http.ResponseWriter, r *http.Request) {
	n, err := db.New(s.db).DeleteRecurringItem(r.Context(), db.DeleteRecurringItemParams{ID: txnID(r), HouseholdID: HouseholdID(r)})
	if err != nil {
		s.internalError(w, err)
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "Recurring item not found.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
