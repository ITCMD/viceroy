// Package reports turns per-day transaction totals into income and spending reports:
// totals per time bucket, broken down by category, category group or merchant.
// It is pure (no DB access) so the aggregation is easy to test.
package reports

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type Interval string

const (
	Month   Interval = "month"
	Quarter Interval = "quarter"
	Year    Interval = "year"
)

func ParseInterval(s string) (Interval, bool) {
	switch Interval(s) {
	case Month, Quarter, Year:
		return Interval(s), true
	}
	return "", false
}

type By string

const (
	ByCategory By = "category"
	ByGroup    By = "group"
	ByMerchant By = "merchant"
)

func ParseBy(s string) (By, bool) {
	switch By(s) {
	case ByCategory, ByGroup, ByMerchant:
		return By(s), true
	}
	return "", false
}

// Row is one signed per-day total (negative = money out). CategoryID 0 = uncategorized;
// Kind is the category group kind ("" when uncategorized).
type Row struct {
	Date       string
	CategoryID int64
	Kind       string
	Merchant   string
	Total      int64
}

type Category struct {
	ID        int64
	Name      string
	Icon      string
	GroupID   int64
	GroupName string
}

// Bucket is one time slice of the report, [Start, End] inclusive.
type Bucket struct {
	Key   string `json:"key"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// Line is one breakdown entry (a category, group or merchant) with its value per bucket.
type Line struct {
	Key    string  `json:"key"`
	ID     int64   `json:"id"` // category or group id; 0 for merchants and uncategorized
	Name   string  `json:"name"`
	Icon   string  `json:"icon"`
	Total  int64   `json:"total"`
	Values []int64 `json:"values"`
}

// Side is income or spending, both as positive numbers (refunds net against spending).
type Side struct {
	Total  int64   `json:"total"`
	Values []int64 `json:"values"`
	Lines  []Line  `json:"lines"`
}

type Report struct {
	Buckets  []Bucket `json:"buckets"`
	Income   Side     `json:"income"`
	Spending Side     `json:"spending"`
}

// periodStart returns the start of the bucket containing d.
func periodStart(d time.Time, iv Interval) time.Time {
	switch iv {
	case Quarter:
		return time.Date(d.Year(), time.Month((int(d.Month())-1)/3*3+1), 1, 0, 0, 0, 0, time.UTC)
	case Year:
		return time.Date(d.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func nextPeriod(d time.Time, iv Interval) time.Time {
	switch iv {
	case Quarter:
		return d.AddDate(0, 3, 0)
	case Year:
		return d.AddDate(1, 0, 0)
	}
	return d.AddDate(0, 1, 0)
}

// Key names the bucket containing date (YYYY-MM-DD): "2026-09", "2026-Q3" or "2026".
func Key(date string, iv Interval) string {
	if len(date) < 7 {
		return ""
	}
	switch iv {
	case Quarter:
		m := int(date[5]-'0')*10 + int(date[6]-'0')
		return fmt.Sprintf("%s-Q%d", date[:4], (m-1)/3+1)
	case Year:
		return date[:4]
	}
	return date[:7]
}

// Buckets splits [from, to] (inclusive dates) into calendar periods, clipped to the range.
func Buckets(from, to time.Time, iv Interval) []Bucket {
	var out []Bucket
	for p := periodStart(from, iv); !p.After(to); p = nextPeriod(p, iv) {
		start, end := p, nextPeriod(p, iv).AddDate(0, 0, -1)
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		s := start.Format(time.DateOnly)
		out = append(out, Bucket{Key: Key(s, iv), Start: s, End: end.Format(time.DateOnly)})
	}
	return out
}

// Build aggregates rows (all inside the buckets' range) into income and spending sides.
// Transfers are left out. Uncategorized money out counts as spending and money in as income.
func Build(rows []Row, cats map[int64]Category, buckets []Bucket, iv Interval, by By) Report {
	idx := make(map[string]int, len(buckets))
	for i, b := range buckets {
		idx[b.Key] = i
	}
	n := len(buckets)
	type acc struct {
		side  *Side
		lines map[string]*Line
	}
	income := acc{&Side{Values: make([]int64, n)}, map[string]*Line{}}
	spending := acc{&Side{Values: make([]int64, n)}, map[string]*Line{}}

	for _, r := range rows {
		bi, ok := idx[Key(r.Date, iv)]
		isIncome, v, counted := Classify(r)
		if !ok || !counted {
			continue
		}
		a := spending
		if isIncome {
			a = income
		}
		key, id, name, icon := lineOf(r, cats, by)
		l := a.lines[key]
		if l == nil {
			l = &Line{Key: key, ID: id, Name: name, Icon: icon, Values: make([]int64, n)}
			a.lines[key] = l
		}
		l.Values[bi] += v
		l.Total += v
		a.side.Values[bi] += v
		a.side.Total += v
	}
	return Report{Buckets: buckets, Income: finish(income.side, income.lines), Spending: finish(spending.side, spending.lines)}
}

// Classify says which side a row counts on and its positive-is-more value there.
// Transfers don't count. Refunds in an expense category come back as negative spending.
func Classify(r Row) (income bool, v int64, counted bool) {
	switch {
	case r.Kind == "transfer":
		return false, 0, false
	case r.Kind == "income" || (r.Kind == "" && r.Total > 0):
		return true, r.Total, true
	}
	return false, -r.Total, true
}

func lineOf(r Row, cats map[int64]Category, by By) (key string, id int64, name, icon string) {
	c, known := cats[r.CategoryID]
	switch by {
	case ByMerchant:
		name = strings.TrimSpace(r.Merchant)
		if name == "" {
			name = "Unknown"
		}
		return "m:" + strings.ToLower(name), 0, name, ""
	case ByGroup:
		if !known {
			return "g0", 0, "Uncategorized", ""
		}
		return fmt.Sprintf("g%d", c.GroupID), c.GroupID, c.GroupName, ""
	}
	if !known {
		return "c0", 0, "Uncategorized", ""
	}
	return fmt.Sprintf("c%d", c.ID), c.ID, c.Name, c.Icon
}

// finish drops zero lines and sorts the rest largest first (ties by name).
func finish(s *Side, lines map[string]*Line) Side {
	s.Lines = []Line{}
	for _, l := range lines {
		if l.Total != 0 {
			s.Lines = append(s.Lines, *l)
		}
	}
	sort.Slice(s.Lines, func(i, j int) bool {
		if s.Lines[i].Total != s.Lines[j].Total {
			return s.Lines[i].Total > s.Lines[j].Total
		}
		return s.Lines[i].Name < s.Lines[j].Name
	})
	return *s
}
