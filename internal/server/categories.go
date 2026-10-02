package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"viceroy/internal/db"
)

// Expense group kinds a category may move between (income and transfers keep their categories:
// moving one across would flip how its amounts count).
var movableKinds = map[string]bool{"fixed": true, "flexible": true, "non_monthly": true}

// handleCategoryLayout saves the order of categories within their groups and moves categories
// between the fixed, flexible and non-monthly groups. Categories left out keep their place.
func (s *Server) handleCategoryLayout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Groups []struct {
			ID          int64   `json:"id"`
			CategoryIDs []int64 `json:"category_ids"`
		} `json:"groups"`
	}
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
	groups, err := q.ListCategoryGroups(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	cats, err := q.ListCategories(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	kind := map[int64]string{}
	for _, g := range groups {
		kind[g.ID] = g.Kind
	}
	current := map[int64]int64{} // category → group
	for _, c := range cats {
		current[c.ID] = c.GroupID
	}
	seen := map[int64]bool{}
	for _, g := range in.Groups {
		k, ok := kind[g.ID]
		if !ok {
			writeError(w, http.StatusBadRequest, "Unknown category group.")
			return
		}
		for i, id := range g.CategoryIDs {
			from, ok := current[id]
			if !ok || seen[id] {
				writeError(w, http.StatusBadRequest, "Unknown or repeated category.")
				return
			}
			seen[id] = true
			if from != g.ID && !(movableKinds[kind[from]] && movableKinds[k]) {
				writeError(w, http.StatusBadRequest, "Categories can only move between Fixed, Flexible and Non-monthly.")
				return
			}
			if err := q.SetCategoryPlace(ctx, db.SetCategoryPlaceParams{GroupID: g.ID, Sort: int64(i), ID: id, HouseholdID: hh}); err != nil {
				s.internalError(w, err)
				return
			}
		}
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	s.handleListCategories(w, r)
}

type categoryIn struct {
	Name    string `json:"name"`
	Icon    string `json:"icon"`
	GroupID int64  `json:"group_id"` // create only
}

// check trims the input and refuses empty, overlong or duplicate names (another category in
// the household with the same name, ignoring case).
func (in *categoryIn) check(cats []db.Category, self int64) string {
	in.Name, in.Icon = strings.TrimSpace(in.Name), strings.TrimSpace(in.Icon)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 60 {
		return "Give the category a name (up to 60 characters)."
	}
	if len(in.Icon) > 32 || utf8.RuneCountInString(in.Icon) > 8 {
		return "The icon should be a single emoji."
	}
	for _, c := range cats {
		if c.ID != self && c.Archived == 0 && strings.EqualFold(c.Name, in.Name) {
			return fmt.Sprintf("There's already a category called “%s”.", c.Name)
		}
	}
	return ""
}

// POST /categories {name, icon, group_id}: a new category at the end of its group.
func (s *Server) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	var in categoryIn
	if !readJSON(w, r, &in) {
		return
	}
	ctx, hh, q := r.Context(), HouseholdID(r), db.New(s.db)
	groups, err := q.ListCategoryGroups(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	cats, err := q.ListCategories(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	found := false
	for _, g := range groups {
		found = found || g.ID == in.GroupID
	}
	if !found {
		writeError(w, http.StatusBadRequest, "Pick a group.")
		return
	}
	if msg := in.check(cats, 0); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	sort := int64(0)
	for _, c := range cats {
		if c.GroupID == in.GroupID && c.Sort >= sort {
			sort = c.Sort + 1
		}
	}
	c, err := q.CreateCategory(ctx, db.CreateCategoryParams{HouseholdID: hh, GroupID: in.GroupID, Name: in.Name, Icon: in.Icon, Sort: sort})
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, categoryDTO{ID: c.ID, Name: c.Name, Icon: c.Icon})
}

// PATCH /categories/{id} {name, icon}
func (s *Server) handleUpdateCategory(w http.ResponseWriter, r *http.Request) {
	var in categoryIn
	if !readJSON(w, r, &in) {
		return
	}
	ctx, hh, q := r.Context(), HouseholdID(r), db.New(s.db)
	id := txnID(r)
	if _, err := q.GetCategory(ctx, db.GetCategoryParams{ID: id, HouseholdID: hh}); err != nil {
		writeError(w, http.StatusNotFound, "Category not found.")
		return
	}
	cats, err := q.ListCategories(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	if msg := in.check(cats, id); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := q.UpdateCategory(ctx, db.UpdateCategoryParams{Name: in.Name, Icon: in.Icon, ID: id, HouseholdID: hh}); err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, categoryDTO{ID: id, Name: in.Name, Icon: in.Icon})
}

// GET /categories/{id}/usage: how many transactions use it (the delete dialog asks where they go).
func (s *Server) handleCategoryUsage(w http.ResponseWriter, r *http.Request) {
	n, err := db.New(s.db).CountCategoryTransactions(r.Context(), db.CountCategoryTransactionsParams{CategoryID: sql.NullInt64{Int64: txnID(r), Valid: true}, HouseholdID: HouseholdID(r)})
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transactions": n})
}

// DELETE /categories/{id}?move_to=<id>: delete a category. Its transactions and rules move to
// move_to, or become uncategorized without it; its budget amounts are deleted.
func (s *Server) handleDeleteCategory(w http.ResponseWriter, r *http.Request) {
	ctx, hh := r.Context(), HouseholdID(r)
	id := txnID(r)
	var to sql.NullInt64
	if v := r.URL.Query().Get("move_to"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n == id {
			writeError(w, http.StatusBadRequest, "Pick another category to move its transactions to.")
			return
		}
		to = sql.NullInt64{Int64: n, Valid: true}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	q := db.New(tx)
	if _, err := q.GetCategory(ctx, db.GetCategoryParams{ID: id, HouseholdID: hh}); err != nil {
		writeError(w, http.StatusNotFound, "Category not found.")
		return
	}
	if to.Valid {
		if _, err := q.GetCategory(ctx, db.GetCategoryParams{ID: to.Int64, HouseholdID: hh}); err != nil {
			writeError(w, http.StatusBadRequest, "Unknown category to move to.")
			return
		}
	}
	from := sql.NullInt64{Int64: id, Valid: true}
	moved, err := q.MoveCategoryTransactions(ctx, db.MoveCategoryTransactionsParams{ToID: to, FromID: from, HouseholdID: hh})
	if err == nil {
		err = q.MoveCategoryRules(ctx, db.MoveCategoryRulesParams{ToID: to, FromID: from, HouseholdID: hh})
	}
	if err == nil {
		_, err = q.DeleteCategory(ctx, db.DeleteCategoryParams{ID: id, HouseholdID: hh})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"moved": moved})
}
