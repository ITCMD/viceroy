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
)

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
	cands, err := q.ListLinkCandidates(ctx, db.ListLinkCandidatesParams{
		AccountID: p.AccountID, AmountLo: p.AmountCents, AmountHi: p.AmountCents,
		DateLo: d.AddDate(0, 0, -DaysAfter).Format(time.DateOnly), DateHi: d.AddDate(0, 0, DaysBefore).Format(time.DateOnly),
		PostedID: p.ID,
	})
	if err != nil {
		return 0, err
	}
	postedText := p.Description + " " + p.Payee + " " + p.Merchant
	best, bestScore, bestGap := int64(0), 0.0, 99
	for _, c := range cands {
		s := Score(c.Description+" "+c.Payee+" "+c.MerchantName, postedText)
		if s < MinScore {
			continue
		}
		gap := dayGap(c.Date, p.Date)
		if s > bestScore || (s == bestScore && gap < bestGap) {
			best, bestScore, bestGap = c.ID, s, gap
		}
	}
	if best == 0 {
		return 0, nil
	}
	return best, apply(ctx, q, best, p.ID, now)
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
