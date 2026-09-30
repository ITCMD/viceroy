package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"viceroy/internal/categorize"
	"viceroy/internal/db"
	"viceroy/internal/money"
)

type ruleDTO struct {
	ID            int64  `json:"id"`
	Priority      int64  `json:"priority"`
	MatchField    string `json:"match_field"`
	MatchOp       string `json:"match_op"`
	MatchValue    string `json:"match_value"`
	AccountID     *int64 `json:"account_id"`
	AmountMin     *int64 `json:"amount_min"`
	AmountMax     *int64 `json:"amount_max"`
	SetCategoryID *int64 `json:"set_category_id"`
	SetMerchant   string `json:"set_merchant"`
	AddTagID      *int64 `json:"add_tag_id"`
	SetHidden     bool   `json:"set_hidden"`
}

func toRuleDTO(r db.Rule) ruleDTO {
	return ruleDTO{
		ID: r.ID, Priority: r.Priority, MatchField: r.MatchField, MatchOp: r.MatchOp, MatchValue: r.MatchValue,
		AccountID: ptr(r.AccountID), AmountMin: ptr(r.AmountMin), AmountMax: ptr(r.AmountMax),
		SetCategoryID: ptr(r.SetCategoryID), SetMerchant: r.SetMerchant, AddTagID: ptr(r.AddTagID), SetHidden: r.SetHidden == 1,
	}
}

// ruleIn is the create/update body. Amounts are decimal strings ("" = no bound); the tag is
// given by name and created if needed.
type ruleIn struct {
	Priority      int64  `json:"priority"`
	MatchField    string `json:"match_field"`
	MatchOp       string `json:"match_op"`
	MatchValue    string `json:"match_value"`
	AccountID     *int64 `json:"account_id"`
	AmountMin     string `json:"amount_min"`
	AmountMax     string `json:"amount_max"`
	SetCategoryID *int64 `json:"set_category_id"`
	SetMerchant   string `json:"set_merchant"`
	AddTag        string `json:"add_tag"`
	SetHidden     bool   `json:"set_hidden"`
}

// validated turns the body into a rule row, or returns a user-facing error.
func (s *Server) validated(r *http.Request, q *db.Queries, in ruleIn) (db.Rule, error) {
	ctx, hh := r.Context(), HouseholdID(r)
	out := db.Rule{HouseholdID: hh, Priority: in.Priority, SetMerchant: strings.TrimSpace(in.SetMerchant), SetHidden: b2i(in.SetHidden)}
	out.MatchValue = strings.TrimSpace(in.MatchValue)
	if out.MatchValue == "" {
		return out, errors.New("Enter text to match.")
	}
	switch in.MatchField {
	case "merchant", "description":
		out.MatchField = in.MatchField
	default:
		return out, errors.New("Match on merchant or original statement.")
	}
	switch in.MatchOp {
	case "contains", "equals", "starts_with":
		out.MatchOp = in.MatchOp
	default:
		return out, errors.New("Choose how to match.")
	}
	if in.AccountID != nil {
		if _, err := q.GetAccount(ctx, db.GetAccountParams{ID: *in.AccountID, HouseholdID: hh}); err != nil {
			return out, errors.New("Unknown account.")
		}
		out.AccountID = sql.NullInt64{Int64: *in.AccountID, Valid: true}
	}
	for _, b := range []struct {
		in  string
		out *sql.NullInt64
	}{{in.AmountMin, &out.AmountMin}, {in.AmountMax, &out.AmountMax}} {
		if strings.TrimSpace(b.in) == "" {
			continue
		}
		c, err := money.ParseCents(b.in)
		if err != nil {
			return out, errors.New("Amounts must look like 12.34.")
		}
		if c < 0 {
			c = -c
		}
		*b.out = sql.NullInt64{Int64: c, Valid: true}
	}
	if in.SetCategoryID != nil {
		if _, err := q.GetCategory(ctx, db.GetCategoryParams{ID: *in.SetCategoryID, HouseholdID: hh}); err != nil {
			return out, errors.New("Unknown category.")
		}
		out.SetCategoryID = sql.NullInt64{Int64: *in.SetCategoryID, Valid: true}
	}
	if out.SetMerchant != "" && categorize.Key(out.SetMerchant) == "" {
		return out, errors.New("Enter a merchant name with letters or digits.")
	}
	if t := strings.TrimSpace(in.AddTag); t != "" {
		tag, err := q.UpsertTag(ctx, db.UpsertTagParams{HouseholdID: hh, Name: t})
		if err != nil {
			return out, err
		}
		out.AddTagID = sql.NullInt64{Int64: tag.ID, Valid: true}
	}
	if !out.SetCategoryID.Valid && out.SetMerchant == "" && !out.AddTagID.Valid && out.SetHidden == 0 {
		return out, errors.New("Choose at least one action.")
	}
	return out, nil
}

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	rules, err := db.New(s.db).ListRules(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := make([]ruleDTO, len(rules))
	for i, rl := range rules {
		out[i] = toRuleDTO(rl)
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": out})
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	var in ruleIn
	if !readJSON(w, r, &in) {
		return
	}
	q := db.New(s.db)
	v, err := s.validated(r, q, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rl, err := q.CreateRule(r.Context(), db.CreateRuleParams{
		HouseholdID: v.HouseholdID, Priority: v.Priority, MatchField: v.MatchField, MatchOp: v.MatchOp,
		MatchValue: v.MatchValue, AccountID: v.AccountID, AmountMin: v.AmountMin, AmountMax: v.AmountMax,
		SetCategoryID: v.SetCategoryID, SetMerchant: v.SetMerchant, AddTagID: v.AddTagID, SetHidden: v.SetHidden,
		CreatedAt: time.Now().Unix(),
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toRuleDTO(rl))
}

func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	var in ruleIn
	if !readJSON(w, r, &in) {
		return
	}
	q := db.New(s.db)
	if _, err := q.GetRule(r.Context(), db.GetRuleParams{ID: txnID(r), HouseholdID: HouseholdID(r)}); err != nil {
		writeError(w, http.StatusNotFound, "Rule not found.")
		return
	}
	v, err := s.validated(r, q, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := q.UpdateRule(r.Context(), db.UpdateRuleParams{
		Priority: v.Priority, MatchField: v.MatchField, MatchOp: v.MatchOp, MatchValue: v.MatchValue,
		AccountID: v.AccountID, AmountMin: v.AmountMin, AmountMax: v.AmountMax, SetCategoryID: v.SetCategoryID,
		SetMerchant: v.SetMerchant, AddTagID: v.AddTagID, SetHidden: v.SetHidden, ID: txnID(r), HouseholdID: v.HouseholdID,
	}); err != nil {
		s.internalError(w, err)
		return
	}
	rl, err := q.GetRule(r.Context(), db.GetRuleParams{ID: txnID(r), HouseholdID: HouseholdID(r)})
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRuleDTO(rl))
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	if err := db.New(s.db).DeleteRule(r.Context(), db.DeleteRuleParams{ID: txnID(r), HouseholdID: HouseholdID(r)}); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleApplyRule runs one rule over existing transactions. Categories the user chose by hand
// are kept.
func (s *Server) handleApplyRule(w http.ResponseWriter, r *http.Request) {
	ctx, hh := r.Context(), HouseholdID(r)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	q := db.New(tx)
	rl, err := q.GetRule(ctx, db.GetRuleParams{ID: txnID(r), HouseholdID: hh})
	if err != nil {
		writeError(w, http.StatusNotFound, "Rule not found.")
		return
	}
	targets, err := q.ListRuleTargets(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	var merchantID sql.NullInt64
	if rl.SetMerchant != "" {
		m, err := q.UpsertMerchant(ctx, db.UpsertMerchantParams{HouseholdID: hh, Name: rl.SetMerchant, Normalized: categorize.Key(rl.SetMerchant)})
		if err != nil {
			s.internalError(w, err)
			return
		}
		merchantID = sql.NullInt64{Int64: m.ID, Valid: true}
	}
	n := 0
	now := time.Now().Unix()
	for _, t := range targets {
		txn := categorize.Txn{ID: t.ID, HouseholdID: hh, AccountID: t.AccountID, AmountCents: t.AmountCents, Description: t.Description, Payee: t.Payee}
		cleaned, _ := categorize.Merchant(t.Payee, t.Description)
		if !categorize.RuleMatches(rl, txn, cleaned) && !categorize.RuleMatches(rl, txn, t.MerchantName) {
			continue
		}
		cur, err := q.GetTransactionByID(ctx, t.ID)
		if err != nil {
			s.internalError(w, err)
			return
		}
		if merchantID.Valid {
			cur.MerchantID = merchantID
		}
		if rl.SetCategoryID.Valid && cur.CategorySource != categorize.SourceUser {
			cur.CategoryID, cur.CategorySource, cur.NeedsReview = rl.SetCategoryID, categorize.SourceRule, 0
		}
		if rl.SetHidden == 1 {
			cur.Hidden = 1
		}
		if err := q.UpdateTransactionUser(ctx, db.UpdateTransactionUserParams{
			Date: cur.Date, AmountCents: cur.AmountCents, Description: cur.Description, MerchantID: cur.MerchantID,
			CategoryID: cur.CategoryID, CategorySource: cur.CategorySource, Notes: cur.Notes, Hidden: cur.Hidden,
			NeedsReview: cur.NeedsReview, UpdatedAt: now, ID: cur.ID, HouseholdID: hh,
		}); err != nil {
			s.internalError(w, err)
			return
		}
		if rl.AddTagID.Valid {
			if err := q.AddTransactionTag(ctx, db.AddTransactionTagParams{TransactionID: t.ID, TagID: rl.AddTagID.Int64, HouseholdID: hh}); err != nil {
				s.internalError(w, err)
				return
			}
		}
		n++
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"updated": n})
}
