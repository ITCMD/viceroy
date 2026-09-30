package accounts

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"viceroy/internal/db"
)

// PaperCash is the built-in cash wallet every household gets. Enabled means active and
// visible; disabled means closed and hidden, which keeps its transactions and history.
const (
	PaperCash     = "paper_cash"
	PaperCashName = "Paper Cash"
)

// EnsurePaperCash creates the household's Paper Cash account if it has never had one.
func EnsurePaperCash(ctx context.Context, q *db.Queries, householdID int64) error {
	_, err := q.GetBuiltinAccount(ctx, db.GetBuiltinAccountParams{HouseholdID: householdID, Builtin: PaperCash})
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := time.Now().Unix()
	_, err = q.CreateBuiltinAccount(ctx, db.CreateBuiltinAccountParams{
		HouseholdID: householdID, Name: PaperCashName, Type: Cash, BalanceAt: sql.NullInt64{Int64: now, Valid: true},
		Builtin: PaperCash, CreatedAt: now, UpdatedAt: now,
	})
	return err
}

// EnsurePaperCashAll backfills Paper Cash for households created before it existed.
func EnsurePaperCashAll(ctx context.Context, q *db.Queries) error {
	ids, err := q.ListHouseholdIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := EnsurePaperCash(ctx, q, id); err != nil {
			return err
		}
	}
	return nil
}

// PaperCashEnabled reports whether the account is switched on.
func PaperCashEnabled(a db.Account) bool {
	return a.Status != "closed" || a.Hidden == 0
}
