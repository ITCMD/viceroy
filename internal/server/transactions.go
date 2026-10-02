package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/accounts"
	"viceroy/internal/categorize"
	"viceroy/internal/db"
	"viceroy/internal/linking"
	"viceroy/internal/money"
)

func (s *Server) transactionRoutes(r chi.Router) {
	r.Get("/transactions", s.handleListTransactions)
	r.Post("/transactions", s.handleCreateTransaction)
	r.Post("/transactions/ai-categorize", s.handleAICategorize)
	r.Get("/transactions/{id}", s.handleGetTransaction)
	r.Patch("/transactions/{id}", s.handleUpdateTransaction)
	r.Delete("/transactions/{id}", s.handleDeleteTransaction)
	r.Get("/transactions/{id}/similar", s.handleSimilarTransactions)
	r.Get("/transactions/{id}/link-candidates", s.handleLinkCandidates)
	r.Post("/transactions/{id}/link", s.handleLinkTransaction)
	r.Post("/transactions/{id}/unlink", s.handleUnlinkTransaction)
	r.Post("/transactions/{id}/ai-undo", s.handleUndoAIChanges)
	r.Get("/categories", s.handleListCategories)
	r.Put("/categories/layout", s.handleCategoryLayout)
	r.Get("/tags", s.handleListTags)
	r.Get("/rules", s.handleListRules)
	r.Post("/rules", s.handleCreateRule)
	r.Patch("/rules/{id}", s.handleUpdateRule)
	r.Delete("/rules/{id}", s.handleDeleteRule)
	r.Post("/rules/{id}/apply", s.handleApplyRule)
}

type tagDTO struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type txnDTO struct {
	ID             int64    `json:"id"`
	AccountID      int64    `json:"account_id"`
	AccountName    string   `json:"account_name"`
	AccountMask    string   `json:"account_mask"`
	Date           string   `json:"date"`
	AmountCents    int64    `json:"amount_cents"`
	Description    string   `json:"description"`
	Merchant       string   `json:"merchant"`
	BankMerchant   string   `json:"bank_merchant"` // merchant cleaned from the bank text, before any rename (rules match it)
	MerchantID     *int64   `json:"merchant_id"`
	CategoryID     *int64   `json:"category_id"`
	CategoryName   string   `json:"category_name"`
	CategoryIcon   string   `json:"category_icon"`
	CategorySource string   `json:"category_source"`
	Notes          string   `json:"notes"`
	Hidden         bool     `json:"hidden"`
	NeedsReview    bool     `json:"needs_review"`
	Pending        bool     `json:"pending"`
	Provisional    bool     `json:"provisional"`
	Source         string   `json:"source"`
	HasLinked      bool     `json:"has_linked"`
	LinkedSource   string   `json:"linked_source"` // source of the pending/email entry linked to this row
	LinkedTxnID    *int64   `json:"linked_txn_id"`
	GoalID         *int64   `json:"goal_id"`
	Tags           []tagDTO `json:"tags"`
}

func toTxnDTO(t db.ListTransactionsRow) txnDTO {
	merchant := t.MerchantName
	if merchant == "" {
		merchant = t.Description
	}
	bank, _ := categorize.Merchant(t.Payee, t.Description)
	return txnDTO{
		BankMerchant: bank,
		ID:           t.ID, AccountID: t.AccountID, AccountName: t.AccountName, AccountMask: t.AccountMask,
		Date: t.Date, AmountCents: t.AmountCents, Description: t.Description, Merchant: merchant,
		MerchantID: ptr(t.MerchantID), CategoryID: ptr(t.CategoryID), CategoryName: t.CategoryName,
		CategoryIcon: t.CategoryIcon, CategorySource: t.CategorySource, Notes: t.Notes,
		Hidden: t.Hidden == 1, NeedsReview: t.NeedsReview == 1, Pending: t.Pending == 1,
		Provisional: t.Provisional == 1, Source: t.Source, HasLinked: t.HasLinked, LinkedSource: t.LinkedSource,
		LinkedTxnID: ptr(t.LinkedTxnID), GoalID: ptr(t.GoalID), Tags: []tagDTO{},
	}
}

// attachTags fills in each transaction's tags with one query.
func attachTags(ctx context.Context, q *db.Queries, list []txnDTO) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]int64, len(list))
	idx := map[int64]int{}
	for i, t := range list {
		ids[i], idx[t.ID] = t.ID, i
	}
	rows, err := q.ListTagsForTransactions(ctx, ids)
	if err != nil {
		return err
	}
	for _, r := range rows {
		i := idx[r.TransactionID]
		list[i].Tags = append(list[i].Tags, tagDTO{ID: r.ID, Name: r.Name, Color: r.Color})
	}
	return nil
}

func queryInt(r *http.Request, key string) (int64, bool) {
	v, err := strconv.ParseInt(r.URL.Query().Get(key), 10, 64)
	return v, err == nil
}

func (s *Server) handleListTransactions(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	p := db.ListTransactionsParams{
		HouseholdID: HouseholdID(r), Q: strings.TrimSpace(qs.Get("q")),
		Uncategorized: b2i(qs.Get("uncategorized") == "1"), NeedsReview: b2i(qs.Get("review") == "1"),
		IncludeHidden: b2i(qs.Get("hidden") == "1"), BeforeDate: "", Lim: 100,
	}
	if v, ok := queryInt(r, "account"); ok {
		p.AccountID = v
	}
	if v, ok := queryInt(r, "category"); ok {
		p.CategoryID = v
	}
	if v, ok := queryInt(r, "limit"); ok && v > 0 && v <= 500 {
		p.Lim = v
	}
	if c := qs.Get("cursor"); c != "" {
		date, id, _ := strings.Cut(c, "|")
		p.BeforeDate = date
		p.BeforeID, _ = strconv.ParseInt(id, 10, 64)
	}
	q := db.New(s.db)
	rows, err := q.ListTransactions(r.Context(), p)
	if err != nil {
		s.internalError(w, err)
		return
	}
	list := make([]txnDTO, len(rows))
	for i, row := range rows {
		list[i] = toTxnDTO(row)
	}
	if err := attachTags(r.Context(), q, list); err != nil {
		s.internalError(w, err)
		return
	}
	next := ""
	if int64(len(rows)) == p.Lim {
		last := rows[len(rows)-1]
		next = fmt.Sprintf("%s|%d", last.Date, last.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"transactions": list, "next_cursor": next})
}

func (s *Server) txnView(ctx context.Context, hh, id int64) (txnDTO, error) {
	q := db.New(s.db)
	v, err := q.GetTransactionView(ctx, db.GetTransactionViewParams{ID: id, HouseholdID: hh})
	if err != nil {
		return txnDTO{}, err
	}
	list := []txnDTO{toTxnDTO(db.ListTransactionsRow(v))}
	err = attachTags(ctx, q, list)
	return list[0], err
}

func txnID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id
}

func (s *Server) handleGetTransaction(w http.ResponseWriter, r *http.Request) {
	ctx, hh := r.Context(), HouseholdID(r)
	t, err := s.txnView(ctx, hh, txnID(r))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Transaction not found.")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	// Linked pending entries (on a posted row), or the posted row this entry is linked to.
	linked := []txnDTO{}
	q := db.New(s.db)
	provs, err := q.ListLinkedProvisional(ctx, sql.NullInt64{Int64: t.ID, Valid: true})
	if err != nil {
		s.internalError(w, err)
		return
	}
	ids := []int64{}
	for _, p := range provs {
		ids = append(ids, p.ID)
	}
	if t.LinkedTxnID != nil {
		ids = append(ids, *t.LinkedTxnID)
	}
	for _, id := range ids {
		v, err := s.txnView(ctx, hh, id)
		if err != nil {
			s.internalError(w, err)
			return
		}
		linked = append(linked, v)
	}
	// The alert email an email transaction came from (bodies are shown via /email/messages/{id}).
	// On a posted row confirmed by SimpleFIN, the email of the alert it was linked to.
	var alert any
	alertTxn := int64(0)
	if t.Source == "email" {
		alertTxn = t.ID
	}
	for _, p := range provs {
		if p.Source == "email" && alertTxn == 0 {
			alertTxn = p.ID
		}
	}
	if alertTxn != 0 {
		if m, err := q.GetEmailMessageForTransaction(ctx, sql.NullInt64{Int64: alertTxn, Valid: true}); err == nil {
			alert = m
		} else if !errors.Is(err, sql.ErrNoRows) {
			s.internalError(w, err)
			return
		}
	}
	changes, err := s.aiChanges(ctx, hh, t.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	var rec any
	if sc, err := s.recurringSchedule(ctx, hh); err != nil {
		s.internalError(w, err)
		return
	} else if it, ok := sc.TrackedFor(t.ID); ok {
		rec = map[string]any{"id": it.ID, "name": it.Name, "cadence": it.Cadence, "next_date": it.NextDate}
	}
	writeJSON(w, http.StatusOK, map[string]any{"transaction": t, "linked": linked, "email": alert, "ai_changes": changes, "recurring": rec})
}

type createTxnIn struct {
	AccountID   int64    `json:"account_id"`
	Date        string   `json:"date"`
	Amount      string   `json:"amount"` // negative = money out
	Description string   `json:"description"`
	CategoryID  *int64   `json:"category_id"`
	Notes       string   `json:"notes"`
	Tags        []string `json:"tags"`
	Pending     bool     `json:"pending"`
	Force       bool     `json:"force"` // create even though it looks like a duplicate
}

func (s *Server) handleCreateTransaction(w http.ResponseWriter, r *http.Request) {
	var in createTxnIn
	if !readJSON(w, r, &in) {
		return
	}
	ctx, hh := r.Context(), HouseholdID(r)
	in.Description = strings.TrimSpace(in.Description)
	if in.Description == "" {
		writeError(w, http.StatusBadRequest, "Enter a merchant or description.")
		return
	}
	if _, err := time.Parse(time.DateOnly, in.Date); err != nil {
		writeError(w, http.StatusBadRequest, "Enter a valid date.")
		return
	}
	amt, err := money.ParseCents(in.Amount)
	if err != nil || amt == 0 {
		writeError(w, http.StatusBadRequest, "Enter an amount like -12.34.")
		return
	}
	q := db.New(s.db)
	if _, err := q.GetAccount(ctx, db.GetAccountParams{ID: in.AccountID, HouseholdID: hh}); err != nil {
		writeError(w, http.StatusBadRequest, "Choose an account.")
		return
	}
	if in.CategoryID != nil {
		if _, err := q.GetCategory(ctx, db.GetCategoryParams{ID: *in.CategoryID, HouseholdID: hh}); err != nil {
			writeError(w, http.StatusBadRequest, "Unknown category.")
			return
		}
	}
	if in.Pending && !in.Force {
		dups, err := linking.Duplicates(ctx, q, in.AccountID, amt, in.Date)
		if err != nil {
			s.internalError(w, err)
			return
		}
		if len(dups) > 0 {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":      "A matching transaction has already posted to this account.",
				"duplicates": dups,
			})
			return
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	q = db.New(tx)
	now := time.Now().Unix()
	id, err := q.InsertManualTransaction(ctx, db.InsertManualTransactionParams{
		HouseholdID: hh, AccountID: in.AccountID, Date: in.Date, AmountCents: amt, Description: in.Description,
		Pending: b2i(in.Pending), Provisional: b2i(in.Pending), Notes: strings.TrimSpace(in.Notes),
		OwnerUserID: sql.NullInt64{Int64: CurrentUser(r).ID, Valid: true}, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	cat, err := categorize.New(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	res, err := cat.Apply(ctx, categorize.Txn{ID: id, HouseholdID: hh, AccountID: in.AccountID, AmountCents: amt, Date: in.Date, Description: in.Description})
	if err != nil {
		s.internalError(w, err)
		return
	}
	if in.CategoryID != nil {
		if err := q.SetTransactionAuto(ctx, db.SetTransactionAutoParams{
			MerchantID: res.MerchantID, CategoryID: sql.NullInt64{Int64: *in.CategoryID, Valid: true},
			CategorySource: categorize.SourceUser, NeedsReview: 0, Hidden: b2i(res.Hidden), ID: id,
		}); err != nil {
			s.internalError(w, err)
			return
		}
	}
	if err := setTags(ctx, q, hh, id, in.Tags, true); err != nil {
		s.internalError(w, err)
		return
	}
	if err := accounts.AdjustManualBalance(ctx, q, in.AccountID, amt); err != nil {
		s.internalError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	t, err := s.txnView(ctx, hh, id)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// setTags replaces a transaction's tags with names (creating missing tags). keep leaves
// existing tags in place and only adds.
func setTags(ctx context.Context, q *db.Queries, hh, id int64, names []string, keep bool) error {
	if names == nil {
		return nil
	}
	if !keep {
		if err := q.ClearTransactionTags(ctx, id); err != nil {
			return err
		}
	}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		tag, err := q.UpsertTag(ctx, db.UpsertTagParams{HouseholdID: hh, Name: n})
		if err != nil {
			return err
		}
		if err := q.AddTransactionTag(ctx, db.AddTransactionTagParams{TransactionID: id, TagID: tag.ID, HouseholdID: hh}); err != nil {
			return err
		}
	}
	return nil
}

type updateTxnIn struct {
	CategoryID  json.RawMessage `json:"category_id"` // number, or null for uncategorized
	Merchant    *string         `json:"merchant"`
	Notes       *string         `json:"notes"`
	Hidden      *bool           `json:"hidden"`
	NeedsReview *bool           `json:"needs_review"`
	Tags        []string        `json:"tags"`
	GoalID      json.RawMessage `json:"goal_id"` // number, or null for none
	// Manual transactions only.
	Date        *string `json:"date"`
	Amount      *string `json:"amount"`
	Description *string `json:"description"`
}

func (s *Server) handleUpdateTransaction(w http.ResponseWriter, r *http.Request) {
	var in updateTxnIn
	if !readJSON(w, r, &in) {
		return
	}
	ctx, hh := r.Context(), HouseholdID(r)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	q := db.New(tx)
	t, err := q.GetTransaction(ctx, db.GetTransactionParams{ID: txnID(r), HouseholdID: hh})
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Transaction not found.")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	bad := func(msg string) { writeError(w, http.StatusBadRequest, msg) }
	oldAmount := t.AmountCents

	if len(in.CategoryID) > 0 {
		if string(in.CategoryID) == "null" {
			t.CategoryID = sql.NullInt64{}
		} else {
			var id int64
			if json.Unmarshal(in.CategoryID, &id) != nil {
				bad("Unknown category.")
				return
			}
			if _, err := q.GetCategory(ctx, db.GetCategoryParams{ID: id, HouseholdID: hh}); err != nil {
				bad("Unknown category.")
				return
			}
			t.CategoryID = sql.NullInt64{Int64: id, Valid: true}
		}
		t.CategorySource = categorize.SourceUser
		t.NeedsReview = 0
	}
	if in.Merchant != nil {
		name := strings.TrimSpace(*in.Merchant)
		key := categorize.Key(name)
		if key == "" {
			bad("Enter a merchant name.")
			return
		}
		m, err := q.UpsertMerchant(ctx, db.UpsertMerchantParams{HouseholdID: hh, Name: name, Normalized: key})
		if err != nil {
			s.internalError(w, err)
			return
		}
		t.MerchantID = sql.NullInt64{Int64: m.ID, Valid: true}
	}
	if len(in.GoalID) > 0 {
		t.GoalID = sql.NullInt64{}
		if string(in.GoalID) != "null" {
			var id int64
			if json.Unmarshal(in.GoalID, &id) != nil {
				bad("Unknown goal.")
				return
			}
			if _, err := q.GetGoal(ctx, db.GetGoalParams{ID: id, HouseholdID: hh}); err != nil {
				bad("Unknown goal.")
				return
			}
			t.GoalID = sql.NullInt64{Int64: id, Valid: true}
		}
		if err := q.SetTransactionGoal(ctx, db.SetTransactionGoalParams{GoalID: t.GoalID, ID: t.ID, HouseholdID: hh}); err != nil {
			s.internalError(w, err)
			return
		}
	}
	if in.Notes != nil {
		t.Notes = strings.TrimSpace(*in.Notes)
	}
	if in.Hidden != nil {
		t.Hidden = b2i(*in.Hidden)
	}
	if in.NeedsReview != nil {
		t.NeedsReview = b2i(*in.NeedsReview)
	}
	if in.Date != nil || in.Amount != nil || in.Description != nil {
		if t.Source != "manual" {
			bad("Only manual transactions can change date, amount or description.")
			return
		}
		if in.Date != nil {
			if _, err := time.Parse(time.DateOnly, *in.Date); err != nil {
				bad("Enter a valid date.")
				return
			}
			t.Date = *in.Date
		}
		if in.Amount != nil {
			amt, err := money.ParseCents(*in.Amount)
			if err != nil || amt == 0 {
				bad("Enter an amount like -12.34.")
				return
			}
			t.AmountCents = amt
		}
		if in.Description != nil {
			if d := strings.TrimSpace(*in.Description); d != "" {
				t.Description = d
			}
		}
	}
	if err := q.UpdateTransactionUser(ctx, db.UpdateTransactionUserParams{
		Date: t.Date, AmountCents: t.AmountCents, Description: t.Description, MerchantID: t.MerchantID,
		CategoryID: t.CategoryID, CategorySource: t.CategorySource, Notes: t.Notes, Hidden: t.Hidden,
		NeedsReview: t.NeedsReview, UpdatedAt: time.Now().Unix(), ID: t.ID, HouseholdID: hh,
	}); err != nil {
		s.internalError(w, err)
		return
	}
	if err := setTags(ctx, q, hh, t.ID, in.Tags, false); err != nil {
		s.internalError(w, err)
		return
	}
	if err := accounts.AdjustManualBalance(ctx, q, t.AccountID, t.AmountCents-oldAmount); err != nil {
		s.internalError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	v, err := s.txnView(ctx, hh, t.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleDeleteTransaction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	q := db.New(tx)
	t, err := q.GetTransaction(ctx, db.GetTransactionParams{ID: txnID(r), HouseholdID: HouseholdID(r)})
	if err != nil {
		writeError(w, http.StatusNotFound, "Transaction not found.")
		return
	}
	if t.Source != "manual" {
		writeError(w, http.StatusBadRequest, "Synced transactions can't be deleted; hide them instead.")
		return
	}
	if err := q.DeleteManualTransaction(ctx, db.DeleteManualTransactionParams{ID: t.ID, HouseholdID: t.HouseholdID}); err != nil {
		s.internalError(w, err)
		return
	}
	if err := accounts.AdjustManualBalance(ctx, q, t.AccountID, -t.AmountCents); err != nil {
		s.internalError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSimilarTransactions(w http.ResponseWriter, r *http.Request) {
	q := db.New(s.db)
	t, err := q.GetTransaction(r.Context(), db.GetTransactionParams{ID: txnID(r), HouseholdID: HouseholdID(r)})
	if err != nil {
		writeError(w, http.StatusNotFound, "Transaction not found.")
		return
	}
	rows := []db.ListMerchantTransactionsRow{}
	if t.MerchantID.Valid {
		rows, err = q.ListMerchantTransactions(r.Context(), db.ListMerchantTransactionsParams{HouseholdID: t.HouseholdID, MerchantID: t.MerchantID, ID: t.ID})
		if err != nil {
			s.internalError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"transactions": rows})
}

func (s *Server) handleLinkCandidates(w http.ResponseWriter, r *http.Request) {
	q := db.New(s.db)
	t, err := q.GetTransaction(r.Context(), db.GetTransactionParams{ID: txnID(r), HouseholdID: HouseholdID(r)})
	if err != nil {
		writeError(w, http.StatusNotFound, "Transaction not found.")
		return
	}
	d, _ := time.Parse(time.DateOnly, t.Date)
	rows, err := q.ListPostedForLink(r.Context(), db.ListPostedForLinkParams{
		AccountID: t.AccountID, DateLo: d.AddDate(0, 0, -7).Format(time.DateOnly), DateHi: d.AddDate(0, 0, 30).Format(time.DateOnly),
		AmountCents: t.AmountCents,
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transactions": rows})
}

func (s *Server) linkResult(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "Transaction not found.")
	case errors.Is(err, linking.ErrNotProvisional), errors.Is(err, linking.ErrAlreadyLinked), errors.Is(err, linking.ErrNotPosted):
		msg := err.Error()
		writeError(w, http.StatusBadRequest, strings.ToUpper(msg[:1])+msg[1:]+".")
	case err != nil:
		s.internalError(w, err)
	default:
		t, err := s.txnView(r.Context(), HouseholdID(r), txnID(r))
		if err != nil {
			s.internalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, t)
	}
}

func (s *Server) handleLinkTransaction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PostedID int64 `json:"posted_id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	s.inTx(w, r, func(q *db.Queries) error {
		return linking.Link(r.Context(), q, HouseholdID(r), txnID(r), in.PostedID, time.Now())
	})
}

func (s *Server) handleUnlinkTransaction(w http.ResponseWriter, r *http.Request) {
	s.inTx(w, r, func(q *db.Queries) error {
		return linking.Unlink(r.Context(), q, HouseholdID(r), txnID(r))
	})
}

// inTx runs fn in a transaction and replies with the transaction in the URL.
func (s *Server) inTx(w http.ResponseWriter, r *http.Request, fn func(*db.Queries) error) {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	if err = fn(db.New(tx)); err == nil {
		err = tx.Commit()
	}
	s.linkResult(w, r, err)
}

// ---- categories & tags ----

type categoryDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon"`
}

type categoryGroupDTO struct {
	ID         int64         `json:"id"`
	Name       string        `json:"name"`
	Kind       string        `json:"kind"`
	Categories []categoryDTO `json:"categories"`
}

func (s *Server) handleListCategories(w http.ResponseWriter, r *http.Request) {
	q := db.New(s.db)
	groups, err := q.ListCategoryGroups(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	cats, err := q.ListCategories(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := make([]categoryGroupDTO, len(groups))
	idx := map[int64]int{}
	for i, g := range groups {
		out[i] = categoryGroupDTO{ID: g.ID, Name: g.Name, Kind: g.Kind, Categories: []categoryDTO{}}
		idx[g.ID] = i
	}
	for _, c := range cats {
		if c.Archived == 1 {
			continue
		}
		i := idx[c.GroupID]
		out[i].Categories = append(out[i].Categories, categoryDTO{ID: c.ID, Name: c.Name, Icon: c.Icon})
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": out})
}

func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	tags, err := db.New(s.db).ListTags(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := make([]tagDTO, len(tags))
	for i, t := range tags {
		out[i] = tagDTO{ID: t.ID, Name: t.Name, Color: t.Color}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": out})
}
