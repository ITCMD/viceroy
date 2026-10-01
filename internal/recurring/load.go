package recurring

import (
	"context"
	"time"

	"viceroy/internal/db"
)

// Load detects a household's recurring series from its history and reports which ones the
// user dismissed (by key). today is a local date.
func Load(ctx context.Context, q *db.Queries, hh int64, today time.Time) ([]Series, map[string]bool, error) {
	rows, err := q.RecurringCandidates(ctx, db.RecurringCandidatesParams{
		HouseholdID: hh, FromDate: today.AddDate(0, 0, -HistoryDays).Format(time.DateOnly),
	})
	if err != nil {
		return nil, nil, err
	}
	txns := make([]Txn, len(rows))
	for i, r := range rows {
		txns[i] = Txn{
			ID: r.ID, Date: r.Date, Amount: r.AmountCents, MerchantID: r.MerchantID.Int64, Merchant: r.Merchant,
			CategoryID: r.CategoryID.Int64, CategoryName: r.CategoryName, CategoryIcon: r.CategoryIcon,
			AccountID: r.AccountID, AccountName: r.AccountName,
		}
	}
	dismissed, err := q.ListRecurringDismissed(ctx, hh)
	if err != nil {
		return nil, nil, err
	}
	isDismissed := map[string]bool{}
	for _, k := range dismissed {
		isDismissed[k] = true
	}
	return Detect(txns, today), isDismissed, nil
}
