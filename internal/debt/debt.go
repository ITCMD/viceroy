// Package debt simulates paying debts down month by month: minimum payments only, or a
// fixed monthly amount (minimums plus extra) aimed at one debt at a time, smallest balance
// first (snowball) or highest rate first (avalanche). Money is int64 cents. It is pure.
package debt

import (
	"sort"
	"time"
)

type Strategy string

const (
	Minimum   Strategy = "minimum"
	Snowball  Strategy = "snowball"
	Avalanche Strategy = "avalanche"
)

// Debt is one balance owed. Balance is positive; APRBps is the yearly rate in basis points.
// PromoMonths is how many plan months an intro 0% rate still covers before APRBps applies.
type Debt struct {
	ID          int64
	Name        string
	Balance     int64
	APRBps      int64
	MinPayment  int64
	PromoMonths int
}

// rate is the yearly rate charged in plan month m (1-based).
func (d Debt) rate(m int) int64 {
	if m <= d.PromoMonths {
		return 0
	}
	return d.APRBps
}

// MaxMonths caps a plan; debts not paid by then never will be at that pace.
const MaxMonths = 600

// Payoff is when one debt is paid off in a plan.
type Payoff struct {
	ID       int64 `json:"id"`
	Months   int   `json:"months"`   // 0 = never (within MaxMonths)
	Interest int64 `json:"interest"` // interest charged until paid off
	Order    int   `json:"order"`    // 1 = targeted (or paid off) first
}

type Plan struct {
	Strategy Strategy `json:"strategy"`
	Months   int      `json:"months"` // until everything is paid; 0 when it never is
	Interest int64    `json:"interest"`
	Payment  int64    `json:"payment"`  // monthly amount paid while every debt is open
	Never    bool     `json:"never"`    // some debt doesn't shrink at this pace
	Balances []int64  `json:"balances"` // total owed at the end of each month, from month 1
	Debts    []Payoff `json:"debts"`
}

// MonthlyInterest is one month of interest on a balance at a yearly rate, rounded to a cent.
func MonthlyInterest(balance, aprBps int64) int64 {
	if balance <= 0 || aprBps <= 0 {
		return 0
	}
	return (balance*aprBps + 60000) / 120000
}

// Order lists debt indexes in the order a strategy targets them, by their regular rates.
func Order(debts []Debt, s Strategy) []int {
	return orderAt(debts, s, MaxMonths+1)
}

// orderAt is the order in plan month m: avalanche goes by the rate charged that month, so a
// debt still on an intro 0% rate waits until it ends.
func orderAt(debts []Debt, s Strategy, m int) []int {
	idx := make([]int, len(debts))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		x, y := debts[idx[a]], debts[idx[b]]
		if s == Avalanche && x.rate(m) != y.rate(m) {
			return x.rate(m) > y.rate(m)
		}
		if x.Balance != y.Balance {
			return x.Balance < y.Balance
		}
		return x.APRBps > y.APRBps
	})
	return idx
}

// Simulate runs a plan. With Snowball or Avalanche, the monthly payment stays at the sum of
// minimums plus extra the whole way: what a paid-off debt freed up rolls onto the next target.
// Minimum pays each debt's minimum and nothing more (extra is ignored). A debt's Order is when
// the plan first put extra toward it or it was paid off, whichever came first.
func Simulate(debts []Debt, s Strategy, extra int64) Plan {
	bal := make([]int64, len(debts))
	payoffs := make([]Payoff, len(debts))
	var budget int64
	for i, d := range debts {
		bal[i] = max(d.Balance, 0)
		payoffs[i].ID = d.ID
		budget += d.MinPayment
	}
	order := Order(debts, s)
	rank := 0
	ranked := func(i int) {
		if payoffs[i].Order == 0 {
			rank++
			payoffs[i].Order = rank
		}
	}
	if s == Minimum {
		for _, i := range order {
			ranked(i)
		}
	}
	promos := false
	for _, d := range debts {
		promos = promos || d.PromoMonths > 0
	}
	if s != Minimum {
		budget += max(extra, 0)
	}
	plan := Plan{Strategy: s, Payment: budget, Balances: []int64{}}
	open := func() bool {
		for _, b := range bal {
			if b > 0 {
				return true
			}
		}
		return false
	}
	for m := 1; m <= MaxMonths && open(); m++ {
		left := budget
		for i, d := range debts {
			if bal[i] <= 0 {
				continue
			}
			in := MonthlyInterest(bal[i], d.rate(m))
			bal[i] += in
			payoffs[i].Interest += in
			plan.Interest += in
			pay := min(d.MinPayment, bal[i])
			bal[i] -= pay
			left -= pay
		}
		if s != Minimum {
			if s == Avalanche && promos {
				order = orderAt(debts, s, m)
			}
			for _, i := range order {
				if left <= 0 {
					break
				}
				pay := min(left, bal[i])
				if pay > 0 {
					ranked(i)
				}
				bal[i] -= pay
				left -= pay
			}
		}
		var total int64
		for i := range debts {
			if bal[i] <= 0 && payoffs[i].Months == 0 && debts[i].Balance > 0 {
				payoffs[i].Months = m
				ranked(i)
			}
			total += max(bal[i], 0)
		}
		plan.Balances = append(plan.Balances, total)
		if total == 0 {
			plan.Months = m
		}
	}
	plan.Never = open()
	for _, i := range order { // never targeted nor paid off at this pace
		ranked(i)
	}
	plan.Debts = payoffs
	return plan
}

// MonthLabel is the month n months after from, as YYYY-MM.
func MonthLabel(from time.Time, n int) string {
	return time.Date(from.Year(), from.Month()+time.Month(n), 1, 0, 0, 0, 0, time.UTC).Format("2006-01")
}
