package budget

import (
	"testing"
	"time"
)

func d(s string) time.Time {
	t, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestWeights(t *testing.T) {
	sep := d("2026-09-01") // 30 days, starts Tuesday
	feb := d("2026-02-01") // 28 days
	cases := []struct {
		name  string
		c     Chunk
		month time.Time
		ones  []int // days with weight 1; nil = all days
	}{
		{"even", Chunk{Kind: Even}, sep, nil},
		{"zero value is even", Chunk{}, sep, nil},
		{"lump day", Chunk{Kind: LumpDay, Day: 5}, sep, []int{5}},
		{"lump day clamps to month end", Chunk{Kind: LumpDay, Day: 31}, feb, []int{28}},
		{"week 1", Chunk{Kind: LumpWeek, Week: 1}, sep, []int{1, 2, 3, 4, 5, 6, 7}},
		{"week 4 runs to month end", Chunk{Kind: LumpWeek, Week: 4}, sep, []int{22, 23, 24, 25, 26, 27, 28, 29, 30}},
		{"every 2 weeks from anchor in month", Chunk{Kind: EveryNWeeks, Weeks: 2, Anchor: "2026-09-04"}, sep, []int{4, 18}},
		{"every 2 weeks from anchor long before", Chunk{Kind: EveryNWeeks, Weeks: 2, Anchor: "2025-01-02"}, sep, []int{10, 24}},
		{"every 2 weeks from anchor after month", Chunk{Kind: EveryNWeeks, Weeks: 2, Anchor: "2026-12-04"}, sep, []int{11, 25}},
		{"weekly", Chunk{Kind: EveryNWeeks, Weeks: 1, Anchor: "2026-09-01"}, sep, []int{1, 8, 15, 22, 29}},
		{"bad anchor falls back to even", Chunk{Kind: EveryNWeeks, Weeks: 2, Anchor: "x"}, sep, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := tc.c.Weights(tc.month)
			if len(w) != DaysIn(tc.month) {
				t.Fatalf("len %d", len(w))
			}
			want := map[int]bool{}
			for _, x := range tc.ones {
				want[x] = true
			}
			for i, v := range w {
				exp := int64(0)
				if tc.ones == nil || want[i+1] {
					exp = 1
				}
				if v != exp {
					t.Fatalf("day %d weight %d, want %d (%v)", i+1, v, exp, w)
				}
			}
		})
	}
}

func TestAllowance(t *testing.T) {
	even28 := Chunk{Kind: Even}.Weights(d("2026-02-01"))
	rent := Chunk{Kind: LumpWeek, Week: 1}.Weights(d("2026-02-01"))
	cases := []struct {
		name                string
		budget, spentBefore int64
		w                   []int64
		from, to            int
		want                int64
	}{
		// Spec example: $300 left with three weeks to go → $100 this week.
		{"spec: 300 left, 3 weeks", 40000, 10000, even28, 8, 14, 10000},
		{"whole month is the budget", 40000, 0, even28, 1, 28, 40000},
		{"first week of even", 28000, 0, even28, 1, 7, 7000},
		{"overspent earlier leaves nothing", 10000, 12000, even28, 8, 14, 0},
		{"rent lump in week 1 gets all of it", 150000, 0, rent, 1, 7, 150000},
		{"rent after week 1 gets nothing", 150000, 0, rent, 8, 14, 0},
		{"rent paid in week 1", 150000, 150000, rent, 8, 14, 0},
		{"last days get the rest", 10000, 9000, even28, 22, 28, 1000},
		{"rounding half up", 1000, 0, even28[:3], 1, 1, 333},
		{"to past month end clamps", 28000, 21000, even28, 22, 40, 7000},
		{"from past month end", 28000, 0, even28, 29, 30, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Allowance(tc.budget, tc.spentBefore, tc.w, tc.from, tc.to); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}

func TestExpected(t *testing.T) {
	w := Chunk{Kind: Even}.Weights(d("2026-09-01"))
	if got := Expected(30000, w, 15); got != 15000 {
		t.Fatalf("even mid-month: %d", got)
	}
	if got := Expected(30000, w, 0); got != 0 {
		t.Fatalf("day 0: %d", got)
	}
	if got := Expected(30000, w, 99); got != 30000 {
		t.Fatalf("past end: %d", got)
	}
	lump := Chunk{Kind: LumpDay, Day: 1}.Weights(d("2026-09-01"))
	if got := Expected(150000, lump, 1); got != 150000 {
		t.Fatalf("lump day 1: %d", got)
	}
}

func TestPeriods(t *testing.T) {
	cases := []struct {
		name       string
		p          Period
		start, end string
	}{
		{"month", MonthPeriod(d("2026-09-30")), "2026-09-01", "2026-10-01"},
		{"week sunday start", WeekPeriod(d("2026-09-30"), time.Sunday), "2026-09-27", "2026-10-04"},
		{"week monday start on monday", WeekPeriod(d("2026-09-28"), time.Monday), "2026-09-28", "2026-10-05"},
		{"biweekly", PaySchedule{Kind: PayBiweekly, Anchor: "2026-01-02"}.PeriodAt(d("2026-09-30")), "2026-09-25", "2026-10-09"},
		{"biweekly anchor in future", PaySchedule{Kind: PayBiweekly, Anchor: "2027-01-01"}.PeriodAt(d("2026-09-30")), "2026-09-25", "2026-10-09"},
		{"weekly on payday", PaySchedule{Kind: PayWeekly, Anchor: "2026-09-04"}.PeriodAt(d("2026-09-25")), "2026-09-25", "2026-10-02"},
		{"semimonthly first half", PaySchedule{Kind: PaySemimonthly, Days: []int{1, 15}}.PeriodAt(d("2026-09-14")), "2026-09-01", "2026-09-15"},
		{"semimonthly second half", PaySchedule{Kind: PaySemimonthly, Days: []int{1, 15}}.PeriodAt(d("2026-09-30")), "2026-09-15", "2026-10-01"},
		{"semimonthly 15/31 in feb", PaySchedule{Kind: PaySemimonthly, Days: []int{15, 31}}.PeriodAt(d("2026-03-10")), "2026-02-28", "2026-03-15"},
		{"semimonthly before first payday", PaySchedule{Kind: PaySemimonthly, Days: []int{5, 20}}.PeriodAt(d("2026-01-02")), "2025-12-20", "2026-01-05"},
		{"monthly", PaySchedule{Kind: PayMonthly, Days: []int{25}}.PeriodAt(d("2026-09-30")), "2026-09-25", "2026-10-25"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if s, e := FormatDate(tc.p.Start), FormatDate(tc.p.End); s != tc.start || e != tc.end {
				t.Fatalf("got [%s, %s) want [%s, %s)", s, e, tc.start, tc.end)
			}
		})
	}
}

func TestParts(t *testing.T) {
	p := Period{d("2026-09-27"), d("2026-10-04")}
	parts := p.Parts()
	if len(parts) != 2 || parts[0].From != 27 || parts[0].To != 30 || parts[1].From != 1 || parts[1].To != 3 ||
		parts[1].Month != d("2026-10-01") {
		t.Fatalf("%+v", parts)
	}
	if n := len(MonthPeriod(d("2026-09-10")).Parts()); n != 1 {
		t.Fatalf("month parts %d", n)
	}
}

func TestPeriodAllowanceAcrossMonths(t *testing.T) {
	// Week Sep 27 - Oct 3: 4 of Sep's 30 days and 3 of Oct's 31.
	p := Period{d("2026-09-27"), d("2026-10-04")}
	budgets := map[string]int64{"2026-09": 30000, "2026-10": 31000}
	spent := map[string]int64{"2026-09": 26000, "2026-10": 0}
	got := PeriodAllowance(p, Chunk{Kind: Even}, func(mp MonthPart) (int64, int64) {
		k := MonthKey(mp.Month)
		return budgets[k], spent[k]
	})
	// Sep: 4000 left over 4 remaining days, all in the period → 4000. Oct: 31000 × 3/31 = 3000.
	if got != 7000 {
		t.Fatalf("got %d", got)
	}
}

func TestResolve(t *testing.T) {
	rows := []AmountRow{
		{Month: "2026-01", Amount: 100, Forward: true},
		{Month: "2026-03", Amount: 200},
		{Month: "2026-06", Amount: 300, Forward: true},
	}
	cases := map[string]int64{
		"2025-12": 0, "2026-01": 100, "2026-02": 100, "2026-03": 200, "2026-04": 100, "2026-06": 300, "2027-01": 300,
	}
	for m, want := range cases {
		if got := Resolve(rows, m); got != want {
			t.Errorf("%s: got %d want %d", m, got, want)
		}
	}
}

func TestValidate(t *testing.T) {
	good := []Chunk{{}, {Kind: LumpDay, Day: 31}, {Kind: LumpWeek, Week: 4}, {Kind: EveryNWeeks, Weeks: 2, Anchor: "2026-01-01"}}
	for _, c := range good {
		if err := c.Validate(); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
	bad := []Chunk{{Kind: LumpDay}, {Kind: LumpWeek, Week: 5}, {Kind: EveryNWeeks, Weeks: 2}, {Kind: "x"}}
	for _, c := range bad {
		if err := c.Validate(); err == nil {
			t.Errorf("%+v: expected error", c)
		}
	}
	ps := PaySchedule{Kind: PaySemimonthly, Days: []int{15, 1}}
	if err := ps.Validate(); err != nil || ps.Days[0] != 1 {
		t.Fatalf("%v %v", err, ps.Days)
	}
	for _, s := range []PaySchedule{{Kind: PayBiweekly}, {Kind: PayMonthly, Days: []int{0}}, {Kind: PaySemimonthly, Days: []int{3, 3}}} {
		if err := s.Validate(); err == nil {
			t.Errorf("%+v: expected error", s)
		}
	}
}

func TestSetAmount(t *testing.T) {
	base := []AmountRow{{Month: "2026-01", Amount: 100, Forward: true}, {Month: "2026-05", Amount: 500}}
	check := func(name string, rows []AmountRow, want map[string]int64) {
		t.Helper()
		for m, v := range want {
			if got := Resolve(rows, m); got != v {
				t.Errorf("%s %s: got %d want %d (%+v)", name, m, got, v, rows)
			}
		}
	}
	r := SetAmount(base, "2026-03", 300, false)
	check("single month", r, map[string]int64{"2026-02": 100, "2026-03": 300, "2026-04": 100, "2026-05": 500, "2026-06": 100})
	r = SetAmount(base, "2026-03", 300, true)
	check("forward", r, map[string]int64{"2026-02": 100, "2026-03": 300, "2026-05": 300, "2027-01": 300})
	r = SetAmount(base, "2026-01", 50, false)
	check("single month on a forward row", r, map[string]int64{"2026-01": 50, "2026-02": 100, "2026-06": 100})
	r = SetAmount(nil, "2026-09", 0, false)
	check("zero on empty", r, map[string]int64{"2026-09": 0, "2026-10": 0})
	if len(r) != 1 {
		t.Errorf("no carry row needed: %+v", r)
	}
}

func TestComputeLine(t *testing.T) {
	// Restaurants, $400/month even over 28-day Feb; $100 spent on Feb 3.
	rows := []AmountRow{{Month: "2026-01", Amount: 40000, Forward: true}}
	spent := func(from, to time.Time) int64 {
		if !d("2026-02-03").Before(from) && d("2026-02-03").Before(to) {
			return 10000
		}
		return 0
	}
	week2 := Period{d("2026-02-08"), d("2026-02-15")}
	l := ComputeLine(week2, Chunk{}, rows, d("2026-02-10"), spent)
	// $300 left over 21 days → $100 for the week; 3 days in → $300×3/21 ≈ $42.86.
	if l.Budget != 10000 || l.Actual != 0 || l.Expected != 4286 {
		t.Fatalf("%+v", l)
	}
	month := MonthPeriod(d("2026-02-10"))
	l = ComputeLine(month, Chunk{}, rows, d("2026-03-01"), spent)
	if l.Budget != 40000 || l.Actual != 10000 || l.Expected != 0 {
		t.Fatalf("%+v", l)
	}
}
