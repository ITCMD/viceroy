// Package linking pairs provisional transactions (manual pending entries, email alerts) with
// the posted transaction that later arrives from the bank, so the purchase counts once.
package linking

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"viceroy/internal/categorize"
	"viceroy/internal/db"
)

const (
	// A provisional entry can match a posted row dated from 1 day before it to 10 days after.
	DaysBefore = 1
	DaysAfter  = 10
	// Minimum description similarity for an automatic link.
	MinScore = 0.5
	// A card charge can post higher than its alert or pending entry (a tip, a gas
	// pre-authorization settling). Money out may grow by up to TipPct percent when the
	// descriptions agree at least TipMinScore. An exact amount always wins.
	TipPct      = 30
	TipMinScore = 0.8
)

// postedRange is the posted amounts a provisional amount may link to.
func postedRange(prov int64) (lo, hi int64) {
	if prov >= 0 {
		return prov, prov
	}
	return prov - (-prov)*TipPct/100, prov
}

// provisionalRange is the provisional amounts a posted amount may link to (the inverse).
func provisionalRange(posted int64) (lo, hi int64) {
	if posted >= 0 {
		return posted, posted
	}
	// |prov| * (100+TipPct)/100 >= |posted|, rounded so the ranges agree.
	minAbs := ((-posted)*100 + 100 + TipPct - 1) / (100 + TipPct)
	return posted, -minAbs
}

// qualifies reports whether a candidate is good enough to link automatically.
func qualifies(exact bool, score float64) bool {
	if exact {
		return score >= MinScore
	}
	return score >= TipMinScore
}

type pick struct {
	id    int64
	exact bool
	score float64
	gap   int
}

func (p *pick) consider(id int64, exact bool, score float64, gap int) {
	if !qualifies(exact, score) {
		return
	}
	if p.id == 0 || (exact && !p.exact) ||
		(exact == p.exact && (score > p.score || (score == p.score && gap < p.gap))) {
		*p = pick{id, exact, score, gap}
	}
}

var (
	ErrNotProvisional = errors.New("only a pending entry can be linked to a posted transaction")
	ErrAlreadyLinked  = errors.New("transaction is already linked")
	ErrNotPosted      = errors.New("target must be a posted transaction")
)

// Posted is the newly arrived bank transaction.
type Posted struct {
	ID          int64
	AccountID   int64
	Date        string
	AmountCents int64
	Description string
	Payee       string
	Merchant    string
}

// LinkPosted finds the best unlinked provisional entry for p and links it. It returns the
// provisional id, or 0 when nothing matched.
func LinkPosted(ctx context.Context, q *db.Queries, p Posted, now time.Time) (int64, error) {
	d, err := time.Parse(time.DateOnly, p.Date)
	if err != nil {
		return 0, err
	}
	lo, hi := provisionalRange(p.AmountCents)
	cands, err := q.ListLinkCandidates(ctx, db.ListLinkCandidatesParams{
		AccountID: p.AccountID, AmountLo: lo, AmountHi: hi,
		DateLo: d.AddDate(0, 0, -DaysAfter).Format(time.DateOnly), DateHi: d.AddDate(0, 0, DaysBefore).Format(time.DateOnly),
		PostedID: p.ID,
	})
	if err != nil {
		return 0, err
	}
	postedText := p.Description + " " + p.Payee + " " + p.Merchant
	var best pick
	for _, c := range cands {
		best.consider(c.ID, c.AmountCents == p.AmountCents, Score(c.Description+" "+c.Payee+" "+c.MerchantName, postedText), dayGap(c.Date, p.Date))
	}
	if best.id == 0 {
		return 0, nil
	}
	return best.id, apply(ctx, q, best.id, p.ID, now)
}

// Link links provisional entry provID to posted transaction postedID by hand.
func Link(ctx context.Context, q *db.Queries, householdID, provID, postedID int64, now time.Time) error {
	prov, err := q.GetTransaction(ctx, db.GetTransactionParams{ID: provID, HouseholdID: householdID})
	if err != nil {
		return err
	}
	posted, err := q.GetTransaction(ctx, db.GetTransactionParams{ID: postedID, HouseholdID: householdID})
	if err != nil {
		return err
	}
	switch {
	case prov.Provisional != 1:
		return ErrNotProvisional
	case prov.LinkedTxnID.Valid:
		return ErrAlreadyLinked
	case posted.Provisional == 1:
		return ErrNotPosted
	}
	return apply(ctx, q, provID, postedID, now)
}

// Unlink breaks a link and blacklists the pair so it's never auto-linked again.
func Unlink(ctx context.Context, q *db.Queries, householdID, provID int64) error {
	prov, err := q.GetTransaction(ctx, db.GetTransactionParams{ID: provID, HouseholdID: householdID})
	if err != nil {
		return err
	}
	if !prov.LinkedTxnID.Valid {
		return nil
	}
	if err := q.BlacklistLink(ctx, db.BlacklistLinkParams{ProvisionalID: provID, PostedID: prov.LinkedTxnID.Int64}); err != nil {
		return err
	}
	return q.UnlinkTransaction(ctx, provID)
}

// apply links the pair; the posted row inherits the entry's category, notes and tags.
func apply(ctx context.Context, q *db.Queries, provID, postedID int64, now time.Time) error {
	if err := q.LinkTransaction(ctx, db.LinkTransactionParams{LinkedTxnID: sql.NullInt64{Int64: postedID, Valid: true}, LinkedAt: sql.NullInt64{Int64: now.Unix(), Valid: true}, ID: provID}); err != nil {
		return err
	}
	prov, err := q.GetTransactionByID(ctx, provID)
	if err != nil {
		return err
	}
	posted, err := q.GetTransactionByID(ctx, postedID)
	if err != nil {
		return err
	}
	changed := false
	if prov.CategoryID.Valid && posted.CategorySource != categorize.SourceUser {
		posted.CategoryID, posted.CategorySource, posted.NeedsReview = prov.CategoryID, categorize.SourceLinked, 0
		changed = true
	}
	if posted.Notes == "" && prov.Notes != "" {
		posted.Notes, changed = prov.Notes, true
	}
	if changed {
		if err := q.UpdateTransactionUser(ctx, db.UpdateTransactionUserParams{
			Date: posted.Date, AmountCents: posted.AmountCents, Description: posted.Description,
			MerchantID: posted.MerchantID, CategoryID: posted.CategoryID, CategorySource: posted.CategorySource,
			Notes: posted.Notes, Hidden: posted.Hidden, NeedsReview: posted.NeedsReview, UpdatedAt: now.Unix(),
			ID: posted.ID, HouseholdID: posted.HouseholdID,
		}); err != nil {
			return err
		}
	}
	return q.CopyTransactionTags(ctx, db.CopyTransactionTagsParams{ToID: postedID, FromID: provID})
}

// Score rates how well a provisional description matches a posted one, 0..1. Entries with no
// usable words (e.g. just an amount) score 1, since amount and date already had to match.
func Score(provisional, posted string) float64 {
	words := tokens(provisional)
	if len(words) == 0 {
		return 1
	}
	hay := categorize.Key(posted)
	hit := 0
	for _, w := range words {
		if strings.Contains(hay, w) {
			hit++
		}
	}
	s := float64(hit) / float64(len(words))
	if t := trigram(categorize.Key(provisional), hay); t > s {
		s = t
	}
	return s
}

func tokens(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range strings.Fields(strings.ToLower(s)) {
		k := categorize.Key(f)
		if len(k) < 3 || seen[k] || isDigits(k) {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// trigram is the share of a's trigrams that also appear in b.
func trigram(a, b string) float64 {
	if len(a) < 3 || len(b) < 3 {
		return 0
	}
	set := map[string]bool{}
	for i := 0; i+3 <= len(b); i++ {
		set[b[i:i+3]] = true
	}
	hit, n := 0, 0
	for i := 0; i+3 <= len(a); i++ {
		n++
		if set[a[i:i+3]] {
			hit++
		}
	}
	return float64(hit) / float64(n)
}

func dayGap(a, b string) int {
	ta, err1 := time.Parse(time.DateOnly, a)
	tb, err2 := time.Parse(time.DateOnly, b)
	if err1 != nil || err2 != nil {
		return 99
	}
	d := int(ta.Sub(tb).Hours() / 24)
	if d < 0 {
		d = -d
	}
	return d
}

// Duplicates lists posted transactions that look like a new pending entry: same account and
// amount, dated from one day before the entry up to the entry's date.
func Duplicates(ctx context.Context, q *db.Queries, accountID, amountCents int64, date string) ([]db.ListPostedDuplicatesRow, error) {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return nil, err
	}
	return q.ListPostedDuplicates(ctx, db.ListPostedDuplicatesParams{
		AccountID: accountID, AmountCents: amountCents,
		DateLo: d.AddDate(0, 0, -1).Format(time.DateOnly), DateHi: d.AddDate(0, 0, 1).Format(time.DateOnly),
	})
}

// Provisional is a new stand-in entry (an email alert) that may arrive after the bank already
// posted the purchase.
type Provisional struct {
	ID          int64
	AccountID   int64
	Date        string
	AmountCents int64
	Description string
	Merchant    string
}

// LinkProvisional links a new provisional entry to an already-posted transaction, using the
// same window and score as LinkPosted. It returns the posted id, or 0 when nothing matched.
func LinkProvisional(ctx context.Context, q *db.Queries, p Provisional, now time.Time) (int64, error) {
	d, err := time.Parse(time.DateOnly, p.Date)
	if err != nil {
		return 0, err
	}
	lo, hi := postedRange(p.AmountCents)
	cands, err := q.ListLinkTargets(ctx, db.ListLinkTargetsParams{
		AccountID: p.AccountID, AmountLo: lo, AmountHi: hi,
		DateLo: d.AddDate(0, 0, -DaysBefore).Format(time.DateOnly), DateHi: d.AddDate(0, 0, DaysAfter).Format(time.DateOnly),
		ProvisionalID: p.ID,
	})
	if err != nil {
		return 0, err
	}
	provText := p.Description + " " + p.Merchant
	var best pick
	for _, c := range cands {
		best.consider(c.ID, c.AmountCents == p.AmountCents, Score(provText, c.Description+" "+c.Payee+" "+c.MerchantName), dayGap(p.Date, c.Date))
	}
	if best.id == 0 {
		return 0, nil
	}
	return best.id, apply(ctx, q, p.ID, best.id, now)
}
