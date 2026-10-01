package email

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"viceroy/internal/ai"
	"viceroy/internal/bills"
	"viceroy/internal/categorize"
	"viceroy/internal/db"
	"viceroy/internal/money"
	"viceroy/internal/recurring"
)

// The email-reading AI runs in a sandbox: these tools are the only things it can do, and every
// limit is enforced here, not in the prompt. A hostile email can at most mislabel a
// transaction near its own date, which is logged and can be undone.
const (
	SandboxDays      = 14  // transactions it may see or change: within this many days of the email
	MaxUpdates       = 3   // distinct transactions it may change per email
	maxSearchResults = 20  // rows returned per search
	maxNote          = 200 // characters it may add to a transaction's notes per update
	maxTags          = 3   // tags per update
)

type sandbox struct {
	s       *Service
	hh      int64
	msg     db.EmailMessage
	from    string // window start, YYYY-MM-DD
	to      string // window end
	changed map[int64]bool
}

func (s *Service) newSandbox(hh int64, msg db.EmailMessage) *sandbox {
	day := time.Unix(msg.ReceivedAt, 0).In(time.Local)
	d := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	return &sandbox{
		s: s, hh: hh, msg: msg, changed: map[int64]bool{},
		from: d.AddDate(0, 0, -SandboxDays).Format(time.DateOnly), to: d.AddDate(0, 0, SandboxDays).Format(time.DateOnly),
	}
}

func schema(s string) json.RawMessage { return json.RawMessage(s) }

// Tools lists the sandbox's tools for the model.
func (b *sandbox) Tools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "search_transactions",
			Description: fmt.Sprintf("Find the household's transactions dated %s to %s (near the email). Amounts are dollars; negative = money out.", b.from, b.to),
			Parameters: schema(`{"type":"object","properties":{
				"query":{"type":"string","description":"Text in the merchant, statement or notes"},
				"amount":{"type":"string","description":"Dollar amount to match (either sign), e.g. \"12.34\""}}}`),
			Run: b.search,
		},
		{
			Name:        "list_categories",
			Description: "All categories with their ids and groups.",
			Parameters:  schema(`{"type":"object","properties":{}}`),
			Run:         b.categories,
		},
		{
			Name:        "list_rules",
			Description: "The household's categorization rules (read-only).",
			Parameters:  schema(`{"type":"object","properties":{}}`),
			Run:         b.rules,
		},
		{
			Name:        "get_schedule",
			Description: "Upcoming recurring bills, subscriptions and paychecks, and card payments due or scheduled.",
			Parameters:  schema(`{"type":"object","properties":{}}`),
			Run:         b.schedule,
		},
		{
			Name: "update_transaction",
			Description: fmt.Sprintf("Update one transaction found with search_transactions that this email clearly describes: set its category, add a short factual note (items, order number), add tags. At most %d transactions per email. A category the user chose is never replaced.", MaxUpdates),
			Parameters: schema(`{"type":"object","required":["id"],"properties":{
				"id":{"type":"integer"},
				"category_id":{"type":"integer"},
				"add_note":{"type":"string","description":"Plain text, max 200 characters, no links"},
				"add_tags":{"type":"array","items":{"type":"string"},"maxItems":3}}}`),
			Run: b.update,
		},
	}
}

type sandboxTxn struct {
	ID        int64    `json:"id"`
	Date      string   `json:"date"`
	Amount    string   `json:"amount"`
	Merchant  string   `json:"merchant"`
	Statement string   `json:"statement"`
	Category  string   `json:"category,omitempty"`
	Notes     string   `json:"notes,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Account   string   `json:"account"`
	Pending   bool     `json:"pending,omitempty"`
}

func (b *sandbox) search(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct{ Query, Amount string }
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, errors.New("invalid arguments")
	}
	var amount int64
	hasAmount := strings.TrimSpace(a.Amount) != ""
	if hasAmount {
		c, err := money.ParseCents(strings.TrimPrefix(strings.TrimSpace(a.Amount), "-"))
		if err != nil {
			return nil, errors.New("amount must look like 12.34")
		}
		amount = c
	}
	q := db.New(b.s.DB)
	end, _ := time.Parse(time.DateOnly, b.to)
	rows, err := q.ListTransactions(ctx, db.ListTransactionsParams{
		HouseholdID: b.hh, Uncategorized: 0, NeedsReview: 0, IncludeHidden: 0, Q: strings.TrimSpace(a.Query),
		BeforeDate: end.AddDate(0, 0, 1).Format(time.DateOnly), Lim: 500,
	})
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	out := []sandboxTxn{}
	for _, r := range rows {
		if r.Date < b.from {
			break // newest first
		}
		if hasAmount && r.AmountCents != amount && r.AmountCents != -amount {
			continue
		}
		name := r.MerchantName
		if name == "" {
			name = r.Payee
		}
		out = append(out, sandboxTxn{ID: r.ID, Date: r.Date, Amount: money.Format(r.AmountCents), Merchant: name, Statement: r.Description,
			Category: r.CategoryName, Notes: r.Notes, Account: r.AccountName, Pending: r.Pending == 1})
		ids = append(ids, r.ID)
		if len(out) == maxSearchResults {
			break
		}
	}
	if len(ids) > 0 {
		tags, err := q.ListTagsForTransactions(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, t := range tags {
			for i := range out {
				if out[i].ID == t.TransactionID {
					out[i].Tags = append(out[i].Tags, t.Name)
				}
			}
		}
	}
	return map[string]any{"window": b.from + " to " + b.to, "transactions": out}, nil
}

func (b *sandbox) categories(ctx context.Context, _ json.RawMessage) (any, error) {
	q := db.New(b.s.DB)
	groups, err := q.ListCategoryGroups(ctx, b.hh)
	if err != nil {
		return nil, err
	}
	gname := map[int64]string{}
	for _, g := range groups {
		gname[g.ID] = g.Name
	}
	cats, err := q.ListCategories(ctx, b.hh)
	if err != nil {
		return nil, err
	}
	type cat struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Group string `json:"group"`
	}
	out := []cat{}
	for _, c := range cats {
		if c.Archived == 0 {
			out = append(out, cat{c.ID, c.Name, gname[c.GroupID]})
		}
	}
	return map[string]any{"categories": out}, nil
}

func (b *sandbox) rules(ctx context.Context, _ json.RawMessage) (any, error) {
	q := db.New(b.s.DB)
	rules, err := q.ListRules(ctx, b.hh)
	if err != nil {
		return nil, err
	}
	cats, err := q.ListCategories(ctx, b.hh)
	if err != nil {
		return nil, err
	}
	cname := map[int64]string{}
	for _, c := range cats {
		cname[c.ID] = c.Name
	}
	type rule struct {
		When     string `json:"when"`
		Category string `json:"category,omitempty"`
		Rename   string `json:"rename_to,omitempty"`
		Hide     bool   `json:"hide,omitempty"`
	}
	out := []rule{}
	for _, r := range rules {
		x := rule{When: r.MatchField + " " + r.MatchOp + " " + strconv.Quote(r.MatchValue), Rename: r.SetMerchant, Hide: r.SetHidden == 1}
		if r.SetCategoryID.Valid {
			x.Category = cname[r.SetCategoryID.Int64]
		}
		out = append(out, x)
	}
	return map[string]any{"rules": out}, nil
}

func (b *sandbox) schedule(ctx context.Context, _ json.RawMessage) (any, error) {
	q := db.New(b.s.DB)
	now := b.s.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	series, dismissed, err := recurring.Load(ctx, q, b.hh, today)
	if err != nil {
		return nil, err
	}
	type item struct {
		Name    string `json:"name"`
		Cadence string `json:"cadence"`
		Amount  string `json:"typical_amount"`
		Next    string `json:"next_date"`
		Account string `json:"account"`
	}
	limit := today.AddDate(0, 0, 45).Format(time.DateOnly)
	recur := []item{}
	for _, sr := range series {
		if !dismissed[sr.Key] && sr.NextDate <= limit {
			recur = append(recur, item{sr.Name, string(sr.Cadence), money.Format(sr.Amount), sr.NextDate, sr.AccountName})
		}
	}
	rows, err := q.ListRecentBills(ctx, db.ListRecentBillsParams{HouseholdID: b.hh, CreatedAt: now.Add(-bills.Lookback).Unix()})
	if err != nil {
		return nil, err
	}
	accts, err := q.ListAccounts(ctx, b.hh)
	if err != nil {
		return nil, err
	}
	aname := map[int64]string{}
	for _, a := range accts {
		aname[a.ID] = a.Name
	}
	type bill struct {
		Account   string `json:"account"`
		DueDate   string `json:"due_date,omitempty"`
		Scheduled string `json:"payment_scheduled,omitempty"`
		Paid      string `json:"paid,omitempty"`
	}
	billsOut := []bill{}
	for id, st := range bills.ByAccount(rows, today.Format(time.DateOnly)) {
		x := bill{Account: aname[id]}
		if st.Due != nil {
			x.DueDate = st.Due.Date.String
		}
		if st.Scheduled != nil {
			x.Scheduled = st.Scheduled.Date.String
		}
		if st.Paid != nil {
			x.Paid = st.Paid.Date.String
		}
		billsOut = append(billsOut, x)
	}
	return map[string]any{"today": today.Format(time.DateOnly), "recurring": recur, "card_payments": billsOut}, nil
}

var (
	urlRe    = regexp.MustCompile(`(?i)\b(?:https?://|www\.)\S+`)
	spacesRe = regexp.MustCompile(`\s+`)
	tagRe    = regexp.MustCompile(`^[\p{L}\p{N} &'\-]{1,30}$`)
)

// cleanText makes model-written text safe to store: no links, no control characters, one
// line, capped.
func cleanText(s string, max int) string {
	s = urlRe.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
	s = strings.TrimSpace(spacesRe.ReplaceAllString(s, " "))
	if r := []rune(s); len(r) > max {
		s = strings.TrimSpace(string(r[:max]))
	}
	return s
}

// update is the one write tool. Each call is its own database transaction and every change is
// recorded in ai_changes with the value before.
func (b *sandbox) update(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		ID         int64    `json:"id"`
		CategoryID *int64   `json:"category_id"`
		AddNote    string   `json:"add_note"`
		AddTags    []string `json:"add_tags"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields() // anything else (amount, date, delete...) is refused outright
	if err := dec.Decode(&a); err != nil {
		return nil, errors.New("only id, category_id, add_note and add_tags can be set")
	}
	tx, err := b.s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := db.New(tx)
	t, err := q.GetTransaction(ctx, db.GetTransactionParams{ID: a.ID, HouseholdID: b.hh})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("no such transaction")
	}
	if err != nil {
		return nil, err
	}
	if t.Date < b.from || t.Date > b.to {
		return nil, fmt.Errorf("only transactions dated %s to %s can be changed", b.from, b.to)
	}
	if !b.changed[t.ID] && len(b.changed) >= MaxUpdates {
		return nil, fmt.Errorf("at most %d transactions can be changed per email", MaxUpdates)
	}
	now := b.s.Now().Unix()
	logChange := func(field, old, new string) error {
		return q.InsertAIChange(ctx, db.InsertAIChangeParams{HouseholdID: b.hh, TransactionID: t.ID,
			EmailMessageID: sql.NullInt64{Int64: b.msg.ID, Valid: b.msg.ID != 0}, Field: field, OldValue: old, NewValue: new, CreatedAt: now})
	}
	var done, skipped []string

	if a.CategoryID != nil && (!t.CategoryID.Valid || t.CategoryID.Int64 != *a.CategoryID) {
		cat, err := q.GetCategory(ctx, db.GetCategoryParams{ID: *a.CategoryID, HouseholdID: b.hh})
		switch {
		case err != nil || cat.Archived == 1:
			skipped = append(skipped, "unknown category_id")
		case t.CategorySource == categorize.SourceUser:
			skipped = append(skipped, "category: the user chose it, so it stays")
		default:
			old := ""
			if t.CategoryID.Valid {
				old = strconv.FormatInt(t.CategoryID.Int64, 10) + ":" + t.CategorySource
			}
			if err := q.SetTransactionCategoryAI(ctx, db.SetTransactionCategoryAIParams{
				CategoryID: sql.NullInt64{Int64: cat.ID, Valid: true}, CategorySource: categorize.SourceAI, UpdatedAt: now, ID: t.ID, HouseholdID: b.hh,
			}); err != nil {
				return nil, err
			}
			if err := logChange("category", old, strconv.FormatInt(cat.ID, 10)); err != nil {
				return nil, err
			}
			done = append(done, "category → "+cat.Name)
		}
	}

	if note := cleanText(a.AddNote, maxNote); note != "" {
		note = "AI: " + note
		if strings.Contains(t.Notes, note) {
			skipped = append(skipped, "note already there")
		} else {
			notes := note
			if t.Notes != "" {
				notes = t.Notes + "\n" + note
			}
			if len(notes) > 2000 {
				skipped = append(skipped, "notes are full")
			} else {
				if err := q.SetTransactionNotes(ctx, db.SetTransactionNotesParams{Notes: notes, UpdatedAt: now, ID: t.ID, HouseholdID: b.hh}); err != nil {
					return nil, err
				}
				if err := logChange("notes", t.Notes, notes); err != nil {
					return nil, err
				}
				done = append(done, "note added")
			}
		}
	}

	if len(a.AddTags) > maxTags {
		skipped = append(skipped, fmt.Sprintf("only the first %d tags were used", maxTags))
		a.AddTags = a.AddTags[:maxTags]
	}
	have := map[string]bool{}
	if tags, err := q.ListTagsForTransactions(ctx, []int64{t.ID}); err == nil {
		for _, tg := range tags {
			have[strings.ToLower(tg.Name)] = true
		}
	}
	for _, name := range a.AddTags {
		name = cleanText(name, 30)
		if !tagRe.MatchString(name) {
			skipped = append(skipped, "tag "+strconv.Quote(name)+" isn't allowed (letters, numbers, spaces)")
			continue
		}
		if have[strings.ToLower(name)] {
			continue
		}
		tag, err := q.UpsertTag(ctx, db.UpsertTagParams{HouseholdID: b.hh, Name: name})
		if err != nil {
			return nil, err
		}
		if err := q.AddTransactionTag(ctx, db.AddTransactionTagParams{TransactionID: t.ID, TagID: tag.ID, HouseholdID: b.hh}); err != nil {
			return nil, err
		}
		if err := logChange("tags", "", strconv.FormatInt(tag.ID, 10)); err != nil {
			return nil, err
		}
		have[strings.ToLower(name)] = true
		done = append(done, "tag "+name)
	}

	if len(done) > 0 {
		// Anything the AI changed waits for the user's review.
		if t.NeedsReview == 0 {
			if err := q.SetTransactionNeedsReview(ctx, db.SetTransactionNeedsReviewParams{NeedsReview: 1, UpdatedAt: now, ID: t.ID, HouseholdID: b.hh}); err != nil {
				return nil, err
			}
			if err := logChange("needs_review", "0", "1"); err != nil {
				return nil, err
			}
		}
		b.changed[t.ID] = true
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]any{"updated": done, "skipped": skipped}, nil
}
