package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"viceroy/internal/ai"
	"viceroy/internal/aicat"
)

// handleAICategorize asks the light AI model to categorize uncategorized transactions from
// the last `days` days, including ones it passed on before.
func (s *Server) handleAICategorize(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Days int `json:"days"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Days == 0 {
		in.Days = 31
	}
	if in.Days < 1 || in.Days > 365 {
		writeError(w, http.StatusBadRequest, "Pick between 1 and 365 days.")
		return
	}
	hh := HouseholdID(r)
	st, err := s.ai.Load(r.Context(), hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	client := s.ai.Track(st.Email(s.ai.Config.BaseURL, s.ai.Referer), hh, "categorize")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	res, err := aicat.Run(ctx, s.db, client, hh, aicat.Options{
		Since: time.Now().AddDate(0, 0, -in.Days).Format(time.DateOnly), IncludeTried: true, Review: st.CatReview,
	})
	if errors.Is(err, ai.ErrNotConfigured) {
		writeError(w, http.StatusBadRequest, "Set up AI in Settings → AI first.")
		return
	}
	if err != nil {
		// Batches already done stay categorized.
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "The AI stopped partway: " + err.Error(), "result": res})
		return
	}
	writeJSON(w, http.StatusOK, res)
}
