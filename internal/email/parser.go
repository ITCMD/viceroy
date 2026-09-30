package email

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"viceroy/internal/money"
)

// FieldSpec says where a value sits in the email. Either Regex (the first capture group, or
// the whole match) or the text right after Before, up to After or the end of the line.
type FieldSpec struct {
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	Regex  string `json:"regex,omitempty"`
}

func (f FieldSpec) empty() bool { return f.Before == "" && f.Regex == "" }

func (f FieldSpec) compile() (*regexp.Regexp, error) {
	if f.Regex != "" {
		return regexp.Compile("(?im)" + f.Regex)
	}
	end := `[ \t]*(?:\n|$)`
	if f.After != "" {
		end = `\s*` + regexp.QuoteMeta(f.After)
	}
	// The value may sit on the next line (HTML tables render label and value as two lines).
	return regexp.Compile(`(?i)` + regexp.QuoteMeta(f.Before) + `[ \t:]*\n?[ \t]*([^\n]+?)` + end)
}

// CustomParser is the JSON a user builds in the filter editor. Date is optional; the email's
// received date is used when it's missing or unparseable.
type CustomParser struct {
	Amount   FieldSpec `json:"amount"`
	Merchant FieldSpec `json:"merchant"`
	Date     FieldSpec `json:"date"`
}

// Parsed is what a parser extracted. AmountCents is always positive; the filter decides sign.
type Parsed struct {
	AmountCents int64  `json:"amount_cents"`
	Merchant    string `json:"merchant"`
	Date        string `json:"date"` // YYYY-MM-DD
}

// parser is a compiled parser: alternatives per field, tried in order.
type parser struct {
	amount, merchant, date []*regexp.Regexp
}

// Template is a built-in parser for one bank's alerts.
type Template struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	p     parser
}

var amountRe = `\$\s?([\d,]+\.\d{2})`

func mustAll(ps ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(ps))
	for i, p := range ps {
		out[i] = regexp.MustCompile(p)
	}
	return out
}

// Labeled lines ("Merchant: X", or "Merchant" then "X" on the next line) are the most common
// alert layout, so every template falls back to them.
var (
	labeledAmount   = `(?im)^(?:transaction |purchase |charge )?amount[ \t:]*\n?[ \t]*` + amountRe
	labeledMerchant = `(?im)^(?:merchant(?: name)?|where|description|payee|at)[ \t:]*\n?[ \t]*([^\n$]{2,80}?)[ \t]*$`
	labeledDate     = `(?im)^(?:transaction |purchase )?date[ \t:]*\n?[ \t]*([^\n]{6,40}?)[ \t]*$`
)

// Templates are the built-in parsers. They're best effort until checked against real alerts.
var Templates = []Template{
	{Name: "generic", Label: "Generic (auto-detect)", p: parser{
		amount: mustAll(labeledAmount, `(?i)(?:amount of|for|charge of|purchase of|transaction of)\s+`+amountRe, amountRe),
		merchant: mustAll(labeledMerchant,
			`(?i)\b(?:at|with)\s+([A-Za-z0-9][^\n]{1,60}?)(?:\s+(?:on|for|was|has|in the amount)\b|[.,;]\s|[.,;]?$|\n)`),
		date: mustAll(labeledDate, `(?i)\bon\s+([A-Z][a-z]+\.? \d{1,2},? \d{4}|\d{1,2}/\d{1,2}/\d{2,4})`),
	}},
	{Name: "chase", Label: "Chase", p: parser{
		// Subject: "Your $12.34 transaction with STARBUCKS" / "You made a $12.34 transaction with STARBUCKS"
		amount:   mustAll(`(?i)`+amountRe+`\s+transaction`, labeledAmount),
		merchant: mustAll(`(?im)transaction with\s+([^\n]+?)\s*$`, labeledMerchant),
		date:     mustAll(labeledDate),
	}},
	{Name: "capital_one", Label: "Capital One", p: parser{
		// "...on September 30, 2026, at STARBUCKS, a pending authorization or purchase in the amount of $12.34..."
		amount:   mustAll(`(?i)amount of\s+`+amountRe, labeledAmount),
		merchant: mustAll(`(?i)\d{4},?\s+at\s+([^\n]+?),\s+a\s+(?:pending|purchase|transaction)`, labeledMerchant),
		date:     mustAll(`(?i)\bon\s+([A-Z][a-z]+ \d{1,2}, \d{4}),?\s+at\b`, labeledDate),
	}},
}

func findTemplate(name string) (Template, bool) {
	for _, t := range Templates {
		if t.Name == name {
			return t, true
		}
	}
	return Template{}, false
}

// ValidateParser checks a filter's parser settings and returns a user-facing error.
func ValidateParser(name, custom string) error {
	_, err := compileParser(name, custom)
	return err
}

func compileParser(name, custom string) (parser, error) {
	if name != "custom" {
		t, ok := findTemplate(name)
		if !ok {
			return parser{}, fmt.Errorf("Unknown parser %q.", name)
		}
		return t.p, nil
	}
	var c CustomParser
	if err := json.Unmarshal([]byte(custom), &c); err != nil {
		return parser{}, errors.New("Custom parser settings are invalid.")
	}
	if c.Amount.empty() || c.Merchant.empty() {
		return parser{}, errors.New("A custom parser needs amount and merchant fields.")
	}
	var p parser
	for _, f := range []struct {
		name string
		spec FieldSpec
		out  *[]*regexp.Regexp
	}{{"amount", c.Amount, &p.amount}, {"merchant", c.Merchant, &p.merchant}, {"date", c.Date, &p.date}} {
		if f.spec.empty() {
			continue
		}
		re, err := f.spec.compile()
		if err != nil {
			return parser{}, fmt.Errorf("The %s pattern is invalid.", f.name)
		}
		*f.out = []*regexp.Regexp{re}
	}
	return p, nil
}

// Parse extracts a transaction from m with the named parser ("custom" uses the custom JSON).
// It never guesses: a missing amount or merchant is an error.
func Parse(name, custom string, m Message) (Parsed, error) {
	p, err := compileParser(name, custom)
	if err != nil {
		return Parsed{}, err
	}
	text := m.Subject + "\n" + m.Text
	var out Parsed
	raw := first(p.amount, text)
	if raw == "" {
		return out, errors.New("no amount found")
	}
	amt, err := money.ParseCents(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "$")))
	if err != nil || amt == 0 {
		return out, fmt.Errorf("amount %q is not a dollar amount", raw)
	}
	if amt < 0 {
		amt = -amt
	}
	out.AmountCents = amt
	out.Merchant = cleanMerchant(first(p.merchant, text))
	if out.Merchant == "" {
		return out, errors.New("no merchant found")
	}
	out.Date = parseDate(first(p.date, text), m.Date)
	return out, nil
}

// first returns the first capture group (or whole match) of the first matching regex.
func first(res []*regexp.Regexp, text string) string {
	for _, re := range res {
		sm := re.FindStringSubmatch(text)
		if sm == nil {
			continue
		}
		v := sm[0]
		if len(sm) > 1 {
			v = sm[1]
		}
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func cleanMerchant(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, ` .,;:-"'`)
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}

var dateLayouts = []string{
	"January 2, 2006", "January 2 2006", "Jan 2, 2006", "Jan. 2, 2006", "Jan 2 2006", "Jan. 2 2006",
	"01/02/2006", "1/2/2006", "01/02/06", "1/2/06", "2006-01-02",
	"02 Jan 2006",
}

var dateChunk = regexp.MustCompile(`(?i)([A-Z][a-z]+day, )?([A-Z][a-z]{2,8}\.? \d{1,2},? \d{4}|\d{1,2}/\d{1,2}/\d{2,4}|\d{4}-\d{2}-\d{2}|\d{1,2} [A-Z][a-z]{2} \d{4})`)

// parseDate reads the alert's date, falling back to the received date when it's missing,
// unparseable or implausible (more than 60 days before, or after, the email).
func parseDate(s string, received time.Time) string {
	if received.IsZero() {
		received = time.Now()
	}
	recv := received.In(time.Local)
	fallback := recv.Format(time.DateOnly)
	if sm := dateChunk.FindStringSubmatch(s); sm != nil {
		m := strings.Replace(strings.Replace(sm[2], "Sept ", "Sep ", 1), "Sept. ", "Sep. ", 1)
		for _, l := range dateLayouts {
			d, err := time.ParseInLocation(l, m, time.Local)
			if err != nil {
				continue
			}
			if d.After(recv.AddDate(0, 0, 1)) || d.Before(recv.AddDate(0, 0, -60)) {
				return fallback
			}
			return d.Format(time.DateOnly)
		}
	}
	return fallback
}
