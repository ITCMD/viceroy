package recurring

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Item is a tracked recurring transaction (confirmed from a suggestion, marked from a
// transaction, or entered by hand).
type Item struct {
	ID           int64
	Name         string
	MerchantID   int64  // 0 = match by MatchText only
	MatchText    string // matched (letters and digits only) inside the merchant or statement
	AccountID    int64  // 0 = any account
	CategoryID   int64
	CategoryName string
	CategoryIcon string
	AccountName  string
	Amount       int64 // signed cents
	AmountVaries bool
	Cadence      Cadence
	Anchor       string // YYYY-MM-DD, any due date
	Day2         int    // semimonthly: the other day of the month
	SeriesKey    string
}

// Occurrence is one due date of a series.
type Occurrence struct {
	Key          string  `json:"key"`
	ItemID       int64   `json:"item_id"` // 0 = detected
	Name         string  `json:"name"`
	Date         string  `json:"date"`
	Amount       int64   `json:"amount"` // paid: the actual amount
	Status       string  `json:"status"` // paid | upcoming | due (late, still in the grace window) | missed
	TxnID        int64   `json:"txn_id"`
	CategoryID   int64   `json:"category_id"`
	CategoryIcon string  `json:"category_icon"`
	Cadence      Cadence `json:"cadence"`
	Source       string  `json:"source"`
}

// Schedule is everything recurring for a household.
type Schedule struct {
	Today       time.Time
	Tracked     []Series // tracked items, by next date
	Suggestions []Series // detected, not tracked and not dismissed (strong and weak)
	Dismissed   []Series

	paid map[int64][]occ // tracked item id > matched transactions, by date
}

// Upcoming is what counts as coming up: tracked items plus strong suggestions, by next date.
func (sc Schedule) Upcoming() []Series {
	out := append([]Series(nil), sc.Tracked...)
	for _, s := range sc.Suggestions {
		if s.Strong {
			out = append(out, s)
		}
	}
	sortByNext(out)
	return out
}

// Tolerance is how many days a payment may land from its due date and still count for it.
func Tolerance(c Cadence) int {
	switch c {
	case Weekly:
		return 2
	case Biweekly, Semimonthly:
		return 4
	case Quarterly:
		return 10
	case Yearly:
		return 15
	}
	return 6
}

// Build matches history to the tracked items, detects suggestions from what's left, and
// computes next dates. dismissed holds dismissed series keys.
func Build(items []Item, txns []Txn, dismissed map[string]bool, today time.Time) Schedule {
	sc := Schedule{Today: today, paid: map[int64][]occ{}}
	claimed := map[int64]bool{}
	tracked := map[string]bool{}
	for _, it := range items {
		var os []occ
		for _, t := range txns {
			if claimed[t.ID] || !it.matches(t) {
				continue
			}
			d, err := time.Parse(time.DateOnly, t.Date)
			if err != nil {
				continue
			}
			claimed[t.ID] = true
			os = append(os, occ{d, t.Amount, t})
		}
		sort.SliceStable(os, func(i, j int) bool { return os[i].date.Before(os[j].date) })
		sc.paid[it.ID] = os
		s := Series{
			ID: it.ID, Source: "tracked", Key: fmt.Sprintf("item:%d", it.ID), Strong: true,
			Name: it.Name, MerchantID: it.MerchantID, MatchText: it.MatchText,
			CategoryID: it.CategoryID, CategoryName: it.CategoryName, CategoryIcon: it.CategoryIcon,
			AccountID: it.AccountID, AccountName: it.AccountName,
			Cadence: it.Cadence, Amount: it.Amount, Variable: it.AmountVaries, Count: len(os),
			Anchor: it.Anchor, Day2: it.Day2,
		}
		if len(os) > 0 {
			s.LastDate = os[len(os)-1].date.Format(time.DateOnly)
		}
		s.NextDate = sc.nextDue(s)
		sc.Tracked = append(sc.Tracked, s)
		if it.SeriesKey != "" {
			tracked[it.SeriesKey] = true
		}
	}
	var rest []Txn
	for _, t := range txns {
		if !claimed[t.ID] {
			rest = append(rest, t)
		}
	}
	for _, s := range Detect(rest, today) {
		switch {
		case tracked[s.Key]:
		case dismissed[s.Key]:
			s.Dismissed = true
			sc.Dismissed = append(sc.Dismissed, s)
		default:
			sc.Suggestions = append(sc.Suggestions, s)
		}
	}
	sortByNext(sc.Tracked)
	return sc
}

func sortByNext(ss []Series) {
	sort.SliceStable(ss, func(i, j int) bool {
		if ss[i].NextDate != ss[j].NextDate {
			return ss[i].NextDate < ss[j].NextDate
		}
		return ss[i].Name < ss[j].Name
	})
}

func (it Item) matches(t Txn) bool {
	if (t.Amount < 0) != (it.Amount < 0) || t.Amount == 0 {
		return false
	}
	if it.AccountID != 0 && t.AccountID != it.AccountID {
		return false
	}
	byMerchant := it.MerchantID != 0 && t.MerchantID == it.MerchantID
	if !byMerchant {
		k := MatchKey(it.MatchText)
		if k == "" || !(strings.Contains(MatchKey(t.Merchant), k) || strings.Contains(MatchKey(t.Description), k)) {
			return false
		}
	}
	return it.AmountVaries || abs(t.Amount-it.Amount)*100 <= abs(it.Amount)*25
}

// MatchKey keeps letters and digits, lowercased (like categorize.Key).
func MatchKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Dates lists a schedule's due dates in [from, to].
func Dates(c Cadence, anchor string, day2 int, from, to time.Time) []time.Time {
	a, err := time.Parse(time.DateOnly, anchor)
	if err != nil || to.Before(from) {
		return nil
	}
	var out []time.Time
	add := func(d time.Time) {
		if !d.Before(from) && !d.After(to) {
			out = append(out, d)
		}
	}
	switch c {
	case Weekly, Biweekly:
		step := 7
		if c == Biweekly {
			step = 14
		}
		k := int(from.Sub(a).Hours()/24) / step
		for d := a.AddDate(0, 0, (k-1)*step); !d.After(to); d = d.AddDate(0, 0, step) {
			add(d)
		}
	default:
		step := map[Cadence]int{Quarterly: 3, Yearly: 12}[c]
		if step == 0 {
			step = 1
		}
		months := (from.Year()-a.Year())*12 + int(from.Month()-a.Month())
		k := months / step
		if months < 0 && months%step != 0 {
			k--
		}
		for m := k - 1; ; m++ {
			first := time.Date(a.Year(), a.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, m*step, 0)
			if first.After(to) {
				break
			}
			days := []int{a.Day()}
			if c == Semimonthly && day2 > 0 {
				days = append(days, day2)
			}
			for _, dd := range days {
				add(time.Date(first.Year(), first.Month(), clampDay(first.Year(), first.Month(), dd), 0, 0, 0, 0, time.UTC))
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	}
	return out
}

// occurrences pairs a series' due dates in [from, to] with its payments.
func (sc Schedule) occurrences(s Series, from, to time.Time) []Occurrence {
	tol := Tolerance(s.Cadence)
	paid := sc.paid[s.ID]
	if s.Source == "detected" {
		paid = s.occ
	}
	used := map[int]bool{}
	var out []Occurrence
	mk := func(d time.Time, status string) Occurrence {
		return Occurrence{
			Key: s.Key, ItemID: s.ID, Name: s.Name, Date: d.Format(time.DateOnly), Amount: s.Amount, Status: status,
			CategoryID: s.CategoryID, CategoryIcon: s.CategoryIcon, Cadence: s.Cadence, Source: s.Source,
		}
	}
	anchor := s.Anchor
	if s.Source == "detected" {
		// Only project forward from the next date; earlier dates are the charges themselves.
		for _, p := range paid {
			if !p.date.Before(from) && !p.date.After(to) {
				o := mk(p.date, "paid")
				o.Amount, o.TxnID = p.amount, p.t.ID
				out = append(out, o)
			}
		}
		nd, _ := time.Parse(time.DateOnly, s.NextDate)
		start := from
		if nd.After(start) {
			start = nd
		}
		for _, d := range Dates(s.Cadence, anchor, s.Day2, start, to) {
			out = append(out, mk(d, sc.status(d, tol)))
		}
		return out
	}
	for _, d := range Dates(s.Cadence, anchor, s.Day2, from.AddDate(0, 0, -tol), to.AddDate(0, 0, tol)) {
		o := mk(d, "")
		best := -1
		for i, p := range paid {
			gap := absInt(int(p.date.Sub(d).Hours() / 24))
			if used[i] || gap > tol {
				continue
			}
			if best < 0 || gap < absInt(int(paid[best].date.Sub(d).Hours()/24)) {
				best = i
			}
		}
		if best >= 0 {
			used[best] = true
			o.Status, o.Amount, o.TxnID = "paid", paid[best].amount, paid[best].t.ID
		} else if d.Format(time.DateOnly) < anchor {
			continue // before the schedule was known, unpaid: not a missed payment
		} else {
			o.Status = sc.status(d, tol)
		}
		if d.Before(from) || d.After(to) {
			continue
		}
		out = append(out, o)
	}
	return out
}

func (sc Schedule) status(d time.Time, tol int) string {
	switch {
	case !d.Before(sc.Today):
		return "upcoming"
	case d.AddDate(0, 0, tol).Before(sc.Today):
		return "missed"
	}
	return "due"
}

// nextDue is the first due date from (today − tolerance) that isn't paid.
func (sc Schedule) nextDue(s Series) string {
	tol := Tolerance(s.Cadence)
	for span := 70; span <= 800; span *= 3 {
		for _, o := range sc.occurrences(s, sc.Today.AddDate(0, 0, -tol), sc.Today.AddDate(0, 0, span)) {
			if o.Status == "upcoming" || o.Status == "due" {
				return o.Date
			}
		}
	}
	return ""
}

// Occurrences lists due dates in [from, to] for tracked items and strong suggestions
// (includeWeak adds the weak ones), by date.
func (sc Schedule) Occurrences(from, to time.Time, includeWeak bool) []Occurrence {
	var out []Occurrence
	for _, s := range sc.Tracked {
		out = append(out, sc.occurrences(s, from, to)...)
	}
	for _, s := range sc.Suggestions {
		if s.Strong || includeWeak {
			out = append(out, sc.occurrences(s, from, to)...)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Due sums unpaid money-out occurrences (upcoming or late) dated in [from, to] per category.
func (sc Schedule) Due(from, to time.Time) map[int64]int64 {
	out := map[int64]int64{}
	for _, o := range sc.Occurrences(from, to, false) {
		if o.Amount < 0 && (o.Status == "upcoming" || o.Status == "due") && o.CategoryID != 0 {
			out[o.CategoryID] += -o.Amount
		}
	}
	return out
}

// TrackedFor is the tracked item a transaction was matched to as a payment.
func (sc Schedule) TrackedFor(txnID int64) (Series, bool) {
	for _, s := range sc.Tracked {
		for _, p := range sc.paid[s.ID] {
			if p.t.ID == txnID {
				return s, true
			}
		}
	}
	return Series{}, false
}

// ValidCadence reports whether c is a known cadence.
func ValidCadence(c Cadence) bool {
	switch c {
	case Weekly, Biweekly, Semimonthly, Monthly, Quarterly, Yearly:
		return true
	}
	return false
}
