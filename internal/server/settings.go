package server

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/accounts"
	"viceroy/internal/budget"
	"viceroy/internal/budgetview"
	"viceroy/internal/db"
)

func (s *Server) settingsRoutes(r chi.Router) {
	r.Get("/settings", s.handleGetSettings)
	r.Patch("/settings", s.handleUpdateSettings)
}

// Logo choices: the viceroy butterfly (default) or the original "V" mark.
const (
	setLogo       = "appearance.logo"
	logoButterfly = "butterfly"
	logoClassic   = "classic"
)

func loadLogo(ctx context.Context, q *db.Queries, hh int64) (string, error) {
	rows, err := q.ListHouseholdSettings(ctx, hh)
	if err != nil {
		return "", err
	}
	for _, r := range rows {
		if r.Key == setLogo && r.Value == logoClassic {
			return logoClassic, nil
		}
	}
	return logoButterfly, nil
}

type settingsDTO struct {
	PaperCashEnabled bool                `json:"paper_cash_enabled"`
	Logo             string              `json:"logo"` // butterfly | classic
	Budget           budgetview.Settings `json:"budget"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	a, err := db.New(s.db).GetBuiltinAccount(r.Context(), db.GetBuiltinAccountParams{HouseholdID: HouseholdID(r), Builtin: accounts.PaperCash})
	if err != nil {
		s.internalError(w, err)
		return
	}
	bs, err := budgetview.LoadSettings(r.Context(), db.New(s.db), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	logo, err := loadLogo(r.Context(), db.New(s.db), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsDTO{PaperCashEnabled: accounts.PaperCashEnabled(a), Logo: logo, Budget: bs})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PaperCashEnabled *bool   `json:"paper_cash_enabled"`
		Logo             *string `json:"logo"`
		Budget           *struct {
			ForwardDefault *bool               `json:"forward_default"`
			WeekStart      *int                `json:"week_start"`
			PaySchedule    *budget.PaySchedule `json:"pay_schedule"`
			UpcomingWindow *string             `json:"upcoming_window"`
			DebtActual     *string             `json:"debt_actual"`
		} `json:"budget"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	q := db.New(s.db)
	if in.Logo != nil {
		if *in.Logo != logoButterfly && *in.Logo != logoClassic {
			writeError(w, http.StatusBadRequest, "Unknown logo.")
			return
		}
		if err := q.SetHouseholdSetting(r.Context(), db.SetHouseholdSettingParams{HouseholdID: HouseholdID(r), Key: setLogo, Value: *in.Logo}); err != nil {
			s.internalError(w, err)
			return
		}
	}
	if b := in.Budget; b != nil {
		msg, err := budgetview.SaveSettings(r.Context(), q, HouseholdID(r), b.ForwardDefault, b.WeekStart, b.PaySchedule, b.UpcomingWindow, b.DebtActual)
		if err != nil {
			s.internalError(w, err)
			return
		}
		if msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
	}
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
