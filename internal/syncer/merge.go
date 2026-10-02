package syncer

import (
	"context"
	"errors"

	"viceroy/internal/db"
)

var ErrMergeSelf = errors.New("cannot merge an account into itself")

// Merge folds account from into account into: transactions move over (duplicates by
// provider id, or same date and amount, are dropped), balance history is kept, and if from
// is the connected one, into takes over its connection. from is then deleted.
func (s *Service) Merge(ctx context.Context, householdID, fromID, intoID int64) error {
	if fromID == intoID {
		return ErrMergeSelf
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := db.New(tx)
	from, err := q.GetAccount(ctx, db.GetAccountParams{ID: fromID, HouseholdID: householdID})
	if err != nil {
		return err
	}
	into, err := q.GetAccount(ctx, db.GetAccountParams{ID: intoID, HouseholdID: householdID})
	if err != nil {
		return err
	}
	now := s.Now().Unix()

	if err := moveTransactions(ctx, q, from.ID, into.ID, now); err != nil {
		return err
	}
	if err := q.MoveSnapshots(ctx, db.MoveSnapshotsParams{IntoID: into.ID, FromID: from.ID}); err != nil {
		return err
	}

	// The connected account wins the provider link.
	if from.ExternalID.Valid && (from.Status == "active" || from.Status == "review" || !into.ExternalID.Valid || into.Status == "disconnected") {
		if err := q.ClearAccountExternal(ctx, db.ClearAccountExternalParams{UpdatedAt: now, ID: from.ID}); err != nil {
			return err
		}
		if err := q.RelinkAccount(ctx, db.RelinkAccountParams{
			ConnectionID: from.ConnectionID, InstitutionID: from.InstitutionID, ExternalID: from.ExternalID,
			InstitutionName: from.InstitutionName, ProviderName: from.ProviderName, UpdatedAt: now, ID: into.ID,
		}); err != nil {
			return err
		}
		if _, err := q.UpdateAccountFromSync(ctx, db.UpdateAccountFromSyncParams{
			InstitutionID: from.InstitutionID, InstitutionName: from.InstitutionName, ProviderName: from.ProviderName,
			Currency: from.Currency, BalanceCents: from.BalanceCents, AvailableCents: from.AvailableCents,
			BalanceAt: from.BalanceAt, UpdatedAt: now, ID: into.ID,
		}); err != nil {
			return err
		}
		if from.ConnectionID.Valid {
			q.InsertSyncEvent(ctx, db.InsertSyncEventParams{
				ConnectionID: from.ConnectionID.Int64, At: now, Kind: "account_relinked", AccountID: nullInt(into.ID),
				Message: from.Name + " merged into " + into.Name,
			})
		}
	}
	if err := q.DeleteAccount(ctx, db.DeleteAccountParams{ID: from.ID, HouseholdID: householdID}); err != nil {
		return err
	}
	return tx.Commit()
}

// moveTransactions moves from's transactions onto into. Ones into already has (same provider
// id, or same date and amount) are dropped: into's copy wins.
func moveTransactions(ctx context.Context, q *db.Queries, fromID, intoID, now int64) error {
	intoTx, err := q.ListAccountTransactionKeys(ctx, intoID)
	if err != nil {
		return err
	}
	byExt := map[string]bool{}
	type key struct {
		date string
		amt  int64
	}
	byKey := map[key]int{}
	for _, t := range intoTx {
		if t.ExternalID.Valid {
			byExt[t.ExternalID.String] = true
		}
		byKey[key{t.Date, t.AmountCents}]++
	}
	fromTx, err := q.ListAccountTransactionKeys(ctx, fromID)
	if err != nil {
		return err
	}
	for _, t := range fromTx {
		k := key{t.Date, t.AmountCents}
		dup := (t.ExternalID.Valid && byExt[t.ExternalID.String]) || byKey[k] > 0
		if dup {
			if byKey[k] > 0 {
				byKey[k]--
			}
			if err := q.DeleteTransaction(ctx, t.ID); err != nil {
				return err
			}
			continue
		}
		if err := q.MoveTransaction(ctx, db.MoveTransactionParams{AccountID: intoID, UpdatedAt: now, ID: t.ID}); err != nil {
			return err
		}
	}
	return nil
}
