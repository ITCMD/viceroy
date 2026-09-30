package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/accounts"
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
	r.Post("/accounts/{id}/resolve", s.handleResolveAccount)
	r.Get("/connections", s.handleListConnections)
	r.Post("/connections", s.handleCreateConnection)
	r.Post("/connections/{id}/sync", s.handleSyncConnection)
	r.Delete("/connections/{id}", s.handleDeleteConnection)
	r.Get("/networth/history", s.handleNetWorthHistory)
}

type accountDTO struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	Type              string `json:"type"`
	Group             string `json:"group"`
	IsLiability       bool   `json:"is_liability"`
	InstitutionName   string `json:"institution_name"`
	InstitutionStatus string `json:"institution_status"`
	ProviderName      string `json:"provider_name"`
	Mask              string `json:"mask"`
	BalanceCents      int64  `json:"balance_cents"`
	AvailableCents    *int64 `json:"available_cents"`
	BalanceAt         *int64 `json:"balance_at"`
	Status            string `json:"status"`
	ReviewCandidateID *int64 `json:"review_candidate_id"`
	IncludeInNetWorth bool   `json:"include_in_net_worth"`
	Hidden            bool   `json:"hidden"`
	IsManual          bool   `json:"is_manual"`
	Builtin           string `json:"builtin"` // "" or accounts.PaperCash
	ConnectionID      *int64 `json:"connection_id"`
	LastSyncedAt      *int64 `json:"last_synced_at"`
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
	out := make([]accountDTO, 0, len(list))
	for _, a := range list {
		d := accountDTO{
			ID: a.ID, Name: a.Name, Type: a.Type, Group: accounts.Group(a.Type), IsLiability: accounts.IsLiability(a.Type),
			InstitutionName: a.InstitutionName, ProviderName: a.ProviderName, Mask: a.Mask,
			BalanceCents: a.BalanceCents, AvailableCents: ptr(a.AvailableCents), BalanceAt: ptr(a.BalanceAt),
			Status: a.Status, ReviewCandidateID: ptr(a.ReviewCandidateID), IncludeInNetWorth: a.IncludeInNetWorth == 1,
			Hidden: a.Hidden == 1, IsManual: a.IsManual == 1, ConnectionID: ptr(a.ConnectionID), InstitutionStatus: "ok",
			Builtin: a.Builtin,
		}
		if a.InstitutionID.Valid {
			if st, ok := instStatus[a.InstitutionID.Int64]; ok {
				d.InstitutionStatus = st
			}
		}
		if a.ConnectionID.Valid {
			d.LastSyncedAt = ptr(lastSync[a.ConnectionID.Int64])
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
	q := db.New(s.db)
	if err := q.UpdateAccountSettings(r.Context(), p); err != nil {
		s.internalError(w, err)
		return
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
	q := db.New(s.db)
	accts, err := q.ListAccounts(ctx, hh)
	if err != nil {
		return nil, err
	}
	counted := map[int64]bool{}
	group := map[int64]string{}
	for _, a := range accts {
		counted[a.ID] = a.IncludeInNetWorth == 1 && a.Status != "ignored" && a.Status != "review"
		group[a.ID] = accounts.Group(a.Type)
	}
	end := time.Now()
	start := end.AddDate(0, 0, -(days - 1))
	startStr := start.Format(time.DateOnly)
	seed, err := q.ListAccountSnapshotsBefore(ctx, db.ListAccountSnapshotsBeforeParams{HouseholdID: hh, Date: startStr})
	if err != nil {
		return nil, err
	}
	snaps, err := q.ListHouseholdSnapshots(ctx, db.ListHouseholdSnapshotsParams{HouseholdID: hh, Date: startStr})
	if err != nil {
		return nil, err
	}
	cur := map[int64]int64{}
	for _, sn := range seed {
		cur[sn.AccountID] = sn.BalanceCents
	}
	byDate := map[string][]db.BalanceSnapshot{}
	for _, sn := range snaps {
		byDate[sn.Date] = append(byDate[sn.Date], sn)
	}
	out := make([]netWorthPoint, 0, days)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		ds := d.Format(time.DateOnly)
		for _, sn := range byDate[ds] {
			cur[sn.AccountID] = sn.BalanceCents
		}
		if len(cur) == 0 {
			continue // no history yet; don't draw a fake zero line
		}
		p := netWorthPoint{Date: ds, Groups: map[string]int64{}}
		ids := make([]int64, 0, len(cur))
		for id := range cur {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			if !counted[id] {
				continue
			}
			b := cur[id]
			p.Groups[group[id]] += b
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
