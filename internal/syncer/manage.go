package syncer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"viceroy/internal/db"
)

// ErrNotOnConnection means an account id isn't one of this connection's SimpleFIN accounts.
var ErrNotOnConnection = errors.New("that account isn't part of this SimpleFIN connection")

// HistoryPending is returned (wrapped) when accounts were added but their history couldn't be
// fetched yet; the next sync fetches the full 90 days instead.
var HistoryPending = errors.New("history will load at the next sync")

// SetTracked chooses which of a connection's accounts Viceroy follows. Excluded accounts become
// 'ignored': their history stays but is left out of everything and sync skips them. Included
// accounts become active and get their last 90 days fetched in one request for just those
// accounts. If that fetch can't happen now, the accounts stay added and an error wrapping
// HistoryPending says so.
func (s *Service) SetTracked(ctx context.Context, connID int64, include, exclude []int64) error {
	defer s.lock(connID)()
	q := db.New(s.DB)
	c, err := q.GetConnectionByID(ctx, connID)
	if err != nil {
		return err
	}
	now := s.Now()
	byID := map[int64]db.Account{}
	list, err := q.ListConnectionAccounts(ctx, nullInt(connID))
	if err != nil {
		return err
	}
	for _, a := range list {
		byID[a.ID] = a
	}
	var added []db.Account
	set := func(ids []int64, status string) error {
		for _, id := range ids {
			a, ok := byID[id]
			if !ok || a.ReplacedBy.Valid {
				return ErrNotOnConnection
			}
			if status == "active" && a.Status != "ignored" {
				continue // already followed
			}
			if status == "active" && a.Status == "ignored" && a.ExternalID.Valid {
				added = append(added, a)
			}
			n, err := q.SetConnectionAccountTracked(ctx, db.SetConnectionAccountTrackedParams{Status: status, UpdatedAt: now.Unix(), ID: id, ConnectionID: nullInt(connID)})
			if err != nil {
				return err
			}
			if n == 0 && status == "ignored" && a.Status != "ignored" {
				return fmt.Errorf("%s can't be removed while it's %s", a.Name, a.Status)
			}
		}
		return nil
	}
	if err := set(exclude, "ignored"); err != nil {
		return err
	}
	if err := set(include, "active"); err != nil {
		return err
	}
	if len(added) == 0 {
		s.changed(c.HouseholdID)
		return nil
	}
	err = s.backfill(ctx, c, added)
	s.changed(c.HouseholdID)
	if err != nil {
		// Fetch everything for the full window next time instead.
		if cerr := q.ClearConnectionSyncedThrough(ctx, connID); cerr != nil {
			return cerr
		}
		return fmt.Errorf("%w (%v)", HistoryPending, err)
	}
	return nil
}

// backfill fetches MaxWindow of history for newly added accounts only.
func (s *Service) backfill(ctx context.Context, c db.Connection, accts []db.Account) error {
	q := db.New(s.DB)
	now := s.Now()
	today := now.Format("2006-01-02")
	count := c.RequestsCount
	if c.RequestsDay != today {
		count = 0
	}
	if count >= DailyRequestCap {
		return ErrDailyCap
	}
	if err := q.SetConnectionRequests(ctx, db.SetConnectionRequestsParams{RequestsDay: today, RequestsCount: count + 1, ID: c.ID}); err != nil {
		return err
	}
	access, err := s.Box.Open(c.SecretEnc)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(accts))
	names := make([]string, 0, len(accts))
	want := map[string]bool{}
	for _, a := range accts {
		ids = append(ids, a.ExternalID.String)
		names = append(names, a.Name)
		want[a.ExternalID.String] = true
	}
	start := now.Add(-MaxWindow)
	set, err := s.Client.Fetch(ctx, access, start, now, ids...)
	if err != nil {
		return err
	}
	// Only the accounts asked for, even if the Bridge returned more.
	kept := set.Accounts[:0]
	for _, a := range set.Accounts {
		if want[a.ID] {
			kept = append(kept, a)
		}
	}
	set.Accounts = kept

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r := &run{s: s, q: db.New(tx), conn: c, now: now, windowStart: start.Format("2006-01-02"), partial: true}
	if err := r.apply(ctx, set); err != nil {
		return err
	}
	r.event(ctx, "accounts_added", 0, fmt.Sprintf("added %s; %d transactions", strings.Join(names, ", "), r.txnCount))
	return tx.Commit()
}

// SetAutoAdd sets whether accounts that appear on the Bridge later are added without asking.
func (s *Service) SetAutoAdd(ctx context.Context, householdID, connID int64, on bool) error {
	v := int64(0)
	if on {
		v = 1
	}
	return db.New(s.DB).SetConnectionAutoAdd(ctx, db.SetConnectionAutoAddParams{AutoAddNew: v, ID: connID, HouseholdID: householdID})
}
