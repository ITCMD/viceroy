// Package syncer pulls SimpleFIN data into the database. It reconciles returned accounts
// against existing ones so reconnecting a bank never creates duplicates or breaks history.
package syncer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"viceroy/internal/accounts"
	"viceroy/internal/categorize"
	"viceroy/internal/db"
	"viceroy/internal/linking"
	"viceroy/internal/money"
	"viceroy/internal/providers/simplefin"
	"viceroy/internal/secrets"
)

const (
	DailyRequestCap = 20 // Bridge allows 24/day per access URL; keep headroom.
	Interval        = 8 * time.Hour
	MaxWindow       = 90 * 24 * time.Hour
	Overlap         = 5 * 24 * time.Hour
	source          = "simplefin"
)

var ErrDailyCap = fmt.Errorf("daily SimpleFIN request limit (%d) reached; syncing resumes tomorrow", DailyRequestCap)

type Service struct {
	DB     *sql.DB
	Box    *secrets.Box
	Client *simplefin.Client
	Log    *slog.Logger
	Now    func() time.Time
	// Changed, when set, is called after a sync (or failed sync) changed the household's data.
	Changed func(householdID int64)

	locks sync.Map // connection id -> *sync.Mutex
}

func New(conn *sql.DB, box *secrets.Box, log *slog.Logger) *Service {
	return &Service{DB: conn, Box: box, Client: simplefin.New(), Log: log, Now: time.Now}
}

func (s *Service) lock(id int64) func() {
	m, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// Connect claims a setup token, stores the connection and runs its first sync.
// The connection is kept even if the first sync fails; the error is returned for display.
func (s *Service) Connect(ctx context.Context, householdID int64, token string) (db.Connection, error) {
	access, err := s.Client.Claim(ctx, token)
	if err != nil {
		return db.Connection{}, err
	}
	sealed, err := s.Box.Seal(access)
	if err != nil {
		return db.Connection{}, err
	}
	c, err := db.New(s.DB).CreateConnection(ctx, db.CreateConnectionParams{
		HouseholdID: householdID, Provider: source, Name: "SimpleFIN Bridge",
		SecretEnc: sealed, CreatedAt: s.Now().Unix(),
	})
	if err != nil {
		return db.Connection{}, err
	}
	return c, s.Sync(ctx, c.ID)
}

// Sync fetches one connection and applies the result. It is safe to call concurrently.
func (s *Service) Sync(ctx context.Context, connID int64) error {
	defer s.lock(connID)()
	q := db.New(s.DB)
	c, err := q.GetConnectionByID(ctx, connID)
	if err != nil {
		return err
	}
	now := s.Now()
	today := now.Format(time.DateOnly)
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
		return s.fail(ctx, c, "error", fmt.Errorf("decrypting access URL: %w", err))
	}
	start := now.Add(-MaxWindow)
	if c.SyncedThrough.Valid {
		if st := time.Unix(c.SyncedThrough.Int64, 0).Add(-Overlap); st.After(start) {
			start = st
		}
	}
	set, err := s.Client.Fetch(ctx, access, start, now)
	if err != nil {
		status := "error"
		if errors.Is(err, simplefin.ErrAuth) {
			status = "revoked"
		}
		return s.fail(ctx, c, status, err)
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r := &run{s: s, q: db.New(tx), conn: c, now: now, windowStart: start.Format(time.DateOnly)}
	if err := r.apply(ctx, set); err != nil {
		return err
	}
	next := now.Add(Interval + time.Duration(rand.Int64N(int64(time.Hour))))
	if err := r.q.SetConnectionSyncOK(ctx, db.SetConnectionSyncOKParams{
		LastSyncAt: nullInt(now.Unix()), SyncedThrough: nullInt(now.Unix()), NextSyncAt: nullInt(next.Unix()), ID: c.ID,
	}); err != nil {
		return err
	}
	msg := fmt.Sprintf("%d accounts, %d transactions", len(set.Accounts), r.txnCount)
	if gen := genErrors(set); gen != "" {
		msg += "; bridge reported: " + gen
	}
	r.event(ctx, "sync_ok", 0, msg)
	if err := tx.Commit(); err != nil {
		return err
	}
	s.changed(c.HouseholdID)
	return nil
}

func (s *Service) changed(householdID int64) {
	if s.Changed != nil {
		s.Changed(householdID)
	}
}

func genErrors(set *simplefin.AccountSet) string {
	var out []string
	for _, e := range set.Errors {
		if !strings.HasPrefix(e.Code, "con.") {
			out = append(out, e.Msg)
		}
	}
	return strings.Join(out, "; ")
}

func (s *Service) fail(ctx context.Context, c db.Connection, status string, cause error) error {
	q := db.New(s.DB)
	next := s.Now().Add(time.Hour)
	if status == "revoked" {
		next = s.Now().Add(Interval)
	}
	if err := q.SetConnectionSyncError(ctx, db.SetConnectionSyncErrorParams{
		Status: status, LastError: cause.Error(), NextSyncAt: nullInt(next.Unix()), ID: c.ID,
	}); err != nil {
		return err
	}
	q.InsertSyncEvent(ctx, db.InsertSyncEventParams{ConnectionID: c.ID, At: s.Now().Unix(), Kind: "sync_error", Message: cause.Error()})
	s.changed(c.HouseholdID)
	return cause
}

// Run syncs due connections until ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		due, err := db.New(s.DB).ListDueConnections(ctx, nullInt(s.Now().Unix()))
		if err != nil && ctx.Err() == nil {
			s.Log.Error("listing due connections", "err", err)
		}
		for _, c := range due {
			if err := s.Sync(ctx, c.ID); err != nil {
				s.Log.Warn("scheduled sync failed", "connection", c.ID, "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// run holds the state of one sync application inside a transaction.
type run struct {
	s           *Service
	q           *db.Queries
	conn        db.Connection
	now         time.Time
	windowStart string
	txnCount    int
	household   []db.Account
	claimed     map[int64]bool // accounts already matched in this run
	fetched     map[string]bool
	cat         *categorize.Categorizer
}

func (r *run) event(ctx context.Context, kind string, accountID int64, msg string) {
	p := db.InsertSyncEventParams{ConnectionID: r.conn.ID, At: r.now.Unix(), Kind: kind, Message: msg}
	if accountID != 0 {
		p.AccountID = nullInt(accountID)
	}
	r.q.InsertSyncEvent(ctx, p)
}

func (r *run) apply(ctx context.Context, set *simplefin.AccountSet) error {
	var err error
	r.household, err = r.q.ListAccounts(ctx, r.conn.HouseholdID)
	if err != nil {
		return err
	}
	r.claimed = map[int64]bool{}
	r.fetched = map[string]bool{}
	for _, a := range set.Accounts {
		r.fetched[a.ID] = true
	}

	connErrs := set.ConnErrors()
	insts := map[string]db.Institution{}
	for _, sc := range set.Connections {
		inst, err := r.q.UpsertInstitution(ctx, db.UpsertInstitutionParams{
			ConnectionID: r.conn.ID, ExternalID: sc.ConnID, Name: sc.Name, Url: sc.OrgURL,
		})
		if err != nil {
			return err
		}
		status, msg := "ok", ""
		if e, bad := connErrs[sc.ConnID]; bad {
			status, msg = "reauth", e.Msg
			if inst.Status != "reauth" {
				r.event(ctx, "reauth", 0, sc.Name+": "+e.Msg)
			}
		}
		if err := r.q.SetInstitutionStatus(ctx, db.SetInstitutionStatusParams{Status: status, LastError: msg, ID: inst.ID}); err != nil {
			return err
		}
		inst.Status = status
		insts[sc.ConnID] = inst
	}

	today := r.now.Format(time.DateOnly)
	for _, sa := range set.Accounts {
		inst := insts[sa.ConnID]
		acct, relinked, err := r.reconcile(ctx, sa, inst)
		if err != nil {
			return fmt.Errorf("account %s: %w", sa.Name, err)
		}
		if acct.Status == "ignored" {
			continue
		}
		if err := r.q.UpsertBalanceSnapshot(ctx, db.UpsertBalanceSnapshotParams{AccountID: acct.ID, Date: today, BalanceCents: acct.BalanceCents}); err != nil {
			return err
		}
		if err := r.transactions(ctx, acct, sa.Transactions, relinked); err != nil {
			return fmt.Errorf("account %s transactions: %w", sa.Name, err)
		}
	}

	// Accounts this connection used to return but didn't this time.
	for _, a := range r.household {
		if !a.ConnectionID.Valid || a.ConnectionID.Int64 != r.conn.ID || r.claimed[a.ID] {
			continue
		}
		if a.Status != "active" || !a.ExternalID.Valid || r.fetched[a.ExternalID.String] {
			continue
		}
		if r.institutionNeedsReauth(a, insts) {
			continue
		}
		if err := r.q.SetAccountStatus(ctx, db.SetAccountStatusParams{Status: "disconnected", UpdatedAt: r.now.Unix(), ID: a.ID}); err != nil {
			return err
		}
		r.event(ctx, "account_disconnected", a.ID, a.Name+" was not shared in the latest sync")
	}
	return nil
}

func (r *run) institutionNeedsReauth(a db.Account, insts map[string]db.Institution) bool {
	for _, inst := range insts {
		if a.InstitutionID.Valid && inst.ID == a.InstitutionID.Int64 && inst.Status == "reauth" {
			return true
		}
	}
	return false
}

func parseOpt(s string) sql.NullInt64 {
	if strings.TrimSpace(s) == "" {
		return sql.NullInt64{}
	}
	v, err := money.ParseCents(s)
	if err != nil {
		return sql.NullInt64{}
	}
	return nullInt(v)
}

// reconcile finds or creates the local account for a SimpleFIN account.
// relinked is true when an existing account was re-pointed at a new provider id.
func (r *run) reconcile(ctx context.Context, sa simplefin.Account, inst db.Institution) (db.Account, bool, error) {
	bal, err := money.ParseCents(sa.Balance)
	if err != nil {
		return db.Account{}, false, err
	}
	avail := parseOpt(sa.AvailableBalance)
	balAt := nullInt(sa.BalanceDate)
	instID := sql.NullInt64{}
	if inst.ID != 0 {
		instID = nullInt(inst.ID)
	}
	currency := sa.Currency
	if currency == "" || len(currency) > 3 {
		currency = "USD"
	}
	update := func(id int64) error {
		return r.q.UpdateAccountFromSync(ctx, db.UpdateAccountFromSyncParams{
			InstitutionID: instID, InstitutionName: inst.Name, ProviderName: sa.Name, Currency: currency,
			BalanceCents: bal, AvailableCents: avail, BalanceAt: balAt, UpdatedAt: r.now.Unix(), ID: id,
		})
	}

	if a, err := r.q.GetAccountByExternal(ctx, db.GetAccountByExternalParams{ConnectionID: nullInt(r.conn.ID), ExternalID: nullStr(sa.ID)}); err == nil {
		r.claimed[a.ID] = true
		if err := update(a.ID); err != nil {
			return a, false, err
		}
		if a.Status == "disconnected" {
			a.Status = "active"
		}
		a.BalanceCents = bal
		return a, false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return db.Account{}, false, err
	}

	mask := accounts.Mask(sa.Name)
	cands := r.candidates(inst.Name, sa.Name, mask)
	if len(cands) == 1 {
		a := cands[0]
		r.claimed[a.ID] = true
		if err := r.q.RelinkAccount(ctx, db.RelinkAccountParams{
			ConnectionID: nullInt(r.conn.ID), InstitutionID: instID, ExternalID: nullStr(sa.ID),
			InstitutionName: inst.Name, ProviderName: sa.Name, UpdatedAt: r.now.Unix(), ID: a.ID,
		}); err != nil {
			return a, false, err
		}
		if err := update(a.ID); err != nil {
			return a, false, err
		}
		r.event(ctx, "account_relinked", a.ID, fmt.Sprintf("%s reconnected to existing account %q", sa.Name, a.Name))
		a.Status, a.BalanceCents = "active", bal
		return a, true, nil
	}

	status, reviewID, kind := "active", sql.NullInt64{}, "account_new"
	if len(cands) > 1 {
		status, reviewID, kind = "review", nullInt(cands[0].ID), "account_review"
	}
	a, err := r.q.CreateAccount(ctx, db.CreateAccountParams{
		HouseholdID: r.conn.HouseholdID, ConnectionID: nullInt(r.conn.ID), InstitutionID: instID,
		ExternalID: nullStr(sa.ID), InstitutionName: inst.Name, ProviderName: sa.Name, Name: sa.Name,
		Mask: mask, Type: accounts.InferType(sa.Name, bal), Currency: currency, BalanceCents: bal,
		AvailableCents: avail, BalanceAt: balAt, Status: status, ReviewCandidateID: reviewID,
		CreatedAt: r.now.Unix(), UpdatedAt: r.now.Unix(),
	})
	if err != nil {
		return a, false, err
	}
	r.claimed[a.ID] = true
	r.household = append(r.household, a)
	msg := "new account " + sa.Name
	if kind == "account_review" {
		msg = fmt.Sprintf("%s may be an existing account (%d possible matches); needs review", sa.Name, len(cands))
	}
	r.event(ctx, kind, a.ID, msg)
	return a, false, nil
}

// candidates returns existing accounts a newly seen provider account may be a reconnection of.
func (r *run) candidates(instName, name, mask string) []db.Account {
	var out []db.Account
	ni, nn := accounts.Normalize(instName), accounts.Normalize(name)
	for _, a := range r.household {
		if r.claimed[a.ID] || a.IsManual == 1 {
			continue
		}
		switch a.Status {
		case "ignored", "closed", "review":
			continue
		}
		if accounts.Normalize(a.InstitutionName) != ni {
			continue
		}
		if mask != "" || a.Mask != "" {
			if a.Mask != mask {
				continue
			}
		} else if accounts.Normalize(a.ProviderName) != nn {
			continue
		}
		free := a.Status == "disconnected" ||
			!a.ConnectionID.Valid || a.ConnectionID.Int64 != r.conn.ID ||
			!a.ExternalID.Valid || !r.fetched[a.ExternalID.String]
		if free {
			out = append(out, a)
		}
	}
	return out
}

// transactions upserts an account's transactions. A transaction with an unseen id may
// "adopt" an existing row instead of inserting: a pending row that has since posted under a
// new id, or (after a relink) the same transaction under the account's new provider ids.
// Adopting keeps the user's category, notes and links.
func (r *run) transactions(ctx context.Context, acct db.Account, txns []simplefin.Transaction, relinked bool) error {
	existing, err := r.q.ListAccountTransactionKeys(ctx, acct.ID)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, t := range txns {
		seen[t.ID] = true
	}
	// Rows that could be adopted: synced rows whose id is absent from this fetch.
	var pool []db.ListAccountTransactionKeysRow
	for _, e := range existing {
		if e.ExternalID.Valid && !seen[e.ExternalID.String] && e.Date >= r.windowStart {
			pool = append(pool, e)
		}
	}
	nowUnix := r.now.Unix()
	for _, t := range txns {
		amt, err := money.ParseCents(t.Amount)
		if err != nil {
			return err
		}
		ts := t.TransactedAt
		if ts == 0 {
			ts = t.Posted
		}
		date := time.Unix(ts, 0).In(time.Local).Format(time.DateOnly)
		pending := int64(0)
		if t.Pending || t.Posted == 0 {
			pending = 1
		}
		r.txnCount++

		id := int64(0)
		if row, err := r.q.GetTransactionByExternal(ctx, db.GetTransactionByExternalParams{AccountID: acct.ID, ExternalID: nullStr(t.ID)}); err == nil {
			id = row.ID
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		} else if i := adoptable(pool, amt, date, relinked); i >= 0 {
			id = pool[i].ID
			pool = append(pool[:i], pool[i+1:]...)
			if err := r.q.SetTransactionExternal(ctx, db.SetTransactionExternalParams{ExternalID: nullStr(t.ID), ID: id}); err != nil {
				return err
			}
		}
		if id != 0 {
			if err := r.q.UpdateSyncedTransaction(ctx, db.UpdateSyncedTransactionParams{
				Date: date, AmountCents: amt, Description: t.Description, Payee: t.Payee, Memo: t.Memo,
				Pending: pending, UpdatedAt: nowUnix, ID: id,
			}); err != nil {
				return err
			}
			continue
		}
		id, err = r.q.InsertSyncedTransaction(ctx, db.InsertSyncedTransactionParams{
			HouseholdID: acct.HouseholdID, AccountID: acct.ID, ExternalID: nullStr(t.ID), Source: source,
			Date: date, AmountCents: amt, Description: t.Description, Payee: t.Payee, Memo: t.Memo,
			Pending: pending, CreatedAt: nowUnix, UpdatedAt: nowUnix,
		})
		if err != nil {
			return err
		}
		if err := r.classify(ctx, acct, id, date, amt, t); err != nil {
			return err
		}
	}

	// Pending rows that vanished were replaced by a posted version (or dropped by the bank).
	stale, err := r.q.ListPendingSynced(ctx, db.ListPendingSyncedParams{AccountID: acct.ID, Source: source, Date: r.windowStart})
	if err != nil {
		return err
	}
	for _, p := range stale {
		if p.ExternalID.Valid && !seen[p.ExternalID.String] {
			if err := r.q.DeleteTransaction(ctx, p.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// classify runs the categorization pipeline on a new transaction, then links it to a matching
// pending entry, whose category (if any) takes precedence.
func (r *run) classify(ctx context.Context, acct db.Account, id int64, date string, amt int64, t simplefin.Transaction) error {
	if r.cat == nil {
		c, err := categorize.New(ctx, r.q, acct.HouseholdID)
		if err != nil {
			return err
		}
		r.cat = c
	}
	res, err := r.cat.Apply(ctx, categorize.Txn{
		ID: id, HouseholdID: acct.HouseholdID, AccountID: acct.ID, AmountCents: amt, Date: date, Description: t.Description, Payee: t.Payee,
	})
	if err != nil {
		return err
	}
	_, err = linking.LinkPosted(ctx, r.q, linking.Posted{
		ID: id, AccountID: acct.ID, Date: date, AmountCents: amt, Description: t.Description, Payee: t.Payee, Merchant: res.Merchant,
	}, r.now)
	return err
}

// adoptable picks the pool row a new transaction replaces, or -1.
func adoptable(pool []db.ListAccountTransactionKeysRow, amt int64, date string, relinked bool) int {
	best, bestDiff := -1, 99
	for i, p := range pool {
		if p.AmountCents != amt {
			continue
		}
		limit := 0
		switch {
		case p.Pending == 1:
			limit = 5
		case relinked:
			limit = 2
		default:
			continue
		}
		d := dayDiff(p.Date, date)
		if d <= limit && d < bestDiff {
			best, bestDiff = i, d
		}
	}
	return best
}

func dayDiff(a, b string) int {
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

func nullInt(v int64) sql.NullInt64   { return sql.NullInt64{Int64: v, Valid: true} }
func nullStr(v string) sql.NullString { return sql.NullString{String: v, Valid: true} }
