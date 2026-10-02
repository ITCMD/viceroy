package server

import (
	"net/http"

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
