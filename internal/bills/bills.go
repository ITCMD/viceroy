// Package bills turns bill events read from emails (payment due, scheduled, received) into each
// account's current state: what's due, and whether a payment covers it.
package bills

import (
	"time"

	"viceroy/internal/db"
)

const (
	Due       = "due"
	Scheduled = "scheduled"
	Paid      = "paid"
)

// Lookback is how far back bill events matter.
const Lookback = 75 * 24 * time.Hour

// Status is one account's bill state. Nil fields are unknown or no longer relevant.
type Status struct {
	Due       *db.AccountBill `json:"-"`
	Scheduled *db.AccountBill `json:"-"` // a payment set up after the due notice, not yet made
	Paid      *db.AccountBill `json:"-"` // a payment made after the due notice (the due is covered)
}

// ByAccount computes the status of every account with bill events. rows must be newest first
// (as ListRecentBills returns them); today is a local date.
func ByAccount(rows []db.AccountBill, today string) map[int64]Status {
	byAcct := map[int64][]db.AccountBill{}
	for _, r := range rows {
		if r.AccountID.Valid {
			byAcct[r.AccountID.Int64] = append(byAcct[r.AccountID.Int64], r)
		}
	}
	out := map[int64]Status{}
	for id, rs := range byAcct {
		var st Status
		for i := range rs {
			if rs[i].Kind == Due {
				st.Due = &rs[i]
				break
			}
		}
		// Payments count when they came after the due notice (or recently, without one).
		after := func(b db.AccountBill) bool {
			if st.Due != nil {
				return b.CreatedAt >= st.Due.CreatedAt
			}
			return true
		}
		for i := range rs {
			b := rs[i]
			if !after(b) {
				continue
			}
			switch {
			case b.Kind == Paid && st.Paid == nil:
				st.Paid = &rs[i]
			case b.Kind == Scheduled && st.Scheduled == nil && st.Paid == nil:
				// A scheduled date that has passed means the payment went out.
				if b.Date.Valid && b.Date.String < today {
					st.Paid = &rs[i]
				} else {
					st.Scheduled = &rs[i]
				}
			}
		}
		if st.Paid != nil {
			st.Scheduled = nil
			st.Due = nil
		}
		// A due date well in the past with nothing heard since is stale.
		if st.Due != nil && st.Due.Date.Valid && st.Due.Date.String < shift(today, -7) {
			st.Due = nil
		}
		if st.Due != nil || st.Scheduled != nil || st.Paid != nil {
			out[id] = st
		}
	}
	return out
}

func shift(date string, days int) string {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return date
	}
	return d.AddDate(0, 0, days).Format(time.DateOnly)
}

// DaysUntil is the number of days from today to date (negative when past), or false.
func DaysUntil(today, date string) (int, bool) {
	a, err1 := time.Parse(time.DateOnly, today)
	b, err2 := time.Parse(time.DateOnly, date)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return int(b.Sub(a).Hours() / 24), true
}
