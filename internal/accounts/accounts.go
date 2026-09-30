// Package accounts holds account types, groups and name heuristics shared by sync and the API.
package accounts

import (
	"regexp"
	"strings"
)

const (
	Checking       = "checking"
	Savings        = "savings"
	Cash           = "cash"
	CreditCard     = "credit_card"
	Investment     = "investment"
	Loan           = "loan"
	Mortgage       = "mortgage"
	OtherAsset     = "other_asset"
	OtherLiability = "other_liability"
)

var Types = []string{Checking, Savings, Cash, CreditCard, Investment, Loan, Mortgage, OtherAsset, OtherLiability}

func ValidType(t string) bool {
	for _, v := range Types {
		if v == t {
			return true
		}
	}
	return false
}

// Group is the Accounts tab a type is listed under.
func Group(t string) string {
	switch t {
	case Checking, Savings, Cash:
		return "cash"
	case CreditCard:
		return "credit"
	case Investment:
		return "investments"
	case Loan, Mortgage:
		return "loans"
	}
	return "other"
}

// IsLiability reports whether a type's balance is money owed. Balances are stored signed
// (negative = owed), so this only affects presentation.
func IsLiability(t string) bool {
	switch t {
	case CreditCard, Loan, Mortgage, OtherLiability:
		return true
	}
	return false
}

var keywordTypes = []struct {
	words []string
	typ   string
}{
	{[]string{"mortgage", "heloc"}, Mortgage},
	{[]string{"loan", "auto finance", "student"}, Loan},
	{[]string{"credit", "visa", "mastercard", "amex", "card", "discover"}, CreditCard},
	{[]string{"401k", "401(k)", "403b", "ira", "roth", "brokerage", "invest", "hsa", "stock", "retirement"}, Investment},
	{[]string{"saving", "money market", "cd "}, Savings},
	{[]string{"checking", "chk", "spend"}, Checking},
}

// InferType guesses an account type from its name and balance (SimpleFIN has no type field).
func InferType(name string, balanceCents int64) string {
	n := " " + strings.ToLower(name) + " "
	for _, kt := range keywordTypes {
		for _, w := range kt.words {
			if w == "ira" || w == "hsa" {
				if regexp.MustCompile(`\b` + w + `\b`).MatchString(n) {
					return kt.typ
				}
				continue
			}
			if strings.Contains(n, w) {
				return kt.typ
			}
		}
	}
	if balanceCents < 0 {
		return CreditCard
	}
	return Checking
}

var maskRe = regexp.MustCompile(`(?i)(?:\((?:\.{0,3}|x*)(\d{4})\)|(?:\.{2,}|x+|#|-|\s)(\d{4}))\s*$`)

// Mask extracts the trailing last-4 digits from an account name, e.g. "Card (1234)", "...1234", "x1234".
func Mask(name string) string {
	m := maskRe.FindStringSubmatch(strings.TrimSpace(name))
	if m == nil {
		return ""
	}
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// Normalize lowercases and strips punctuation/spacing for fuzzy name comparison.
func Normalize(s string) string {
	return nonAlnum.ReplaceAllString(strings.ToLower(s), "")
}
