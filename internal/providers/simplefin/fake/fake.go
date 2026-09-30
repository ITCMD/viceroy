// Package fake is an in-process SimpleFIN Bridge used by tests and the e2e suite.
// Scenarios model the reconnect problems sync has to survive.
package fake

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"viceroy/internal/providers/simplefin"
)

const (
	user = "viceroy"
	pass = "fake-secret"
)

// Scenarios:
//   - initial: Capital One (checking, savings, card, two same-named Savor cards) + Ally savings.
//   - relinked: Capital One re-authorized with a new conn_id and new account ids, and only
//     some accounts shared: checking, card, one Savor card, plus a new Venture card.
//   - reauth: Capital One login broken (con.auth); its accounts are absent.
//   - posted: initial plus a new Chipotle charge on checking today, for pending-entry linking.
var Scenarios = []string{"initial", "relinked", "reauth", "posted"}

type Server struct {
	mu       sync.Mutex
	scenario string
	claimed  map[string]bool
	Requests int
	Now      func() time.Time
	mux      *http.ServeMux
	baseURL  string
}

func New() *Server {
	s := &Server{scenario: "initial", claimed: map[string]bool{}, Now: time.Now}
	m := http.NewServeMux()
	m.HandleFunc("POST /claim/{id}", s.claim)
	m.HandleFunc("GET /sf/accounts", s.accounts)
	m.HandleFunc("GET /_control/token", s.token)
	m.HandleFunc("POST /_control/scenario", s.setScenario)
	s.mux = m
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.baseURL == "" {
		s.mu.Lock()
		s.baseURL = "http://" + r.Host
		s.mu.Unlock()
	}
	s.mux.ServeHTTP(w, r)
}

// Token returns a fresh setup token for a server reachable at base (e.g. http://127.0.0.1:1234).
func Token(base string, id string) string {
	return base64.StdEncoding.EncodeToString([]byte(base + "/claim/" + id))
}

func (s *Server) SetScenario(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scenario = name
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	id := strconv.FormatInt(time.Now().UnixNano(), 36)
	fmt.Fprint(w, Token("http://"+r.Host, id))
}

func (s *Server) setScenario(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	for _, n := range Scenarios {
		if n == in.Name {
			s.SetScenario(n)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	http.Error(w, "unknown scenario", http.StatusBadRequest)
}

func (s *Server) claim(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := r.PathValue("id")
	if s.claimed[id] {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	s.claimed[id] = true
	fmt.Fprintf(w, "http://%s:%s@%s/sf", user, pass, r.Host)
}

func (s *Server) accounts(w http.ResponseWriter, r *http.Request) {
	u, p, ok := r.BasicAuth()
	if !ok || u != user || p != pass {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	start, _ := strconv.ParseInt(r.URL.Query().Get("start-date"), 10, 64)
	s.mu.Lock()
	s.Requests++
	set := build(s.scenario, s.Now())
	s.mu.Unlock()
	for i := range set.Accounts {
		a := &set.Accounts[i]
		kept := a.Transactions[:0]
		for _, t := range a.Transactions {
			if t.Posted >= start || (t.Posted == 0 && t.TransactedAt >= start) {
				kept = append(kept, t)
			}
		}
		a.Transactions = kept
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"errlist": set.Errors, "connections": set.Connections, "accounts": set.Accounts,
	})
}

type acct struct {
	id, conn, name, balance string
	txns                    []txn
}

type txn struct {
	daysAgo int
	amount  string
	desc    string
	pending bool
}

var groceries = []txn{
	{2, "-54.12", "WHOLEFDS MKT #10234", true},
	{3, "-12.50", "SQ *BLUE BOTTLE COFFEE", false},
	{6, "-86.40", "TRADER JOE'S #552", false},
	{10, "2450.00", "ACME CORP PAYROLL", false},
	{14, "-1850.00", "RENT PAYMENT - OAK APTS", false},
	{21, "-64.99", "COMCAST CABLE", false},
}

func build(scenario string, now time.Time) simplefin.AccountSet {
	day := func(n int) int64 {
		d := now.AddDate(0, 0, -n)
		return time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, time.UTC).Unix()
	}
	var accts []acct
	var conns []simplefin.Connection
	var errs []simplefin.Error
	ally := acct{"ACT-ally-sav", "CON-ally", "Online Savings (4444)", "15230.55", []txn{{5, "12.31", "INTEREST PAID", false}}}
	allyConn := simplefin.Connection{ConnID: "CON-ally", Name: "Ally Bank", OrgID: "ally", OrgURL: "https://ally.com"}
	switch scenario {
	case "initial", "posted":
		chk := groceries
		if scenario == "posted" {
			chk = append(groceries[:len(groceries):len(groceries)], txn{0, "-18.75", "CHIPOTLE 2231 AUSTIN TX", false})
		}
		conns = []simplefin.Connection{{ConnID: "CON-c1", Name: "Capital One", OrgID: "capone", OrgURL: "https://capitalone.com"}, allyConn}
		accts = []acct{
			{"ACT-c1-chk", "CON-c1", "360 Checking (1111)", "3120.44", chk},
			{"ACT-c1-sav", "CON-c1", "360 Performance Savings (2222)", "8800.00", nil},
			{"ACT-c1-qs", "CON-c1", "Quicksilver Card (3333)", "-742.18", []txn{{1, "-23.45", "UBER *TRIP", true}, {4, "-118.20", "AMAZON.COM*2K4", false}}},
			{"ACT-c1-sv1", "CON-c1", "Savor Card", "-120.00", nil},
			{"ACT-c1-sv2", "CON-c1", "Savor Card", "-45.00", nil},
			ally,
		}
	case "relinked":
		conns = []simplefin.Connection{{ConnID: "CON-c2", Name: "Capital One", OrgID: "capone", OrgURL: "https://capitalone.com"}, allyConn}
		// The pending Whole Foods charge has posted with a new id and a slightly different date.
		posted := append([]txn{{1, "-54.12", "WHOLEFDS MKT #10234", false}}, groceries[1:]...)
		accts = []acct{
			{"ACT-c2-chk", "CON-c2", "360 Checking (1111)", "3066.32", posted},
			{"ACT-c2-qs", "CON-c2", "Quicksilver Card (3333)", "-765.63", []txn{{0, "-23.45", "UBER *TRIP", false}, {4, "-118.20", "AMAZON.COM*2K4", false}}},
			{"ACT-c2-sv", "CON-c2", "Savor Card", "-130.00", nil},
			{"ACT-c2-vt", "CON-c2", "Venture Card (5555)", "-310.00", []txn{{2, "-310.00", "DELTA AIR LINES", false}}},
			ally,
		}
	case "reauth":
		conns = []simplefin.Connection{{ConnID: "CON-c1", Name: "Capital One", OrgID: "capone", OrgURL: "https://capitalone.com"}, allyConn}
		errs = []simplefin.Error{{Code: "con.auth", Msg: "Capital One needs you to sign in again.", ConnID: "CON-c1"}}
		accts = []acct{ally}
	}
	out := simplefin.AccountSet{Connections: conns, Errors: errs}
	if out.Errors == nil {
		out.Errors = []simplefin.Error{}
	}
	for _, a := range accts {
		sa := simplefin.Account{ID: a.id, Name: a.name, ConnID: a.conn, Currency: "USD", Balance: a.balance, BalanceDate: now.Unix()}
		for i, t := range a.txns {
			st := simplefin.Transaction{
				ID:     fmt.Sprintf("TRN-%s-%d-%s", strings.TrimPrefix(a.id, "ACT-"), i, strings.ToLower(t.desc[:3])),
				Amount: t.amount, Description: t.desc, Pending: t.pending, TransactedAt: day(t.daysAgo),
			}
			if !t.pending {
				st.Posted = day(t.daysAgo)
			}
			sa.Transactions = append(sa.Transactions, st)
		}
		if sa.Transactions == nil {
			sa.Transactions = []simplefin.Transaction{}
		}
		out.Accounts = append(out.Accounts, sa)
	}
	return out
}
