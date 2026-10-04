// Package notify raises alerts (over budget, ahead of pace, large transaction, account stopped
// syncing), stores them for the in-app list and delivers them with Web Push.
package notify

import (
	"errors"
	"fmt"
	"strings"

	"viceroy/internal/budget"
	"viceroy/internal/budgetview"
	"viceroy/internal/db"
)

// Prefs are one user's notification settings.
type Prefs struct {
	OverBudget    bool  `json:"over_budget"`
	Pacing        bool  `json:"pacing"`
	PacingPct     int   `json:"pacing_pct"` // alert when spending runs this % ahead of plan
	LargeTxn      bool  `json:"large_txn"`
	LargeTxnCents int64 `json:"large_txn_cents"`
	Disconnected  bool  `json:"disconnected"`
	PaymentDue    bool  `json:"payment_due"`  // a card payment is due within DueSoonDays and none is scheduled
	BankNotices   bool  `json:"bank_notices"` // messages the AI read from bank emails (security alerts...)
}

var DefaultPrefs = Prefs{OverBudget: true, Pacing: true, PacingPct: 25, LargeTxn: true, LargeTxnCents: 500_00, Disconnected: true, PaymentDue: true, BankNotices: true}

// DueSoonDays is how early a payment-due reminder fires.
const DueSoonDays = 3

func (p Prefs) Validate() error {
	if p.PacingPct < 5 || p.PacingPct > 200 {
		return errors.New("pacing threshold must be between 5% and 200%")
	}
	if p.LargeTxnCents < 1_00 {
		return errors.New("large transaction amount must be at least $1")
	}
	return nil
}

// Alert is one notification before it is stored. Key dedupes it: an alert with a key the user
// already got is dropped.
type Alert struct {
	Kind  string `json:"kind"`
	Key   string `json:"key"`
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
}

// minPaceGap keeps pacing alerts quiet for tiny amounts.
const minPaceGap = 10_00

// BudgetAlerts checks the expense lines of a month budget view: over budget, or (when not over)
// spending more than PacingPct ahead of what the category's schedule plans through the end of
// this week (Line.WeekExpected). Pacing alerts at most once per category per week.
func BudgetAlerts(v budgetview.View, p Prefs) []Alert {
	var out []Alert
	for _, g := range v.Groups {
		if g.Kind == "income" || g.Kind == "goals" {
			continue
		}
		for _, l := range g.Lines {
			if l.Budget <= 0 {
				continue
			}
			switch {
			case l.Actual > l.Budget:
				if p.OverBudget {
					out = append(out, Alert{
						Kind: "over_budget", Key: fmt.Sprintf("over:%d:%s", l.ID, v.Month), URL: "/budget",
						Title: "Over budget: " + l.Name,
						Body:  fmt.Sprintf("%s spent of %s this month (%s over).", Dollars(l.Actual), Dollars(l.Budget), Dollars(l.Actual-l.Budget)),
					})
				}
			case p.Pacing && l.WeekExpected > 0 && l.WeekExpected < l.Budget &&
				l.Actual*100 > l.WeekExpected*int64(100+p.PacingPct) && l.Actual-l.WeekExpected >= minPaceGap:
				out = append(out, Alert{
					Kind: "pacing", Key: fmt.Sprintf("pace:%d:%s", l.ID, v.PaceThrough), URL: "/budget",
					Title: l.Name + " is ahead of pace",
					Body: fmt.Sprintf("%s spent so far; about %s was planned through %s (%s budget).",
						Dollars(l.Actual), Dollars(l.WeekExpected), paceDayLabel(v.PaceThrough), Dollars(l.Budget)),
				})
			}
		}
	}
	return out
}

// paceDayLabel formats a YYYY-MM-DD date as "Sat, Oct 10".
func paceDayLabel(d string) string {
	t, err := budget.ParseDate(d)
	if err != nil {
		return "this week"
	}
	return t.Format("Mon, Jan 2")
}

// LargeTxnAlert describes a large transaction.
func LargeTxnAlert(t db.ListLargeTransactionsRow) Alert {
	body := t.Merchant + " · " + t.AccountName
	if t.Source == "email" {
		body += " (email alert)"
	}
	return Alert{
		Kind: "large_txn", Key: fmt.Sprintf("txn:%d", t.ID), URL: "/transactions",
		Title: "Large transaction: " + Dollars(-t.AmountCents), Body: body,
	}
}

// BrokenAccountAlert describes an account that stopped syncing. since (the sync event time)
// makes a later break of the same account alert again.
func BrokenAccountAlert(a db.ListBrokenAccountsRow) Alert {
	body := "It wasn't shared in the latest SimpleFIN sync. Check the connection on SimpleFIN Bridge."
	if a.Status == "active" && a.InstitutionStatus == "reauth" {
		body = "The bank needs you to sign in again on SimpleFIN Bridge."
	}
	return Alert{
		Kind: "disconnected", Key: fmt.Sprintf("disc:%d:%d", a.ID, a.Since), URL: "/accounts",
		Title: a.Name + " stopped syncing", Body: body,
	}
}

// DueSoonAlert reminds about a bill due in days days (0 = today) with nothing scheduled.
func DueSoonAlert(b db.AccountBill, account string, days int) Alert {
	when := "today"
	switch {
	case days == 1:
		when = "tomorrow"
	case days > 1:
		when = fmt.Sprintf("in %d days", days)
	}
	body := "Nothing is scheduled yet."
	if b.AmountCents.Valid {
		body = "Balance " + Dollars(b.AmountCents.Int64)
		if b.MinimumCents.Valid {
			body += ", minimum " + Dollars(b.MinimumCents.Int64)
		}
		body += ". Nothing is scheduled yet."
	}
	return Alert{Kind: "payment_due", Key: fmt.Sprintf("billdue:%d", b.ID), URL: "/accounts", Title: account + " payment due " + when, Body: body}
}

// Dollars formats cents as "$1,234.56" (or "-$5.00").
func Dollars(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	whole := fmt.Sprint(cents / 100)
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return fmt.Sprintf("%s$%s.%02d", sign, b.String(), cents%100)
}
