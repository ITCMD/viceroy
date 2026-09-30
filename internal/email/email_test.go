package email

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"viceroy/internal/db"
	"viceroy/internal/email/fakeimap"
	"viceroy/internal/secrets"
)

func eml(t *testing.T, name string) []byte {
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func waitFor(t *testing.T, what string, f func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if f() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestWatcherRoutesAndLinks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	q := db.New(conn)
	box, _ := secrets.New(make([]byte, 32))
	svc := New(conn, box, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Poll = 200 * time.Millisecond

	h, err := q.CreateHousehold(ctx, db.CreateHouseholdParams{Name: "H", CreatedAt: 1})
	if err != nil {
		t.Fatal(err)
	}
	mkAcct := func(name string, manual int64, bal int64) db.Account {
		a, err := q.CreateAccount(ctx, db.CreateAccountParams{HouseholdID: h.ID, Name: name, Type: "checking", Currency: "USD",
			BalanceCents: bal, Status: "active", IsManual: manual, CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	sapphire, cu := mkAcct("Sapphire", 0, -5000), mkAcct("CU Checking", 1, 100000)
	// The bank already posted the Starbucks purchase before the alert is read.
	posted, err := q.InsertSyncedTransaction(ctx, db.InsertSyncedTransactionParams{
		HouseholdID: h.ID, AccountID: sapphire.ID, ExternalID: sql.NullString{String: "sf-1", Valid: true}, Source: "simplefin",
		Date: "2026-09-30", AmountCents: -1234, Description: "STARBUCKS STORE 123 SEATTLE WA", CreatedAt: 1, UpdatedAt: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	mkFilter := func(sender, parser string, acct int64) {
		if _, err := q.InsertEmailFilter(ctx, db.InsertEmailFilterParams{HouseholdID: h.ID, Name: sender, Enabled: 1,
			Sender: sender, AccountID: acct, Parser: parser, Sign: "debit", CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	mkFilter("chase.com", "chase", sapphire.ID)
	mkFilter("mycu.org", "generic", cu.ID)

	imapSrv, err := fakeimap.Start("127.0.0.1:0", "me@example.com", "app-pass")
	if err != nil {
		t.Fatal(err)
	}
	defer imapSrv.Close()
	for _, f := range []string{"chase.eml", "labeled.eml", "unknown.eml"} {
		imapSrv.Deliver(eml(t, f))
	}
	host, portS, _ := net.SplitHostPort(imapSrv.Addr)
	port, _ := strconv.Atoi(portS)
	if _, err := Test(Conn{Host: host, Port: port, Security: "none", Username: "me@example.com", Password: "nope", Folder: "INBOX"}); err == nil {
		t.Fatal("bad password accepted")
	}
	if n, err := Test(Conn{Host: host, Port: port, Security: "none", Username: "me@example.com", Password: "app-pass", Folder: "INBOX"}); err != nil || n != 3 {
		t.Fatalf("Test = %d, %v", n, err)
	}
	pw, _ := box.Seal("app-pass")
	mb, err := q.InsertMailbox(ctx, db.InsertMailboxParams{HouseholdID: h.ID, Host: host, Port: int64(port), Security: "none",
		Username: "me@example.com", PasswordEnc: pw, Folder: "INBOX", Enabled: 1, CreatedAt: 1})
	if err != nil {
		t.Fatal(err)
	}

	go svc.Run(ctx)
	byStatus := func() map[string][]db.ListEmailMessagesRow {
		rows, err := q.ListEmailMessages(ctx, db.ListEmailMessagesParams{HouseholdID: h.ID, Status: "", Lim: 100})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string][]db.ListEmailMessagesRow{}
		for _, r := range rows {
			out[r.Status] = append(out[r.Status], r)
			out["all"] = append(out["all"], r)
		}
		return out
	}
	waitFor(t, "initial messages", func() bool { return len(byStatus()["all"]) == 3 })
	st := byStatus()
	if len(st[StatusParsed]) != 2 || len(st[StatusUnrouted]) != 1 || st[StatusUnrouted][0].Subject != "Our fall sale starts now" {
		t.Fatalf("statuses: %+v", st)
	}

	// Chase alert on a synced account: provisional, linked to the already-posted row.
	for _, m := range st[StatusParsed] {
		txn, err := q.GetTransactionByID(ctx, m.TransactionID.Int64)
		if err != nil {
			t.Fatal(err)
		}
		switch txn.AccountID {
		case sapphire.ID:
			if txn.Source != "email" || txn.Provisional != 1 || txn.AmountCents != -1234 || txn.LinkedTxnID.Int64 != posted {
				t.Fatalf("chase txn: %+v", txn)
			}
		case cu.ID:
			if txn.Provisional != 0 || txn.AmountCents != -4810 || txn.Date != "2026-09-30" {
				t.Fatalf("cu txn: %+v", txn)
			}
		}
	}
	acct, _ := q.GetAccount(ctx, db.GetAccountParams{ID: cu.ID, HouseholdID: h.ID})
	if acct.BalanceCents != 100000-4810 {
		t.Fatalf("manual balance = %d", acct.BalanceCents)
	}

	// New mail arrives while idling; a repeat Message-ID is ignored.
	imapSrv.Deliver(eml(t, "capitalone.eml"))
	imapSrv.Deliver(eml(t, "chase.eml"))
	waitFor(t, "capital one alert", func() bool { return len(byStatus()[StatusUnrouted]) == 2 })
	waitFor(t, "cursor", func() bool {
		m, _ := q.GetMailboxByID(ctx, mb.ID)
		return m.LastUid == 5 && m.Status == "ok"
	})
	if n := len(byStatus()["all"]); n != 4 {
		t.Fatalf("messages = %d, want 4 (duplicate skipped)", n)
	}

	// Adding a filter afterwards picks up the waiting alert.
	mkFilter("capitalone.com", "capital_one", cu.ID)
	n, err := svc.Reroute(ctx, h.ID)
	if err != nil || n != 1 {
		t.Fatalf("Reroute = %d, %v", n, err)
	}
	if st := byStatus(); len(st[StatusUnrouted]) != 1 || len(st[StatusParsed]) != 3 {
		t.Fatalf("after reroute: %+v", st)
	}
}
