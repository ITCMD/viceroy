package email

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"viceroy/internal/accounts"
	"viceroy/internal/db"
	"viceroy/internal/money"
)

// Recipe is the AI's reading of a transaction email: the values, and where each one sits in the
// email so Viceroy can read the next email like it without AI. It's a proposal only: CheckRecipe
// decides whether a filter can be built from it.
type Recipe struct {
	Direction       string     `json:"direction"` // out | in
	Amount          string     `json:"amount"`
	Merchant        string     `json:"merchant"`
	Date            string     `json:"date"`
	AccountText     string     `json:"account_text"`     // e.g. "ending in 2080"
	SubjectContains string     `json:"subject_contains"` // e.g. "Withdrawal notice"
	AmountRule      FieldSpec  `json:"amount_rule"`
	MerchantRule    FieldSpec  `json:"merchant_rule"`
	DateRule        *FieldSpec `json:"date_rule"`
}

// RecipeCheck is Viceroy's verdict on a recipe, stored with the email (ai_recipe) so the filter
// editor can start from it. Problem is empty when a filter was (or can be) built.
type RecipeCheck struct {
	Recipe    Recipe `json:"recipe"`
	AccountID int64  `json:"account_id,omitempty"`
	Problem   string `json:"problem,omitempty"`
	// The filter Viceroy built; set when Problem is empty.
	Sender       string `json:"sender,omitempty"`
	SubjectMatch string `json:"subject_match,omitempty"`
	BodyMatch    string `json:"body_match,omitempty"`
	Parser       string `json:"parser,omitempty"`
	CustomParser string `json:"custom_parser,omitempty"`
	Sign         string `json:"sign,omitempty"`
}

func clip(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > n {
		s = string(r[:n])
	}
	return s
}

func cleanSpec(f FieldSpec) FieldSpec {
	return FieldSpec{Before: clip(strings.TrimSpace(f.Before), 80), After: clip(strings.TrimSpace(f.After), 80), Regex: clip(f.Regex, 200)}
}

// parseRecipe reads the model's "transaction" object loosely; CheckRecipe does the real checks.
func parseRecipe(raw json.RawMessage) *Recipe {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var v struct {
		Direction       string     `json:"direction"`
		Amount          any        `json:"amount"`
		Merchant        string     `json:"merchant"`
		Date            any        `json:"date"`
		AccountText     any        `json:"account_text"`
		SubjectContains any        `json:"subject_contains"`
		AmountRule      FieldSpec  `json:"amount_rule"`
		MerchantRule    FieldSpec  `json:"merchant_rule"`
		DateRule        *FieldSpec `json:"date_rule"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	r := &Recipe{
		Direction: strings.ToLower(strings.TrimSpace(v.Direction)), Amount: clip(str(v.Amount), 20),
		Merchant: clip(strings.TrimSpace(v.Merchant), 100), Date: clip(str(v.Date), 10),
		AccountText: clip(str(v.AccountText), 40), SubjectContains: clip(str(v.SubjectContains), 80),
		AmountRule: cleanSpec(v.AmountRule), MerchantRule: cleanSpec(v.MerchantRule),
	}
	if v.DateRule != nil {
		d := cleanSpec(*v.DateRule)
		if !d.empty() {
			r.DateRule = &d
		}
	}
	return r
}

var fourDigits = regexp.MustCompile(`\d{4}`)

func containsFold(text, part string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(part))
}

// CheckRecipe turns a recipe into a narrow filter for this email, or explains why it can't.
// Nothing the AI says is trusted: the sender comes from the email itself, the account from the
// stored last 4 digits, and the reading rules must reproduce the AI's values, which must
// themselves appear in the email.
func CheckRecipe(ctx context.Context, q *db.Queries, hh int64, m Message, r Recipe) (RecipeCheck, error) {
	out := RecipeCheck{Recipe: r}
	fail := func(format string, a ...any) (RecipeCheck, error) {
		out.Problem = fmt.Sprintf(format, a...)
		return out, nil
	}
	text := m.Subject + "\n" + m.Text
	switch r.Direction {
	case "out":
		out.Sign = "debit"
	case "in":
		out.Sign = "credit"
	default:
		return fail("it isn't clear whether money went out or came in")
	}
	amount, err := money.ParseCents(r.Amount)
	if err != nil || amount <= 0 {
		return fail("no clear amount")
	}
	if !strings.Contains(strings.ReplaceAll(text, ",", ""), money.Format(amount)) {
		return fail("the amount %s isn't in the email", money.Format(amount))
	}
	if !ValidMerchant(r.Merchant) || !containsFold(text, r.Merchant) {
		return fail("no clear merchant")
	}

	// Account: by the stored last 4 digits (not the account's name, which the user may change).
	if r.AccountText == "" || !containsFold(m.Text, r.AccountText) {
		return fail("couldn't tell which account it's for")
	}
	digits := fourDigits.FindAllString(r.AccountText, -1)
	if len(digits) == 0 {
		return fail("couldn't tell which account it's for")
	}
	last4 := digits[len(digits)-1]
	accts, err := accountsByLast4(ctx, q, hh, last4)
	if err != nil {
		return out, err
	}
	switch len(accts) {
	case 0:
		return fail("no account ending in %s", last4)
	case 1:
		out.AccountID = accts[0].ID
	default:
		return fail("%d accounts end in %s", len(accts), last4)
	}

	// Reading rules: the AI's own, else a built-in template, as long as it reproduces the values.
	want := Parsed{AmountCents: amount, Merchant: cleanMerchant(r.Merchant), Date: r.Date}
	same := func(p Parsed) bool {
		return p.AmountCents == want.AmountCents && strings.EqualFold(p.Merchant, want.Merchant) && (want.Date == "" || p.Date == want.Date)
	}
	custom := CustomParser{Amount: r.AmountRule, Merchant: r.MerchantRule}
	if r.DateRule != nil {
		custom.Date = *r.DateRule
	}
	cj, _ := json.Marshal(custom)
	if p, err := Parse("custom", string(cj), m); err == nil && same(p) {
		out.Parser, out.CustomParser = "custom", string(cj)
	} else {
		for _, t := range Templates {
			if p, err := Parse(t.Name, "", m); err == nil && same(p) {
				out.Parser = t.Name
				break
			}
		}
	}
	if out.Parser == "" {
		return fail("its reading rules didn't give the same amount, merchant and date")
	}

	// Match exactly this sender, the subject phrase (when it's really in the subject) and the account.
	out.Sender = strings.ToLower(strings.TrimSpace(m.FromAddr))
	if r.SubjectContains != "" && containsFold(m.Subject, r.SubjectContains) {
		out.SubjectMatch = r.SubjectContains
	}
	out.BodyMatch = r.AccountText
	if out.Sender == "" || !(Filter{Sender: out.Sender, Subject: out.SubjectMatch, Body: out.BodyMatch}).Matches(m) {
		return fail("couldn't build a filter that matches this email")
	}
	return out, nil
}

// accountsByLast4 lists open accounts whose stored mask (or, for older rows without one, the
// digits in the name) ends in last4.
func accountsByLast4(ctx context.Context, q *db.Queries, hh int64, last4 string) ([]db.Account, error) {
	accts, err := q.ListAccounts(ctx, hh)
	if err != nil {
		return nil, err
	}
	var out []db.Account
	for _, a := range accts {
		if a.Status == "closed" || a.Status == "ignored" || a.Status == "review" {
			continue
		}
		if strings.HasSuffix(a.Mask, last4) || a.Mask == "" && strings.HasSuffix(accounts.Mask(a.Name), last4) {
			out = append(out, a)
		}
	}
	return out, nil
}

// SuggestAccount finds the one account whose last 4 digits appear in the email after words
// like "ending in" or "…", and the exact phrase, for the filter editor.
func SuggestAccount(ctx context.Context, q *db.Queries, hh int64, m Message) (accountID int64, phrase string, err error) {
	text := m.Subject + "\n" + m.Text
	seen := map[string]bool{}
	for _, sm := range accountPhrase.FindAllStringSubmatch(text, -1) {
		last4 := sm[2]
		if seen[last4] {
			continue
		}
		seen[last4] = true
		accts, err := accountsByLast4(ctx, q, hh, last4)
		if err != nil {
			return 0, "", err
		}
		if len(accts) == 1 {
			return accts[0].ID, strings.TrimSpace(sm[0]), nil
		}
	}
	return 0, "", nil
}

var accountPhrase = regexp.MustCompile(`(?i)(ending in|ending|account|card|acct\.?|x+|\*+|…|\.\.\.|-)\s?(\d{4})\b`)

var creditWords = regexp.MustCompile(`(?i)\b(deposit(ed)?|refund(ed)?|credit(ed)?|received|direct deposit|money in|transfer in)\b`)

// GuessSign says whether an alert is money in ("credit") by its wording, else "debit".
func GuessSign(m Message) string {
	if creditWords.MatchString(m.Subject) {
		return "credit"
	}
	return "debit"
}
