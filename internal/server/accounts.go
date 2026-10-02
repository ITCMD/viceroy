package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/accounts"
	"viceroy/internal/bills"
	"viceroy/internal/branding"
	"viceroy/internal/budgetview"
	"viceroy/internal/db"
	"viceroy/internal/money"
	"viceroy/internal/providers/simplefin"
	"viceroy/internal/syncer"
)

func (s *Server) accountRoutes(r chi.Router) {
	r.Get("/accounts", s.handleListAccounts)
	r.Post("/accounts", s.handleCreateAccount)
	r.Post("/accounts/merge", s.handleMergeAccounts)
	r.Patch("/accounts/{id}", s.handleUpdateAccount)
	r.Delete("/accounts/{id}", s.handleDeleteAccount)
	r.Get("/accounts/{id}/history", s.handleAccountHistory)
	r.Post("/accounts/{id}/resolve", s.handleResolveAccount)
	r.Post("/accounts/{id}/color/suggest", s.handleSuggestAccountColor)
	r.Get("/accounts/{id}/logo", s.handleGetAccountLogo)
	r.Put("/accounts/{id}/logo", s.handlePutAccountLogo)
	r.Delete("/accounts/{id}/logo", s.handleDeleteAccountLogo)
	r.Get("/connections", s.handleListConnections)
	r.Post("/connections", s.handleCreateConnection)
	r.Patch("/connections/{id}", s.handleUpdateConnection)
	r.Post("/connections/{id}/sync", s.handleSyncConnection)
	r.Put("/connections/{id}/accounts", s.handleConnectionAccounts)
	r.Delete("/connections/{id}", s.handleDeleteConnection)
	r.Get("/networth/history", s.handleNetWorthHistory)
}

type accountDTO struct {
	ID                int64    `json:"id"`
	Name              string   `json:"name"`
	Type              string   `json:"type"`
	Group             string   `json:"group"`
	IsLiability       bool     `json:"is_liability"`
	InstitutionName   string   `json:"institution_name"`
	InstitutionStatus string   `json:"institution_status"`
	ProviderName      string   `json:"provider_name"`
	Mask              string   `json:"mask"`
	BalanceCents      int64    `json:"balance_cents"`
	AvailableCents    *int64   `json:"available_cents"`
	BalanceAt         *int64   `json:"balance_at"`
	Status            string   `json:"status"`
	ReviewCandidateID *int64   `json:"review_candidate_id"`
	IncludeInNetWorth bool     `json:"include_in_net_worth"`
	Hidden            bool     `json:"hidden"`
	IsManual          bool     `json:"is_manual"`
	Builtin           string   `json:"builtin"` // "" or accounts.PaperCash
	ConnectionID      *int64   `json:"connection_id"`
	LastSyncedAt      *int64   `json:"last_synced_at"`
	Bill              *billDTO `json:"bill"`         // from bank emails the AI read; nil = nothing known
	Color             string   `json:"color"`        // #rrggbb, "" until picked
	ColorSource       string   `json:"color_source"` // ai | auto | user | ""
	LogoURL           *string  `json:"logo_url"`     // uploaded logo, nil = none
	InvertBalance     bool     `json:"invert_balance"` // the bank's balance sign is flipped on sync
	Offered           bool     `json:"offered"`        // shared on SimpleFIN after setup; not added yet
}

// billDTO is an account's payment state. Amounts are cents; dates YYYY-MM-DD.
type billDTO struct {
	DueDate        *string `json:"due_date"`
	DueCents       *int64  `json:"due_cents"`
	MinimumCents   *int64  `json:"minimum_cents"`
	ScheduledDate  *string `json:"scheduled_date"`
	ScheduledCents *int64  `json:"scheduled_cents"`
	PaidDate       *string `json:"paid_date"`
	PaidCents      *int64  `json:"paid_cents"`
}

func strPtr(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}

func toBillDTO(st bills.Status) *billDTO {
	b := &billDTO{}
	if d := st.Due; d != nil {
		b.DueDate, b.DueCents, b.MinimumCents = strPtr(d.Date), ptr(d.AmountCents), ptr(d.MinimumCents)
	}
	if d := st.Scheduled; d != nil {
		b.ScheduledDate, b.ScheduledCents = strPtr(d.Date), ptr(d.AmountCents)
	}
	if d := st.Paid; d != nil {
		b.PaidDate, b.PaidCents = strPtr(d.Date), ptr(d.AmountCents)
	}
	return b
}

func ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}

func (s *Server) accountDTOs(ctx context.Context, hh int64) ([]accountDTO, error) {
	q := db.New(s.db)
	list, err := q.ListAccounts(ctx, hh)
	if err != nil {
		return nil, err
	}
	conns, err := q.ListConnections(ctx, hh)
	if err != nil {
		return nil, err
	}
	insts, err := q.ListInstitutions(ctx, hh)
	if err != nil {
		return nil, err
	}
	lastSync := map[int64]sql.NullInt64{}
	for _, c := range conns {
		lastSync[c.ID] = c.LastSyncAt
	}
	instStatus := map[int64]string{}
	for _, i := range insts {
		instStatus[i.ID] = i.Status
	}
	billRows, err := q.ListRecentBills(ctx, db.ListRecentBillsParams{HouseholdID: hh, CreatedAt: time.Now().Add(-bills.Lookback).Unix()})
	if err != nil {
		return nil, err
	}
	billState := bills.ByAccount(billRows, budgetview.Today().Format(time.DateOnly))
	logoRows, err := q.ListAccountLogoTimes(ctx, hh)
	if err != nil {
		return nil, err
	}
	logos := map[int64]int64{}
	for _, l := range logoRows {
		logos[l.AccountID] = l.UpdatedAt
	}
	out := make([]accountDTO, 0, len(list))
	for _, a := range list {
		d := accountDTO{
			ID: a.ID, Name: a.Name, Type: a.Type, Group: accounts.Group(a.Type), IsLiability: accounts.IsLiability(a.Type),
			InstitutionName: a.InstitutionName, ProviderName: a.ProviderName, Mask: a.Mask,
			BalanceCents: a.BalanceCents, AvailableCents: ptr(a.AvailableCents), BalanceAt: ptr(a.BalanceAt),
			Status: a.Status, ReviewCandidateID: ptr(a.ReviewCandidateID), IncludeInNetWorth: a.IncludeInNetWorth == 1,
			Hidden: a.Hidden == 1, IsManual: a.IsManual == 1, ConnectionID: ptr(a.ConnectionID), InstitutionStatus: "ok",
			Builtin: a.Builtin, Color: a.Color, ColorSource: a.ColorSource,
			InvertBalance: a.InvertBalance == 1, Offered: a.OfferedAt.Valid && a.Status == "ignored",
		}
		if v, ok := logos[a.ID]; ok {
			u := fmt.Sprintf("/api/accounts/%d/logo?v=%d", a.ID, v)
			d.LogoURL = &u
		}
		if a.InstitutionID.Valid {
			if st, ok := instStatus[a.InstitutionID.Int64]; ok {
				d.InstitutionStatus = st
			}
		}
		if a.ConnectionID.Valid {
			d.LastSyncedAt = ptr(lastSync[a.ConnectionID.Int64])
		}
		if st, ok := billState[a.ID]; ok {
			d.Bill = toBillDTO(st)
		}
		out = append(out, d)
	}
	return out, nil
}

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	list, err := s.accountDTOs(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": list, "types": accounts.Types})
}

// parseBalance reads a user-entered balance. Liabilities are entered as the positive amount
// owed and stored negative.
func parseBalance(in, typ string) (int64, error) {
	if strings.TrimSpace(in) == "" {
		return 0, nil
	}
	c, err := money.ParseCents(in)
	if err != nil {
		return 0, errors.New("Balance must be an amount like 1,234.56.")
	}
	if accounts.IsLiability(typ) && c > 0 {
		c = -c
	}
	return c, nil
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Balance string `json:"balance"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "Name is required.")
		return
	}
	if !accounts.ValidType(in.Type) {
		writeError(w, http.StatusBadRequest, "Choose an account type.")
		return
	}
	bal, err := parseBalance(in.Balance, in.Type)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now()
	q := db.New(s.db)
	a, err := q.CreateAccount(r.Context(), db.CreateAccountParams{
		HouseholdID: HouseholdID(r), Name: in.Name, Type: in.Type, Currency: "USD", BalanceCents: bal,
		BalanceAt: sql.NullInt64{Int64: now.Unix(), Valid: true}, Status: "active", IsManual: 1,
		CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	if err := q.UpsertBalanceSnapshot(r.Context(), db.UpsertBalanceSnapshotParams{AccountID: a.ID, Date: now.Format(time.DateOnly), BalanceCents: bal}); err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": a.ID})
}

func (s *Server) loadAccount(w http.ResponseWriter, r *http.Request) (db.Account, bool) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	a, err := db.New(s.db).GetAccount(r.Context(), db.GetAccountParams{ID: id, HouseholdID: HouseholdID(r)})
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Account not found.")
		return a, false
	}
	if err != nil {
		s.internalError(w, err)
		return a, false
	}
	return a, true
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadAccount(w, r)
	if !ok {
		return
	}
	var in struct {
		Name              *string `json:"name"`
		Type              *string `json:"type"`
		IncludeInNetWorth *bool   `json:"include_in_net_worth"`
		Hidden            *bool   `json:"hidden"`
		Closed            *bool   `json:"closed"`
		Balance           *string `json:"balance"`
		Color             *string `json:"color"` // #rrggbb, or "" to let Viceroy pick again
		InvertBalance     *bool   `json:"invert_balance"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	p := db.UpdateAccountSettingsParams{
		Name: a.Name, Type: a.Type, IncludeInNetWorth: a.IncludeInNetWorth, Hidden: a.Hidden, Status: a.Status,
		UpdatedAt: time.Now().Unix(), ID: a.ID, HouseholdID: a.HouseholdID,
	}
	if in.Name != nil {
		if p.Name = strings.TrimSpace(*in.Name); p.Name == "" {
			writeError(w, http.StatusBadRequest, "Name is required.")
			return
		}
	}
	if in.Type != nil && *in.Type != a.Type {
		if a.Builtin != "" {
			writeError(w, http.StatusBadRequest, "Paper Cash is always a cash account.")
			return
		}
		if !accounts.ValidType(*in.Type) {
			writeError(w, http.StatusBadRequest, "Unknown account type.")
			return
		}
		p.Type = *in.Type
	}
	if in.IncludeInNetWorth != nil {
		p.IncludeInNetWorth = b2i(*in.IncludeInNetWorth)
	}
	if in.Hidden != nil {
		p.Hidden = b2i(*in.Hidden)
	}
	if in.Closed != nil {
		switch {
		case *in.Closed:
			p.Status = "closed"
		case a.Status == "closed":
			p.Status = "active"
			if a.ExternalID.Valid && !a.ConnectionID.Valid {
				p.Status = "disconnected"
			}
		}
	}
	if in.Color != nil && *in.Color != "" && !branding.Valid(*in.Color) {
		writeError(w, http.StatusBadRequest, "Colors look like #1a2b3c.")
		return
	}
	q := db.New(s.db)
	if err := q.UpdateAccountSettings(r.Context(), p); err != nil {
		s.internalError(w, err)
		return
	}
	if in.Color != nil {
		c, src := strings.ToLower(*in.Color), branding.SourceUser
		if c == "" {
			src = "" // picked again in the background
		}
		if err := q.SetAccountColor(r.Context(), db.SetAccountColorParams{Color: c, ColorSource: src, UpdatedAt: p.UpdatedAt, ID: a.ID, HouseholdID: a.HouseholdID}); err != nil {
			s.internalError(w, err)
			return
		}
	}
	if in.InvertBalance != nil && b2i(*in.InvertBalance) != a.InvertBalance {
		if a.IsManual == 1 {
			writeError(w, http.StatusBadRequest, "Only synced accounts can flip the bank's balance.")
			return
		}
		// Flip what's stored too, so the balance and its history read right straight away.
		if err := q.SetAccountInvert(r.Context(), db.SetAccountInvertParams{InvertBalance: b2i(*in.InvertBalance), UpdatedAt: p.UpdatedAt, ID: a.ID, HouseholdID: a.HouseholdID}); err != nil {
			s.internalError(w, err)
			return
		}
		if err := q.NegateAccountSnapshots(r.Context(), a.ID); err != nil {
			s.internalError(w, err)
			return
		}
	}
	if in.Balance != nil {
		if a.IsManual != 1 {
			writeError(w, http.StatusBadRequest, "Only manual accounts have an editable balance.")
			return
		}
		bal, err := parseBalance(*in.Balance, p.Type)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		now := time.Now()
		if err := q.SetManualBalance(r.Context(), db.SetManualBalanceParams{
			BalanceCents: bal, BalanceAt: sql.NullInt64{Int64: now.Unix(), Valid: true}, UpdatedAt: now.Unix(), ID: a.ID, HouseholdID: a.HouseholdID,
		}); err != nil {
			s.internalError(w, err)
			return
		}
		if err := q.UpsertBalanceSnapshot(r.Context(), db.UpsertBalanceSnapshotParams{AccountID: a.ID, Date: now.Format(time.DateOnly), BalanceCents: bal}); err != nil {
			s.internalError(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadAccount(w, r)
	if !ok {
		return
	}
	if a.Builtin != "" {
		writeError(w, http.StatusBadRequest, "Paper Cash can't be deleted; turn it off in Settings instead.")
		return
	}
	if err := db.New(s.db).DeleteAccount(r.Context(), db.DeleteAccountParams{ID: a.ID, HouseholdID: a.HouseholdID}); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleResolveAccount(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadAccount(w, r)
	if !ok {
		return
	}
	var in struct {
		Action   string `json:"action"`
		TargetID int64  `json:"target_id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	var err error
	q := db.New(s.db)
	now := time.Now().Unix()
	switch in.Action {
	case "link":
		err = s.sync.Merge(r.Context(), a.HouseholdID, a.ID, in.TargetID)
	case "keep":
		err = q.SetAccountStatus(r.Context(), db.SetAccountStatusParams{Status: "active", UpdatedAt: now, ID: a.ID})
	case "ignore":
		err = q.SetAccountStatus(r.Context(), db.SetAccountStatusParams{Status: "ignored", UpdatedAt: now, ID: a.ID})
	default:
		writeError(w, http.StatusBadRequest, "Unknown action.")
		return
	}
	s.mergeResult(w, err)
}

func (s *Server) handleMergeAccounts(w http.ResponseWriter, r *http.Request) {
	var in struct {
		From int64 `json:"from"`
		Into int64 `json:"into"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if from, err := db.New(s.db).GetAccount(r.Context(), db.GetAccountParams{ID: in.From, HouseholdID: HouseholdID(r)}); err == nil && from.Builtin != "" {
		writeError(w, http.StatusBadRequest, "Paper Cash can't be merged away; merge other accounts into it instead.")
		return
	}
	s.mergeResult(w, s.sync.Merge(r.Context(), HouseholdID(r), in.From, in.Into))
}

func (s *Server) mergeResult(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "Account not found.")
	case errors.Is(err, syncer.ErrMergeSelf):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.internalError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// ---- connections ----

type institutionDTO struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Status    string `json:"status"`
	LastError string `json:"last_error"`
}

type syncEventDTO struct {
	At        int64  `json:"at"`
	Kind      string `json:"kind"`
	AccountID *int64 `json:"account_id"`
	Message   string `json:"message"`
}

type connectionDTO struct {
	ID                int64            `json:"id"`
	Name              string           `json:"name"`
	Provider          string           `json:"provider"`
	Status            string           `json:"status"`
	LastError         string           `json:"last_error"`
	LastSyncAt        *int64           `json:"last_sync_at"`
	NextSyncAt        *int64           `json:"next_sync_at"`
	RequestsRemaining int64            `json:"requests_remaining"`
	RequestsCap       int64            `json:"requests_cap"`       // Viceroy's own daily limit (the Bridge allows 24)
	IntervalHours     int64            `json:"interval_hours"`     // scheduled syncs run about this often
	AutoAddNew        bool             `json:"auto_add_new"`       // accounts shared later are added without asking
	Institutions      []institutionDTO `json:"institutions"`
	Events            []syncEventDTO   `json:"events"`
}

func (s *Server) handleListConnections(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := db.New(s.db)
	conns, err := q.ListConnections(ctx, HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	insts, err := q.ListInstitutions(ctx, HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	today := time.Now().Format(time.DateOnly)
	out := make([]connectionDTO, 0, len(conns))
	for _, c := range conns {
		d := connectionDTO{
			ID: c.ID, Name: c.Name, Provider: c.Provider, Status: c.Status, LastError: c.LastError,
			LastSyncAt: ptr(c.LastSyncAt), NextSyncAt: ptr(c.NextSyncAt), RequestsRemaining: syncer.DailyRequestCap,
			RequestsCap: syncer.DailyRequestCap, IntervalHours: int64(syncer.Interval.Hours()), AutoAddNew: c.AutoAddNew == 1,
			Institutions: []institutionDTO{}, Events: []syncEventDTO{},
		}
		if c.RequestsDay == today {
			d.RequestsRemaining = max(0, syncer.DailyRequestCap-c.RequestsCount)
		}
		for _, i := range insts {
			if i.ConnectionID == c.ID {
				d.Institutions = append(d.Institutions, institutionDTO{ID: i.ID, Name: i.Name, URL: i.Url, Status: i.Status, LastError: i.LastError})
			}
		}
		evs, err := q.ListSyncEvents(ctx, db.ListSyncEventsParams{ConnectionID: c.ID, Limit: 20})
		if err != nil {
			s.internalError(w, err)
			return
		}
		for _, e := range evs {
			d.Events = append(d.Events, syncEventDTO{At: e.At, Kind: e.Kind, AccountID: ptr(e.AccountID), Message: e.Message})
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": out})
}

func (s *Server) handleCreateConnection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SetupToken string `json:"setup_token"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	c, err := s.sync.Connect(r.Context(), HouseholdID(r), in.SetupToken)
	if c.ID == 0 {
		switch {
		case errors.Is(err, simplefin.ErrBadToken), errors.Is(err, simplefin.ErrTokenUsed):
			writeError(w, http.StatusBadRequest, err.Error())
		case err != nil:
			s.log.Warn("simplefin claim failed", "err", err)
			writeError(w, http.StatusBadGateway, err.Error())
		}
		return
	}
	out := map[string]any{"id": c.ID}
	if err != nil {
		out["sync_error"] = err.Error()
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) loadConnection(w http.ResponseWriter, r *http.Request) (db.Connection, bool) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	c, err := db.New(s.db).GetConnection(r.Context(), db.GetConnectionParams{ID: id, HouseholdID: HouseholdID(r)})
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Connection not found.")
		return c, false
	}
	if err != nil {
		s.internalError(w, err)
		return c, false
	}
	return c, true
}

func (s *Server) handleSyncConnection(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadConnection(w, r)
	if !ok {
		return
	}
	if err := s.sync.Sync(r.Context(), c.ID); err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, syncer.ErrDailyCap) {
			status = http.StatusTooManyRequests
		}
		writeError(w, status, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdateConnection(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadConnection(w, r)
	if !ok {
		return
	}
	var in struct {
		AutoAddNew *bool `json:"auto_add_new"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.AutoAddNew != nil {
		if err := s.sync.SetAutoAdd(r.Context(), c.HouseholdID, c.ID, *in.AutoAddNew); err != nil {
			s.internalError(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleConnectionAccounts picks which of a connection's accounts Viceroy follows. Accounts
// added get their history fetched right away; if that can't happen now, it loads at the next
// sync and the response says so.
func (s *Server) handleConnectionAccounts(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadConnection(w, r)
	if !ok {
		return
	}
	var in struct {
		Include []int64 `json:"include"`
		Exclude []int64 `json:"exclude"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	err := s.sync.SetTracked(r.Context(), c.ID, in.Include, in.Exclude)
	switch {
	case errors.Is(err, syncer.HistoryPending):
		s.log.Warn("simplefin history fetch for added accounts failed", "connection", c.ID, "err", err)
		writeJSON(w, http.StatusOK, map[string]any{"warning": "Added, but SimpleFIN couldn't send their history right now; it will load at the next sync."})
	case errors.Is(err, syncer.ErrNotOnConnection):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil && strings.Contains(err.Error(), "can't be removed"):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.internalError(w, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{})
	}
}

func (s *Server) handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadConnection(w, r)
	if !ok {
		return
	}
	q := db.New(s.db)
	// Accounts keep their history; they just stop syncing.
	if err := q.DisconnectConnectionAccounts(r.Context(), db.DisconnectConnectionAccountsParams{UpdatedAt: time.Now().Unix(), ConnectionID: sql.NullInt64{Int64: c.ID, Valid: true}}); err != nil {
		s.internalError(w, err)
		return
	}
	if err := q.DeleteConnection(r.Context(), db.DeleteConnectionParams{ID: c.ID, HouseholdID: c.HouseholdID}); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- net worth ----

type netWorthPoint struct {
	Date        string `json:"date"`
	Assets      int64  `json:"assets"`
	Liabilities int64  `json:"liabilities"`
	Net         int64  `json:"net"`
	// Signed balance per account group (cash, credit, investments, loans, other).
	Groups map[string]int64 `json:"groups"`
}

// handleNetWorthHistory returns one point per day, carrying each account's last known
// balance forward. Only accounts included in net worth (and not ignored) count.
func (s *Server) handleNetWorthHistory(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 3660 {
		days = 90
	}
	out, err := s.netWorthHistory(r.Context(), HouseholdID(r), days)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"points": out})
}

// netWorthHistory returns one point per day for the last days days (from the first snapshot).
func (s *Server) netWorthHistory(ctx context.Context, hh int64, days int) ([]netWorthPoint, error) {
	accts, src, err := s.balanceSources(ctx, hh)
	if err != nil {
		return nil, err
	}
	var series []accountHistory
	earliest := ""
	for _, a := range accts {
		if a.IncludeInNetWorth != 1 || a.Status == "ignored" || a.Status == "review" {
			continue
		}
		h, known, ok := src.history(a)
		if !ok {
			continue
		}
		if earliest == "" || known < earliest {
			earliest = known
		}
		series = append(series, h)
	}
	out := []netWorthPoint{}
	for _, ds := range historyDays(days, earliest) {
		p := netWorthPoint{Date: ds, Groups: map[string]int64{}}
		for i := range series {
			b := series[i].at(ds)
			p.Groups[series[i].group] += b
			if b >= 0 {
				p.Assets += b
			} else {
				p.Liabilities -= b
			}
		}
		p.Net = p.Assets - p.Liabilities
		out = append(out, p)
	}
	return out, nil
}

// balanceSources is what account balance histories are built from: snapshots and daily
// transaction totals per account.
type balanceSources struct {
	snaps map[int64][]db.BalanceSnapshot
	txns  map[int64]map[string]int64
}

func (s *Server) balanceSources(ctx context.Context, hh int64) ([]db.Account, balanceSources, error) {
	q := db.New(s.db)
	src := balanceSources{snaps: map[int64][]db.BalanceSnapshot{}, txns: map[int64]map[string]int64{}}
	accts, err := q.ListAccounts(ctx, hh)
	if err != nil {
		return nil, src, err
	}
	snaps, err := q.ListHouseholdSnapshots(ctx, db.ListHouseholdSnapshotsParams{HouseholdID: hh, Date: ""})
	if err != nil {
		return nil, src, err
	}
	totals, err := q.DailyAccountTotals(ctx, hh)
	if err != nil {
		return nil, src, err
	}
	for _, t := range totals {
		if src.txns[t.AccountID] == nil {
			src.txns[t.AccountID] = map[string]int64{}
		}
		src.txns[t.AccountID][t.Date] = t.Total
	}
	for _, sn := range snaps {
		src.snaps[sn.AccountID] = append(src.snaps[sn.AccountID], sn)
	}
	return accts, src, nil
}

// history builds one account's balance history and the earliest day anything is known about
// it; ok is false when nothing is.
func (src balanceSources) history(a db.Account) (h accountHistory, known string, ok bool) {
	today := time.Now().Format(time.DateOnly)
	h = accountHistory{group: accounts.Group(a.Type), snaps: src.snaps[a.ID]}
	anchorDate, anchorBal := today, a.BalanceCents // no snapshot yet: today's balance
	if len(h.snaps) > 0 {
		anchorDate, anchorBal = h.snaps[0].Date, h.snaps[0].BalanceCents
	}
	h.before = backfill(anchorDate, anchorBal, src.txns[a.ID])
	h.anchor = anchorDate
	known = anchorDate
	for d := range src.txns[a.ID] {
		known = min(known, d)
	}
	if len(h.snaps) == 0 && len(src.txns[a.ID]) == 0 && a.BalanceCents == 0 {
		return h, "", false
	}
	return h, known, true
}

// historyDays lists the days of a history ending today: the last `days` days, but not before
// earliest ("" = nothing known, no days; don't draw a fake zero line). It walks plain dates (UTC
// midnights) up to today's local date, not the local clock: in the evening UTC is already
// tomorrow.
func historyDays(days int, earliest string) []string {
	if earliest == "" {
		return nil
	}
	end := time.Now()
	start := end.AddDate(0, 0, -(days - 1)).Format(time.DateOnly)
	from, _ := time.Parse(time.DateOnly, max(start, earliest))
	last, _ := time.Parse(time.DateOnly, end.Format(time.DateOnly))
	out := make([]string, 0, days)
	for d := from; !d.After(last); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format(time.DateOnly))
	}
	return out
}

type balancePoint struct {
	Date    string `json:"date"`
	Balance int64  `json:"balance"`
}

// handleAccountHistory returns one balance per day for an account (signed, like balance_cents).
func (s *Server) handleAccountHistory(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadAccount(w, r)
	if !ok {
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 3660 {
		days = 30
	}
	_, src, err := s.balanceSources(r.Context(), a.HouseholdID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := []balancePoint{}
	if h, known, ok := src.history(a); ok {
		for _, d := range historyDays(days, known) {
			out = append(out, balancePoint{Date: d, Balance: h.at(d)})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"points": out})
}

// accountHistory yields one account's balance on any day: the latest snapshot on or before
// it, or before the first snapshot a balance rebuilt from transactions.
type accountHistory struct {
	group  string
	snaps  []db.BalanceSnapshot // ascending by date
	anchor string               // first snapshot date (or today)
	before func(date string) int64
	next   int   // index of the next snapshot not yet applied (days are visited in order)
	cur    int64 // balance from the latest applied snapshot
}

func (h *accountHistory) at(date string) int64 {
	if date < h.anchor || len(h.snaps) == 0 {
		return h.before(date)
	}
	for h.next < len(h.snaps) && h.snaps[h.next].Date <= date {
		h.cur = h.snaps[h.next].BalanceCents
		h.next++
	}
	return h.cur
}

// backfill returns the balance on a day before anchorDate: the anchor balance minus every
// transaction dated after that day, up to and including anchorDate. Before the account's first
// transaction it stays flat.
func backfill(anchorDate string, anchorBal int64, daily map[string]int64) func(string) int64 {
	dates := make([]string, 0, len(daily))
	for d := range daily {
		if d <= anchorDate {
			dates = append(dates, d)
		}
	}
	sort.Strings(dates)
	// suffix[i] = sum of daily[dates[i:]]
	suffix := make([]int64, len(dates)+1)
	for i := len(dates) - 1; i >= 0; i-- {
		suffix[i] = suffix[i+1] + daily[dates[i]]
	}
	return func(date string) int64 {
		i := sort.SearchStrings(dates, date)
		if i < len(dates) && dates[i] == date {
			i++ // that day's transactions are already in the balance
		}
		return anchorBal - suffix[i]
	}
}
