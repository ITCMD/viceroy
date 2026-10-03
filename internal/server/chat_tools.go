package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"viceroy/internal/accounts"
	"viceroy/internal/ai"
	"viceroy/internal/budget"
	"viceroy/internal/budgetview"
	"viceroy/internal/db"
	"viceroy/internal/money"
	"viceroy/internal/reports"
)

// Chat tools are read-only views of one household's data. Amounts go to the model as dollar
// strings ("-12.30" = money out) so it never has to reason about cents.

func usd(cents int64) string { return money.Format(cents) }

// dollarsArg parses an optional dollar amount from a tool argument (number or string).
func dollarsArg(v json.RawMessage) (int64, bool, error) {
	s := strings.Trim(strings.TrimSpace(string(v)), `"`)
	if s == "" || s == "null" {
		return 0, false, nil
	}
	c, err := money.ParseCents(strings.TrimPrefix(s, "$"))
	if err != nil {
		return 0, false, fmt.Errorf("invalid amount %q", s)
	}
	return c, true, nil
}

func decodeArgs(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return errors.New("invalid arguments: " + err.Error())
	}
	return nil
}

func dateArg(s string, def time.Time) (time.Time, error) {
	if s == "" {
		return def, nil
	}
	d, err := budget.ParseDate(s)
	if err != nil {
		return def, fmt.Errorf("dates must be YYYY-MM-DD, got %q", s)
	}
	return d, nil
}

func schema(s string) json.RawMessage { return json.RawMessage(s) }

func (s *Server) chatTools(hh int64) []ai.Tool {
	q := db.New(s.db)
	return []ai.Tool{
		{
			Name:        "budget_status",
			Description: "Budget vs actual for each category in a budget period (month, week or paycheck), with a summary. 'planned_by_today' is how much of the budget the category's spending schedule expects to be used by today.",
			Parameters: schema(`{"type":"object","properties":{
				"view":{"type":"string","enum":["month","week","paycheck"],"description":"Period type, default month"},
				"date":{"type":"string","description":"Any date in the period, YYYY-MM-DD; default today"}}}`),
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var a struct{ View, Date string }
				if err := decodeArgs(raw, &a); err != nil {
					return nil, err
				}
				view := budget.View(a.View)
				if view != budget.ViewWeek && view != budget.ViewPaycheck {
					view = budget.ViewMonth
				}
				now := budgetview.Today()
				at, err := dateArg(a.Date, now)
				if err != nil {
					return nil, err
				}
				v, err := budgetview.Build(ctx, q, hh, view, at, now)
				if err != nil {
					return nil, err
				}
				type line struct {
					Group         string `json:"group"`
					Name          string `json:"name"`
					Budget        string `json:"budget"`
					Actual        string `json:"actual"`
					Remaining     string `json:"remaining"`
					PlannedByNow  string `json:"planned_by_today,omitempty"`
					BudgetedMonth string `json:"monthly_budget,omitempty"`
				}
				lines := []line{}
				for _, g := range v.Groups {
					for _, l := range g.Lines {
						if l.Budget == 0 && l.Actual == 0 {
							continue
						}
						ln := line{Group: g.Name, Name: l.Name, Budget: usd(l.Budget), Actual: usd(l.Actual), Remaining: usd(l.Budget - l.Actual)}
						if l.Expected > 0 {
							ln.PlannedByNow = usd(l.Expected)
						}
						if view != budget.ViewMonth {
							ln.BudgetedMonth = usd(l.MonthBudget)
						}
						lines = append(lines, ln)
					}
				}
				sm := v.Summary
				return map[string]any{
					"view": v.View, "start": v.Start, "end": v.End, "today": v.Today,
					"note": "For income lines 'actual' is money received; for expenses and goal contributions it is money spent/saved.",
					"summary": map[string]string{
						"income_budget": usd(sm.IncomeBudget), "income_actual": usd(sm.IncomeActual),
						"expense_budget": usd(sm.ExpenseBudget), "expense_actual": usd(sm.ExpenseActual),
						"goals_budget": usd(sm.GoalsBudget), "goals_actual": usd(sm.GoalsActual),
						"left_to_budget": usd(sm.LeftToBudget), "left_actual": usd(sm.LeftActual),
					},
					"categories": lines,
				}, nil
			},
		},
		{
			Name:        "spending_report",
			Description: "Spending and income totals over a date range, broken down by category, category group or merchant, with a value per month/quarter/year. Transfers are excluded; refunds net against spending.",
			Parameters: schema(`{"type":"object","properties":{
				"from":{"type":"string","description":"YYYY-MM-DD, default the first day of the month 5 months ago"},
				"to":{"type":"string","description":"YYYY-MM-DD inclusive, default today"},
				"by":{"type":"string","enum":["category","group","merchant"],"description":"default category"},
				"interval":{"type":"string","enum":["month","quarter","year"],"description":"default month"}}}`),
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var a struct{ From, To, By, Interval string }
				if err := decodeArgs(raw, &a); err != nil {
					return nil, err
				}
				to, err := dateArg(a.To, budgetview.Today())
				if err != nil {
					return nil, err
				}
				from, err := dateArg(a.From, time.Date(to.Year(), to.Month()-5, 1, 0, 0, 0, 0, time.UTC))
				if err != nil {
					return nil, err
				}
				if from.After(to) || to.Sub(from) > 10*366*24*time.Hour {
					return nil, errors.New("pick a range of at most 10 years with from before to")
				}
				iv, ok := reports.ParseInterval(a.Interval)
				if !ok {
					iv = reports.Month
				}
				by, ok := reports.ParseBy(a.By)
				if !ok {
					by = reports.ByCategory
				}
				rows, err := loadReportRows(ctx, q, hh, from, to)
				if err != nil {
					return nil, err
				}
				cats, err := loadReportCategories(ctx, q, hh)
				if err != nil {
					return nil, err
				}
				rep := reports.Build(rows, cats, reports.Buckets(from, to, iv), iv, by)
				keys := make([]string, len(rep.Buckets))
				for i, b := range rep.Buckets {
					keys[i] = b.Key
				}
				side := func(sd reports.Side, max int) map[string]any {
					type ln struct {
						Name     string   `json:"name"`
						Total    string   `json:"total"`
						PerPriod []string `json:"per_period"`
					}
					lines := []ln{}
					for i, l := range sd.Lines {
						if i == max {
							break
						}
						vals := make([]string, len(l.Values))
						for j, v := range l.Values {
							vals[j] = usd(v)
						}
						lines = append(lines, ln{l.Name, usd(l.Total), vals})
					}
					per := make([]string, len(sd.Values))
					for i, v := range sd.Values {
						per[i] = usd(v)
					}
					out := map[string]any{"total": usd(sd.Total), "per_period": per, "lines": lines}
					if len(sd.Lines) > max {
						out["more_lines_omitted"] = len(sd.Lines) - max
					}
					return out
				}
				return map[string]any{
					"from": budget.FormatDate(from), "to": budget.FormatDate(to), "periods": keys, "by": by,
					"spending": side(rep.Spending, 30), "income": side(rep.Income, 10),
				}, nil
			},
		},
		{
			Name:        "search_transactions",
			Description: "Find transactions (newest first). All filters are optional. Amounts are signed: negative = money out.",
			Parameters: schema(`{"type":"object","properties":{
				"query":{"type":"string","description":"Text in the merchant, statement, payee or notes"},
				"from":{"type":"string","description":"YYYY-MM-DD"},
				"to":{"type":"string","description":"YYYY-MM-DD inclusive"},
				"category":{"type":"string","description":"Category name, e.g. Groceries"},
				"account":{"type":"string","description":"Account name (partial match)"},
				"min_abs_amount":{"type":"number","description":"Only transactions at least this many dollars (either direction)"},
				"max_abs_amount":{"type":"number"},
				"limit":{"type":"integer","description":"Max transactions to list, default 25, max 100"}}}`),
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					Query, From, To, Category, Account string
					MinAbs                             json.RawMessage `json:"min_abs_amount"`
					MaxAbs                             json.RawMessage `json:"max_abs_amount"`
					Limit                              int
				}
				if err := decodeArgs(raw, &a); err != nil {
					return nil, err
				}
				return s.searchTransactions(ctx, q, hh, a.Query, a.From, a.To, a.Category, a.Account, a.MinAbs, a.MaxAbs, a.Limit)
			},
		},
		{
			Name:        "net_worth",
			Description: "Current net worth (assets, liabilities, per account group) and its history, one point per month plus today.",
			Parameters: schema(`{"type":"object","properties":{
				"months":{"type":"integer","description":"How far back, default 12, max 120"}}}`),
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var a struct{ Months int }
				if err := decodeArgs(raw, &a); err != nil {
					return nil, err
				}
				if a.Months <= 0 || a.Months > 120 {
					a.Months = 12
				}
				pts, err := s.netWorthHistory(ctx, hh, a.Months*31)
				if err != nil {
					return nil, err
				}
				if len(pts) == 0 {
					return map[string]string{"note": "No balance history yet."}, nil
				}
				fmtPt := func(p netWorthPoint) map[string]any {
					g := map[string]string{}
					for k, v := range p.Groups {
						g[k] = usd(v)
					}
					return map[string]any{"date": p.Date, "assets": usd(p.Assets), "liabilities": usd(p.Liabilities), "net": usd(p.Net), "groups": g}
				}
				hist := []map[string]any{}
				for i, p := range pts {
					if i == 0 || strings.HasSuffix(p.Date, "-01") || i == len(pts)-1 {
						hist = append(hist, fmtPt(p))
					}
				}
				return map[string]any{"current": fmtPt(pts[len(pts)-1]), "history": hist}, nil
			},
		},
		{
			Name:        "list_accounts",
			Description: "All accounts with type, current balance (negative = owed), institution and sync status.",
			Parameters:  schema(`{"type":"object","properties":{}}`),
			Run: func(ctx context.Context, _ json.RawMessage) (any, error) {
				accts, err := q.ListAccounts(ctx, hh)
				if err != nil {
					return nil, err
				}
				type acct struct {
					Name        string `json:"name"`
					Type        string `json:"type"`
					Group       string `json:"group"`
					Balance     string `json:"balance"`
					Institution string `json:"institution,omitempty"`
					Status      string `json:"status"`
					Manual      bool   `json:"manual,omitempty"`
					InNetWorth  bool   `json:"in_net_worth"`
				}
				out := []acct{}
				for _, a := range accts {
					if a.Hidden == 1 || a.Status == "closed" {
						continue
					}
					out = append(out, acct{a.Name, a.Type, accounts.Group(a.Type), usd(a.BalanceCents), a.InstitutionName, a.Status, a.IsManual == 1, a.IncludeInNetWorth == 1})
				}
				return map[string]any{"accounts": out}, nil
			},
		},
		{
			Name:        "upcoming_recurring",
			Description: "Recurring bills, subscriptions and paychecks (tracked by the user or detected from history), with their next expected date and typical amount.",
			Parameters: schema(`{"type":"object","properties":{
				"days":{"type":"integer","description":"Only series due within this many days, default 30; 0 = all"}}}`),
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				a := struct{ Days *int }{}
				if err := decodeArgs(raw, &a); err != nil {
					return nil, err
				}
				days := 30
				if a.Days != nil {
					days = *a.Days
				}
				sc, err := s.recurringSchedule(ctx, hh)
				if err != nil {
					return nil, err
				}
				limit := budget.FormatDate(budgetview.Today().AddDate(0, 0, days))
				type series struct {
					Name     string `json:"name"`
					Cadence  string `json:"cadence"`
					Amount   string `json:"typical_amount"`
					Variable bool   `json:"amount_varies,omitempty"`
					Next     string `json:"next_date"`
					Last     string `json:"last_date"`
					Category string `json:"category,omitempty"`
					Account  string `json:"account"`
				}
				out := []series{}
				for _, r := range sc.Upcoming() {
					if days > 0 && r.NextDate > limit {
						continue
					}
					out = append(out, series{r.Name, string(r.Cadence), usd(r.Amount), r.Variable, r.NextDate, r.LastDate, r.CategoryName, r.AccountName})
				}
				return map[string]any{"today": budget.FormatDate(budgetview.Today()), "series": out}, nil
			},
		},
		{
			Name:        "list_goals",
			Description: "Savings goals with target, amount saved so far and target date.",
			Parameters:  schema(`{"type":"object","properties":{}}`),
			Run: func(ctx context.Context, _ json.RawMessage) (any, error) {
				goals, err := q.ListGoals(ctx, hh)
				if err != nil {
					return nil, err
				}
				out := []map[string]any{}
				for _, g := range goals {
					if g.Archived == 1 {
						continue
					}
					m := map[string]any{"name": g.Name, "target": usd(g.TargetCents), "saved": usd(g.StartingCents + g.ContributedCents - g.WithdrawnCents)}
					if g.TargetDate.Valid {
						m["target_date"] = g.TargetDate.String
					}
					out = append(out, m)
				}
				return map[string]any{"goals": out}, nil
			},
		},
		{
			Name: "debt_payoff",
			Description: "Debts owed (credit cards, loans) with APR, minimum payment, monthly interest and interest charged in the last 12 months, " +
				"plus payoff plans: minimums only, snowball (smallest balance first) and avalanche (highest APR first) with extra_dollars a month on top.",
			Parameters: schema(`{"type":"object","properties":{"extra_dollars":{"type":"number","description":"Extra paid each month beyond the minimums (default 0)"}}}`),
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					Extra json.RawMessage `json:"extra_dollars"`
				}
				if err := decodeArgs(raw, &a); err != nil {
					return nil, err
				}
				extra, _, err := dollarsArg(a.Extra)
				if err != nil {
					return nil, err
				}
				rep, err := s.debtReport(ctx, hh, max(extra, 0))
				if err != nil {
					return nil, err
				}
				return debtForModel(rep), nil
			},
		},
	}
}

// debtForModel is the debt report in dollars, without the month-by-month series.
func debtForModel(rep debtReport) map[string]any {
	debts := []map[string]any{}
	for _, d := range rep.Debts {
		debts = append(debts, map[string]any{
			"name": d.Name, "type": d.Type, "owed": usd(d.Balance), "apr_percent": float64(d.APRBps) / 100, "apr_source": d.APRSource,
			"min_payment": usd(d.MinPayment), "min_payment_source": d.MinPaymentSource, "zero_percent_intro_until": d.PromoUntil,
			"monthly_interest": usd(d.MonthlyInterest), "interest_charged_last_12_months": usd(d.InterestPaid12m),
		})
	}
	names := map[int64]string{}
	for _, d := range rep.Debts {
		names[d.AccountID] = d.Name
	}
	plans := map[string]any{}
	for k, p := range rep.Plans {
		order := make([]map[string]any, 0, len(p.Debts))
		for _, d := range p.Debts {
			m := map[string]any{"name": names[d.ID], "order": d.Order, "interest": usd(d.Interest)}
			if d.Months > 0 {
				m["paid_off_month"] = debtMonth(rep.Start, d.Months)
			} else {
				m["paid_off_month"] = "never at this pace"
			}
			order = append(order, m)
		}
		plan := map[string]any{"monthly_payment": usd(p.Payment), "total_interest": usd(p.Interest), "debts": order}
		if p.Never {
			plan["debt_free"] = "never at this pace"
		} else {
			plan["debt_free"] = debtMonth(rep.Start, p.Months)
		}
		plans[k] = plan
	}
	return map[string]any{
		"total_owed": usd(rep.Total), "monthly_interest": usd(rep.MonthlyInterest), "interest_charged_last_12_months": usd(rep.InterestPaid12m),
		"extra_per_month": usd(rep.Extra), "debts": debts, "plans": plans,
		"terms_complete": rep.Ready,
		"note": "apr_source/min_payment_source missing = the user hasn't entered it (shown as 0). Plans only exist once every debt has both; " +
			"until then, ask the user to enter them under Reports › Debt Free Future instead of guessing.",
	}
}

func debtMonth(start string, n int) string {
	t, _ := time.Parse("2006-01", start)
	return t.AddDate(0, n, 0).Format("January 2006")
}

func (s *Server) searchTransactions(ctx context.Context, q *db.Queries, hh int64, text, fromS, toS, category, account string, minRaw, maxRaw json.RawMessage, limit int) (any, error) {
	if limit <= 0 {
		limit = 25
	}
	limit = min(limit, 100)
	from, err := dateArg(fromS, time.Time{})
	if err != nil {
		return nil, err
	}
	to, err := dateArg(toS, time.Time{})
	if err != nil {
		return nil, err
	}
	minAbs, hasMin, err := dollarsArg(minRaw)
	if err != nil {
		return nil, err
	}
	maxAbs, hasMax, err := dollarsArg(maxRaw)
	if err != nil {
		return nil, err
	}
	p := db.ListTransactionsParams{HouseholdID: hh, Uncategorized: 0, NeedsReview: 0, IncludeHidden: 0, Q: strings.TrimSpace(text), BeforeDate: "", FromDate: "", ToDate: "", Lim: 2000}
	if !to.IsZero() {
		p.BeforeDate, p.BeforeID = budget.FormatDate(to.AddDate(0, 0, 1)), 0
	}
	if category != "" {
		cats, err := q.ListCategories(ctx, hh)
		if err != nil {
			return nil, err
		}
		var found *int64
		for _, c := range cats {
			if strings.EqualFold(c.Name, strings.TrimSpace(category)) {
				found = &c.ID
				break
			}
		}
		if found == nil {
			names := make([]string, 0, len(cats))
			for _, c := range cats {
				names = append(names, c.Name)
			}
			sort.Strings(names)
			return nil, fmt.Errorf("no category named %q; categories are: %s", category, strings.Join(names, ", "))
		}
		p.CategoryID = *found
	}
	if account != "" {
		accts, err := q.ListAccounts(ctx, hh)
		if err != nil {
			return nil, err
		}
		for _, a := range accts {
			if strings.Contains(strings.ToLower(a.Name), strings.ToLower(strings.TrimSpace(account))) {
				p.AccountID = a.ID
				break
			}
		}
		if p.AccountID == nil {
			return nil, fmt.Errorf("no account matching %q", account)
		}
	}
	rows, err := q.ListTransactions(ctx, p)
	if err != nil {
		return nil, err
	}
	type txn struct {
		Date     string `json:"date"`
		Merchant string `json:"merchant"`
		Original string `json:"statement,omitempty"`
		Amount   string `json:"amount"`
		Category string `json:"category,omitempty"`
		Account  string `json:"account"`
		Pending  bool   `json:"pending,omitempty"`
		Notes    string `json:"notes,omitempty"`
	}
	out := []txn{}
	matched, total := 0, int64(0)
	for _, r := range rows {
		if !from.IsZero() && r.Date < budget.FormatDate(from) {
			break // rows are newest first
		}
		abs := max(r.AmountCents, -r.AmountCents)
		if (hasMin && abs < minAbs) || (hasMax && abs > maxAbs) {
			continue
		}
		matched++
		total += r.AmountCents
		if len(out) < limit {
			name := r.MerchantName
			if name == "" {
				name = r.Payee
			}
			orig := ""
			if r.Description != name {
				orig = r.Description
			}
			out = append(out, txn{r.Date, name, orig, usd(r.AmountCents), r.CategoryName, r.AccountName, r.Pending == 1, r.Notes})
		}
	}
	res := map[string]any{"matched": matched, "sum_of_matched": usd(total), "transactions": out}
	if matched > len(out) {
		res["note"] = fmt.Sprintf("showing the newest %d of %d matches", len(out), matched)
	}
	if len(rows) == int(p.Lim) {
		res["truncated"] = "only the newest 2000 transactions were searched; narrow the date range for older ones"
	}
	return res, nil
}
