// Package simplefin is a client for the SimpleFIN Bridge protocol (v2, with v1 fallbacks).
package simplefin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrTokenUsed means the claim endpoint returned 403: the token was already claimed
	// (possibly by someone else) or never existed.
	ErrTokenUsed    = errors.New("this setup token was already used or is invalid; if you did not claim it, it may be compromised, so disable it on SimpleFIN Bridge")
	ErrBadToken     = errors.New("that doesn't look like a SimpleFIN setup token")
	ErrAuth         = errors.New("SimpleFIN rejected the access credentials; reconnect this connection")
	ErrRateLimited  = errors.New("SimpleFIN rate limit reached; try again later")
	ErrPaymentOwing = errors.New("SimpleFIN Bridge subscription needs attention (payment required)")
)

type Client struct {
	HTTP *http.Client
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// DecodeToken turns a setup token into its claim URL. Claim URLs must be https
// unless they point at a loopback host (used by the fake Bridge in tests).
func DecodeToken(token string) (*url.URL, error) {
	token = strings.TrimSpace(token)
	raw, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(token)
	}
	if err != nil {
		return nil, ErrBadToken
	}
	u, err := url.Parse(strings.TrimSpace(string(raw)))
	if err != nil || u.Host == "" {
		return nil, ErrBadToken
	}
	if err := checkScheme(u); err != nil {
		return nil, err
	}
	return u, nil
}

func checkScheme(u *url.URL) error {
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if ip := net.ParseIP(u.Hostname()); (ip != nil && ip.IsLoopback()) || u.Hostname() == "localhost" {
			return nil
		}
	}
	return fmt.Errorf("SimpleFIN URL must use https (got %q)", u.Scheme)
}

// Claim exchanges a setup token for an access URL.
func (c *Client) Claim(ctx context.Context, token string) (string, error) {
	u, err := DecodeToken(token)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Length", "0")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("contacting SimpleFIN: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode == http.StatusForbidden {
		return "", ErrTokenUsed
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("SimpleFIN claim failed: HTTP %d", resp.StatusCode)
	}
	access := strings.TrimSpace(string(body))
	au, err := url.Parse(access)
	if err != nil || au.User == nil {
		return "", errors.New("SimpleFIN returned an invalid access URL")
	}
	if err := checkScheme(au); err != nil {
		return "", err
	}
	return access, nil
}

// ---- response model ----

type Error struct {
	Code      string `json:"code"`
	Msg       string `json:"msg"`
	ConnID    string `json:"conn_id"`
	AccountID string `json:"account_id"`
}

type Connection struct {
	ConnID string `json:"conn_id"`
	Name   string `json:"name"`
	OrgID  string `json:"org_id"`
	OrgURL string `json:"org_url"`
}

type Transaction struct {
	ID           string `json:"id"`
	Posted       int64  `json:"posted"`
	Amount       string `json:"amount"`
	Description  string `json:"description"`
	Payee        string `json:"payee"`
	Memo         string `json:"memo"`
	TransactedAt int64  `json:"transacted_at"`
	Pending      bool   `json:"pending"`
}

type org struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Domain string `json:"domain"`
	URL    string `json:"url"`
}

type Account struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	ConnID           string        `json:"conn_id"`
	Currency         string        `json:"currency"`
	Balance          string        `json:"balance"`
	AvailableBalance string        `json:"available-balance"`
	BalanceDate      int64         `json:"balance-date"`
	Transactions     []Transaction `json:"transactions"`
	Org              *org          `json:"org"` // v1 only
}

type AccountSet struct {
	Errors      []Error      `json:"-"`
	Connections []Connection `json:"connections"`
	Accounts    []Account    `json:"accounts"`
}

// ConnErrors returns errors that apply to an institution login, keyed by conn_id.
func (s *AccountSet) ConnErrors() map[string]Error {
	out := map[string]Error{}
	for _, e := range s.Errors {
		if e.ConnID != "" && strings.HasPrefix(e.Code, "con.") {
			out[e.ConnID] = e
		}
	}
	return out
}

func (s *AccountSet) UnmarshalJSON(b []byte) error {
	type plain AccountSet
	var raw struct {
		plain
		ErrList []Error  `json:"errlist"`
		Errors  []string `json:"errors"` // v1
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*s = AccountSet(raw.plain)
	s.Errors = raw.ErrList
	for _, m := range raw.Errors {
		s.Errors = append(s.Errors, Error{Code: "gen.v1", Msg: m})
	}
	// v1: synthesize connections from each account's org.
	if len(s.Connections) == 0 {
		seen := map[string]bool{}
		for i := range s.Accounts {
			a := &s.Accounts[i]
			if a.Org == nil {
				continue
			}
			id := a.Org.ID
			if id == "" {
				id = a.Org.Domain
			}
			if a.ConnID == "" {
				a.ConnID = id
			}
			if !seen[id] {
				seen[id] = true
				s.Connections = append(s.Connections, Connection{ConnID: id, Name: a.Org.Name, OrgURL: a.Org.URL})
			}
		}
	}
	return nil
}

// Fetch retrieves accounts and transactions between start and end (inclusive of start).
// With accountIDs, only those accounts are returned (the Bridge counts these against a
// separate per-account quota).
func (c *Client) Fetch(ctx context.Context, accessURL string, start, end time.Time, accountIDs ...string) (*AccountSet, error) {
	u, err := url.Parse(accessURL)
	if err != nil {
		return nil, errors.New("invalid stored access URL")
	}
	if err := checkScheme(u); err != nil {
		return nil, err
	}
	user := u.User
	u.User = nil
	u.Path = strings.TrimRight(u.Path, "/") + "/accounts"
	q := url.Values{}
	q.Set("version", "2")
	q.Set("pending", "1")
	q.Set("start-date", strconv.FormatInt(start.Unix(), 10))
	q.Set("end-date", strconv.FormatInt(end.Unix(), 10))
	for _, id := range accountIDs {
		q.Add("account", id)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if user != nil {
		pw, _ := user.Password()
		req.SetBasicAuth(user.Username(), pw)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contacting SimpleFIN: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden, http.StatusUnauthorized:
		return nil, ErrAuth
	case http.StatusPaymentRequired:
		return nil, ErrPaymentOwing
	case http.StatusTooManyRequests:
		return nil, ErrRateLimited
	default:
		return nil, fmt.Errorf("SimpleFIN fetch failed: HTTP %d", resp.StatusCode)
	}
	var set AccountSet
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&set); err != nil {
		return nil, fmt.Errorf("decoding SimpleFIN response: %w", err)
	}
	return &set, nil
}
