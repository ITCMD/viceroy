package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"viceroy/internal/categorize"
	"viceroy/internal/db"
	"viceroy/internal/money"
)

type ruleTagDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type ruleDTO struct {
	ID            int64        `json:"id"`
	Priority      int64        `json:"priority"`
	MatchField    string       `json:"match_field"`
	MatchOp       string       `json:"match_op"`
	MatchValue    string       `json:"match_value"`
	AccountID     *int64       `json:"account_id"`
	AmountMin     *int64       `json:"amount_min"`
	AmountMax     *int64       `json:"amount_max"`
	Direction     string       `json:"direction"`
	DayMin        *int64       `json:"day_min"`
	DayMax        *int64       `json:"day_max"`
	SetCategoryID *int64       `json:"set_category_id"`
	SetMerchant   string       `json:"set_merchant"`
	Tags          []ruleTagDTO `json:"tags"`
	SetGoalID     *int64       `json:"set_goal_id"`
	SetHidden     bool         `json:"set_hidden"`
}

func toRuleDTO(r db.Rule, tags []ruleTagDTO) ruleDTO {
	if tags == nil {
		tags = []ruleTagDTO{}
	}
	return ruleDTO{
		ID: r.ID, Priority: r.Priority, MatchField: r.MatchField, MatchOp: r.MatchOp, MatchValue: r.MatchValue,
		AccountID: ptr(r.AccountID), AmountMin: ptr(r.AmountMin), AmountMax: ptr(r.AmountMax),
		Direction: r.Direction, DayMin: ptr(r.DayMin), DayMax: ptr(r.DayMax),
		SetCategoryID: ptr(r.SetCategoryID), SetMerchant: r.SetMerchant, Tags: tags, SetGoalID: ptr(r.SetGoalID),
		SetHidden: r.SetHidden == 1,
	}
}

// ruleTagNames maps rule id > its tags, for DTOs.
func ruleTagNames(r *http.Request, q *db.Queries) (map[int64][]ruleTagDTO, error) {
	rows, err := q.ListRuleTags(r.Context(), HouseholdID(r))
	if err != nil {
		return nil, err
	}
	out := map[int64][]ruleTagDTO{}
	for _, t := range rows {
		out[t.RuleID] = append(out[t.RuleID], ruleTagDTO{ID: t.ID, Name: t.Name})
	}
	return out, nil
}

// ruleIn is the create/update body. Amounts are decimal strings ("" = no bound); tags are
// given by name and created if needed (add_tag is the older single-tag form).
type ruleIn struct {
	Priority      int64    `json:"priority"`
	MatchField    string   `json:"match_field"`
	MatchOp       string   `json:"match_op"`
	MatchValue    string   `json:"match_value"`
	AccountID     *int64   `json:"account_id"`
	AmountMin     string   `json:"amount_min"`
	AmountMax     string   `json:"amount_max"`
	Direction     string   `json:"direction"`
	DayMin        *int64   `json:"day_min"`
	DayMax        *int64   `json:"day_max"`
	SetCategoryID *int64   `json:"set_category_id"`
	SetMerchant   string   `json:"set_merchant"`
	AddTag        string   `json:"add_tag"`
	Tags          []string `json:"tags"`
	SetGoalID     *int64   `json:"set_goal_id"`
	SetHidden     bool     `json:"set_hidden"`
}

// validated turns the body into a rule row plus its tag ids, or returns a user-facing error.
func (s *Server) validated(r *http.Request, q *db.Queries, in ruleIn) (db.Rule, []int64, error) {
	ctx, hh := r.Context(), HouseholdID(r)
	out := db.Rule{HouseholdID: hh, Priority: in.Priority, SetMerchant: strings.TrimSpace(in.SetMerchant), SetHidden: b2i(in.SetHidden)}
	out.MatchValue = strings.TrimSpace(in.MatchValue)
	switch in.MatchField {
	case "merchant", "description":
		out.MatchField = in.MatchField
	default:
		return out, nil, errors.New("Match on merchant or original statement.")
	}
	switch in.MatchOp {
	case "contains", "equals", "starts_with":
		out.MatchOp = in.MatchOp
	default:
		return out, nil, errors.New("Choose how to match.")
	}
	if in.AccountID != nil {
		if _, err := q.GetAccount(ctx, db.GetAccountParams{ID: *in.AccountID, HouseholdID: hh}); err != nil {
			return out, nil, errors.New("Unknown account.")
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
			return out, nil, errors.New("Amounts must look like 12.34.")
		}
		if c < 0 {
			c = -c
		}
		*b.out = sql.NullInt64{Int64: c, Valid: true}
	}
	if out.AmountMin.Valid && out.AmountMax.Valid && out.AmountMin.Int64 > out.AmountMax.Int64 {
		return out, nil, errors.New("The lowest amount is above the highest.")
	}
	switch in.Direction {
	case "", "out", "in":
		out.Direction = in.Direction
	default:
		return out, nil, errors.New("Direction must be out or in.")
	}
	for _, d := range []struct {
		in  *int64
		out *sql.NullInt64
	}{{in.DayMin, &out.DayMin}, {in.DayMax, &out.DayMax}} {
		if d.in == nil {
			continue
		}
		if *d.in < 1 || *d.in > 31 {
			return out, nil, errors.New("Days of the month run from 1 to 31.")
		}
		*d.out = sql.NullInt64{Int64: *d.in, Valid: true}
	}
	if out.MatchValue == "" && !out.AccountID.Valid && !out.AmountMin.Valid && !out.AmountMax.Valid {
		return out, nil, errors.New("Enter text to match, or pick an account or amount.")
	}
	if in.SetCategoryID != nil {
		if _, err := q.GetCategory(ctx, db.GetCategoryParams{ID: *in.SetCategoryID, HouseholdID: hh}); err != nil {
			return out, nil, errors.New("Unknown category.")
		}
		out.SetCategoryID = sql.NullInt64{Int64: *in.SetCategoryID, Valid: true}
	}
	if in.SetGoalID != nil {
		if _, err := q.GetGoal(ctx, db.GetGoalParams{ID: *in.SetGoalID, HouseholdID: hh}); err != nil {
			return out, nil, errors.New("Unknown goal.")
		}
		out.SetGoalID = sql.NullInt64{Int64: *in.SetGoalID, Valid: true}
	}
	if out.SetMerchant != "" && categorize.Key(out.SetMerchant) == "" {
		return out, nil, errors.New("Enter a merchant name with letters or digits.")
	}
	names := in.Tags
	if t := strings.TrimSpace(in.AddTag); t != "" {
		names = append(names, t)
	}
	var tags []int64
	for _, n := range names {
		if n = strings.TrimSpace(n); n == "" {
			continue
		}
		tag, err := q.UpsertTag(ctx, db.UpsertTagParams{HouseholdID: hh, Name: n})
		if err != nil {
			return out, nil, err
		}
		tags = append(tags, tag.ID)
	}
	if !out.SetCategoryID.Valid && out.SetMerchant == "" && len(tags) == 0 && out.SetHidden == 0 && !out.SetGoalID.Valid {
		return out, nil, errors.New("Choose at least one action.")
	}
	return out, tags, nil
}

func setRuleTags(ctx context.Context, q *db.Queries, ruleID int64, tags []int64) error {
	if err := q.ClearRuleTags(ctx, ruleID); err != nil {
		return err
	}
	for _, t := range tags {
		if err := q.AddRuleTag(ctx, db.AddRuleTagParams{RuleID: ruleID, TagID: t}); err != nil {
			return err
		}
	}
	return nil
}

// ruleOut reads a rule back with its tags.
func (s *Server) ruleOut(r *http.Request, q *db.Queries, id int64) (ruleDTO, error) {
	rl, err := q.GetRule(r.Context(), db.GetRuleParams{ID: id, HouseholdID: HouseholdID(r)})
	if err != nil {
		return ruleDTO{}, err
	}
	tags, err := ruleTagNames(r, q)
	if err != nil {
		return ruleDTO{}, err
	}
	return toRuleDTO(rl, tags[id]), nil
}

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	q := db.New(s.db)
	rules, err := q.ListRules(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	tags, err := ruleTagNames(r, q)
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := make([]ruleDTO, len(rules))
	for i, rl := range rules {
		out[i] = toRuleDTO(rl, tags[rl.ID])
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": out})
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	s.saveRule(w, r, 0)
}

func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	s.saveRule(w, r, txnID(r))
}

// saveRule creates (id 0) or updates a rule and its tags in one DB transaction.
func (s *Server) saveRule(w http.ResponseWriter, r *http.Request, id int64) {
	var in ruleIn
	if !readJSON(w, r, &in) {
		return
	}
	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	q := db.New(tx)
	if id != 0 {
		if _, err := q.GetRule(ctx, db.GetRuleParams{ID: id, HouseholdID: HouseholdID(r)}); err != nil {
			writeError(w, http.StatusNotFound, "Rule not found.")
			return
		}
	}
	v, tags, err := s.validated(r, q, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if id == 0 {
		rl, err := q.CreateRule(ctx, db.CreateRuleParams{
			HouseholdID: v.HouseholdID, Priority: v.Priority, MatchField: v.MatchField, MatchOp: v.MatchOp,
			MatchValue: v.MatchValue, AccountID: v.AccountID, AmountMin: v.AmountMin, AmountMax: v.AmountMax,
			Direction: v.Direction, DayMin: v.DayMin, DayMax: v.DayMax, SetCategoryID: v.SetCategoryID,
			SetMerchant: v.SetMerchant, SetHidden: v.SetHidden, SetGoalID: v.SetGoalID, CreatedAt: time.Now().Unix(),
		})
		if err != nil {
			s.internalError(w, err)
			return
		}
		id = rl.ID
	} else if err := q.UpdateRule(ctx, db.UpdateRuleParams{
		Priority: v.Priority, MatchField: v.MatchField, MatchOp: v.MatchOp, MatchValue: v.MatchValue,
		AccountID: v.AccountID, AmountMin: v.AmountMin, AmountMax: v.AmountMax, Direction: v.Direction,
		DayMin: v.DayMin, DayMax: v.DayMax, SetCategoryID: v.SetCategoryID, SetMerchant: v.SetMerchant,
		SetHidden: v.SetHidden, SetGoalID: v.SetGoalID, ID: id, HouseholdID: v.HouseholdID,
	}); err != nil {
		s.internalError(w, err)
		return
	}
	if err := setRuleTags(ctx, q, id, tags); err != nil {
		s.internalError(w, err)
		return
	}
	out, err := s.ruleOut(r, q, id)
	if err != nil {
		s.internalError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	status := http.StatusOK
	if txnID(r) == 0 {
		status = http.StatusCreated
	}
	writeJSON(w, status, out)
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	if err := db.New(s.db).DeleteRule(r.Context(), db.DeleteRuleParams{ID: txnID(r), HouseholdID: HouseholdID(r)}); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// applyRuleIn is the optional body of POST /rules/{id}/apply.
type applyRuleIn struct {
	DryRun bool `json:"dry_run"` // only count
	// Also replace categories the user picked by hand (imported history counts as hand-picked).
	OverrideUser bool `json:"override_user"`
}

// handleApplyRule runs one rule over existing transactions (pending and email entries
// included; linked stand-ins are skipped since their posted row carries the change). It
// reports how many transactions changed (or would, with dry_run) and how many matching ones
// keep a hand-picked category the rule would otherwise replace.
func (s *Server) handleApplyRule(w http.ResponseWriter, r *http.Request) {
	var in applyRuleIn
	if r.ContentLength != 0 && !readJSON(w, r, &in) {
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
	rl, err := q.GetRule(ctx, db.GetRuleParams{ID: txnID(r), HouseholdID: hh})
	if err != nil {
		writeError(w, http.StatusNotFound, "Rule not found.")
		return
	}
	ruleTags, err := categorize.RuleTags(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	tags := ruleTags[rl.ID]
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
	changed, kept, onlyKept := 0, 0, 0
	now := time.Now().Unix()
	for _, t := range targets {
		txn := categorize.Txn{ID: t.ID, HouseholdID: hh, AccountID: t.AccountID, AmountCents: t.AmountCents, Date: t.Date, Description: t.Description, Payee: t.Payee}
		cleaned, _ := categorize.Merchant(t.Payee, t.Description)
		if !categorize.RuleMatches(rl, txn, cleaned) && !categorize.RuleMatches(rl, txn, t.MerchantName) {
			continue
		}
		cur, err := q.GetTransactionByID(ctx, t.ID)
		if err != nil {
			s.internalError(w, err)
			return
		}
		next, dirty, blocked := cur, false, false
		if merchantID.Valid && cur.MerchantID != merchantID {
			next.MerchantID, dirty = merchantID, true
		}
		if rl.SetCategoryID.Valid && cur.CategoryID != rl.SetCategoryID {
			if cur.CategorySource == categorize.SourceUser && !in.OverrideUser {
				kept, blocked = kept+1, true
			} else {
				next.CategoryID, next.CategorySource, next.NeedsReview, dirty = rl.SetCategoryID, categorize.SourceRule, 0, true
			}
		}
		if rl.SetHidden == 1 && cur.Hidden == 0 {
			next.Hidden, dirty = 1, true
		}
		goal := rl.SetGoalID.Valid && cur.GoalID != rl.SetGoalID
		have, err := q.ListTransactionTagIDs(ctx, t.ID)
		if err != nil {
			s.internalError(w, err)
			return
		}
		var addTags []int64
		for _, tag := range tags {
			if !slices.Contains(have, tag) {
				addTags = append(addTags, tag)
			}
		}
		if !dirty && !goal && len(addTags) == 0 {
			if blocked {
				onlyKept++
			}
			continue
		}
		changed++
		if in.DryRun {
			continue
		}
		if dirty {
			if err := q.UpdateTransactionUser(ctx, db.UpdateTransactionUserParams{
				Date: next.Date, AmountCents: next.AmountCents, Description: next.Description, MerchantID: next.MerchantID,
				CategoryID: next.CategoryID, CategorySource: next.CategorySource, Notes: next.Notes, Hidden: next.Hidden,
				NeedsReview: next.NeedsReview, UpdatedAt: now, ID: next.ID, HouseholdID: hh,
			}); err != nil {
				s.internalError(w, err)
				return
			}
		}
		if goal {
			if err := q.SetTransactionGoal(ctx, db.SetTransactionGoalParams{GoalID: rl.SetGoalID, ID: t.ID, HouseholdID: hh}); err != nil {
				s.internalError(w, err)
				return
			}
		}
		for _, tag := range addTags {
			if err := q.AddTransactionTag(ctx, db.AddTransactionTagParams{TransactionID: t.ID, TagID: tag, HouseholdID: hh}); err != nil {
				s.internalError(w, err)
				return
			}
		}
	}
	if in.DryRun {
		// with_hand_picked = what override_user would change.
		writeJSON(w, http.StatusOK, map[string]int{"matches": changed, "hand_picked": kept, "with_hand_picked": changed + onlyKept})
		return
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"updated": changed, "hand_picked": kept})
}
