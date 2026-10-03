package budgetview

import (
	"context"
	"database/sql"
	"time"

	"viceroy/internal/accounts"
	"viceroy/internal/budget"
	"viceroy/internal/db"
)

// Debt Repayment lines: the builtin Debt Repayment category is split into one line per debt
// account plus "Other debt payments".
//
// A debt account's actual is read from the account itself: payments into it, less (in "net"
// mode) new charges on it. Card purchases already count in their own categories, so net mode
// keeps that money from counting twice; "paid" mode shows the whole payment. A Debt Repayment
// transaction on a bank account counts toward its debt through the matching payment on the
// debt account (same amount within matchDays), or as Other when there isn't one (a lender
// that isn't connected).

const (
	DebtRepaymentKey = "debt_repayment" // categories.builtin (categorize.DebtRepayment)
	DebtActualNet    = "net"
	DebtActualPaid   = "paid"
	matchDays        = 5
)

// DebtSplit holds the money behind the Debt Repayment lines over a date range.
type DebtSplit struct {
	CategoryID int64
	Accounts   []db.Account // debt accounts that may get a line (not ignored, in review or replaced)
	Mode       string
	Paid       Totals // per account: money in (payments, refunds)
	Charged    Totals // per account: money out (charges, interest), positive
	Other      Totals // under key 0: Debt Repayment spending not matched to a debt account
}

func isDebtAccount(a db.Account) bool {
	return accounts.IsLiability(a.Type) && a.Status != "ignored" && a.Status != "review" && !a.ReplacedBy.Valid
}

// LoadDebtSplit reads the split for the Debt Repayment category catID over [from, to).
func LoadDebtSplit(ctx context.Context, q *db.Queries, hh, catID int64, from, to time.Time, mode string) (*DebtSplit, error) {
	d := &DebtSplit{CategoryID: catID, Mode: mode, Paid: Totals{}, Charged: Totals{}, Other: Totals{}}
	all, err := q.ListAccounts(ctx, hh)
	if err != nil {
		return nil, err
	}
	liability := map[int64]bool{}
	for _, a := range all {
		if accounts.IsLiability(a.Type) {
			liability[a.ID] = true
		}
		if isDebtAccount(a) {
			d.Accounts = append(d.Accounts, a)
		}
	}
	flows, err := q.DailyDebtAccountFlows(ctx, db.DailyDebtAccountFlowsParams{HouseholdID: hh, FromDate: budget.FormatDate(from), ToDate: budget.FormatDate(to)})
	if err != nil {
		return nil, err
	}
	for _, f := range flows {
		d.Paid.add(f.AccountID, f.Date, f.Paid)
		d.Charged.add(f.AccountID, f.Date, f.Charged)
	}
	txns, err := q.CategoryTxns(ctx, db.CategoryTxnsParams{HouseholdID: hh, CategoryID: sql.NullInt64{Int64: catID, Valid: true}, FromDate: budget.FormatDate(from), ToDate: budget.FormatDate(to)})
	if err != nil {
		return nil, err
	}
	pays, err := q.DebtAccountPayments(ctx, db.DebtAccountPaymentsParams{
		HouseholdID: hh, FromDate: budget.FormatDate(from.AddDate(0, 0, -matchDays)), ToDate: budget.FormatDate(to.AddDate(0, 0, matchDays+1)),
	})
	if err != nil {
		return nil, err
	}
	used := make([]bool, len(pays))
	for _, t := range txns {
		if liability[t.AccountID] {
			continue // on the debt account itself: already in its flows
		}
		if t.AmountCents < 0 && matchPayment(pays, used, t.Date, -t.AmountCents) {
			continue
		}
		d.Other.add(0, t.Date, -t.AmountCents)
	}
	return d, nil
}

// matchPayment marks the first unused payment of exactly amount within matchDays of date.
func matchPayment(pays []db.DebtAccountPaymentsRow, used []bool, date string, amount int64) bool {
	day, err := budget.ParseDate(date)
	if err != nil {
		return false
	}
	for i, p := range pays {
		if used[i] || p.AmountCents != amount {
			continue
		}
		pd, err := budget.ParseDate(p.Date)
		if err != nil {
			continue
		}
		if diff := pd.Sub(day).Hours() / 24; diff >= -matchDays && diff <= matchDays {
			used[i] = true
			return true
		}
	}
	return false
}

// Spent is a debt account's actual over [a, b).
func (d *DebtSplit) Spent(id int64) func(a, b time.Time) int64 {
	return func(a, b time.Time) int64 {
		v := d.Paid.Sum(id, a, b)
		if d.Mode != DebtActualPaid {
			v -= d.Charged.Sum(id, a, b)
		}
		return v
	}
}

// OtherSpent is Debt Repayment spending over [a, b) that no debt account line explains.
func (d *DebtSplit) OtherSpent(a, b time.Time) int64 { return d.Other.Sum(0, a, b) }

// active reports whether an account had any money in or out over [a, b).
func (d *DebtSplit) active(id int64, a, b time.Time) bool {
	return d.Paid.Sum(id, a, b) != 0 || d.Charged.Sum(id, a, b) != 0
}

// Lines builds the sub-lines for period p: a line for each debt account still owed, budgeted
// or active this period (largest balance first), then Other when it has a budget or spending.
// chunk (the category's timing) paces every sub-line.
func (d *DebtSplit) Lines(p budget.Period, chunk budget.Chunk, amounts Amounts, month string, now time.Time) []Line {
	var out []Line
	from := budget.MonthStart(p.Start)
	for _, a := range sortByOwed(d.Accounts) {
		rows := amounts.Accounts[a.ID]
		owed := -a.BalanceCents
		budgeted := budget.Resolve(rows, month) != 0
		if !(owed > 0 && a.Status != "closed") && !budgeted && !d.active(a.ID, from, p.End) {
			continue
		}
		l := budget.ComputeLine(p, chunk, rows, now, d.Spent(a.ID), nil)
		out = append(out, Line{
			ID: a.ID, AccountID: a.ID, Name: a.Name, Budget: l.Budget, Actual: l.Actual, Expected: l.Expected,
			MonthBudget: budget.Resolve(rows, month), Chunk: chunk,
		})
	}
	if len(out) == 0 {
		return nil
	}
	rows := amounts.Cats[d.CategoryID]
	l := budget.ComputeLine(p, chunk, rows, now, d.OtherSpent, nil)
	if budget.Resolve(rows, month) != 0 || l.Actual != 0 {
		out = append(out, Line{
			ID: d.CategoryID, Other: true, Name: "Other debt payments", Budget: l.Budget, Actual: l.Actual, Expected: l.Expected,
			MonthBudget: budget.Resolve(rows, month), Chunk: chunk,
		})
	}
	return out
}

func sortByOwed(as []db.Account) []db.Account {
	out := append([]db.Account(nil), as...)
	for i := 1; i < len(out); i++ { // insertion sort: stable, lists are short
		for j := i; j > 0 && out[j].BalanceCents < out[j-1].BalanceCents; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
