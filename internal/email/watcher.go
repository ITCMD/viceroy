package email

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"viceroy/internal/db"
)

// FirstFetchWindow is how far back the first check of a folder reads, so existing alerts can
// be used to build filters.
const FirstFetchWindow = 30 * 24 * time.Hour

const fetchBatch = 50

// Conn holds what's needed to reach a mailbox.
type Conn struct {
	Host, Security, Username, Password, Folder string
	Port                                       int
}

func (c Conn) addr() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }

// Dial connects, logs in and opens the folder read-only (EXAMINE), so nothing is ever marked,
// moved or deleted.
func Dial(c Conn, opts *imapclient.Options) (*imapclient.Client, *imap.SelectData, error) {
	if opts == nil {
		opts = &imapclient.Options{}
	}
	opts.TLSConfig = &tls.Config{ServerName: c.Host}
	opts.Dialer = &net.Dialer{Timeout: 20 * time.Second}
	var (
		cl  *imapclient.Client
		err error
	)
	switch c.Security {
	case "starttls":
		cl, err = imapclient.DialStartTLS(c.addr(), opts)
	case "none":
		cl, err = imapclient.DialInsecure(c.addr(), opts)
	default:
		cl, err = imapclient.DialTLS(c.addr(), opts)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to %s: %w", c.addr(), err)
	}
	if err := cl.Login(c.Username, c.Password).Wait(); err != nil {
		cl.Close()
		return nil, nil, fmt.Errorf("login failed: %w", err)
	}
	sel, err := cl.Select(c.Folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		cl.Close()
		return nil, nil, fmt.Errorf("opening folder %q: %w", c.Folder, err)
	}
	return cl, sel, nil
}

// Test checks that a mailbox can be reached and returns the folder's message count.
func Test(c Conn) (uint32, error) {
	cl, sel, err := Dial(c, nil)
	if err != nil {
		return 0, err
	}
	defer cl.Close()
	cl.Logout().Wait()
	return sel.NumMessages, nil
}

type watcher struct {
	cancel  context.CancelFunc
	poke    chan struct{}
	version string // mailbox settings the watcher started with
}

func (w *watcher) wake() {
	select {
	case w.poke <- struct{}{}:
	default:
	}
}

func mailboxVersion(m db.EmailMailbox) string {
	return fmt.Sprint(m.Host, m.Port, m.Security, m.Username, m.Folder, string(m.PasswordEnc))
}

// Run keeps one watcher per enabled mailbox until ctx is cancelled, restarting watchers whose
// settings changed, and prunes old message bodies daily.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	lastPrune := time.Time{}
	for {
		s.reconcile(ctx)
		if s.Now().Sub(lastPrune) > 24*time.Hour {
			lastPrune = s.Now()
			if err := db.New(s.DB).PruneEmailBodies(ctx, s.Now().Add(-BodyRetention).Unix()); err != nil && ctx.Err() == nil {
				s.Log.Warn("pruning email bodies", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			s.mu.Lock()
			for id, w := range s.watchers {
				w.cancel()
				delete(s.watchers, id)
			}
			s.mu.Unlock()
			return
		case <-t.C:
		case id := <-s.refresh():
			s.reconcile(ctx)
			s.mu.Lock()
			if w := s.watchers[id]; w != nil {
				w.wake()
			}
			s.mu.Unlock()
		}
	}
}

func (s *Service) refresh() chan int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refreshC == nil {
		s.refreshC = make(chan int64, 8)
	}
	return s.refreshC
}

// Refresh makes Run pick up mailbox changes now and check mailbox id (0 = none) right away.
// It never blocks; without a running Run it does nothing.
func (s *Service) Refresh(id int64) {
	select {
	case s.refresh() <- id:
	default:
	}
}

func (s *Service) reconcile(ctx context.Context) {
	boxes, err := db.New(s.DB).ListEnabledMailboxes(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.Log.Error("listing mailboxes", "err", err)
		}
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.watchers == nil {
		s.watchers = map[int64]*watcher{}
	}
	want := map[int64]db.EmailMailbox{}
	for _, b := range boxes {
		want[b.ID] = b
	}
	for id, w := range s.watchers {
		if b, ok := want[id]; !ok || mailboxVersion(b) != w.version {
			w.cancel()
			delete(s.watchers, id)
		}
	}
	if ctx.Err() != nil {
		return
	}
	for id, b := range want {
		if s.watchers[id] != nil {
			continue
		}
		wctx, cancel := context.WithCancel(ctx)
		w := &watcher{cancel: cancel, poke: make(chan struct{}, 1), version: mailboxVersion(b)}
		s.watchers[id] = w
		go s.watch(wctx, id, w)
	}
}

// watch keeps a session open to one mailbox, reconnecting with backoff after errors.
func (s *Service) watch(ctx context.Context, id int64, w *watcher) {
	backoff := time.Minute
	for ctx.Err() == nil {
		err := s.session(ctx, id, w)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.Log.Warn("email mailbox error", "mailbox", id, "err", err)
			db.New(s.DB).SetMailboxStatus(context.Background(), db.SetMailboxStatusParams{
				Status: "error", LastError: err.Error(), LastCheckedAt: nullInt(s.Now().Unix()), ID: id,
			})
		}
		select {
		case <-ctx.Done():
			return
		case <-w.poke:
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 15*time.Minute)
	}
}

func (s *Service) conn(m db.EmailMailbox) (Conn, error) {
	pw, err := s.Box.Open(m.PasswordEnc)
	if err != nil {
		return Conn{}, errors.New("stored password can't be decrypted; re-enter it")
	}
	return Conn{Host: m.Host, Port: int(m.Port), Security: m.Security, Username: m.Username, Password: pw, Folder: m.Folder}, nil
}

// session connects, catches up, then waits for new mail (IDLE, or polling) until an error.
func (s *Service) session(ctx context.Context, id int64, w *watcher) error {
	q := db.New(s.DB)
	m, err := q.GetMailboxByID(ctx, id)
	if err != nil {
		return err
	}
	c, err := s.conn(m)
	if err != nil {
		return err
	}
	newMail := make(chan struct{}, 1)
	cl, sel, err := Dial(c, &imapclient.Options{UnilateralDataHandler: &imapclient.UnilateralDataHandler{
		Mailbox: func(d *imapclient.UnilateralDataMailbox) {
			if d.NumMessages != nil {
				select {
				case newMail <- struct{}{}:
				default:
				}
			}
		},
	}})
	if err != nil {
		return err
	}
	defer cl.Close()
	go func() {
		<-ctx.Done()
		cl.Close()
	}()

	canIdle := cl.Caps().Has(imap.CapIdle)
	for {
		if err := s.catchUp(ctx, cl, m, sel.UIDValidity); err != nil {
			return err
		}
		q.SetMailboxStatus(ctx, db.SetMailboxStatusParams{Status: "ok", LastCheckedAt: nullInt(s.Now().Unix()), ID: id})
		if m, err = q.GetMailboxByID(ctx, id); err != nil {
			return err
		}

		timer := time.NewTimer(s.Poll)
		var idle *imapclient.IdleCommand
		if canIdle {
			if idle, err = cl.Idle(); err != nil {
				timer.Stop()
				return err
			}
		}
		select {
		case <-ctx.Done():
		case <-newMail:
		case <-w.poke:
		case <-timer.C:
		case <-cl.Closed():
		}
		timer.Stop()
		if idle != nil {
			if err := idle.Close(); err != nil && ctx.Err() == nil {
				return err
			}
			if err := idle.Wait(); err != nil && ctx.Err() == nil {
				return err
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		if !canIdle {
			if err := cl.Noop().Wait(); err != nil {
				return err
			}
		}
	}
}

// catchUp fetches and ingests messages newer than the stored cursor. A new or changed
// UIDVALIDITY restarts from the last FirstFetchWindow; Message-ID dedupe prevents repeats.
func (s *Service) catchUp(ctx context.Context, cl *imapclient.Client, m db.EmailMailbox, uidValidity uint32) error {
	q := db.New(s.DB)
	last := imap.UID(m.LastUid)
	crit := &imap.SearchCriteria{}
	if int64(uidValidity) != m.UidValidity {
		last = 0
		crit.Since = s.Now().Add(-FirstFetchWindow)
	} else {
		crit.UID = []imap.UIDSet{{imap.UIDRange{Start: last + 1, Stop: 0}}}
	}
	res, err := cl.UIDSearch(crit, nil).Wait()
	if err != nil {
		return fmt.Errorf("searching folder: %w", err)
	}
	var uids []imap.UID
	for _, u := range res.AllUIDs() {
		if u > last { // "N:*" always returns the newest message, even when older than N
			uids = append(uids, u)
		}
	}
	if len(uids) == 0 && int64(uidValidity) != m.UidValidity {
		return q.SetMailboxCursor(ctx, db.SetMailboxCursorParams{UidValidity: int64(uidValidity), LastUid: 0, ID: m.ID})
	}
	body := &imap.FetchItemBodySection{Peek: true}
	for start := 0; start < len(uids); start += fetchBatch {
		batch := uids[start:min(start+fetchBatch, len(uids))]
		msgs, err := cl.Fetch(imap.UIDSetNum(batch...), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{body}}).Collect()
		if err != nil {
			return fmt.Errorf("fetching messages: %w", err)
		}
		for _, msg := range msgs {
			raw := msg.FindBodySection(body)
			if raw == nil {
				continue
			}
			if _, err := s.Ingest(ctx, m.HouseholdID, m.ID, uint32(msg.UID), raw); err != nil {
				s.Log.Warn("skipping unreadable email", "mailbox", m.ID, "uid", msg.UID, "err", err)
			}
			if msg.UID > last {
				last = msg.UID
			}
		}
		if err := q.SetMailboxCursor(ctx, db.SetMailboxCursorParams{UidValidity: int64(uidValidity), LastUid: int64(last), ID: m.ID}); err != nil {
			return err
		}
	}
	return nil
}

func nullInt(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }
