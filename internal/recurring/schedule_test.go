package recurring

import (
	"testing"
	"time"
)

func TestDates(t *testing.T) {
	fmtAll := func(ds []time.Time) []string {
		out := make([]string, len(ds))
		for i, d := range ds {
			out[i] = d.Format(time.DateOnly)
		}
		return out
	}
	cases := []struct {
		c      Cadence
		anchor string
		day2   int
		from   string
		to     string
		want   []string
	}{
		{Monthly, "2026-01-31", 0, "2026-02-01", "2026-04-30", []string{"2026-02-28", "2026-03-31", "2026-04-30"}},
		{Biweekly, "2026-10-09", 0, "2026-09-01", "2026-10-10", []string{"2026-09-11", "2026-09-25", "2026-10-09"}},
		{Semimonthly, "2026-10-01", 15, "2026-10-01", "2026-11-05", []string{"2026-10-01", "2026-10-15", "2026-11-01"}},
		{Quarterly, "2026-11-20", 0, "2026-01-01", "2026-12-31", []string{"2026-02-20", "2026-05-20", "2026-08-20", "2026-11-20"}},
		{Yearly, "2027-03-02", 0, "2026-01-01", "2026-12-31", []string{"2026-03-02"}},
	}
	for _, c := range cases {
		got := fmtAll(Dates(c.c, c.anchor, c.day2, date(c.from), date(c.to)))
		if len(got) != len(c.want) {
			t.Errorf("%s %s: %v, want %v", c.c, c.anchor, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s %s: %v, want %v", c.c, c.anchor, got, c.want)
				break
			}
		}
	}
}

func TestBuildSchedule(t *testing.T) {
	today := date("2026-10-02")
	var txns []Txn
	// Rent: tracked by hand by match text, paid Sep 1, nothing yet for Oct 1 (late, in grace).
	txns = append(txns, Txn{ID: 1, Date: "2026-09-01", Amount: -150000, Merchant: "Zelle", Description: "ZELLE TO OAK APTS LLC", CategoryID: 9},
		Txn{ID: 2, Date: "2026-08-01", Amount: -150000, Merchant: "Zelle", Description: "ZELLE TO OAK APTS LLC", CategoryID: 9})
	// Netflix: detected (strong), confirmed as a tracked item by series key.
	txns = append(txns, series(3, "Netflix", -1599, "2026-06-03", "2026-07-03", "2026-08-03", "2026-09-03")...)
	// Two similar monthly charges: a weak suggestion.
	txns = append(txns, series(4, "Cloud Box", -2000, "2026-08-10", "2026-09-09")...)
	// Two different amounts a month apart: not suggested.
	txns = append(txns, series(5, "Corner Deli", -900, "2026-08-10")...)
	txns = append(txns, series(5, "Corner Deli", -2400, "2026-09-10")...)
	items := []Item{
		{ID: 1, Name: "Rent", MatchText: "oak apts", Amount: -150000, Cadence: Monthly, Anchor: "2026-08-01", CategoryID: 9},
		{ID: 2, Name: "Netflix", MerchantID: 3, Amount: -1599, Cadence: Monthly, Anchor: "2026-10-03", SeriesKey: "m3-", CategoryID: 7},
	}
	sc := Build(items, txns, map[string]bool{}, today)
	if len(sc.Tracked) != 2 || sc.Tracked[0].Name != "Rent" || sc.Tracked[0].NextDate != "2026-10-01" || sc.Tracked[0].Count != 2 {
		t.Fatalf("tracked %+v", sc.Tracked)
	}
	if n := sc.Tracked[1]; n.NextDate != "2026-10-03" || n.LastDate != "2026-09-03" || n.Count != 4 {
		t.Fatalf("netflix %+v", n)
	}
	if len(sc.Suggestions) != 1 || sc.Suggestions[0].Name != "Cloud Box" || sc.Suggestions[0].Strong {
		t.Fatalf("suggestions %+v", sc.Suggestions)
	}
	if up := sc.Upcoming(); len(up) != 2 {
		t.Fatalf("upcoming %+v", up)
	}
	occ := sc.Occurrences(date("2026-09-01"), date("2026-10-31"), false)
	status := map[string]string{}
	for _, o := range occ {
		status[o.Name+" "+o.Date] = o.Status
	}
	want := map[string]string{
		"Rent 2026-09-01": "paid", "Rent 2026-10-01": "due", "Netflix 2026-09-03": "paid", "Netflix 2026-10-03": "upcoming",
	}
	for k, v := range want {
		if status[k] != v {
			t.Errorf("%s = %q, want %q (all %v)", k, status[k], v, status)
		}
	}
	// Rent's anchor is Aug 1: nothing before it shows as missed.
	if _, ok := status["Rent 2026-07-01"]; ok {
		t.Error("occurrence before the anchor")
	}
	due := sc.Due(today, today.AddDate(0, 0, 7))
	if due[7] != 1599 || due[9] != 0 {
		t.Errorf("due %v", due) // rent was due Oct 1, before the window
	}
	if due := sc.Due(date("2026-10-01"), date("2026-10-08")); due[9] != 150000 {
		t.Errorf("due with late rent %v", due)
	}
	// A dismissed suggestion moves to Dismissed.
	sc = Build(nil, txns, map[string]bool{"m4-": true}, today)
	if len(sc.Dismissed) != 1 || sc.Dismissed[0].Name != "Cloud Box" {
		t.Fatalf("dismissed %+v", sc.Dismissed)
	}
}
