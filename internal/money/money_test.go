package money

import "testing"

func TestParseCents(t *testing.T) {
	cases := map[string]int64{
		"0": 0, "12": 1200, "12.3": 1230, "12.34": 1234, "-12.34": -1234,
		"12.345": 1235, "-12.345": -1235, "12.344": 1234, "1,234.56": 123456,
		"$5.00": 500, "-$5.00": -500, "$-5.00": -500, "+3.10": 310, ".5": 50, "  7.01 ": 701,
	}
	for in, want := range cases {
		got, err := ParseCents(in)
		if err != nil || got != want {
			t.Errorf("ParseCents(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "abc", "1.2.3", "12a", "-", "."} {
		if _, err := ParseCents(bad); err == nil {
			t.Errorf("ParseCents(%q) should fail", bad)
		}
	}
}

func TestFormat(t *testing.T) {
	for cents, want := range map[int64]string{0: "0.00", 5: "0.05", -1234: "-12.34", 100000: "1000.00"} {
		if got := Format(cents); got != want {
			t.Errorf("Format(%d) = %q, want %q", cents, got, want)
		}
	}
}
