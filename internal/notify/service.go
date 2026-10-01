package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"viceroy/internal/bills"
	"viceroy/internal/budget"
	"viceroy/internal/budgetview"
	"viceroy/internal/db"
)

// Keys is the server's VAPID key pair (base64url, as webpush-go generates them).
type Keys struct {
	Public  string `json:"public"`
	Private string `json:"private"`
}

// LoadOrCreateKeys reads the VAPID keys from path, generating them on first boot.
func LoadOrCreateKeys(path string) (Keys, error) {
	var k Keys
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if k.Private, k.Public, err = webpush.GenerateVAPIDKeys(); err != nil {
			return k, err
		}
		b, _ = json.Marshal(k)
		return k, os.WriteFile(path, b, 0o600)
	}
	if err != nil {
		return k, err
	}
	if err := json.Unmarshal(b, &k); err != nil || k.Public == "" || k.Private == "" {
		return k, fmt.Errorf("%s: invalid VAPID key file", path)
	}
	return k, nil
}

// Payload is what the service worker receives.
type Payload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
}

type Service struct {
	DB   *sql.DB
	Log  *slog.Logger
	Now  func() time.Time
	Keys Keys
	// Subject is the VAPID contact (an https URL or mailto:). Empty = the recipient's email.
	Subject string
	// Push delivers one message; it returns the push service's HTTP status. Tests replace it.
	Push func(ctx context.Context, sub db.PushSubscription, subject string, payload []byte) (int, error)
	// Debounce delays evaluation after a change so a burst of transactions is checked once.
	Debounce time.Duration

	once     sync.Once
	triggerC chan int64
}

func New(conn *sql.DB, log *slog.Logger, keys Keys, subject string) *Service {
	s := &Service{DB: conn, Log: log, Now: time.Now, Keys: keys, Subject: subject, Debounce: 3 * time.Second}
	s.Push = s.webPush
	return s
}

func (s *Service) triggers() chan int64 {
	s.once.Do(func() { s.triggerC = make(chan int64, 64) })
	return s.triggerC
}

// Changed asks for the household to be evaluated soon. It never blocks.
func (s *Service) Changed(householdID int64) {
	select {
	case s.triggers() <- householdID:
	default:
	}
}

// Run evaluates households after changes (debounced) and every hour, and prunes old
// notifications daily, until ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	pending := map[int64]bool{}
	var fire <-chan time.Time
	lastPrune := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case hh := <-s.triggers():
			pending[hh] = true
			if fire == nil {
				fire = time.After(s.Debounce)
			}
		case <-fire:
			fire = nil
			for hh := range pending {
				s.evaluate(ctx, hh)
			}
			clear(pending)
		case <-tick.C:
			ids, err := db.New(s.DB).ListHouseholdIDs(ctx)
			if err != nil {
				s.Log.Warn("notify: listing households", "err", err)
				continue
			}
			for _, hh := range ids {
				s.evaluate(ctx, hh)
			}
			if s.Now().Sub(lastPrune) > 24*time.Hour {
				lastPrune = s.Now()
				db.New(s.DB).PruneNotifications(ctx, s.Now().AddDate(0, 0, -90).Unix())
			}
		}
	}
}

func (s *Service) evaluate(ctx context.Context, hh int64) {
	if err := s.Evaluate(ctx, hh); err != nil && ctx.Err() == nil {
		s.Log.Warn("notify: evaluating", "household", hh, "err", err)
	}
}

// LoadPrefs returns the user's prefs, or the defaults.
func LoadPrefs(ctx context.Context, q *db.Queries, userID int64) (Prefs, error) {
	p := DefaultPrefs
	raw, err := q.GetNotificationPrefs(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if json.Unmarshal([]byte(raw), &p) != nil || p.Validate() != nil {
		return DefaultPrefs, nil
	}
	return p, nil
}

// largeTxnWindow limits large-transaction alerts to transactions that are both new and recent,
// so a first sync that imports months of history stays quiet.
const largeTxnWindow = 3 * 24 * time.Hour

// Evaluate checks the household's data against each member's prefs and sends new alerts.
func (s *Service) Evaluate(ctx context.Context, hh int64) error {
	q := db.New(s.DB)
	users, err := q.ListHouseholdUsers(ctx, hh)
	if err != nil || len(users) == 0 {
		return err
	}
	now := s.Now()
	today := budget.Date(now.Year(), now.Month(), now.Day())
	var (
		view    *budgetview.View
		large   []db.ListLargeTransactionsRow
		broken  []db.ListBrokenAccountsRow
		dueSoon []Alert
		loaded  = map[string]bool{}
	)
	for _, u := range users {
		p, err := LoadPrefs(ctx, q, u.ID)
		if err != nil {
			return err
		}
		var alerts []Alert
		if p.OverBudget || p.Pacing {
			if !loaded["budget"] {
				loaded["budget"] = true
				v, err := budgetview.Build(ctx, q, hh, budget.ViewMonth, today, today)
				if err != nil {
					return err
				}
				view = &v
			}
			alerts = append(alerts, BudgetAlerts(*view, p)...)
		}
		if p.LargeTxn {
			if !loaded["large"] {
				loaded["large"] = true
				// Load at the smallest threshold any member could want; filter per user below.
				if large, err = q.ListLargeTransactions(ctx, db.ListLargeTransactionsParams{
					HouseholdID: hh, CreatedSince: now.Add(-largeTxnWindow).Unix(),
					FromDate: budget.FormatDate(today.Add(-largeTxnWindow)), MaxAmount: -1_00,
				}); err != nil {
					return err
				}
			}
			for _, t := range large {
				if -t.AmountCents >= p.LargeTxnCents {
					alerts = append(alerts, LargeTxnAlert(t))
				}
			}
		}
		if p.PaymentDue {
			if !loaded["bills"] {
				loaded["bills"] = true
				if dueSoon, err = s.dueSoon(ctx, q, hh, budget.FormatDate(today)); err != nil {
					return err
				}
			}
			alerts = append(alerts, dueSoon...)
		}
		if p.Disconnected {
			if !loaded["broken"] {
				loaded["broken"] = true
				if broken, err = q.ListBrokenAccounts(ctx, hh); err != nil {
					return err
				}
			}
			for _, a := range broken {
				alerts = append(alerts, BrokenAccountAlert(a))
			}
		}
		for _, a := range alerts {
			if _, _, err := s.Notify(ctx, u, a); err != nil {
				return err
			}
		}
	}
	return nil
}

// dueSoon lists reminders for bills due within DueSoonDays with no payment scheduled or made.
func (s *Service) dueSoon(ctx context.Context, q *db.Queries, hh int64, today string) ([]Alert, error) {
	rows, err := q.ListRecentBills(ctx, db.ListRecentBillsParams{HouseholdID: hh, CreatedAt: s.Now().Add(-bills.Lookback).Unix()})
	if err != nil {
		return nil, err
	}
	accts, err := q.ListAccounts(ctx, hh)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	for _, a := range accts {
		names[a.ID] = a.Name
	}
	var out []Alert
	for id, st := range bills.ByAccount(rows, today) {
		if st.Due == nil || st.Scheduled != nil || !st.Due.Date.Valid {
			continue
		}
		if days, ok := bills.DaysUntil(today, st.Due.Date.String); ok && days >= 0 && days <= DueSoonDays {
			out = append(out, DueSoonAlert(*st.Due, names[id], days))
		}
	}
	return out, nil
}

// NotifyHousehold sends an alert to every member whose prefs allow it.
func (s *Service) NotifyHousehold(ctx context.Context, hh int64, a Alert, allowed func(Prefs) bool) error {
	q := db.New(s.DB)
	users, err := q.ListHouseholdUsers(ctx, hh)
	if err != nil {
		return err
	}
	for _, u := range users {
		p, err := LoadPrefs(ctx, q, u.ID)
		if err != nil {
			return err
		}
		if allowed != nil && !allowed(p) {
			continue
		}
		if _, _, err := s.Notify(ctx, u, a); err != nil {
			return err
		}
	}
	return nil
}

// Notify stores the alert for the user and pushes it to their devices; sent counts the devices
// that accepted it. created is false (and nothing is sent) when the user already got an alert
// with the same key.
func (s *Service) Notify(ctx context.Context, u db.User, a Alert) (created bool, sent int, err error) {
	q := db.New(s.DB)
	n, err := q.InsertNotification(ctx, db.InsertNotificationParams{
		UserID: u.ID, Kind: a.Kind, DedupeKey: a.Key, Title: a.Title, Body: a.Body, Url: a.URL, CreatedAt: s.Now().Unix(),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	s.Log.Info("notification", "user", u.ID, "kind", a.Kind, "title", a.Title)
	sent, err = s.PushAll(ctx, u, Payload{Title: n.Title, Body: n.Body, URL: n.Url, Tag: a.Key})
	return true, sent, err
}

// PushAll sends the payload to every device of the user and returns how many accepted it.
// Subscriptions the push service reports as gone are deleted.
func (s *Service) PushAll(ctx context.Context, u db.User, p Payload) (int, error) {
	q := db.New(s.DB)
	subs, err := q.ListPushSubscriptions(ctx, u.ID)
	if err != nil {
		return 0, err
	}
	body, _ := json.Marshal(p)
	subject := s.Subject
	if subject == "" {
		subject = u.Email
	}
	sent := 0
	for _, sub := range subs {
		status, err := s.Push(ctx, sub, subject, body)
		switch {
		case err != nil:
			s.Log.Warn("push failed", "subscription", sub.ID, "err", err)
		case status == http.StatusNotFound || status == http.StatusGone:
			q.DeletePushEndpoint(ctx, sub.Endpoint)
		case status >= 400:
			s.Log.Warn("push rejected", "subscription", sub.ID, "status", status)
		default:
			sent++
		}
	}
	return sent, nil
}

func (s *Service) webPush(ctx context.Context, sub db.PushSubscription, subject string, payload []byte) (int, error) {
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
	}, &webpush.Options{
		Subscriber: strings.TrimPrefix(subject, "mailto:"), VAPIDPublicKey: s.Keys.Public, VAPIDPrivateKey: s.Keys.Private,
		TTL: 24 * 60 * 60, Urgency: webpush.UrgencyNormal,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	})
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}
