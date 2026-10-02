package recurring

import (
	"testing"
	"time"
)

func date(s string) time.Time {
	d, _ := time.Parse(time.DateOnly, s)
	return d
}

// series builds charges for one merchant on the given dates.
func series(mid int64, name string, amt int64, dates ...string) []Txn {
	out := make([]Txn, len(dates))
	for i, d := range dates {
		out[i] = Txn{ID: mid*100 + int64(i), Date: d, Amount: amt, MerchantID: mid, Merchant: name}
	}
	return out
}

func find(ss []Series, name string) *Series {
	for i := range ss {
		if ss[i].Name == name {
			return &ss[i]
		}
	}
	return nil
}

func TestDetect(t *testing.T) {
	today := date("2026-09-20")
	var txns []Txn
	txns = append(txns, series(1, "Netflix", -1599, "2026-05-03", "2026-06-03", "2026-07-03", "2026-08-03", "2026-09-03")...)
	txns = append(txns, series(2, "Acme Payroll", 250000, "2026-07-01", "2026-07-15", "2026-08-01", "2026-08-14", "2026-09-01", "2026-09-15")...)
	txns = append(txns, series(3, "Gym", -2500, "2026-08-09", "2026-08-23", "2026-09-06")...)
	txns = append(txns, series(4, "Domain", -1200, "2024-10-02", "2025-10-01")...)
	txns = append(txns, series(5, "Old Sub", -999, "2026-01-05", "2026-02-05", "2026-03-05")...) // stopped
	txns = append(txns, series(6, "Random Cafe", -450, "2026-08-02", "2026-08-05", "2026-08-30", "2026-09-12")...)
	// Utility with a varying bill.
	txns = append(txns, Txn{Date: "2026-06-12", Amount: -8000, MerchantID: 7, Merchant: "City Power"},
		Txn{Date: "2026-07-13", Amount: -9500, MerchantID: 7, Merchant: "City Power"},
		Txn{Date: "2026-08-12", Amount: -11000, MerchantID: 7, Merchant: "City Power"},
		Txn{Date: "2026-09-11", Amount: -9000, MerchantID: 7, Merchant: "City Power"})
	// A store with random purchases plus one steady monthly subscription.
	txns = append(txns, series(8, "Amazon", -1499, "2026-06-18", "2026-07-18", "2026-08-18", "2026-09-18")...)
	txns = append(txns, series(8, "Amazon", -4321, "2026-06-25")...)
	txns = append(txns, series(8, "Amazon", -8765, "2026-08-02")...)
	txns = append(txns, series(8, "Amazon", -2210, "2026-09-09")...)

	got := Detect(txns, today)

	check := func(name string, cad Cadence, amt int64, next string, variable bool) {
		t.Helper()
		s := find(got, name)
		if s == nil {
			t.Fatalf("%s not detected; got %+v", name, got)
		}
		if s.Cadence != cad || s.Amount != amt || s.NextDate != next || s.Variable != variable {
			t.Errorf("%s = %s %d next %s variable %v; want %s %d next %s variable %v", name, s.Cadence, s.Amount, s.NextDate, s.Variable, cad, amt, next, variable)
		}
	}
	check("Netflix", Monthly, -1599, "2026-10-03", false)
	check("Acme Payroll", Semimonthly, 250000, "2026-10-01", false)
	check("Gym", Biweekly, -2500, "2026-09-20", false)
	check("Domain", Yearly, -1200, "2026-10-01", false)
	check("City Power", Monthly, -9000, "2026-10-12", true)
	check("Amazon", Monthly, -1499, "2026-10-18", false)
	for _, n := range []string{"Old Sub", "Random Cafe"} {
		if find(got, n) != nil {
			t.Errorf("%s should not be recurring", n)
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].NextDate > got[i].NextDate {
			t.Errorf("not sorted by next date: %s before %s", got[i-1].NextDate, got[i].NextDate)
		}
	}
	if s := find(got, "Amazon"); s != nil && s.Key != "m8-:14" {
		t.Errorf("cluster key = %q", s.Key)
	}
}

func TestNextAcrossMonthEnd(t *testing.T) {
	// Paid on the 31st, then once on the 1st: next is the 31st, not the end of next month.
	os := []occ{{date: date("2026-07-31")}, {date: date("2026-08-31")}, {date: date("2026-10-01")}}
	if got := next(Monthly, os, nil).Format(time.DateOnly); got != "2026-10-31" {
		t.Errorf("next = %s", got)
	}
}

func TestNextClampsMonthEnd(t *testing.T) {
	os := []occ{{date: date("2026-01-31")}, {date: date("2026-03-31")}}
	if got := next(Monthly, os, nil).Format(time.DateOnly); got != "2026-04-30" {
		t.Errorf("next = %s", got)
	}
}
