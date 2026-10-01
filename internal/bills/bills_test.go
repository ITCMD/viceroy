package bills

import (
	"database/sql"
	"testing"

	"viceroy/internal/db"
)

func bill(id, acct int64, kind, date string, created int64) db.AccountBill {
	return db.AccountBill{ID: id, AccountID: sql.NullInt64{Int64: acct, Valid: acct != 0}, Kind: kind,
		Date: sql.NullString{String: date, Valid: date != ""}, CreatedAt: created}
}

func TestByAccount(t *testing.T) {
	today := "2026-10-01"
	rows := []db.AccountBill{ // newest first
		bill(9, 4, Scheduled, "2026-09-20", 900), // account 4: scheduled date passed → paid
		bill(8, 4, Due, "2026-09-25", 800),
		bill(7, 3, Due, "2026-09-01", 700),      // account 3: due a month ago, nothing since → stale
		bill(6, 2, Paid, "2026-09-28", 600),     // account 2: paid after the due → covered
		bill(5, 2, Due, "2026-10-05", 500),
		bill(4, 1, Scheduled, "2026-10-08", 400), // account 1: due + scheduled
		bill(3, 1, Due, "2026-10-10", 300),
		bill(2, 1, Paid, "2026-09-10", 200), // last cycle's payment: before the due, ignored
		bill(1, 0, Due, "2026-10-03", 100),  // no account: skipped
	}
	got := ByAccount(rows, today)
	if s := got[1]; s.Due == nil || s.Due.ID != 3 || s.Scheduled == nil || s.Scheduled.ID != 4 || s.Paid != nil {
		t.Errorf("account 1 = %+v", s)
	}
	if s := got[2]; s.Due != nil || s.Paid == nil || s.Paid.ID != 6 {
		t.Errorf("account 2 = %+v", s)
	}
	if _, ok := got[3]; ok {
		t.Errorf("account 3 should be stale: %+v", got[3])
	}
	if s := got[4]; s.Due != nil || s.Scheduled != nil || s.Paid == nil || s.Paid.ID != 9 {
		t.Errorf("account 4 = %+v", s)
	}
	if len(got) != 3 {
		t.Errorf("accounts = %d", len(got))
	}
}
