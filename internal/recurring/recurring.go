// Package recurring finds repeating transactions (subscriptions, bills, paychecks) in
// transaction history: the same merchant, a steady cadence and a similar amount. Detection is
// pure and recomputed on demand; series the household confirms or enters by hand are stored as
// tracked items (schedule.go), and dismissals are stored by series key.
package recurring

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type Cadence string

const (
	Weekly      Cadence = "weekly"
	Biweekly    Cadence = "biweekly"
	Semimonthly Cadence = "semimonthly"
	Monthly     Cadence = "monthly"
	Quarterly   Cadence = "quarterly"
	Yearly      Cadence = "yearly"
)

// HistoryDays is how far back Detect should be given transactions (long enough to see a
// yearly charge twice and still call it active).
const HistoryDays = 800

type Txn struct {
	ID           int64
	Date         string // YYYY-MM-DD
	Amount       int64  // signed cents
	MerchantID   int64  // 0 = none; Merchant is then the payee/description
	Merchant     string
	Description  string // original statement
	CategoryID   int64
	CategoryName string
	CategoryIcon string
	AccountID    int64
	AccountName  string
}

// Series is a recurring transaction: detected from history (Source "detected", ID 0) or a
// tracked item (Source "tracked").
type Series struct {
	ID           int64   `json:"id"`     // tracked item id; 0 = detected
	Source       string  `json:"source"` // detected | tracked
	Key          string  `json:"key"`    // detected: series key; tracked: "item:<id>"
	Strong       bool    `json:"strong"` // detected: repeated enough to count as upcoming without confirming
	Name         string  `json:"name"`
	MerchantID   int64   `json:"merchant_id"`
	CategoryID   int64   `json:"category_id"`
	CategoryName string  `json:"category_name"`
	CategoryIcon string  `json:"category_icon"`
	AccountID    int64   `json:"account_id"`
	AccountName  string  `json:"account_name"`
	Cadence      Cadence `json:"cadence"`
	Amount       int64   `json:"amount"`   // typical (median) signed amount
	Variable     bool    `json:"variable"` // amounts differ by more than 5%
	Count        int     `json:"count"`
	LastDate     string  `json:"last_date"`
	NextDate     string  `json:"next_date"`
	Anchor       string  `json:"anchor_date"` // a due date the schedule repeats from
	Day2         int     `json:"day2"`        // semimonthly: the other day of the month
	MatchText    string  `json:"match_text"`  // tracked: text matched against merchants
	Dismissed    bool    `json:"dismissed"`

	occ []occ // detected: the charges it was built from
}

type rule struct {
	cadence   Cadence
	min, max  int // accepted gap in days
	minCount  int // strong: counts as upcoming on its own
	weakCount int // suggested only (e.g. two similar monthly charges)
	stale     int // days after the last charge before the series is considered over
}

var rules = []rule{
	{Weekly, 6, 8, 4, 4, 11},
	{Biweekly, 12, 17, 3, 3, 21}, // 17: the 15th to the 1st of a 31-day month (semimonthly)
	{Monthly, 26, 35, 3, 2, 45},
	{Quarterly, 84, 98, 3, 2, 130},
	{Yearly, 350, 380, 2, 2, 400},
}

type occ struct {
	date   time.Time
	amount int64
	t      Txn
}

// Detect returns active recurring series, sorted by next date. Weak ones (Strong false) are
// only suggestions.
func Detect(txns []Txn, today time.Time) []Series {
	groups := map[string][]occ{}
	var order []string
	for _, t := range txns {
		d, err := time.Parse(time.DateOnly, t.Date)
		if err != nil || t.Amount == 0 {
			continue
		}
		k := groupKey(t)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], occ{d, t.Amount, t})
	}
	var out []Series
	for _, k := range order {
		os := groups[k]
		sort.SliceStable(os, func(i, j int) bool { return os[i].date.Before(os[j].date) })
		if s, ok := detect(k, os, today, true); ok {
			out = append(out, s)
			continue
		}
		// A merchant with one steady charge among other purchases (a subscription at a
		// store): try the largest cluster of similar amounts on its own.
		if c := largestCluster(os); len(c) >= 2 && len(c) < len(os) {
			if s, ok := detect(fmt.Sprintf("%s:%d", k, abs(median(amounts(c)))/100), c, today, false); ok {
				out = append(out, s)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].NextDate != out[j].NextDate {
			return out[i].NextDate < out[j].NextDate
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func groupKey(t Txn) string {
	sign := "-"
	if t.Amount > 0 {
		sign = "+"
	}
	if t.MerchantID > 0 {
		return fmt.Sprintf("m%d%s", t.MerchantID, sign)
	}
	return "d:" + strings.ToLower(strings.Join(strings.Fields(t.Merchant), " ")) + sign
}

func detect(key string, os []occ, today time.Time, checkAmounts bool) (Series, bool) {
	os = mergeSameDay(os)
	if len(os) < 2 {
		return Series{}, false
	}
	gaps := make([]int, len(os)-1)
	for i := 1; i < len(os); i++ {
		gaps[i-1] = int(os[i].date.Sub(os[i-1].date).Hours()/24 + 0.5)
	}
	mg := medianInt(gaps)
	amts := amounts(os)
	med := median(amts)
	if checkAmounts && !similar(amts, med) {
		return Series{}, false
	}
	last := os[len(os)-1]
	sinceLast := int(today.Sub(last.date).Hours() / 24)
	for _, r := range rules {
		need := r.minCount
		if checkAmounts { // a whole merchant; a cluster inside a busy merchant must be strong
			need = r.weakCount
		}
		if mg < r.min || mg > r.max || len(os) < need || sinceLast > r.stale || sinceLast < -1 {
			continue
		}
		strong := len(os) >= r.minCount
		if !strong && !closeAmounts(amts) {
			continue
		}
		good := 0
		for _, g := range gaps {
			if g >= r.min && g <= r.max {
				good++
			}
		}
		if good*10 < len(gaps)*7 {
			continue
		}
		cad := r.cadence
		var days []int
		if cad == Biweekly {
			if d, ok := semimonthlyDays(os); ok {
				cad, days = Semimonthly, d
			}
		}
		s := Series{
			Source: "detected", Strong: strong, occ: os,
			Key: key, Name: last.t.Merchant, MerchantID: last.t.MerchantID,
			CategoryID: last.t.CategoryID, CategoryName: last.t.CategoryName, CategoryIcon: last.t.CategoryIcon,
			AccountID: last.t.AccountID, AccountName: last.t.AccountName,
			Cadence: cad, Amount: med, Count: len(os), LastDate: last.date.Format(time.DateOnly),
		}
		for _, a := range amts {
			if abs(a-med)*20 > abs(med) {
				s.Variable = true
			}
		}
		s.NextDate = next(cad, os, days).Format(time.DateOnly)
		s.Anchor = s.NextDate
		if len(days) == 2 {
			s.Day2 = days[0]
			if day(s.NextDate) == days[0] {
				s.Day2 = days[1]
			}
		}
		return s, true
	}
	return Series{}, false
}

// next projects the charge after the last one.
func next(c Cadence, os []occ, semiDays []int) time.Time {
	last := os[len(os)-1].date
	switch c {
	case Weekly:
		return last.AddDate(0, 0, 7)
	case Biweekly:
		return last.AddDate(0, 0, 14)
	case Semimonthly:
		for d := last.AddDate(0, 0, 1); ; d = d.AddDate(0, 0, 1) {
			for _, sd := range semiDays {
				if d.Day() == clampDay(d.Year(), d.Month(), sd) {
					return d
				}
			}
		}
	case Quarterly:
		return snapToDay(last.AddDate(0, 0, 91), typicalDay(os))
	case Yearly:
		return addMonths(last, 12, last.Day())
	}
	return snapToDay(last.AddDate(0, 0, 30), typicalDay(os))
}

// snapToDay picks the date nearest to approx whose day of the month is day (clamped). A
// charge on the 31st that once landed on the 1st is next due on the 31st, not a month later.
func snapToDay(approx time.Time, day int) time.Time {
	best := approx
	for i, n := range []int{-1, 0, 1} {
		c := addMonths(approx, n, day)
		if i == 0 || absInt(int(c.Sub(approx).Hours()/24)) < absInt(int(best.Sub(approx).Hours()/24)) {
			best = c
		}
	}
	return best
}

// semimonthlyDays reports whether ~15-day gaps fall on two fixed days of the month
// (e.g. the 1st and 15th) rather than every 14 days.
func semimonthlyDays(os []occ) ([]int, bool) {
	if len(os) < 4 {
		return nil, false
	}
	// Split the days of the month at their widest gap into an early and a late payday.
	ds := make([]int, len(os))
	for i, o := range os {
		ds[i] = o.date.Day()
	}
	sort.Ints(ds)
	cut, widest := 0, 0
	for i := 1; i < len(ds); i++ {
		if g := ds[i] - ds[i-1]; g > widest {
			cut, widest = i, g
		}
	}
	if widest < 7 {
		return nil, false
	}
	early, late := ds[:cut], ds[cut:]
	a, b := medianInt(early), medianInt(late)
	for _, o := range os {
		d := o.date.Day()
		if absInt(d-a) > 2 && absInt(d-b) > 3 {
			return nil, false
		}
	}
	return []int{a, b}, true
}

func addMonths(d time.Time, n, day int) time.Time {
	first := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, n, 0)
	return time.Date(first.Year(), first.Month(), clampDay(first.Year(), first.Month(), day), 0, 0, 0, 0, time.UTC)
}

func clampDay(y int, m time.Month, day int) int {
	last := time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return min(day, last)
}

func typicalDay(os []occ) int {
	ds := make([]int, len(os))
	for i, o := range os {
		ds[i] = o.date.Day()
	}
	return medianInt(ds)
}

func mergeSameDay(os []occ) []occ {
	out := make([]occ, 0, len(os))
	for _, o := range os {
		if n := len(out); n > 0 && out[n-1].date.Equal(o.date) {
			out[n-1].amount += o.amount
			continue
		}
		out = append(out, o)
	}
	return out
}

// similar: at least 2/3 of amounts are within 35% of the median.
func similar(amts []int64, med int64) bool {
	ok := 0
	for _, a := range amts {
		if abs(a-med)*100 <= abs(med)*35 {
			ok++
		}
	}
	return ok*3 >= len(amts)*2
}

// closeAmounts: every amount within 15% of the first (for a series seen only a few times).
func closeAmounts(amts []int64) bool {
	for _, a := range amts {
		if abs(a-amts[0])*100 > abs(amts[0])*15 {
			return false
		}
	}
	return true
}

func day(s string) int {
	d, _ := time.Parse(time.DateOnly, s)
	return d.Day()
}

// largestCluster groups amounts that sit within 10% of their neighbor and returns the
// biggest group (in date order).
func largestCluster(os []occ) []occ {
	s := append([]occ(nil), os...)
	sort.SliceStable(s, func(i, j int) bool { return abs(s[i].amount) < abs(s[j].amount) })
	var best, cur []occ
	for i, o := range s {
		if i > 0 && abs(o.amount)*10 > abs(s[i-1].amount)*11+1000 {
			cur = nil
		}
		cur = append(cur, o)
		if len(cur) > len(best) {
			best = append([]occ(nil), cur...)
		}
	}
	sort.SliceStable(best, func(i, j int) bool { return best[i].date.Before(best[j].date) })
	return best
}

func amounts(os []occ) []int64 {
	out := make([]int64, len(os))
	for i, o := range os {
		out[i] = o.amount
	}
	return out
}

func median(v []int64) int64 {
	s := append([]int64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

func medianInt(v []int) int {
	s := append([]int(nil), v...)
	sort.Ints(s)
	return s[len(s)/2]
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
