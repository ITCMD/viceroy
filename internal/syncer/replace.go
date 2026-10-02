package syncer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"viceroy/internal/db"
)

// ErrReplaceBuiltin means Paper Cash was picked; it can't be replaced or be the replacement.
var ErrReplaceBuiltin = errors.New("Paper Cash can't be replaced")

// Replace swaps an account for another one that is really the same account, for sync
// duplicates (the aggregator starts reporting an account under a new id, or through a second
// login). The replacement takes over the old account's transactions (its own copies win),
// balance history, rules, email filters, recurring items, bills, name, type, color, logo and
// settings, and starts being followed. The old account is deleted; if it is still linked to
// SimpleFIN it is kept instead as a hidden, ignored tombstone so its bank account isn't offered
// again. If the replacement wasn't syncing yet, its history is fetched; when that can't happen
// now, the error wraps HistoryPending and the next sync does it.
func (s *Service) Replace(ctx context.Context, householdID, oldID, newID int64) error {
	if oldID == newID {
		return ErrMergeSelf
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := db.New(tx)
	old, err := q.GetAccount(ctx, db.GetAccountParams{ID: oldID, HouseholdID: householdID})
	if err != nil {
		return err
	}
	repl, err := q.GetAccount(ctx, db.GetAccountParams{ID: newID, HouseholdID: householdID})
	if err != nil {
		return err
	}
	if old.Builtin != "" || repl.Builtin != "" {
		return ErrReplaceBuiltin
	}
	now := s.Now().Unix()

	if err := moveTransactions(ctx, q, old.ID, repl.ID, now); err != nil {
		return err
	}
	// The replacement's own readings win on days both have one.
	if err := q.MoveSnapshots(ctx, db.MoveSnapshotsParams{IntoID: repl.ID, FromID: old.ID}); err != nil {
		return err
	}
	if err := q.DeleteAccountSnapshots(ctx, old.ID); err != nil {
		return err
	}
	moves := []func(context.Context, int64, int64) error{
		func(ctx context.Context, from, into int64) error {
			return q.RepointRules(ctx, db.RepointRulesParams{IntoID: sql.NullInt64{Int64: into, Valid: true}, FromID: sql.NullInt64{Int64: from, Valid: true}})
		},
		func(ctx context.Context, from, into int64) error {
			return q.RepointEmailFilters(ctx, db.RepointEmailFiltersParams{IntoID: into, FromID: from})
		},
		func(ctx context.Context, from, into int64) error {
			return q.RepointRecurring(ctx, db.RepointRecurringParams{IntoID: sql.NullInt64{Int64: into, Valid: true}, FromID: sql.NullInt64{Int64: from, Valid: true}})
		},
		func(ctx context.Context, from, into int64) error {
			return q.RepointBills(ctx, db.RepointBillsParams{IntoID: sql.NullInt64{Int64: into, Valid: true}, FromID: sql.NullInt64{Int64: from, Valid: true}})
		},
		func(ctx context.Context, from, into int64) error {
			return q.MoveAccountLogo(ctx, db.MoveAccountLogoParams{IntoID: into, FromID: from})
		},
	}
	for _, m := range moves {
		if err := m(ctx, old.ID, repl.ID); err != nil {
			return err
		}
	}
	if err := q.CopyAccountSettings(ctx, db.CopyAccountSettingsParams{
		Name: old.Name, Type: old.Type, IncludeInNetWorth: old.IncludeInNetWorth, Hidden: old.Hidden,
		Color: old.Color, ColorSource: old.ColorSource, OwnerUserID: old.OwnerUserID, UpdatedAt: now, ID: repl.ID,
	}); err != nil {
		return err
	}
	if old.ExternalID.Valid && old.ConnectionID.Valid {
		if err := q.SetAccountReplaced(ctx, db.SetAccountReplacedParams{ReplacedBy: sql.NullInt64{Int64: repl.ID, Valid: true}, UpdatedAt: now, ID: old.ID}); err != nil {
			return err
		}
	} else if err := q.DeleteAccount(ctx, db.DeleteAccountParams{ID: old.ID, HouseholdID: householdID}); err != nil {
		return err
	}
	if repl.ConnectionID.Valid {
		q.InsertSyncEvent(ctx, db.InsertSyncEventParams{
			ConnectionID: repl.ConnectionID.Int64, At: now, Kind: "account_replaced", AccountID: nullInt(repl.ID),
			Message: fmt.Sprintf("%s replaced by %s", old.Name, repl.ProviderName),
		})
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.changed(householdID)

	// A replacement that wasn't being followed has none of its own transactions yet.
	if repl.Status != "ignored" || !repl.ConnectionID.Valid || !repl.ExternalID.Valid {
		return nil
	}
	defer s.lock(repl.ConnectionID.Int64)()
	c, err := db.New(s.DB).GetConnectionByID(ctx, repl.ConnectionID.Int64)
	if err != nil {
		return err
	}
	fresh, err := db.New(s.DB).GetAccount(ctx, db.GetAccountParams{ID: repl.ID, HouseholdID: householdID})
	if err != nil {
		return err
	}
	if err := s.backfill(ctx, c, []db.Account{fresh}); err != nil {
		if cerr := db.New(s.DB).ClearConnectionSyncedThrough(ctx, c.ID); cerr != nil {
			return cerr
		}
		return fmt.Errorf("%w (%v)", HistoryPending, err)
	}
	s.changed(householdID)
	return nil
}
