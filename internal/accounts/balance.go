package accounts

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"viceroy/internal/db"
)

// AdjustManualBalance moves a manual account's running balance by delta (a manual or email
// transaction was added, changed or removed) and records today's snapshot. Synced accounts
// are left alone: their balance comes from the bank.
func AdjustManualBalance(ctx context.Context, q *db.Queries, accountID, delta int64) error {
	if delta == 0 {
		return nil
	}
	now := time.Now()
	bal, err := q.AdjustManualBalance(ctx, db.AdjustManualBalanceParams{Delta: delta, Now: sql.NullInt64{Int64: now.Unix(), Valid: true}, ID: accountID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return q.UpsertBalanceSnapshot(ctx, db.UpsertBalanceSnapshotParams{AccountID: accountID, Date: now.Format(time.DateOnly), BalanceCents: bal})
}
