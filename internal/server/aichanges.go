package server

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"viceroy/internal/categorize"
	"viceroy/internal/db"
)

// aiChangeDTO is one change the email-reading AI made to a transaction.
type aiChangeDTO struct {
	Field        string `json:"field"` // category | notes | tags | needs_review
	Description  string `json:"description"`
	EmailID      *int64 `json:"email_id"`
	EmailSubject string `json:"email_subject"`
	CreatedAt    int64  `json:"created_at"`
}

func (s *Server) aiChanges(ctx context.Context, hh, txnID int64) ([]aiChangeDTO, error) {
	q := db.New(s.db)
	rows, err := q.ListAIChanges(ctx, db.ListAIChangesParams{TransactionID: txnID, HouseholdID: hh})
	if err != nil {
		return nil, err
	}
	out := []aiChangeDTO{}
	for _, c := range rows {
		d := aiChangeDTO{Field: c.Field, EmailID: ptr(c.EmailMessageID), EmailSubject: c.EmailSubject, CreatedAt: c.CreatedAt}
		switch c.Field {
		case "category":
			d.Description = "Set the category"
			if id, err := strconv.ParseInt(c.NewValue, 10, 64); err == nil {
				if cat, err := q.GetCategory(ctx, db.GetCategoryParams{ID: id, HouseholdID: hh}); err == nil {
					d.Description = "Set the category to " + cat.Name
				}
			}
		case "notes":
			d.Description = "Added a note"
		case "tags":
			d.Description = "Added a tag"
			if id, err := strconv.ParseInt(c.NewValue, 10, 64); err == nil {
				if tags, err := q.ListTags(ctx, hh); err == nil {
					for _, tg := range tags {
						if tg.ID == id {
							d.Description = "Added the tag " + tg.Name
						}
					}
				}
			}
		case "needs_review":
			d.Description = "Flagged for review"
		}
		out = append(out, d)
	}
	return out, nil
}

// POST /transactions/{id}/ai-undo: reverts what the AI changed, leaving anything the user has
// changed since alone.
func (s *Server) handleUndoAIChanges(w http.ResponseWriter, r *http.Request) {
	ctx, hh, id := r.Context(), HouseholdID(r), txnID(r)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	q := db.New(tx)
	t, err := q.GetTransaction(ctx, db.GetTransactionParams{ID: id, HouseholdID: hh})
	if err != nil {
		writeError(w, http.StatusNotFound, "Transaction not found.")
		return
	}
	rows, err := q.ListAIChanges(ctx, db.ListAIChangesParams{TransactionID: id, HouseholdID: hh})
	if err != nil {
		s.internalError(w, err)
		return
	}
	now := time.Now().Unix()
	notes := t.Notes
	for i := len(rows) - 1; i >= 0; i-- {
		c := rows[i]
		switch c.Field {
		case "category":
			// Only if it's still the AI's choice.
			if t.CategorySource != categorize.SourceAI || strconv.FormatInt(t.CategoryID.Int64, 10) != c.NewValue {
				continue
			}
			cat, src := sql.NullInt64{}, ""
			if old, oldSrc, ok := strings.Cut(c.OldValue, ":"); ok {
				if n, err := strconv.ParseInt(old, 10, 64); err == nil {
					cat, src = sql.NullInt64{Int64: n, Valid: true}, oldSrc
				}
			}
			if err := q.SetTransactionCategoryAI(ctx, db.SetTransactionCategoryAIParams{CategoryID: cat, CategorySource: src, UpdatedAt: now, ID: id, HouseholdID: hh}); err != nil {
				s.internalError(w, err)
				return
			}
		case "notes":
			// Remove the AI's lines; keep anything the user wrote.
			added := strings.TrimPrefix(c.NewValue, c.OldValue)
			for _, line := range strings.Split(strings.TrimPrefix(added, "\n"), "\n") {
				if line != "" {
					notes = strings.Replace(notes, line, "", 1)
				}
			}
		case "tags":
			if n, err := strconv.ParseInt(c.NewValue, 10, 64); err == nil {
				if err := q.RemoveTransactionTag(ctx, db.RemoveTransactionTagParams{TransactionID: id, TagID: n}); err != nil {
					s.internalError(w, err)
					return
				}
			}
		case "needs_review":
			if err := q.SetTransactionNeedsReview(ctx, db.SetTransactionNeedsReviewParams{NeedsReview: 0, UpdatedAt: now, ID: id, HouseholdID: hh}); err != nil {
				s.internalError(w, err)
				return
			}
		}
	}
	if notes != t.Notes {
		notes = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(notes, "\n\n", "\n"), "\n\n", "\n"))
		if err := q.SetTransactionNotes(ctx, db.SetTransactionNotesParams{Notes: notes, UpdatedAt: now, ID: id, HouseholdID: hh}); err != nil {
			s.internalError(w, err)
			return
		}
	}
	if err := q.MarkAIChangesUndone(ctx, db.MarkAIChangesUndoneParams{UndoneAt: sql.NullInt64{Int64: now, Valid: true}, TransactionID: id, HouseholdID: hh}); err != nil {
		s.internalError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	v, err := s.txnView(ctx, hh, id)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
