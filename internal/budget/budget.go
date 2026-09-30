// Package budget is the pure budget math: periods (month, week, paycheck), chunk schedules
// that say when in the month a category's money is usually spent, sub-period allowances and
// pacing. It has no DB access. Money is int64 cents; chunk weights are integers so no floats
// are involved anywhere.
//
// Dates are civil dates carried as time.Time at UTC midnight.
package budget

import (
	"fmt"
	"sort"
	"time"
)

const layout = "2006-01-02"

// ParseDate parses YYYY-MM-DD into a UTC-midnight date.
func ParseDate(s string) (time.Time, error) {
	return time.Parse(layout, s)
}

// FormatDate is the inverse of ParseDate.
func FormatDate(t time.Time) string { return t.Format(layout) }

// Date builds a UTC-midnight date; out-of-range days normalize like time.Date.
func Date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// MonthStart returns the first day of t's month.
func MonthStart(t time.Time) time.Time { return Date(t.Year(), t.Month(), 1) }

// DaysIn returns the number of days in t's month.
func DaysIn(t time.Time) int { return Date(t.Year(), t.Month()+1, 0).Day() }

// MonthKey is the "YYYY-MM" key budget amounts are stored under.
func MonthKey(t time.Time) string { return t.Format("2006-01") }

// ParseMonth parses a "YYYY-MM" key into the first day of that month.
func ParseMonth(s string) (time.Time, error) { return time.Parse("2006-01", s) }

func days(a, b time.Time) int { return int(b.Sub(a).Hours() / 24) }

// ---- chunk schedules ----

type ChunkKind string

const (
	Even        ChunkKind = "even"          // spread evenly over the month
	LumpDay     ChunkKind = "day"           // all on one day of the month
	LumpWeek    ChunkKind = "week"          // spread over one week of the month (1-4; week 4 runs to month end)
	EveryNWeeks ChunkKind = "every_n_weeks" // equal lumps every N weeks from an anchor date
)

// Chunk describes when in the month a category's money is usually spent.
type Chunk struct {
	Kind   ChunkKind `json:"kind"`
	Day    int       `json:"day,omitempty"`
	Week   int       `json:"week,omitempty"`
	Anchor string    `json:"anchor,omitempty"` // YYYY-MM-DD, for every_n_weeks
	Weeks  int       `json:"weeks,omitempty"`  // interval, for every_n_weeks
}

// Validate normalizes an empty kind to Even and rejects bad params.
func (c *Chunk) Validate() error {
	switch c.Kind {
	case "", Even:
		*c = Chunk{Kind: Even}
	case LumpDay:
		if c.Day < 1 || c.Day > 31 {
			return fmt.Errorf("day must be 1-31")
		}
		*c = Chunk{Kind: LumpDay, Day: c.Day}
	case LumpWeek:
		if c.Week < 1 || c.Week > 4 {
			return fmt.Errorf("week must be 1-4")
		}
		*c = Chunk{Kind: LumpWeek, Week: c.Week}
	case EveryNWeeks:
		if c.Weeks < 1 || c.Weeks > 4 {
			return fmt.Errorf("every 1-4 weeks")
		}
		if _, err := ParseDate(c.Anchor); err != nil {
			return fmt.Errorf("anchor date must be YYYY-MM-DD")
		}
		*c = Chunk{Kind: EveryNWeeks, Anchor: c.Anchor, Weeks: c.Weeks}
	default:
		return fmt.Errorf("unknown chunk kind %q", c.Kind)
	}
	return nil
}

// Weights returns one integer weight per day of month's month (index 0 = day 1). The sum is
// always positive; a schedule that lands on no day of the month falls back to even.
func (c Chunk) Weights(month time.Time) []int64 {
	n := DaysIn(month)
	w := make([]int64, n)
	switch c.Kind {
	case LumpDay:
		w[min(c.Day, n)-1] = 1
	case LumpWeek:
		from := (c.Week - 1) * 7
		to := from + 7
		if c.Week >= 4 {
			to = n
		}
		for i := from; i < to && i < n; i++ {
			w[i] = 1
		}
	case EveryNWeeks:
		anchor, err := ParseDate(c.Anchor)
		if err == nil && c.Weeks > 0 {
			step := c.Weeks * 7
			first := MonthStart(month)
			// First occurrence on or after the month start.
			off := days(anchor, first) % step
			if off < 0 {
				off += step
			}
			start := 0
			if off > 0 {
				start = step - off
			}
			for i := start; i < n; i += step {
				w[i] = 1
			}
		}
	}
	if sum(w) == 0 {
		for i := range w {
			w[i] = 1
		}
	}
	return w
}

func sum(w []int64) int64 {
	var s int64
	for _, v := range w {
		s += v
	}
	return s
}

// share returns amount × num / den rounded half away from zero.
func share(amount, num, den int64) int64 {
	if den == 0 {
		return 0
	}
	p := amount * num
	if p >= 0 {
		return (p + den/2) / den
	}
	return -((-p + den/2) / den)
}

// Allowance is the part of a month's budget available for days [from, to] (1-based,
// inclusive) of that month, given what was already spent earlier in the month:
//
//	(budget − spentBefore) × weight(from..to) / weight(from..month end)
//
// So $300 left with three even weeks to go gives $100 this week, while a lump on day 1
// gives the full amount to the week containing day 1 and nothing after it. Overspending
// earlier in the month leaves nothing (never a negative allowance).
func Allowance(budget, spentBefore int64, w []int64, from, to int) int64 {
	remaining := budget - spentBefore
	if remaining <= 0 || from < 1 || to < from || from > len(w) {
		return 0
	}
	to = min(to, len(w))
	return share(remaining, sum(w[from-1:to]), sum(w[from-1:]))
}

// Expected is how much of budget the schedule expects to be spent by the end of day
// (1-based) of the month; the pacing line.
func Expected(budget int64, w []int64, day int) int64 {
	if day <= 0 {
		return 0
	}
	day = min(day, len(w))
	return share(budget, sum(w[:day]), sum(w))
}

// ---- periods ----

type View string

const (
	ViewMonth    View = "month"
	ViewWeek     View = "week"
	ViewPaycheck View = "paycheck"
)

// Period is a half-open date range [Start, End).
type Period struct {
	Start, End time.Time
}

// Days returns the number of days in the period.
func (p Period) Days() int { return days(p.Start, p.End) }

// Contains reports whether d falls in the period.
func (p Period) Contains(d time.Time) bool { return !d.Before(p.Start) && d.Before(p.End) }

// MonthPart is the slice of a period inside one calendar month, as 1-based inclusive days.
type MonthPart struct {
	Month    time.Time // first of the month
	From, To int
}

// Parts splits the period by calendar month.
func (p Period) Parts() []MonthPart {
	var out []MonthPart
	for d := p.Start; d.Before(p.End); {
		m := MonthStart(d)
		next := Date(m.Year(), m.Month()+1, 1)
		end := next
		if p.End.Before(end) {
			end = p.End
		}
		out = append(out, MonthPart{Month: m, From: d.Day(), To: end.AddDate(0, 0, -1).Day()})
		d = end
	}
	return out
}

// MonthPeriod is the calendar month containing d.
func MonthPeriod(d time.Time) Period {
	m := MonthStart(d)
	return Period{m, Date(m.Year(), m.Month()+1, 1)}
}

// WeekPeriod is the 7-day week containing d that starts on weekStart.
func WeekPeriod(d time.Time, weekStart time.Weekday) Period {
	back := (int(d.Weekday()) - int(weekStart) + 7) % 7
	s := d.AddDate(0, 0, -back)
	return Period{s, s.AddDate(0, 0, 7)}
}

// PayKind is how often paychecks arrive.
type PayKind string

const (
	PayWeekly      PayKind = "weekly"
	PayBiweekly    PayKind = "biweekly"
	PaySemimonthly PayKind = "semimonthly"
	PayMonthly     PayKind = "monthly"
)

// PaySchedule defines paycheck periods: each runs from one payday up to the next.
type PaySchedule struct {
	Kind   PayKind `json:"kind"`
	Anchor string  `json:"anchor,omitempty"` // a known payday, for weekly/biweekly
	Days   []int   `json:"days,omitempty"`   // days of month: two for semimonthly, one for monthly
}

// DefaultPaySchedule is used until the user sets one: the 1st and 15th.
var DefaultPaySchedule = PaySchedule{Kind: PaySemimonthly, Days: []int{1, 15}}

// Validate normalizes and checks the schedule.
func (s *PaySchedule) Validate() error {
	switch s.Kind {
	case PayWeekly, PayBiweekly:
		if _, err := ParseDate(s.Anchor); err != nil {
			return fmt.Errorf("pay date must be YYYY-MM-DD")
		}
		s.Days = nil
	case PaySemimonthly:
		if len(s.Days) != 2 {
			return fmt.Errorf("semimonthly needs two days")
		}
		sort.Ints(s.Days)
		if s.Days[0] < 1 || s.Days[1] > 31 || s.Days[0] == s.Days[1] {
			return fmt.Errorf("pay days must be two different days 1-31")
		}
		s.Anchor = ""
	case PayMonthly:
		if len(s.Days) != 1 || s.Days[0] < 1 || s.Days[0] > 31 {
			return fmt.Errorf("monthly needs one day 1-31")
		}
		s.Anchor = ""
	default:
		return fmt.Errorf("unknown pay schedule %q", s.Kind)
	}
	return nil
}

// payday returns the given day of m's month, clamped to the month's last day.
func payday(m time.Time, day int) time.Time {
	return Date(m.Year(), m.Month(), min(day, DaysIn(m)))
}

// PeriodAt returns the paycheck period containing d.
func (s PaySchedule) PeriodAt(d time.Time) Period {
	switch s.Kind {
	case PayWeekly, PayBiweekly:
		step := 7
		if s.Kind == PayBiweekly {
			step = 14
		}
		anchor, err := ParseDate(s.Anchor)
		if err != nil {
			anchor = d
		}
		off := days(anchor, d) % step
		if off < 0 {
			off += step
		}
		start := d.AddDate(0, 0, -off)
		return Period{start, start.AddDate(0, 0, step)}
	case PaySemimonthly, PayMonthly:
		// Candidate paydays in the previous, current and next months; pick the latest ≤ d.
		var cands []time.Time
		for dm := -1; dm <= 1; dm++ {
			m := Date(d.Year(), d.Month()+time.Month(dm), 1)
			for _, day := range s.Days {
				cands = append(cands, payday(m, day))
			}
		}
		sort.Slice(cands, func(i, j int) bool { return cands[i].Before(cands[j]) })
		for i := len(cands) - 1; i >= 0; i-- {
			if !cands[i].After(d) {
				return Period{cands[i], cands[i+1]}
			}
		}
	}
	return MonthPeriod(d)
}

// PeriodFor returns the period of the given view containing d.
func PeriodFor(v View, d time.Time, weekStart time.Weekday, pay PaySchedule) Period {
	switch v {
	case ViewWeek:
		return WeekPeriod(d, weekStart)
	case ViewPaycheck:
		return pay.PeriodAt(d)
	}
	return MonthPeriod(d)
}

// ---- amounts ----

// AmountRow is one stored budget amount. A Forward row also applies to every later month
// that has no row of its own.
type AmountRow struct {
	Month   string // YYYY-MM
	Amount  int64
	Forward bool
}

// Resolve returns the budget for month: its own row if any, else the latest earlier Forward
// row, else 0.
func Resolve(rows []AmountRow, month string) int64 {
	var best *AmountRow
	for i := range rows {
		r := &rows[i]
		if r.Month == month {
			return r.Amount
		}
		if r.Forward && r.Month < month && (best == nil || r.Month > best.Month) {
			best = r
		}
	}
	if best == nil {
		return 0
	}
	return best.Amount
}

// PeriodAllowance sums Allowance over each calendar-month part of p. monthData returns the
// month's budget and what was spent in that month before the part starts. For a whole
// month this is just the month's budget.
func PeriodAllowance(p Period, c Chunk, monthData func(MonthPart) (budget, spentBefore int64)) int64 {
	var total int64
	for _, part := range p.Parts() {
		b, spent := monthData(part)
		total += Allowance(b, spent, c.Weights(part.Month), part.From, part.To)
	}
	return total
}

// SetAmount returns rows with month's budget set to amount.
//
// With forward, amount also applies to every later month: later rows are dropped. Without
// it, only month changes; if month held a forward row, the following month gets a forward
// row carrying the old value so later months keep what they had.
func SetAmount(rows []AmountRow, month string, amount int64, forward bool) []AmountRow {
	m, err := ParseMonth(month)
	if err != nil {
		return rows
	}
	next := MonthKey(m.AddDate(0, 1, 0))
	oldNext := Resolve(rows, next)
	hasNext := false
	var out []AmountRow
	for _, r := range rows {
		if r.Month == month || (forward && r.Month > month) {
			continue
		}
		hasNext = hasNext || r.Month == next
		out = append(out, r)
	}
	out = append(out, AmountRow{Month: month, Amount: amount, Forward: forward})
	if !forward && !hasNext && Resolve(out, next) != oldNext {
		out = append(out, AmountRow{Month: next, Amount: oldNext, Forward: true})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Month < out[j].Month })
	return out
}

// Line is one budget row for a period.
type Line struct {
	Budget   int64 // allowance for the period
	Actual   int64 // spent (or received) in the period
	Expected int64 // pacing: allowance up to and including today; 0 when today is outside the period
}

// ComputeLine works out a category's or goal's numbers for p. spent returns the
// sign-adjusted amount (positive = spent, for expenses) over [from, to).
func ComputeLine(p Period, c Chunk, rows []AmountRow, today time.Time, spent func(from, to time.Time) int64) Line {
	allowance := func(q Period) int64 {
		return PeriodAllowance(q, c, func(mp MonthPart) (int64, int64) {
			return Resolve(rows, MonthKey(mp.Month)), spent(mp.Month, Date(mp.Month.Year(), mp.Month.Month(), mp.From))
		})
	}
	l := Line{Budget: allowance(p), Actual: spent(p.Start, p.End)}
	if p.Contains(today) {
		l.Expected = allowance(Period{p.Start, today.AddDate(0, 0, 1)})
	}
	return l
}
