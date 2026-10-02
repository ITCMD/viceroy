// Package categorize assigns merchants and categories to new transactions:
// user rules → merchant history → (optional AI suggester) → uncategorized + needs review.
package categorize

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"

	"viceroy/internal/db"
)

// Category sources recorded on transactions.
const (
	SourceRule    = "rule"
	SourceHistory = "history"
	SourceAI      = "ai"
	SourceUser    = "user"
	SourceLinked  = "linked"
)

var (
	// Card-processor and POS prefixes that hide the real merchant.
	prefixRe = regexp.MustCompile(`(?i)^(sq \*|sq\*|tst\*|tst \*|sp \*|sp\*|pp\*|paypal \*|py \*|pos (debit |purchase )?|debit card purchase |purchase authorized on \d\d/\d\d |ach (debit|credit) |checkcard \d+ |dd \*|in \*|bt\*)`)
	// Store numbers, reference ids, dates and phone numbers.
	noiseRe = regexp.MustCompile(`(?i)#\s*\d+|\b\d{3,}\b|\b\d{1,2}/\d{1,2}(/\d{2,4})?\b|\b\d{3}-\d{3}-\d{4}\b|\*+[a-z0-9]*`)
	// Trailing "CITY ST" location suffix, e.g. "AUSTIN TX".
	stateRe = regexp.MustCompile(`\s+[A-Za-z .]+\s+(A[LKZR]|C[AOT]|D[EC]|FL|GA|HI|I[ADLN]|K[SY]|LA|M[ADEINOST]|N[CDEHJMVY]|O[HKR]|PA|RI|S[CD]|T[NX]|UT|V[AT]|W[AIVY])$`)
	spaceRe = regexp.MustCompile(`\s+`)
	// Trailing store/terminal numbers ("BLUE BOTTLE 12").
	trailNumRe = regexp.MustCompile(`(\s+\d+)+$`)
)

// Merchant turns a payee (preferred) or raw description into a display name and a lookup key.
// It returns empty strings when nothing useful is left.
func Merchant(payee, description string) (name, key string) {
	s := strings.TrimSpace(payee)
	if s == "" {
		s = strings.TrimSpace(description)
	}
	s = prefixRe.ReplaceAllString(s, "")
	s = noiseRe.ReplaceAllString(s, " ")
	s = spaceRe.ReplaceAllString(strings.TrimSpace(s), " ")
	if t := trailNumRe.ReplaceAllString(s, ""); t != "" {
		s = t
	}
	if t := stateRe.ReplaceAllString(s, ""); len(t) >= 3 && t != s && strings.ToUpper(s) == s {
		// Only strip locations from all-caps bank strings, where they're reliably appended.
		s = strings.TrimSpace(t)
	}
	s = strings.Trim(s, " -.,*")
	key = Key(s)
	if key == "" {
		return "", ""
	}
	return titleCase(s), key
}

// Key is the normalized lookup key: lowercase letters and digits only.
func Key(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// titleCase converts shouty bank text ("WHOLE FOODS MKT") to "Whole Foods Mkt"; mixed-case
// input is kept as the user or bank wrote it.
func titleCase(s string) string {
	if strings.ToUpper(s) != s {
		return s
	}
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// Suggester is the optional AI step. It returns a category id from cats, or ok=false.
type Suggester interface {
	Suggest(ctx context.Context, merchant, description string, amountCents int64, cats []db.Category) (id int64, ok bool)
}

// Txn is what the pipeline needs to know about a transaction.
type Txn struct {
	ID          int64
	HouseholdID int64
	AccountID   int64
	AmountCents int64
	Date        string // YYYY-MM-DD; rules with a day-of-month range need it
	Description string
	Payee       string
}

// Result is the pipeline's decision for one transaction.
type Result struct {
	MerchantID  sql.NullInt64
	Merchant    string
	CategoryID  sql.NullInt64
	Source      string
	TagIDs      []int64
	GoalID      sql.NullInt64
	Hidden      bool
	NeedsReview bool
}

// Categorizer runs the pipeline for one household. Rules are loaded once, so create one per
// sync or request.
type Categorizer struct {
	q         *db.Queries
	household int64
	rules     []db.Rule
	tags      map[int64][]int64 // rule id → tag ids
	Suggester Suggester
}

func New(ctx context.Context, q *db.Queries, householdID int64) (*Categorizer, error) {
	rules, err := q.ListRules(ctx, householdID)
	if err != nil {
		return nil, err
	}
	tags, err := RuleTags(ctx, q, householdID)
	if err != nil {
		return nil, err
	}
	return &Categorizer{q: q, household: householdID, rules: rules, tags: tags}, nil
}

// RuleTags maps each of the household's rules to the tag ids it adds.
func RuleTags(ctx context.Context, q *db.Queries, householdID int64) (map[int64][]int64, error) {
	rows, err := q.ListRuleTags(ctx, householdID)
	if err != nil {
		return nil, err
	}
	out := map[int64][]int64{}
	for _, r := range rows {
		out[r.RuleID] = append(out[r.RuleID], r.ID)
	}
	return out, nil
}

// Decide works out merchant and category without writing to the transaction.
func (c *Categorizer) Decide(ctx context.Context, t Txn) (Result, error) {
	var res Result
	name, key := Merchant(t.Payee, t.Description)
	if key != "" {
		m, err := c.q.UpsertMerchant(ctx, db.UpsertMerchantParams{HouseholdID: c.household, Name: name, Normalized: key})
		if err != nil {
			return res, err
		}
		res.MerchantID, res.Merchant = sql.NullInt64{Int64: m.ID, Valid: true}, m.Name
	}
	// Rules see both the cleaned bank name and the merchant's (possibly renamed) display name.
	rule, matched := c.match(t, name)
	if !matched && res.Merchant != name {
		rule, matched = c.match(t, res.Merchant)
	}
	if matched {
		if rule.SetMerchant != "" {
			m, err := c.q.UpsertMerchant(ctx, db.UpsertMerchantParams{HouseholdID: c.household, Name: rule.SetMerchant, Normalized: Key(rule.SetMerchant)})
			if err != nil {
				return res, err
			}
			res.MerchantID, res.Merchant = sql.NullInt64{Int64: m.ID, Valid: true}, m.Name
		}
		res.TagIDs = c.tags[rule.ID]
		res.GoalID = rule.SetGoalID
		res.Hidden = rule.SetHidden == 1
		if rule.SetCategoryID.Valid {
			res.CategoryID, res.Source = rule.SetCategoryID, SourceRule
			return res, nil
		}
	}
	if res.MerchantID.Valid {
		id, err := c.q.MerchantHistoryCategory(ctx, db.MerchantHistoryCategoryParams{HouseholdID: c.household, MerchantID: res.MerchantID})
		if err == nil && id.Valid {
			res.CategoryID, res.Source = id, SourceHistory
			return res, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return res, err
		}
	}
	if c.Suggester != nil {
		cats, err := c.q.ListCategories(ctx, c.household)
		if err != nil {
			return res, err
		}
		if id, ok := c.Suggester.Suggest(ctx, res.Merchant, t.Description, t.AmountCents, cats); ok {
			res.CategoryID, res.Source = sql.NullInt64{Int64: id, Valid: true}, SourceAI
			res.NeedsReview = true
			return res, nil
		}
	}
	res.NeedsReview = true
	return res, nil
}

// Apply decides and stores the result on a newly inserted transaction.
func (c *Categorizer) Apply(ctx context.Context, t Txn) (Result, error) {
	res, err := c.Decide(ctx, t)
	if err != nil {
		return res, err
	}
	if err := c.q.SetTransactionAuto(ctx, db.SetTransactionAutoParams{
		MerchantID: res.MerchantID, CategoryID: res.CategoryID, CategorySource: res.Source,
		NeedsReview: b2i(res.NeedsReview), Hidden: b2i(res.Hidden), ID: t.ID,
	}); err != nil {
		return res, err
	}
	for _, tag := range res.TagIDs {
		if err := c.q.AddTransactionTag(ctx, db.AddTransactionTagParams{TransactionID: t.ID, TagID: tag, HouseholdID: c.household}); err != nil {
			return res, err
		}
	}
	if res.GoalID.Valid {
		if err := c.q.SetTransactionGoal(ctx, db.SetTransactionGoalParams{GoalID: res.GoalID, ID: t.ID, HouseholdID: c.household}); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (c *Categorizer) match(t Txn, merchant string) (db.Rule, bool) {
	for _, r := range c.rules {
		if RuleMatches(r, t, merchant) {
			return r, true
		}
	}
	return db.Rule{}, false
}

// RuleMatches reports whether rule r applies to t (whose cleaned merchant name is merchant).
func RuleMatches(r db.Rule, t Txn, merchant string) bool {
	if r.AccountID.Valid && r.AccountID.Int64 != t.AccountID {
		return false
	}
	abs := t.AmountCents
	if abs < 0 {
		abs = -abs
	}
	if r.AmountMin.Valid && abs < r.AmountMin.Int64 {
		return false
	}
	if r.AmountMax.Valid && abs > r.AmountMax.Int64 {
		return false
	}
	switch r.Direction {
	case "out":
		if t.AmountCents >= 0 {
			return false
		}
	case "in":
		if t.AmountCents <= 0 {
			return false
		}
	}
	if (r.DayMin.Valid || r.DayMax.Valid) && !dayInRange(t.Date, r.DayMin, r.DayMax) {
		return false
	}
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	var fields []string
	switch r.MatchField {
	case "description":
		fields = []string{t.Description, t.Payee}
	default:
		// Merchant names compare on letters and digits only, so "Trader Joe's" from an email
		// alert matches "TRADER JOES" from the bank.
		fields, norm = []string{merchant}, Key
	}
	want := norm(r.MatchValue)
	if want == "" {
		// No text condition: account, amount or day decide, if there are any.
		return r.AccountID.Valid || r.AmountMin.Valid || r.AmountMax.Valid
	}
	for _, f := range fields {
		f = norm(f)
		if f == "" {
			continue
		}
		switch r.MatchOp {
		case "equals":
			if f == want {
				return true
			}
		case "starts_with":
			if strings.HasPrefix(f, want) {
				return true
			}
		default:
			if strings.Contains(f, want) {
				return true
			}
		}
	}
	return false
}

// dayInRange reports whether date's day of the month is within [min, max]; a missing bound
// is open, and min > max wraps around the month end (28-3 = the 28th through the 3rd).
func dayInRange(date string, min, max sql.NullInt64) bool {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return false
	}
	day := int64(d.Day())
	lo, hi := int64(1), int64(31)
	if min.Valid {
		lo = min.Int64
	}
	if max.Valid {
		hi = max.Int64
	}
	if lo <= hi {
		return day >= lo && day <= hi
	}
	return day >= lo || day <= hi
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
