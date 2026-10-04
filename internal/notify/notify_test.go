package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"viceroy/internal/auth"
	"viceroy/internal/budget"
	"viceroy/internal/budgetview"
	"viceroy/internal/db"
)

func TestDollars(t *testing.T) {
	for in, want := range map[int64]string{0: "$0.00", 5: "$0.05", 123456: "$1,234.56", 100000000: "$1,000,000.00", -2500: "-$25.00"} {
		if got := Dollars(in); got != want {
			t.Errorf("Dollars(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestPaceDay(t *testing.T) {
	sept := budget.MonthPeriod(budget.Date(2026, 9, 1))
	for today, want := range map[string]string{
		"2026-09-01": "2026-09-07", // Tue: the first week runs to Sat 5th, but at least day 7
		"2026-09-08": "2026-09-12", // Tue → Sat
		"2026-09-13": "2026-09-19", // Sun starts a week
		"2026-09-29": "2026-09-30", // capped at month end
	} {
		d, _ := budget.ParseDate(today)
		if got := budget.FormatDate(budgetview.PaceDay(sept, d, time.Sunday)); got != want {
			t.Errorf("PaceDay(%s) = %s, want %s", today, got, want)
		}
	}
	if d := budgetview.PaceDay(sept, budget.Date(2026, 10, 1), time.Sunday); !d.IsZero() {
		t.Errorf("outside the period = %v", d)
	}
}

func TestBudgetAlerts(t *testing.T) {
	line := func(id int64, budget, actual, expected int64) budgetview.Line {
		return budgetview.Line{ID: id, Name: "Cat", Budget: budget, Actual: actual, WeekExpected: expected}
	}
	v := budgetview.View{Month: "2026-09", PaceThrough: "2026-09-12", Groups: []budgetview.Group{
		{Kind: "income", Lines: []budgetview.Line{line(1, 100_00, 500_00, 50_00)}}, // income never alerts
		{Kind: "flexible", Lines: []budgetview.Line{
			line(2, 300_00, 320_00, 200_00), // over
			line(3, 300_00, 200_00, 100_00), // 100% ahead of pace
			line(4, 300_00, 110_00, 100_00), // 10% ahead: under threshold
			line(5, 30_00, 12_00, 5_00),     // ahead by %, but only $7
			line(6, 0, 50_00, 0),            // unbudgeted
			line(7, 300_00, 250_00, 300_00), // period over (expected = budget): no pacing
		}},
	}}
	got := BudgetAlerts(v, DefaultPrefs)
	keys := []string{}
	for _, a := range got {
		keys = append(keys, a.Key)
	}
	if strings.Join(keys, ",") != "over:2:2026-09,pace:3:2026-09-12" {
		t.Fatalf("keys = %v", keys)
	}
	if got[0].Body != "$320.00 spent of $300.00 this month ($20.00 over)." {
		t.Errorf("over body = %q", got[0].Body)
	}
	if got[1].Body != "$200.00 spent so far; about $100.00 was planned through Sat, Sep 12 ($300.00 budget)." {
		t.Errorf("pace body = %q", got[1].Body)
	}
	p := DefaultPrefs
	p.OverBudget = false
	p.PacingPct = 5
	got = BudgetAlerts(v, p)
	keys = keys[:0]
	for _, a := range got {
		keys = append(keys, a.Key)
	}
	if strings.Join(keys, ",") != "pace:3:2026-09-12,pace:4:2026-09-12" {
		t.Fatalf("keys with pacing 5%% and no over alerts = %v", keys)
	}
}

type pushed struct {
	endpoint string
	payload  Payload
}

type env struct {
	t    *testing.T
	ctx  context.Context
	conn *sql.DB
	svc  *Service
	user db.User
	hh   int64
	acct int64
	sent []pushed
	now  time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	ctx := context.Background()
	u, err := auth.New(conn).Setup(ctx, auth.SetupInput{Name: "Ada", Email: "ada@example.com", Password: "correct horse battery"})
	if err != nil {
		t.Fatal(err)
	}
	h, err := db.New(conn).GetUserHousehold(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, ctx: ctx, conn: conn, user: u, hh: h.ID, now: time.Now()}
	e.svc = New(conn, slog.New(slog.DiscardHandler), Keys{Public: "pub", Private: "priv"}, "")
	e.svc.Now = func() time.Time { return e.now }
	e.svc.Push = func(_ context.Context, sub db.PushSubscription, subject string, payload []byte) (int, error) {
		if subject != "ada@example.com" {
			t.Errorf("subject = %q", subject)
		}
		var p Payload
		json.Unmarshal(payload, &p)
		e.sent = append(e.sent, pushed{sub.Endpoint, p})
		if strings.Contains(sub.Endpoint, "gone") {
			return 410, nil
		}
		return 201, nil
	}
	e.acct = e.exec(`INSERT INTO accounts (household_id, name, type, created_at, updated_at) VALUES (?, 'Checking', 'checking', 1, 1)`, e.hh)
	for _, ep := range []string{"https://push.example/ok", "https://push.example/gone"} {
		if _, err := db.New(conn).UpsertPushSubscription(ctx, db.UpsertPushSubscriptionParams{UserID: u.ID, Endpoint: ep, P256dh: "k", Auth: "a", CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func (e *env) exec(q string, args ...any) int64 {
	e.t.Helper()
	res, err := e.conn.Exec(q, args...)
	if err != nil {
		e.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (e *env) txn(source, date string, cents int64, category int64, desc string) int64 {
	cat := sql.NullInt64{Int64: category, Valid: category != 0}
	return e.exec(`INSERT INTO transactions (household_id, account_id, source, date, amount_cents, description, category_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, e.hh, e.acct, source, date, cents, desc, cat, e.now.Unix(), e.now.Unix())
}

func (e *env) category(name string) int64 {
	var id int64
	if err := e.conn.QueryRow(`SELECT id FROM categories WHERE household_id = ? AND name = ?`, e.hh, name).Scan(&id); err != nil {
		e.t.Fatalf("category %s: %v", name, err)
	}
	return id
}

func (e *env) titles() []string {
	out := []string{}
	for _, p := range e.sent {
		if strings.HasSuffix(p.endpoint, "/ok") {
			out = append(out, p.payload.Title)
		}
	}
	e.sent = nil
	return out
}

func TestEvaluate(t *testing.T) {
	e := newEnv(t)
	today := budget.FormatDate(budgetview.Today())
	groceries := e.category("Groceries")
	month := budget.MonthKey(budgetview.Today())
	e.exec(`INSERT INTO budget_amounts (household_id, category_id, month, amount_cents, forward) VALUES (?, ?, ?, 100_00, 1)`, e.hh, groceries, month)

	// A big grocery run: over budget and a large transaction (default threshold $500).
	e.txn("simplefin", today, -620_00, groceries, "WHOLE FOODS")
	// Entered by hand: no large-transaction alert.
	e.txn("manual", today, -900_00, 0, "Cash for car")
	// Old history imported by a first sync: too old to alert.
	e.txn("simplefin", budget.FormatDate(budgetview.Today().AddDate(0, 0, -20)), -700_00, 0, "OLD")

	if err := e.svc.Evaluate(e.ctx, e.hh); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(e.titles(), " | ")
	if got != "Over budget: Groceries | Large transaction: $620.00" {
		t.Fatalf("alerts = %q", got)
	}
	// The gone subscription was removed after its 410.
	subs, _ := db.New(e.conn).ListPushSubscriptions(e.ctx, e.user.ID)
	if len(subs) != 1 {
		t.Fatalf("subscriptions = %d, want 1", len(subs))
	}

	// Running again sends nothing: every alert is deduped.
	if err := e.svc.Evaluate(e.ctx, e.hh); err != nil {
		t.Fatal(err)
	}
	if got := e.titles(); len(got) != 0 {
		t.Fatalf("second run sent %v", got)
	}

	// An email alert that later links to its posted twin alerts once (on the email row).
	email := e.txn("email", today, -800_00, 0, "BEST BUY")
	e.svc.Evaluate(e.ctx, e.hh)
	posted := e.txn("simplefin", today, -800_00, 0, "BEST BUY 123")
	e.exec(`UPDATE transactions SET linked_txn_id = ? WHERE id = ?`, posted, email)
	e.svc.Evaluate(e.ctx, e.hh)
	if got := strings.Join(e.titles(), " | "); got != "Large transaction: $800.00" {
		t.Fatalf("email + posted twin alerts = %q", got)
	}

	// Disconnected account alerts once per break.
	e.exec(`INSERT INTO connections (id, household_id, provider, name, secret_enc, created_at) VALUES (1, ?, 'simplefin', 'SF', x'00', 1)`, e.hh)
	e.exec(`UPDATE accounts SET connection_id = 1, status = 'disconnected' WHERE id = ?`, e.acct)
	e.exec(`INSERT INTO sync_events (connection_id, at, kind, account_id) VALUES (1, 100, 'account_disconnected', ?)`, e.acct)
	e.svc.Evaluate(e.ctx, e.hh)
	e.svc.Evaluate(e.ctx, e.hh)
	if got := strings.Join(e.titles(), " | "); got != "Checking stopped syncing" {
		t.Fatalf("disconnect alerts = %q", got)
	}
	e.exec(`INSERT INTO sync_events (connection_id, at, kind, account_id) VALUES (1, 200, 'account_disconnected', ?)`, e.acct)
	e.svc.Evaluate(e.ctx, e.hh)
	if got := e.titles(); len(got) != 1 {
		t.Fatalf("a second break should alert again, got %v", got)
	}

	// Prefs off: a new large transaction stays quiet.
	b, _ := json.Marshal(Prefs{PacingPct: 25, LargeTxnCents: 100_00})
	db.New(e.conn).SetNotificationPrefs(e.ctx, db.SetNotificationPrefsParams{UserID: e.user.ID, Prefs: string(b)})
	e.txn("simplefin", today, -2000_00, 0, "TV")
	e.svc.Evaluate(e.ctx, e.hh)
	if got := e.titles(); len(got) != 0 {
		t.Fatalf("with everything off, sent %v", got)
	}
	var stored int
	e.conn.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = ?`, e.user.ID).Scan(&stored)
	if stored != 5 {
		t.Errorf("stored notifications = %d, want 5", stored)
	}
}

func TestRunDebounces(t *testing.T) {
	e := newEnv(t)
	e.svc.Debounce = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	go e.svc.Run(ctx)
	e.txn("simplefin", budget.FormatDate(budgetview.Today()), -600_00, 0, "BIG")
	for range 5 {
		e.svc.Changed(e.hh)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		var n int
		e.conn.QueryRow(`SELECT COUNT(*) FROM notifications`).Scan(&n)
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("notifications = %d after Changed", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLoadOrCreateKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "vapid.json")
	k1, err := LoadOrCreateKeys(p)
	if err != nil || k1.Public == "" || k1.Private == "" {
		t.Fatalf("create: %+v %v", k1, err)
	}
	k2, err := LoadOrCreateKeys(p)
	if err != nil || k2 != k1 {
		t.Fatalf("reload: %+v %v", k2, err)
	}
}

func TestPaymentDueSoon(t *testing.T) {
	e := newEnv(t)
	card := e.exec(`INSERT INTO accounts (household_id, name, type, created_at, updated_at) VALUES (?, 'Quicksilver', 'credit_card', 1, 1)`, e.hh)
	day := func(n int) string { return budget.FormatDate(budgetview.Today().AddDate(0, 0, n)) }
	add := func(kind, date string, created int64) int64 {
		return e.exec(`INSERT INTO account_bills (household_id, account_id, kind, amount_cents, minimum_cents, date, created_at) VALUES (?, ?, ?, 24510, 3500, ?, ?)`,
			e.hh, card, kind, date, created)
	}
	add("due", day(6), e.now.Unix()-100)
	e.svc.Evaluate(e.ctx, e.hh)
	if got := e.titles(); len(got) != 0 {
		t.Fatalf("due in 6 days alerted: %v", got)
	}
	e.exec(`DELETE FROM account_bills`)
	add("due", day(2), e.now.Unix()-100)
	e.svc.Evaluate(e.ctx, e.hh)
	e.svc.Evaluate(e.ctx, e.hh)
	if got := strings.Join(e.titles(), " | "); got != "Quicksilver payment due in 2 days" {
		t.Fatalf("due soon = %q", got)
	}
	var body string
	e.conn.QueryRow(`SELECT body FROM notifications WHERE kind = 'payment_due'`).Scan(&body)
	if body != "Balance $245.10, minimum $35.00. Nothing is scheduled yet." {
		t.Errorf("body = %q", body)
	}
	// Scheduled: a new due notice next cycle stays quiet.
	e.exec(`DELETE FROM account_bills`)
	add("due", day(1), e.now.Unix()-100)
	add("scheduled", day(1), e.now.Unix()-50)
	e.svc.Evaluate(e.ctx, e.hh)
	if got := e.titles(); len(got) != 0 {
		t.Fatalf("scheduled payment still alerted: %v", got)
	}
}
