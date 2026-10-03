package server

import (
	"context"
	"encoding/json"
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
	APRSource        string  `json:"apr_source"`            // user | missing
	PromoUntil       string  `json:"promo_until,omitempty"` // 0% through this date, then APRBps; only while it hasn't ended
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
	Strategy        debt.Strategy        `json:"strategy"` // the saved plan's strategy (snowball | avalanche)
	Ready           bool                 `json:"ready"`    // every debt has an APR and a minimum; plans are empty until then
	Plans           map[string]debt.Plan `json:"plans"`
}

const setDebtPlan = "debt.plan"

// debtPlan is the payoff plan the household follows: saved from Debt Free Future, read by the
// budget to suggest each debt's payment.
type debtPlan struct {
	Strategy debt.Strategy `json:"strategy"`
	Extra    int64         `json:"extra"` // cents a month beyond the minimums
}

func (s *Server) loadDebtPlan(ctx context.Context, hh int64) (debtPlan, error) {
	p := debtPlan{Strategy: debt.Avalanche, Extra: 10000}
	rows, err := db.New(s.db).ListHouseholdSettings(ctx, hh)
	if err != nil {
		return p, err
	}
	for _, r := range rows {
		if r.Key == setDebtPlan {
			var v debtPlan
			if json.Unmarshal([]byte(r.Value), &v) == nil && (v.Strategy == debt.Snowball || v.Strategy == debt.Avalanche) && v.Extra >= 0 {
				p = v
			}
		}
	}
	return p, nil
}

func parseExtra(v string) (int64, bool) {
	c, err := money.ParseCents(strings.TrimSpace(v))
	return c, err == nil && c >= 0 && c <= 100_000_000
}

// GET /reports/debt?extra=200: debts owed with their cost, two years of balances, and payoff
// plans (minimums only, snowball and avalanche with extra dollars a month on top; extra
// defaults to the saved plan's). Rates and minimums are never guessed: the plans are only run
// once the user has entered them all.
func (s *Server) handleDebtReport(w http.ResponseWriter, r *http.Request) {
	ctx, hh := r.Context(), HouseholdID(r)
	plan, err := s.loadDebtPlan(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	extra := plan.Extra
	if v := r.URL.Query().Get("extra"); strings.TrimSpace(v) != "" {
		c, ok := parseExtra(v)
		if !ok {
			writeError(w, http.StatusBadRequest, "extra must be a dollar amount like 200")
			return
		}
		extra = c
	}
	rep, err := s.debtReport(ctx, hh, extra)
	if err != nil {
		s.internalError(w, err)
		return
	}
	rep.Strategy = plan.Strategy
	writeJSON(w, http.StatusOK, rep)
}

// PUT /reports/debt/plan {strategy, extra}: the plan to follow.
func (s *Server) handleSaveDebtPlan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Strategy debt.Strategy `json:"strategy"`
		Extra    string        `json:"extra"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Strategy != debt.Snowball && in.Strategy != debt.Avalanche {
		writeError(w, http.StatusBadRequest, "Strategy must be snowball or avalanche.")
		return
	}
	p := debtPlan{Strategy: in.Strategy}
	if strings.TrimSpace(in.Extra) != "" {
		c, ok := parseExtra(in.Extra)
		if !ok {
			writeError(w, http.StatusBadRequest, "Extra must be a dollar amount like 200.")
			return
		}
		p.Extra = c
	}
	b, _ := json.Marshal(p)
	if err := db.New(s.db).SetHouseholdSetting(r.Context(), db.SetHouseholdSettingParams{HouseholdID: HouseholdID(r), Key: setDebtPlan, Value: string(b)}); err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
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
		promoMonths := 0
		if a.PromoUntil != nil && *a.PromoUntil >= today.Format(time.DateOnly) {
			d.PromoUntil = *a.PromoUntil
			promoMonths = monthsThrough(today, d.PromoUntil)
		} else {
			d.MonthlyInterest = debt.MonthlyInterest(owed, d.APRBps)
		}
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
		sims = append(sims, debt.Debt{ID: a.ID, Name: a.Name, Balance: owed, APRBps: d.APRBps, MinPayment: d.MinPayment, PromoMonths: promoMonths})
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

// monthsThrough counts the plan months (each ending a month after the last) that end on or
// before until: how many statements an intro rate ending that day still covers.
func monthsThrough(today time.Time, until string) int {
	n := 0
	for n < debt.MaxMonths && today.AddDate(0, n+1, 0).Format(time.DateOnly) <= until {
		n++
	}
	return n
}
