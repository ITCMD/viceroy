package email

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"viceroy/internal/accounts"
	"viceroy/internal/db"
	"viceroy/internal/money"
)

// StatusApplied marks an email a filter or the user acted on without creating a transaction
// (e.g. it set an account's balance).
const StatusApplied = "applied"

// What a filter does with the emails it catches.
const (
	ActionTransaction = "transaction"
	ActionBalance     = "balance"
	ActionIgnore      = "ignore"
)

// ValidAction reports whether a is a filter action.
func ValidAction(a string) bool {
	return a == ActionTransaction || a == ActionBalance || a == ActionIgnore
}

// Facts is what Viceroy verified in a notice the AI read (today: a balance summary). It's
// stored with the email (ai_facts) and drives the notice's one-click actions. Problem is
// non-empty when the AI's reading didn't hold up; nothing can be applied then.
type Facts struct {
	Kind        string `json:"kind"` // balance
	AccountID   int64  `json:"account_id,omitempty"`
	AccountText string `json:"account_text,omitempty"` // e.g. "Quicksilver (0428)"
	// AmountCents is the balance as written (positive = the usual sign: money in an asset,
	// money owed on a card or loan); BalanceCents is it with the account's sign.
	AmountCents  int64  `json:"amount_cents"`
	BalanceCents int64  `json:"balance_cents"`
	AsOf         string `json:"as_of,omitempty"` // YYYY-MM-DD
	// CanAlways is true when Viceroy reads the same balance from the email without AI, so a
	// filter can do this for every email like it.
	CanAlways bool   `json:"can_always,omitempty"`
	Problem   string `json:"problem,omitempty"`
}

// Balance is a balance read from an email without AI.
type Balance struct {
	AmountCents int64  `json:"amount_cents"` // as written; negative for "-$5.00"
	AsOf        string `json:"as_of"`
}

var (
	balanceAmount = regexp.MustCompile(`(?i)balance(?:\s+(?:of|is|was|amount))?[ \t:]*\n?[ \t]*(-?)\s?\$\s?([\d,]+\.\d{2})`)
	balanceAsOf   = regexp.MustCompile(`(?i)\bas of\s+((?:[A-Z][a-z]+day,? )?(?:[A-Z][a-z]{2,8}\.? \d{1,2},? \d{4}|\d{1,2}/\d{1,2}/\d{2,4}|\d{4}-\d{2}-\d{2}))`)
)

// ParseBalance reads a balance summary: the amount after "balance" (or where a custom
// parser's amount rule points) and the "as of" date, else the received date.
func ParseBalance(parserName, custom string, m Message) (Balance, error) {
	text := m.Subject + "\n" + m.Text
	raw, neg := "", false
	if parserName == "custom" {
		var c CustomParser
		if err := json.Unmarshal([]byte(custom), &c); err != nil || c.Amount.empty() {
			return Balance{}, errors.New("the custom parser has no amount rule")
		}
		re, err := c.Amount.compile()
		if err != nil {
			return Balance{}, errors.New("the amount pattern is invalid")
		}
		raw = strings.TrimSpace(first([]*regexp.Regexp{re}, text))
		raw, neg = strings.CutPrefix(raw, "-")
		raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "$"))
	} else if sm := balanceAmount.FindStringSubmatch(text); sm != nil {
		raw, neg = sm[2], sm[1] == "-"
	}
	if raw == "" {
		return Balance{}, errors.New("no balance found")
	}
	c, err := money.ParseCents(raw)
	if err != nil || c < 0 {
		return Balance{}, fmt.Errorf("balance %q is not a dollar amount", raw)
	}
	if neg {
		c = -c
	}
	asOf := ""
	if sm := balanceAsOf.FindStringSubmatch(text); sm != nil {
		asOf = sm[1]
	}
	return Balance{AmountCents: c, AsOf: parseDate(asOf, m.Date)}, nil
}

// signedBalance turns a balance as written into the account's sign: what's owed on a card or
// loan is negative.
func signedBalance(amount int64, acctType string) int64 {
	if accounts.IsLiability(acctType) {
		return -amount
	}
	return amount
}

// CheckBalance verifies the AI's reading of a balance summary. The amount must appear in the
// email as written, and the last 4 digits must name exactly one open account.
func CheckBalance(ctx context.Context, q *db.Queries, hh int64, m Message, res AIResult) (Facts, error) {
	f := Facts{Kind: "balance", AccountText: res.AccountText, AsOf: res.Date}
	if !res.Amount.Valid {
		f.Problem = "no clear balance"
		return f, nil
	}
	f.AmountCents = res.Amount.Int64
	text := strings.ReplaceAll(m.Subject+"\n"+m.Text, ",", "")
	if !strings.Contains(text, money.Format(f.AmountCents)) {
		f.Problem = fmt.Sprintf("the balance %s isn't in the email", money.Format(f.AmountCents))
		return f, nil
	}
	last4 := res.Last4
	if res.AccountText != "" && containsFold(m.Subject+"\n"+m.Text, res.AccountText) {
		if d := fourDigits.FindAllString(res.AccountText, -1); len(d) > 0 {
			last4 = d[len(d)-1]
		}
	} else {
		f.AccountText = ""
	}
	if last4 == "" || !strings.Contains(m.Subject+"\n"+m.Text, last4) {
		f.Problem = "couldn't tell which account it's for"
		return f, nil
	}
	accts, err := accountsByLast4(ctx, q, hh, last4)
	if err != nil {
		return f, err
	}
	switch len(accts) {
	case 0:
		f.Problem = "no account ending in " + last4
		return f, nil
	case 1:
	default:
		f.Problem = fmt.Sprintf("%d accounts end in %s", len(accts), last4)
		return f, nil
	}
	f.AccountID = accts[0].ID
	if f.AsOf != "" && f.AsOf > m.Date.In(time.Local).AddDate(0, 0, 1).Format(time.DateOnly) {
		f.AsOf = "" // a balance can't be as of a future date
	}
	if f.AsOf == "" {
		f.AsOf = m.Date.In(time.Local).Format(time.DateOnly)
	}
	// Viceroy reads the same amount by itself: a filter can repeat this without AI.
	// ("-$5.00" is a credit balance on a card; the AI reports amounts without a sign.)
	if b, err := ParseBalance("generic", "", m); err == nil && (b.AmountCents == f.AmountCents || b.AmountCents == -f.AmountCents) {
		f.AmountCents = b.AmountCents
		f.CanAlways = f.AccountText != ""
	}
	f.BalanceCents = signedBalance(f.AmountCents, accts[0].Type)
	return f, nil
}

// readingTime is when a balance "as of" a date was true: the email's time on that day, else
// the end of that day (never after the email arrived).
func readingTime(asOf string, received time.Time) time.Time {
	d, err := time.ParseInLocation(time.DateOnly, asOf, time.Local)
	if err != nil || d.Format(time.DateOnly) == received.In(time.Local).Format(time.DateOnly) {
		return received
	}
	end := d.AddDate(0, 0, 1).Add(-time.Second)
	if end.After(received) {
		return received
	}
	return end
}

// ErrStaleBalance means the account already has a newer balance than the email's.
var ErrStaleBalance = errors.New("the account already has a newer balance")

// ApplyBalance records a balance read from an email. A manual account takes it as its balance;
// a synced one takes it as a newer reading, which the next sync replaces only when the bank's
// balance is newer still. It returns a line saying what happened.
func ApplyBalance(ctx context.Context, q *db.Queries, hh, accountID, amount int64, asOf string, received, now time.Time) (string, error) {
	acct, err := q.GetAccount(ctx, db.GetAccountParams{ID: accountID, HouseholdID: hh})
	if err != nil {
		return "", err
	}
	at := readingTime(asOf, received)
	if acct.IsManual == 0 && acct.BalanceAt.Valid && acct.BalanceAt.Int64 >= at.Unix() {
		return "", ErrStaleBalance
	}
	bal := signedBalance(amount, acct.Type)
	if err := q.SetManualBalance(ctx, db.SetManualBalanceParams{
		BalanceCents: bal, BalanceAt: sql.NullInt64{Int64: at.Unix(), Valid: true}, UpdatedAt: now.Unix(), ID: acct.ID, HouseholdID: hh,
	}); err != nil {
		return "", err
	}
	date := at.In(time.Local).Format(time.DateOnly)
	if err := q.UpsertBalanceSnapshot(ctx, db.UpsertBalanceSnapshotParams{AccountID: acct.ID, Date: date, BalanceCents: bal}); err != nil {
		return "", err
	}
	return fmt.Sprintf("Set %s balance to %s", acct.Name, dollars(amount)), nil
}

// applyFilterAction runs a non-transaction filter on a stored message.
func (s *Service) applyFilterAction(ctx context.Context, q *db.Queries, row db.EmailMessage, f db.EmailFilter) error {
	filterID := sql.NullInt64{Int64: f.ID, Valid: true}
	if f.Action == ActionIgnore {
		return q.SetEmailApplied(ctx, db.SetEmailAppliedParams{Status: StatusIgnored, FilterID: filterID, Applied: "Ignored by " + f.Name, ID: row.ID})
	}
	m := FromRow(row)
	b, err := ParseBalance(f.Parser, f.CustomParser, m)
	if err != nil {
		return q.SetEmailMessageResult(ctx, db.SetEmailMessageResultParams{Status: StatusParseFailed, FilterID: filterID, Error: err.Error(), ID: row.ID})
	}
	msg, err := ApplyBalance(ctx, q, row.HouseholdID, f.AccountID, b.AmountCents, b.AsOf, m.Date, s.Now())
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return q.SetEmailMessageResult(ctx, db.SetEmailMessageResultParams{Status: StatusParseFailed, FilterID: filterID, Error: "the filter's account no longer exists", ID: row.ID})
	case errors.Is(err, ErrStaleBalance):
		msg = "Skipped: the bank sync already has a newer balance"
	case err != nil:
		return err
	}
	return q.SetEmailApplied(ctx, db.SetEmailAppliedParams{Status: StatusApplied, FilterID: filterID, Applied: msg, ID: row.ID})
}

// dollars renders cents for people: "$1234.50", "-$5.00".
func dollars(c int64) string {
	if c < 0 {
		return "-$" + money.Format(-c)
	}
	return "$" + money.Format(c)
}

// ValidateFilterParser checks a filter's parser settings for its action: a balance filter's
// custom parser needs only an amount rule, an ignore filter reads nothing.
func ValidateFilterParser(action, name, custom string) error {
	switch action {
	case ActionIgnore:
		return nil
	case ActionBalance:
		if name != "custom" {
			return nil
		}
		var c CustomParser
		if err := json.Unmarshal([]byte(custom), &c); err != nil || c.Amount.empty() {
			return errors.New("A custom parser needs an amount field.")
		}
		if _, err := c.Amount.compile(); err != nil {
			return errors.New("The amount pattern is invalid.")
		}
		return nil
	}
	return ValidateParser(name, custom)
}

// PreviewParse reads m the way a filter with this action would, for the filter editor. A
// balance comes back as its amount and "as of" date, with "Balance" as the merchant.
func PreviewParse(action, name, custom string, m Message) (Parsed, error) {
	switch action {
	case ActionIgnore:
		return Parsed{Merchant: "Ignored", Date: parseDate("", m.Date)}, nil
	case ActionBalance:
		b, err := ParseBalance(name, custom, m)
		if err != nil {
			return Parsed{}, err
		}
		return Parsed{AmountCents: b.AmountCents, Merchant: "Balance", Date: b.AsOf}, nil
	}
	return Parse(name, custom, m)
}
