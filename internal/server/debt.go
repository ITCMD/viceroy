package server

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"viceroy/internal/accounts"
	"viceroy/internal/budgetview"
	"viceroy/internal/db"
	"viceroy/internal/debt"
	"viceroy/internal/money"
)

type debtDTO struct {
	AccountID        int64   `json:"account_id"`
	Name             string  `json:"name"`
	Type             string  `json:"type"`
	InstitutionName  string  `json:"institution_name"`
	Color            string  `json:"color"`
	LogoURL          *string `json:"logo_url"`
	Balance          int64   `json:"balance"` // owed, positive
	APRBps           int64   `json:"apr_bps"`
	APRSource        string  `json:"apr_source"` // user | missing
	MinPayment       int64   `json:"min_payment"`
	MinPaymentSource string  `json:"min_payment_source"` // user | bill (the bank's statement email) | missing
	MonthlyInterest  int64   `json:"monthly_interest"`
	InterestPaid12m  int64   `json:"interest_paid_12m"` // interest/finance charges posted in the last 12 months
}

type debtHistoryPoint struct {
	Date     string          `json:"date"`
	Total    int64           `json:"total"`
	Accounts map[int64]int64 `json:"accounts"`
}

type debtReport struct {
	Debts           []debtDTO            `json:"debts"`
	Total           int64                `json:"total"`
	MonthlyInterest int64                `json:"monthly_interest"`
	InterestPaid12m int64                `json:"interest_paid_12m"`
	History         []debtHistoryPoint   `json:"history"`
	Start           string               `json:"start"` // YYYY-MM of plan month 0 (this month)
	Extra           int64                `json:"extra"`
	Ready           bool                 `json:"ready"` // every debt has an APR and a minimum; plans are empty until then
	Plans           map[string]debt.Plan `json:"plans"`
}

// GET /reports/debt?extra=200: debts owed with their cost, two years of balances, and payoff
// plans (minimums only, snowball and avalanche with extra dollars a month on top). Rates and
// minimums are never guessed: the plans are only run once the user has entered them all.
func (s *Server) handleDebtReport(w http.ResponseWriter, r *http.Request) {
	var extra int64
	if v := strings.TrimSpace(r.URL.Query().Get("extra")); v != "" {
		c, err := money.ParseCents(v)
		if err != nil || c < 0 || c > 100_000_000 {
			writeError(w, http.StatusBadRequest, "extra must be a dollar amount like 200")
			return
		}
		extra = c
	}
	rep, err := s.debtReport(r.Context(), HouseholdID(r), extra)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) debtReport(ctx context.Context, hh int64, extra int64) (debtReport, error) {
	q := db.New(s.db)
	today := budgetview.Today()
	rep := debtReport{Debts: []debtDTO{}, History: []debtHistoryPoint{}, Extra: extra, Start: today.Format("2006-01"), Ready: true, Plans: map[string]debt.Plan{}}
	dtos, err := s.accountDTOs(ctx, hh)
	if err != nil {
		return rep, err
	}
	charges, err := q.InterestCharges(ctx, db.InterestChargesParams{HouseholdID: hh, FromDate: today.AddDate(-1, 0, 0).Format(time.DateOnly)})
	if err != nil {
		return rep, err
	}
	paid := map[int64]int64{}
	for _, c := range charges {
		paid[c.AccountID] = c.Total
	}
	var sims []debt.Debt
	for _, a := range dtos {
		owed := -a.BalanceCents
		if !a.IsLiability || owed <= 0 || a.Status == "ignored" || a.Status == "review" || a.Status == "closed" || a.ReplacedBy != nil {
			continue
		}
		d := debtDTO{
			AccountID: a.ID, Name: a.Name, Type: a.Type, InstitutionName: a.InstitutionName, Color: a.Color, LogoURL: a.LogoURL,
			Balance: owed, InterestPaid12m: paid[a.ID],
		}
		d.APRSource, d.MinPaymentSource = "missing", "missing"
		if a.APRBps != nil {
			d.APRBps, d.APRSource = *a.APRBps, "user"
		}
		d.MonthlyInterest = debt.MonthlyInterest(owed, d.APRBps)
		switch {
		case a.MinPaymentCents != nil:
			d.MinPayment, d.MinPaymentSource = *a.MinPaymentCents, "user"
		case a.Bill != nil && a.Bill.MinimumCents != nil && *a.Bill.MinimumCents > 0:
			d.MinPayment, d.MinPaymentSource = *a.Bill.MinimumCents, "bill"
		}
		if d.APRSource == "missing" || d.MinPaymentSource == "missing" {
			rep.Ready = false
		}
		rep.Debts = append(rep.Debts, d)
		rep.Total += owed
		rep.MonthlyInterest += d.MonthlyInterest
		sims = append(sims, debt.Debt{ID: a.ID, Name: a.Name, Balance: owed, APRBps: d.APRBps, MinPayment: d.MinPayment})
	}
	for _, c := range charges {
		rep.InterestPaid12m += c.Total
	}
	sort.SliceStable(rep.Debts, func(i, j int) bool { return rep.Debts[i].Balance > rep.Debts[j].Balance })
	if rep.Ready && len(sims) > 0 {
		for _, st := range []debt.Strategy{debt.Minimum, debt.Snowball, debt.Avalanche} {
			rep.Plans[string(st)] = debt.Simulate(sims, st, extra)
		}
	}

	// Month-end owed balances for the last 24 months (and today), per liability account.
	accts, src, err := s.balanceSources(ctx, hh)
	if err != nil {
		return rep, err
	}
	var hs []accountHistory
	var ids []int64
	earliest := ""
	for _, a := range accts {
		if !accounts.IsLiability(a.Type) || a.Status == "ignored" || a.Status == "review" || a.ReplacedBy.Valid {
			continue
		}
		h, known, ok := src.history(a)
		if !ok {
			continue
		}
		if earliest == "" || known < earliest {
			earliest = known
		}
		hs, ids = append(hs, h), append(ids, a.ID)
	}
	if earliest == "" {
		return rep, nil
	}
	var dates []string
	for m := 23; m >= 1; m-- {
		end := time.Date(today.Year(), today.Month()-time.Month(m)+1, 0, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
		if end >= earliest[:8]+"01" {
			dates = append(dates, end)
		}
	}
	dates = append(dates, today.Format(time.DateOnly))
	for _, d := range dates {
		p := debtHistoryPoint{Date: d, Accounts: map[int64]int64{}}
		for i := range hs {
			if owed := -hs[i].at(d); owed > 0 {
				p.Accounts[ids[i]] = owed
				p.Total += owed
			}
		}
		rep.History = append(rep.History, p)
	}
	return rep, nil
}
