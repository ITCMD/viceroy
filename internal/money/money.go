// Package money converts between decimal strings and int64 cents.
package money

import (
	"fmt"
	"strings"
)

// ParseCents parses a decimal string like "-1234.5" or "12.345" into cents,
// rounding half away from zero past two decimal places. Commas and a leading
// "$" or "+" are accepted.
func ParseCents(s string) (int64, error) {
	orig := s
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	neg := false
	if strings.HasPrefix(s, "-") {
		neg, s = true, s[1:]
	} else if strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	s = strings.TrimPrefix(s, "$")
	if strings.HasPrefix(s, "-") { // "$-5.00"
		neg, s = !neg, s[1:]
	}
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" && frac == "" {
		return 0, fmt.Errorf("invalid amount %q", orig)
	}
	var cents int64
	for _, c := range whole {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid amount %q", orig)
		}
		cents = cents*10 + int64(c-'0')
		if cents > 1e15 {
			return 0, fmt.Errorf("amount out of range %q", orig)
		}
	}
	cents *= 100
	for i, c := range frac {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid amount %q", orig)
		}
		switch i {
		case 0:
			cents += int64(c-'0') * 10
		case 1:
			cents += int64(c - '0')
		case 2:
			if c >= '5' {
				cents++
			}
		}
	}
	if neg {
		cents = -cents
	}
	return cents, nil
}

// Format renders cents as a plain decimal string ("-12.30").
func Format(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}
