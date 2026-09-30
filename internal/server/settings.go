package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/accounts"
	"viceroy/internal/db"
)

func (s *Server) settingsRoutes(r chi.Router) {
	r.Get("/settings", s.handleGetSettings)
	r.Patch("/settings", s.handleUpdateSettings)
}

type settingsDTO struct {
	PaperCashEnabled bool `json:"paper_cash_enabled"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	a, err := db.New(s.db).GetBuiltinAccount(r.Context(), db.GetBuiltinAccountParams{HouseholdID: HouseholdID(r), Builtin: accounts.PaperCash})
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsDTO{PaperCashEnabled: accounts.PaperCashEnabled(a)})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PaperCashEnabled *bool `json:"paper_cash_enabled"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	q := db.New(s.db)
	if in.PaperCashEnabled != nil {
		a, err := q.GetBuiltinAccount(r.Context(), db.GetBuiltinAccountParams{HouseholdID: HouseholdID(r), Builtin: accounts.PaperCash})
		if err != nil {
			s.internalError(w, err)
			return
		}
		// Off = closed and hidden (history kept); on = active and visible.
		status, hidden := "active", int64(0)
		if !*in.PaperCashEnabled {
			status, hidden = "closed", 1
		}
		if err := q.UpdateAccountSettings(r.Context(), db.UpdateAccountSettingsParams{
			Name: a.Name, Type: a.Type, IncludeInNetWorth: a.IncludeInNetWorth, Hidden: hidden, Status: status,
			UpdatedAt: time.Now().Unix(), ID: a.ID, HouseholdID: a.HouseholdID,
		}); err != nil {
			s.internalError(w, err)
			return
		}
	}
	s.handleGetSettings(w, r)
}
