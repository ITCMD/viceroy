package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/aicat"
	"viceroy/internal/budget"
	"viceroy/internal/budgetview"
	"viceroy/internal/canibuy"
	"viceroy/internal/db"
	"viceroy/internal/money"
)

func (s *Server) canIBuyRoutes(r chi.Router) {
	r.Post("/can-i-buy", s.handleCanIBuy)
}

// POST /can-i-buy {text, amount?, category_id?}: checks a purchase against this month's
// budget and pacing. The light AI model fills in whatever the person left out (category,
// price); a price seen in past purchases beats the AI's guess.
func (s *Server) handleCanIBuy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text       string `json:"text"`
		Amount     string `json:"amount"`
		CategoryID int64  `json:"category_id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Text = strings.TrimSpace(in.Text)
	if len(in.Text) > 300 {
		in.Text = in.Text[:300]
	}
	ctx, hh, q := r.Context(), HouseholdID(r), db.New(s.db)
	var amount int64
	source := "you"
	if strings.TrimSpace(in.Amount) != "" {
		c, err := money.ParseCents(in.Amount)
		if err != nil || c <= 0 {
			writeError(w, http.StatusBadRequest, "Enter the amount as a positive number, like 12.50.")
			return
		}
		amount = c
	}
	if in.Text == "" && (in.CategoryID == 0 || amount == 0) {
		writeError(w, http.StatusBadRequest, "Say what you want to buy.")
		return
	}

	catID := in.CategoryID
	var historyCount int
	if catID == 0 || amount == 0 {
		client, err := s.ai.EmailClient(ctx, hh)
		if err != nil {
			s.internalError(w, err)
			return
		}
		if !client.Configured() {
			writeError(w, http.StatusServiceUnavailable, "AI isn't set up yet (Settings → AI). Pick a category and enter the amount instead.")
			return
		}
		client.Feature = "canibuy"
		cats, err := aicat.Categories(ctx, q, hh)
		if err != nil {
			s.internalError(w, err)
			return
		}
		reply, err := client.CompleteJSON(ctx, canibuy.Messages(in.Text, cats))
		if err != nil {
			s.log.Warn("can i buy", "err", err)
			writeError(w, http.StatusBadGateway, "The AI didn't answer: "+err.Error())
			return
		}
		g, ok := canibuy.ParseReply(reply, cats)
		if !ok {
			writeError(w, http.StatusBadGateway, "The AI couldn't tell which category this is. Pick one below.")
			return
		}
		if catID == 0 {
			catID = g.CategoryID
		}
		if amount == 0 {
			if g.Search != "" {
				rows, err := q.ListTransactions(ctx, db.ListTransactionsParams{
					HouseholdID: hh, Uncategorized: 0, NeedsReview: 0, IncludeHidden: 0, Q: g.Search, BeforeDate: "",
					FromDate: budget.FormatDate(budgetview.Today().AddDate(0, -6, 0)), ToDate: "", Direction: "out", Lim: 20,
				})
				if err != nil {
					s.internalError(w, err)
					return
				}
				var past []int64
				for _, t := range rows {
					past = append(past, t.AmountCents)
				}
				if len(past) >= 2 {
					amount, source, historyCount = canibuy.Median(past), "history", len(past)
				}
			}
			if amount == 0 && g.AmountCents > 0 {
				amount, source = g.AmountCents, "estimate"
			}
			if amount == 0 {
				writeError(w, http.StatusBadGateway, "Couldn't guess a price for that. Enter the amount.")
				return
			}
		}
	}

	today := budgetview.Today()
	v, err := budgetview.Build(ctx, q, hh, budget.ViewMonth, today, today)
	if err != nil {
		s.internalError(w, err)
		return
	}
	var line *budgetview.Line
	var group budgetview.Group
	for _, g := range v.Groups {
		for i := range g.Lines {
			if g.Lines[i].ID == catID && g.Kind != "income" && g.Kind != "goals" {
				line, group = &g.Lines[i], g
			}
		}
	}
	if line == nil {
		writeError(w, http.StatusBadRequest, "Pick a spending category.")
		return
	}
	end, _ := budget.ParseDate(v.End)
	pace, err := budget.ParseDate(v.PaceThrough)
	if err != nil {
		pace = end
	}
	sc, err := s.recurringSchedule(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	// Late charges (due before today, not yet seen) still count, as on the budget screen.
	line.Upcoming = sc.Due(today.AddDate(0, 0, -7), end)[catID]

	writeJSON(w, http.StatusOK, map[string]any{
		"category":      map[string]any{"id": line.ID, "name": line.Name, "icon": line.Icon},
		"amount_cents":  amount,
		"amount_source": source, // you | history | estimate
		"history_count": historyCount,
		"verdict":       canibuy.Judge(*line, group, amount, today, pace, end),
	})
}
