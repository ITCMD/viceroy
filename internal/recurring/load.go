package recurring

import (
	"context"
	"time"

	"viceroy/internal/db"
)

// Load builds a household's schedule: tracked items matched to history, and suggestions
// detected from the rest. today is a local date.
func Load(ctx context.Context, q *db.Queries, hh int64, today time.Time) (Schedule, error) {
	rows, err := q.RecurringCandidates(ctx, db.RecurringCandidatesParams{
		HouseholdID: hh, FromDate: today.AddDate(0, 0, -HistoryDays).Format(time.DateOnly),
	})
	if err != nil {
		return Schedule{}, err
	}
	txns := make([]Txn, len(rows))
	for i, r := range rows {
		txns[i] = Txn{
			ID: r.ID, Date: r.Date, Amount: r.AmountCents, MerchantID: r.MerchantID.Int64, Merchant: r.Merchant, Description: r.Description,
			CategoryID: r.CategoryID.Int64, CategoryName: r.CategoryName, CategoryIcon: r.CategoryIcon,
			AccountID: r.AccountID, AccountName: r.AccountName,
		}
	}
	dismissed, err := q.ListRecurringDismissed(ctx, hh)
	if err != nil {
		return Schedule{}, err
	}
	isDismissed := map[string]bool{}
	for _, k := range dismissed {
		isDismissed[k] = true
	}
	rawItems, err := q.ListRecurringItems(ctx, hh)
	if err != nil {
		return Schedule{}, err
	}
	cats, err := q.ListCategories(ctx, hh)
	if err != nil {
		return Schedule{}, err
	}
	accts, err := q.ListAccounts(ctx, hh)
	if err != nil {
		return Schedule{}, err
	}
	catName, catIcon, acctName := map[int64]string{}, map[int64]string{}, map[int64]string{}
	for _, c := range cats {
		catName[c.ID], catIcon[c.ID] = c.Name, c.Icon
	}
	for _, a := range accts {
		acctName[a.ID] = a.Name
	}
	items := make([]Item, len(rawItems))
	for i, r := range rawItems {
		items[i] = Item{
			ID: r.ID, Name: r.Name, MerchantID: r.MerchantID.Int64, MatchText: r.MatchText,
			AccountID: r.AccountID.Int64, AccountName: acctName[r.AccountID.Int64],
			CategoryID: r.CategoryID.Int64, CategoryName: catName[r.CategoryID.Int64], CategoryIcon: catIcon[r.CategoryID.Int64],
			Amount: r.AmountCents, AmountVaries: r.AmountVaries == 1, Cadence: Cadence(r.Cadence),
			Anchor: r.AnchorDate, Day2: int(r.Day2), SeriesKey: r.SeriesKey,
		}
	}
	return Build(items, txns, isDismissed, today), nil
}
