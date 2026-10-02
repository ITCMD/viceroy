package server

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/db"
)

func (s *Server) annotationRoutes(r chi.Router) {
	r.Get("/networth/annotations", s.handleListAnnotations)
	r.Post("/networth/annotations", s.handleCreateAnnotation)
	r.Patch("/networth/annotations/{id}", s.handleUpdateAnnotation)
	r.Delete("/networth/annotations/{id}", s.handleDeleteAnnotation)
}

type annotationDTO struct {
	ID            int64  `json:"id"`
	Date          string `json:"date"`
	Label         string `json:"label"`
	Icon          string `json:"icon"`
	TransactionID int64  `json:"transaction_id"` // 0 = none
	TxnName       string `json:"txn_name"`
	TxnAmount     int64  `json:"txn_amount"`
	TxnDate       string `json:"txn_date"`
}

// GET /networth/annotations: notes pinned to days on the net worth chart.
func (s *Server) handleListAnnotations(w http.ResponseWriter, r *http.Request) {
	rows, err := db.New(s.db).ListNetworthAnnotations(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := make([]annotationDTO, len(rows))
	for i, a := range rows {
		out[i] = annotationDTO{a.ID, a.Date, a.Label, a.Icon, a.TransactionID.Int64, a.TxnName, a.TxnAmount, a.TxnDate}
	}
	writeJSON(w, http.StatusOK, map[string]any{"annotations": out})
}

type annotationIn struct {
	Date          string `json:"date"`
	Label         string `json:"label"`
	Icon          string `json:"icon"`
	TransactionID int64  `json:"transaction_id"`
}

func (in *annotationIn) check(ctx context.Context, q *db.Queries, hh int64) string {
	in.Label, in.Icon = strings.TrimSpace(in.Label), strings.TrimSpace(in.Icon)
	if _, err := time.Parse(time.DateOnly, in.Date); err != nil {
		return "Pick a date."
	}
	if in.Label == "" || utf8.RuneCountInString(in.Label) > 80 {
		return "Give it a name (up to 80 characters)."
	}
	if len(in.Icon) > 32 || utf8.RuneCountInString(in.Icon) > 8 {
		return "The icon should be a single emoji."
	}
	if in.TransactionID != 0 {
		if t, err := q.GetTransactionByID(ctx, in.TransactionID); err != nil || t.HouseholdID != hh {
			return "Unknown transaction."
		}
	}
	return ""
}

func (s *Server) handleCreateAnnotation(w http.ResponseWriter, r *http.Request) {
	var in annotationIn
	if !readJSON(w, r, &in) {
		return
	}
	ctx, hh, q := r.Context(), HouseholdID(r), db.New(s.db)
	if msg := in.check(ctx, q, hh); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	id, err := q.CreateNetworthAnnotation(ctx, db.CreateNetworthAnnotationParams{
		HouseholdID: hh, Date: in.Date, Label: in.Label, Icon: in.Icon,
		TransactionID: sql.NullInt64{Int64: in.TransactionID, Valid: in.TransactionID != 0}, CreatedAt: time.Now().Unix(),
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleUpdateAnnotation(w http.ResponseWriter, r *http.Request) {
	var in annotationIn
	if !readJSON(w, r, &in) {
		return
	}
	ctx, hh, q := r.Context(), HouseholdID(r), db.New(s.db)
	if msg := in.check(ctx, q, hh); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	n, err := q.UpdateNetworthAnnotation(ctx, db.UpdateNetworthAnnotationParams{
		Date: in.Date, Label: in.Label, Icon: in.Icon,
		TransactionID: sql.NullInt64{Int64: in.TransactionID, Valid: in.TransactionID != 0}, ID: txnID(r), HouseholdID: hh,
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "Note not found.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteAnnotation(w http.ResponseWriter, r *http.Request) {
	n, err := db.New(s.db).DeleteNetworthAnnotation(r.Context(), db.DeleteNetworthAnnotationParams{ID: txnID(r), HouseholdID: HouseholdID(r)})
	if err != nil {
		s.internalError(w, err)
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "Note not found.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
