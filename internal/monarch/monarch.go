// Package monarch reads Monarch Money's CSV exports: Transactions (Settings → Data → Export)
// and account Balances history.
package monarch

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"viceroy/internal/accounts"
	"viceroy/internal/money"
)

type Txn struct {
	Line        int
	Date        string // YYYY-MM-DD
	Merchant    string
	Category    string // "" when Uncategorized
	Account     string // as exported, e.g. "Checking (...2080)"
	Statement   string // original statement
	Notes       string
	Amount      int64 // cents, negative = money out
	Tags        []string
	NeedsReview bool
	ID          string // Monarch's transaction id
}

type Balance struct {
	Date    string
	Account string
	Amount  int64
}

// header maps column names (case-insensitive) to indexes and checks the required ones exist.
func header(row []string, required ...string) (map[string]int, error) {
	idx := map[string]int{}
	for i, h := range row {
		idx[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\uFEFF")))] = i
	}
	var missing []string
	for _, r := range required {
		if _, ok := idx[r]; !ok {
			missing = append(missing, r)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing column(s): %s", strings.Join(missing, ", "))
	}
	return idx, nil
}

func reader(r io.Reader) *csv.Reader {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	return cr
}

func get(row []string, idx map[string]int, col string) string {
	i, ok := idx[col]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

// parseDate accepts YYYY-MM-DD (Monarch's format) and M/D/YYYY.
func parseDate(s string) (string, error) {
	for _, layout := range []string{time.DateOnly, "1/2/2006", "01/02/2006"} {
		if d, err := time.Parse(layout, s); err == nil {
			return d.Format(time.DateOnly), nil
		}
	}
	return "", fmt.Errorf("invalid date %q", s)
}

// ParseTransactions reads a Monarch transactions export.
func ParseTransactions(r io.Reader) ([]Txn, error) {
	cr := reader(r)
	head, err := cr.Read()
	if err != nil {
		return nil, errors.New("the transactions file is empty")
	}
	idx, err := header(head, "date", "merchant", "category", "account", "amount")
	if err != nil {
		return nil, fmt.Errorf("this doesn't look like a Monarch transactions export: %w", err)
	}
	var out []Txn
	for line := 2; ; line++ {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if len(row) == 1 && strings.TrimSpace(row[0]) == "" {
			continue
		}
		t := Txn{
			Line: line, Merchant: get(row, idx, "merchant"), Category: get(row, idx, "category"),
			Account: get(row, idx, "account"), Statement: get(row, idx, "original statement"),
			Notes: get(row, idx, "notes"), ID: get(row, idx, "id"),
			NeedsReview: strings.EqualFold(get(row, idx, "reviewed"), "needs review"),
		}
		if t.Date, err = parseDate(get(row, idx, "date")); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if t.Amount, err = money.ParseCents(get(row, idx, "amount")); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if t.Account == "" {
			return nil, fmt.Errorf("line %d: no account", line)
		}
		if strings.EqualFold(t.Category, "uncategorized") {
			t.Category = ""
		}
		for _, tag := range strings.Split(get(row, idx, "tags"), ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				t.Tags = append(t.Tags, tag)
			}
		}
		out = append(out, t)
	}
	return out, nil
}

// ParseBalances reads a Monarch balance history export (Date, Balance, Account).
func ParseBalances(r io.Reader) ([]Balance, error) {
	cr := reader(r)
	head, err := cr.Read()
	if err != nil {
		return nil, errors.New("the balances file is empty")
	}
	idx, err := header(head, "date", "balance", "account")
	if err != nil {
		return nil, fmt.Errorf("this doesn't look like a Monarch balances export: %w", err)
	}
	var out []Balance
	for line := 2; ; line++ {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if len(row) == 1 && strings.TrimSpace(row[0]) == "" {
			continue
		}
		b := Balance{Account: get(row, idx, "account")}
		if b.Date, err = parseDate(get(row, idx, "date")); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if b.Amount, err = money.ParseCents(get(row, idx, "balance")); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if b.Account != "" {
			out = append(out, b)
		}
	}
	return out, nil
}

// Account summarizes one Monarch account across both files.
type Account struct {
	Key           string `json:"key"`  // the exported account name
	Name          string `json:"name"` // without the "(...1234)" suffix
	Mask          string `json:"mask"`
	Transactions  int    `json:"transactions"`
	FirstDate     string `json:"first_date"`
	LastDate      string `json:"last_date"`
	Balances      int    `json:"balances"`
	LatestBalance int64  `json:"latest_balance"` // from the balances file, else the sum of transactions
	LatestDate    string `json:"latest_date"`
	HasBalance    bool   `json:"has_balance"`
	Type          string `json:"type"` // suggested account type
}

// SplitName turns "Checking (...2080)" into ("Checking", "2080").
func SplitName(s string) (name, mask string) {
	mask = accounts.Mask(s)
	name = strings.TrimSpace(s)
	if mask != "" {
		if i := strings.LastIndex(name, "("); i > 0 && strings.HasSuffix(name, ")") {
			name = strings.TrimSpace(name[:i])
		}
	}
	return name, mask
}

// Accounts lists every account named in either file, ordered by transaction count.
func Accounts(txns []Txn, bals []Balance) []Account {
	by := map[string]*Account{}
	get := func(key string) *Account {
		a, ok := by[key]
		if !ok {
			a = &Account{Key: key}
			a.Name, a.Mask = SplitName(key)
			by[key] = a
		}
		return a
	}
	sums := map[string]int64{}
	for _, t := range txns {
		a := get(t.Account)
		a.Transactions++
		sums[t.Account] += t.Amount
		if a.FirstDate == "" || t.Date < a.FirstDate {
			a.FirstDate = t.Date
		}
		if t.Date > a.LastDate {
			a.LastDate = t.Date
		}
	}
	for _, b := range bals {
		a := get(b.Account)
		a.Balances++
		if b.Date >= a.LatestDate {
			a.LatestDate, a.LatestBalance, a.HasBalance = b.Date, b.Amount, true
		}
	}
	out := make([]Account, 0, len(by))
	for key, a := range by {
		if !a.HasBalance {
			a.LatestBalance, a.LatestDate = sums[key], a.LastDate
		}
		a.Type = accounts.InferType(a.Name, a.LatestBalance)
		// A liability with no transactions at all is far more likely a loan (a car, say)
		// than a credit card.
		if a.Type == accounts.CreditCard && a.Transactions == 0 && !accounts.HasTypeKeyword(a.Name) {
			a.Type = accounts.Loan
		}
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Transactions != out[j].Transactions {
			return out[i].Transactions > out[j].Transactions
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Categories counts transactions per Monarch category ("" = uncategorized), most used first.
func Categories(txns []Txn) []CategoryCount {
	n := map[string]int{}
	for _, t := range txns {
		n[t.Category]++
	}
	out := make([]CategoryCount, 0, len(n))
	for name, c := range n {
		out = append(out, CategoryCount{name, c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

type CategoryCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Known Monarch default categories Viceroy doesn't seed: where to create them and their icon.
var known = map[string]struct{ Kind, Icon string }{
	"auto payment":                       {"fixed", "🚗"},
	"cash & atm":                         {"flexible", "🏧"},
	"financial & legal services":         {"flexible", "⚖️"},
	"financial fees":                     {"flexible", "💸"},
	"advertising & promotion":            {"flexible", "📣"},
	"business utilities & communication": {"flexible", "📞"},
	"office supplies & expenses":         {"flexible", "🖇️"},
	"postage & shipping":                 {"flexible", "📮"},
	"business travel & meals":            {"flexible", "🧳"},
	"employee wages & contract labor":    {"flexible", "👷"},
	"personal":                           {"flexible", "🙂"},
	"fast food":                          {"flexible", "🍔"},
	"dentist":                            {"flexible", "🦷"},
	"furniture & housewares":             {"flexible", "🛋️"},
	"electronics":                        {"flexible", "💻"},
	"child activities":                   {"flexible", "🧸"},
	"check":                              {"flexible", "🧾"},
	"rental income":                      {"income", "🏘️"},
	"dividends & capital gains":          {"income", "📈"},
	"garbage":                            {"fixed", "🗑️"},
	"tithing":                            {"fixed", "⛪"},
	"storage unit":                       {"fixed", "📦"},
	"grocery subscriptions":              {"flexible", "🥬"},
}

// Suggest returns the group kind and icon to create an unknown category with.
func Suggest(name string) (kind, icon string) {
	if k, ok := known[strings.ToLower(name)]; ok {
		return k.Kind, k.Icon
	}
	if strings.Contains(strings.ToLower(name), "income") {
		return "income", "💵"
	}
	return "flexible", "🏷️"
}
