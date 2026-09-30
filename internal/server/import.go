package server

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/accounts"
	"viceroy/internal/categorize"
	"viceroy/internal/db"
	"viceroy/internal/monarch"
)

func (s *Server) importRoutes(r chi.Router) {
	r.Post("/import/monarch/preview", s.handleMonarchPreview)
	r.Post("/import/monarch", s.handleMonarchImport)
}

// maxImportBody allows several years of exports.
const maxImportBody = 32 << 20

type monarchFiles struct {
	Transactions string `json:"transactions"`
	Balances     string `json:"balances"`
}

// parse reads both files; msg is a user-facing error.
func (f monarchFiles) parse() (txns []monarch.Txn, bals []monarch.Balance, msg string) {
	if strings.TrimSpace(f.Transactions) == "" && strings.TrimSpace(f.Balances) == "" {
		return nil, nil, "Choose the Monarch transactions export (and optionally the balances export)."
	}
	var err error
	if strings.TrimSpace(f.Transactions) != "" {
		if txns, err = monarch.ParseTransactions(strings.NewReader(f.Transactions)); err != nil {
			return nil, nil, "Transactions file: " + err.Error()
		}
	}
	if strings.TrimSpace(f.Balances) != "" {
		if bals, err = monarch.ParseBalances(strings.NewReader(f.Balances)); err != nil {
			return nil, nil, "Balances file: " + err.Error()
		}
	}
	return txns, bals, ""
}

const monarchPrefix = "monarch:"

type importAccountDTO struct {
	monarch.Account
	AccountID *int64 `json:"account_id"` // suggested existing account, if any
}

type importCategoryDTO struct {
	monarch.CategoryCount
	CategoryID *int64 `json:"category_id"` // matching Viceroy category, if any
	GroupID    int64  `json:"group_id"`    // suggested group to create it in otherwise
	Icon       string `json:"icon"`
}

// POST /import/monarch/preview: what the files contain and a suggested mapping.
func (s *Server) handleMonarchPreview(w http.ResponseWriter, r *http.Request) {
	var in monarchFiles
	if !readJSONLimit(w, r, &in, maxImportBody) {
		return
	}
	txns, bals, msg := in.parse()
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	ctx, hh, q := r.Context(), HouseholdID(r), db.New(s.db)
	accts, err := q.ListAccounts(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	cats, groups, err := s.categoryIndex(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	seen, err := importedIDs(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	already := 0
	from, to := "", ""
	for _, t := range txns {
		if t.ID != "" && seen[monarchPrefix+t.ID] {
			already++
		}
		if from == "" || t.Date < from {
			from = t.Date
		}
		to = max(to, t.Date)
	}
	outAccts := []importAccountDTO{}
	for _, a := range monarch.Accounts(txns, bals) {
		d := importAccountDTO{Account: a}
		if id, ok := suggestAccount(a, accts); ok {
			d.AccountID = &id
		}
		outAccts = append(outAccts, d)
	}
	outCats := []importCategoryDTO{}
	for _, c := range monarch.Categories(txns) {
		d := importCategoryDTO{CategoryCount: c}
		if c.Name != "" {
			if id, ok := cats[strings.ToLower(c.Name)]; ok {
				d.CategoryID = &id
			} else {
				kind, icon := monarch.Suggest(c.Name)
				d.GroupID, d.Icon = groups[kind], icon
			}
		}
		outCats = append(outCats, d)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"transactions": len(txns), "balances": len(bals), "from": from, "to": to,
		"already_imported": already, "accounts": outAccts, "categories": outCats,
	})
}

// suggestAccount finds an existing account for a Monarch one: same last 4 and the same kind of
// account (cash, credit...), else the same name.
func suggestAccount(a monarch.Account, accts []db.Account) (int64, bool) {
	for _, pass := range []func(db.Account) bool{
		func(x db.Account) bool {
			return a.Mask != "" && (x.Mask == a.Mask || accounts.Mask(x.Name) == a.Mask) && accounts.Group(x.Type) == accounts.Group(a.Type)
		},
		func(x db.Account) bool { return accounts.Normalize(x.Name) == accounts.Normalize(a.Name) },
	} {
		for _, x := range accts {
			if x.Status != "closed" && x.Status != "ignored" && pass(x) {
				return x.ID, true
			}
		}
	}
	return 0, false
}

// categoryIndex maps lowercased category names to ids, and group kinds to their first group.
func (s *Server) categoryIndex(ctx context.Context, q *db.Queries, hh int64) (map[string]int64, map[string]int64, error) {
	cats, err := q.ListCategories(ctx, hh)
	if err != nil {
		return nil, nil, err
	}
	groups, err := q.ListCategoryGroups(ctx, hh)
	if err != nil {
		return nil, nil, err
	}
	byName := map[string]int64{}
	for _, c := range cats {
		if _, dup := byName[strings.ToLower(c.Name)]; !dup && c.Archived == 0 {
			byName[strings.ToLower(c.Name)] = c.ID
		}
	}
	byKind := map[string]int64{}
	for _, g := range groups {
		if _, ok := byKind[g.Kind]; !ok {
			byKind[g.Kind] = g.ID
		}
	}
	return byName, byKind, nil
}

func importedIDs(ctx context.Context, q *db.Queries, hh int64) (map[string]bool, error) {
	ids, err := q.ImportedExternalIDs(ctx, hh)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id.String] = true
	}
	return out, nil
}

type importAccountIn struct {
	Key       string `json:"key"`
	AccountID int64  `json:"account_id"` // map onto this account
	Create    *struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"create"` // or create a manual account
	// neither = skip this account
}

type importCategoryIn struct {
	Name       string `json:"name"`
	CategoryID int64  `json:"category_id"` // use this category
	Create     *struct {
		GroupID int64  `json:"group_id"`
		Icon    string `json:"icon"`
	} `json:"create"` // or create one with the Monarch name
	// neither = leave uncategorized
}

type importResult struct {
	AccountsCreated   int `json:"accounts_created"`
	CategoriesCreated int `json:"categories_created"`
	Imported          int `json:"imported"`
	Matched           int `json:"matched"`           // already in Viceroy (e.g. synced); details copied over
	AlreadyImported   int `json:"already_imported"`  // from an earlier import of the same export
	SkippedAccounts   int `json:"skipped"`           // rows of accounts left out
	Snapshots         int `json:"balance_snapshots"` // balance history days added
}

// matchWindow is how far apart (days) an imported row and an existing one may be dated.
const matchWindow = 3

// POST /import/monarch: imports with the chosen mapping, all or nothing. Importing the same
// export again skips rows it already imported.
func (s *Server) handleMonarchImport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		monarchFiles
		Accounts   []importAccountIn  `json:"accounts"`
		Categories []importCategoryIn `json:"categories"`
	}
	if !readJSONLimit(w, r, &in, maxImportBody) {
		return
	}
	txns, bals, msg := in.parse()
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	bad := func(m string) { writeError(w, http.StatusBadRequest, m) }
	ctx, hh := r.Context(), HouseholdID(r)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	q := db.New(tx)
	now := time.Now().Unix()
	var res importResult

	summary := map[string]monarch.Account{}
	for _, a := range monarch.Accounts(txns, bals) {
		summary[a.Key] = a
	}
	// Accounts.
	target := map[string]int64{}
	created := map[int64]bool{}
	for _, m := range in.Accounts {
		sum, ok := summary[m.Key]
		switch {
		case !ok:
			bad("Unknown account in mapping: " + m.Key)
			return
		case m.AccountID != 0:
			if _, err := q.GetAccount(ctx, db.GetAccountParams{ID: m.AccountID, HouseholdID: hh}); err != nil {
				bad("Unknown account for " + m.Key + ".")
				return
			}
			target[m.Key] = m.AccountID
		case m.Create != nil:
			name := strings.TrimSpace(m.Create.Name)
			if name == "" || !accounts.ValidType(m.Create.Type) {
				bad("Give " + m.Key + " a name and a valid type.")
				return
			}
			var at sql.NullInt64
			if d, err := time.ParseInLocation(time.DateOnly, sum.LatestDate, time.Local); err == nil {
				at = sql.NullInt64{Int64: d.Unix(), Valid: true}
			}
			a, err := q.CreateAccount(ctx, db.CreateAccountParams{
				HouseholdID: hh, InstitutionName: "", ProviderName: m.Key, Name: name, Mask: sum.Mask, Type: m.Create.Type,
				Currency: "USD", BalanceCents: sum.LatestBalance, BalanceAt: at, Status: "active", IsManual: 1,
				CreatedAt: now, UpdatedAt: now,
			})
			if err != nil {
				s.internalError(w, err)
				return
			}
			target[m.Key] = a.ID
			created[a.ID] = true
			res.AccountsCreated++
		}
	}

	// Categories. "Create" reuses a category of the same name, so importing twice is safe.
	existingCats, _, err := s.categoryIndex(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	catOf := map[string]sql.NullInt64{}
	for _, m := range in.Categories {
		switch {
		case m.CategoryID != 0:
			if _, err := q.GetCategory(ctx, db.GetCategoryParams{ID: m.CategoryID, HouseholdID: hh}); err != nil {
				bad("Unknown category for " + m.Name + ".")
				return
			}
			catOf[m.Name] = sql.NullInt64{Int64: m.CategoryID, Valid: true}
		case m.Create != nil && existingCats[strings.ToLower(strings.TrimSpace(m.Name))] != 0:
			catOf[m.Name] = sql.NullInt64{Int64: existingCats[strings.ToLower(strings.TrimSpace(m.Name))], Valid: true}
		case m.Create != nil && strings.TrimSpace(m.Name) != "":
			g, err := s.groupOf(ctx, q, hh, m.Create.GroupID)
			if err != nil {
				bad("Pick a group for the new category " + m.Name + ".")
				return
			}
			c, err := q.CreateCategory(ctx, db.CreateCategoryParams{HouseholdID: hh, GroupID: g, Name: strings.TrimSpace(m.Name), Icon: m.Create.Icon, Sort: 1000})
			if err != nil {
				s.internalError(w, err)
				return
			}
			catOf[m.Name] = sql.NullInt64{Int64: c.ID, Valid: true}
			res.CategoriesCreated++
		}
	}

	// Transactions.
	seen, err := importedIDs(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	matched := map[int64]bool{}
	tags := map[string]int64{}
	for _, t := range txns {
		acct, ok := target[t.Account]
		if !ok {
			res.SkippedAccounts++
			continue
		}
		ext := sql.NullString{String: monarchPrefix + t.ID, Valid: t.ID != ""}
		if ext.Valid && seen[ext.String] {
			res.AlreadyImported++
			continue
		}
		cat := catOf[t.Category]
		src := ""
		if cat.Valid {
			src = categorize.SourceUser
		}
		var id int64
		if !created[acct] {
			id, err = s.enrichMatch(ctx, q, acct, t, cat, matched, now)
			if err != nil {
				s.internalError(w, err)
				return
			}
		}
		if id != 0 {
			res.Matched++
		} else {
			var merchant sql.NullInt64
			if t.Merchant != "" {
				m, err := q.UpsertMerchant(ctx, db.UpsertMerchantParams{HouseholdID: hh, Name: t.Merchant, Normalized: categorize.Key(t.Merchant)})
				if err != nil {
					s.internalError(w, err)
					return
				}
				merchant = sql.NullInt64{Int64: m.ID, Valid: true}
			}
			desc := t.Statement
			if desc == "" {
				desc = t.Merchant
			}
			id, err = q.InsertImportedTransaction(ctx, db.InsertImportedTransactionParams{
				HouseholdID: hh, AccountID: acct, ExternalID: ext, Date: t.Date, AmountCents: t.Amount,
				Description: desc, Payee: t.Merchant, MerchantID: merchant, CategoryID: cat, CategorySource: src,
				Notes: t.Notes, NeedsReview: b2i(t.NeedsReview), CreatedAt: now, UpdatedAt: now,
			})
			if err != nil {
				s.internalError(w, err)
				return
			}
			seen[ext.String] = ext.Valid
			res.Imported++
		}
		for _, name := range t.Tags {
			tagID, ok := tags[name]
			if !ok {
				tg, err := q.UpsertTag(ctx, db.UpsertTagParams{HouseholdID: hh, Name: name})
				if err != nil {
					s.internalError(w, err)
					return
				}
				tagID, tags[name] = tg.ID, tg.ID
			}
			if err := q.AddTransactionTag(ctx, db.AddTransactionTagParams{TransactionID: id, TagID: tagID, HouseholdID: hh}); err != nil {
				s.internalError(w, err)
				return
			}
		}
	}

	// Balance history. Existing snapshots (e.g. from syncs) win over imported ones.
	for _, b := range bals {
		acct, ok := target[b.Account]
		if !ok {
			continue
		}
		if err := q.InsertBalanceSnapshotIfMissing(ctx, db.InsertBalanceSnapshotIfMissingParams{AccountID: acct, Date: b.Date, BalanceCents: b.Amount}); err != nil {
			s.internalError(w, err)
			return
		}
		res.Snapshots++
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	s.log.Info("monarch import", "household", hh, "imported", res.Imported, "matched", res.Matched, "accounts", res.AccountsCreated)
	writeJSON(w, http.StatusOK, res)
}

// enrichMatch looks for a transaction already in the account that this imported row
// duplicates (same amount, within matchWindow days, not matched yet). On a match it copies
// Monarch's category (unless the user already chose one), notes and "needs review", and returns
// its id; 0 = no match.
func (s *Server) enrichMatch(ctx context.Context, q *db.Queries, acct int64, t monarch.Txn, cat sql.NullInt64, matched map[int64]bool, now int64) (int64, error) {
	d, err := time.Parse(time.DateOnly, t.Date)
	if err != nil {
		return 0, err
	}
	cands, err := q.ImportMatchCandidates(ctx, db.ImportMatchCandidatesParams{
		AccountID: acct, AmountCents: t.Amount,
		FromDate: d.AddDate(0, 0, -matchWindow).Format(time.DateOnly), ToDate: d.AddDate(0, 0, matchWindow).Format(time.DateOnly),
	})
	if err != nil {
		return 0, err
	}
	best, bestGap := -1, 0
	for i, c := range cands {
		if matched[c.ID] {
			continue
		}
		cd, _ := time.Parse(time.DateOnly, c.Date)
		gap := int(cd.Sub(d).Abs().Hours() / 24)
		if best < 0 || gap < bestGap {
			best, bestGap = i, gap
		}
	}
	if best < 0 {
		return 0, nil
	}
	c := cands[best]
	matched[c.ID] = true
	catID, src := c.CategoryID, c.CategorySource
	if cat.Valid && src != categorize.SourceUser {
		catID, src = cat, categorize.SourceUser
	}
	notes := c.Notes
	if notes == "" {
		notes = t.Notes
	}
	return c.ID, q.EnrichImportedMatch(ctx, db.EnrichImportedMatchParams{
		CategoryID: catID, CategorySource: src, Notes: notes, NeedsReview: b2i(t.NeedsReview || (c.NeedsReview == 1 && !catID.Valid)), UpdatedAt: now, ID: c.ID,
	})
}

// groupOf checks a category group belongs to the household.
func (s *Server) groupOf(ctx context.Context, q *db.Queries, hh, id int64) (int64, error) {
	groups, err := q.ListCategoryGroups(ctx, hh)
	if err != nil {
		return 0, err
	}
	for _, g := range groups {
		if g.ID == id {
			return id, nil
		}
	}
	return 0, sql.ErrNoRows
}
