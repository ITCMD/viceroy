package email

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"viceroy/internal/accounts"
	"viceroy/internal/categorize"
	"viceroy/internal/db"
	"viceroy/internal/linking"
	"viceroy/internal/secrets"
)

// Message statuses.
const (
	StatusUnrouted    = "unrouted"
	StatusParsed      = "parsed"
	StatusParseFailed = "parse_failed"
	StatusIgnored     = "ignored"
)

// BodyRetention is how long message bodies are kept (for previews and re-routing).
const BodyRetention = 90 * 24 * time.Hour

type Service struct {
	DB  *sql.DB
	Box *secrets.Box
	Log *slog.Logger
	Now func() time.Time
	// Poll is the fallback check interval while IDLE is quiet (or unsupported).
	Poll time.Duration
	// Changed, when set, is called after routing may have created transactions.
	Changed func(householdID int64)
	// AI, when set, reads unrouted emails from mailboxes that allow it (see airead.go), and
	// Notice receives what it found.
	AI     AIReader
	Notice func(ctx context.Context, householdID int64, n Notice)

	mu       sync.Mutex
	watchers map[int64]*watcher
	refreshC chan int64
	aiC      chan struct{}
}

func New(conn *sql.DB, box *secrets.Box, log *slog.Logger) *Service {
	return &Service{DB: conn, Box: box, Log: log, Now: time.Now, Poll: 5 * time.Minute}
}

// Ingest stores one raw message and routes it. A message already seen (same Message-ID in the
// household) is skipped and returns 0.
func (s *Service) Ingest(ctx context.Context, householdID, mailboxID int64, uid uint32, raw []byte) (int64, error) {
	m, err := ParseMessage(raw)
	if err != nil {
		return 0, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	q := db.New(tx)
	received := m.Date
	if received.IsZero() {
		received = s.Now()
	}
	id, err := q.InsertEmailMessage(ctx, db.InsertEmailMessageParams{
		HouseholdID: householdID, MailboxID: sql.NullInt64{Int64: mailboxID, Valid: mailboxID != 0},
		MessageID: m.MessageID, Uid: int64(uid), FromAddr: m.FromAddr, FromName: m.FromName, Subject: m.Subject,
		ReceivedAt: received.Unix(), BodyText: m.Text, CreatedAt: s.Now().Unix(),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil // duplicate
	}
	if err != nil {
		return 0, err
	}
	filters, err := q.ListEmailFilters(ctx, householdID)
	if err != nil {
		return 0, err
	}
	row, err := q.GetEmailMessage(ctx, db.GetEmailMessageParams{ID: id, HouseholdID: householdID})
	if err != nil {
		return 0, err
	}
	if err := s.route(ctx, q, row, filters); err != nil {
		return 0, err
	}
	return id, s.commit(tx, householdID)
}

// commit commits tx and reports the change.
func (s *Service) commit(tx *sql.Tx, householdID int64) error {
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.Changed != nil {
		s.Changed(householdID)
	}
	s.wakeAI()
	return nil
}

// Reroute runs open messages (unrouted or parse_failed, newest 500) through the filters again,
// e.g. after a filter was added or fixed. It returns how many became transactions.
func (s *Service) Reroute(ctx context.Context, householdID int64) (int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	q := db.New(tx)
	filters, err := q.ListEmailFilters(ctx, householdID)
	if err != nil {
		return 0, err
	}
	msgs, err := q.ListRecentEmailBodies(ctx, db.ListRecentEmailBodiesParams{HouseholdID: householdID, OnlyOpen: 1, Lim: 500})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range msgs {
		if err := s.route(ctx, q, m, filters); err != nil {
			return 0, err
		}
		if after, err := q.GetEmailMessage(ctx, db.GetEmailMessageParams{ID: m.ID, HouseholdID: householdID}); err == nil && after.Status == StatusParsed {
			n++
		}
	}
	return n, s.commit(tx, householdID)
}

// Retry routes one message again (the user fixed its filter, or un-ignored it).
func (s *Service) Retry(ctx context.Context, householdID, id int64) (db.EmailMessage, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return db.EmailMessage{}, err
	}
	defer tx.Rollback()
	q := db.New(tx)
	m, err := q.GetEmailMessage(ctx, db.GetEmailMessageParams{ID: id, HouseholdID: householdID})
	if err != nil {
		return m, err
	}
	if m.Status == StatusParsed && m.TransactionID.Valid {
		return m, nil
	}
	filters, err := q.ListEmailFilters(ctx, householdID)
	if err != nil {
		return m, err
	}
	if err := s.route(ctx, q, m, filters); err != nil {
		return m, err
	}
	if m, err = q.GetEmailMessage(ctx, db.GetEmailMessageParams{ID: id, HouseholdID: householdID}); err != nil {
		return m, err
	}
	return m, s.commit(tx, householdID)
}

// FromRow rebuilds the parse input from a stored message.
func FromRow(m db.EmailMessage) Message {
	return Message{
		MessageID: m.MessageID, FromAddr: m.FromAddr, FromName: m.FromName, Subject: m.Subject,
		Date: time.Unix(m.ReceivedAt, 0), Text: m.BodyText,
	}
}

// FilterOf converts a stored filter to its matcher.
func FilterOf(f db.EmailFilter) Filter {
	return Filter{Sender: f.Sender, Subject: f.SubjectMatch, Body: f.BodyMatch, Regex: f.UseRegex == 1}
}

// Match returns the first enabled filter that matches m.
func Match(filters []db.EmailFilter, m Message) (db.EmailFilter, bool) {
	for _, f := range filters {
		if f.Enabled == 1 && FilterOf(f).Matches(m) {
			return f, true
		}
	}
	return db.EmailFilter{}, false
}

// route matches a stored message and, on a successful parse, creates its transaction.
func (s *Service) route(ctx context.Context, q *db.Queries, row db.EmailMessage, filters []db.EmailFilter) error {
	m := FromRow(row)
	f, ok := Match(filters, m)
	if !ok {
		if err := q.SetEmailMessageResult(ctx, db.SetEmailMessageResultParams{Status: StatusUnrouted, ID: row.ID}); err != nil {
			return err
		}
		return s.queueAI(ctx, q, row)
	}
	filterID := sql.NullInt64{Int64: f.ID, Valid: true}
	p, err := Parse(f.Parser, f.CustomParser, m)
	if err != nil {
		return q.SetEmailMessageResult(ctx, db.SetEmailMessageResultParams{Status: StatusParseFailed, FilterID: filterID, Error: err.Error(), ID: row.ID})
	}
	txnID, err := s.createTransaction(ctx, q, row.HouseholdID, f, p)
	if errors.Is(err, sql.ErrNoRows) {
		return q.SetEmailMessageResult(ctx, db.SetEmailMessageResultParams{Status: StatusParseFailed, FilterID: filterID, Error: "the filter's account no longer exists", ID: row.ID})
	}
	if err != nil {
		return err
	}
	return q.SetEmailMessageResult(ctx, db.SetEmailMessageResultParams{
		Status: StatusParsed, FilterID: filterID, TransactionID: sql.NullInt64{Int64: txnID, Valid: true}, ID: row.ID,
	})
}

// createTransaction books a parsed alert. On an account that also syncs from the bank it is a
// provisional stand-in that links to the posted transaction (now, if that already arrived, or
// when it does); on an email-only (manual) account it's final and moves the running balance.
func (s *Service) createTransaction(ctx context.Context, q *db.Queries, householdID int64, f db.EmailFilter, p Parsed) (int64, error) {
	acct, err := q.GetAccount(ctx, db.GetAccountParams{ID: f.AccountID, HouseholdID: householdID})
	if err != nil {
		return 0, err
	}
	amt := -p.AmountCents
	if f.Sign == "credit" {
		amt = p.AmountCents
	}
	provisional := int64(0)
	if acct.IsManual == 0 {
		provisional = 1
	}
	now := s.Now()
	id, err := q.InsertEmailTransaction(ctx, db.InsertEmailTransactionParams{
		HouseholdID: householdID, AccountID: acct.ID, Date: p.Date, AmountCents: amt,
		Description: p.Merchant, Payee: p.Merchant, Pending: provisional, Provisional: provisional,
		CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	})
	if err != nil {
		return 0, err
	}
	cat, err := categorize.New(ctx, q, householdID)
	if err != nil {
		return 0, err
	}
	res, err := cat.Apply(ctx, categorize.Txn{ID: id, HouseholdID: householdID, AccountID: acct.ID, AmountCents: amt, Description: p.Merchant, Payee: p.Merchant})
	if err != nil {
		return 0, err
	}
	if provisional == 1 {
		_, err = linking.LinkProvisional(ctx, q, linking.Provisional{
			ID: id, AccountID: acct.ID, Date: p.Date, AmountCents: amt, Description: p.Merchant, Merchant: res.Merchant,
		}, now)
		return id, err
	}
	return id, accounts.AdjustManualBalance(ctx, q, acct.ID, amt)
}
