package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/auth"
	"viceroy/internal/db"
)

// Household management: members, roles, join links and password reset links. Changes are
// admin only and never available to API keys.
func (s *Server) householdRoutes(r chi.Router) {
	r.Get("/household", s.handleGetHousehold)
	r.Group(func(r chi.Router) {
		r.Use(sessionOnly)
		r.Patch("/household", s.handleRenameHousehold)
		r.Post("/household/invites", s.handleCreateInvite)
		r.Delete("/household/invites/{id}", s.handleRevokeInvite)
		r.Patch("/household/members/{id}", s.handleUpdateMember)
		r.Delete("/household/members/{id}", s.handleRemoveMember)
		r.Patch("/me", s.handleUpdateMe)
		r.Post("/me/password", s.handleChangePassword)
		r.Post("/me/email", s.handleChangeEmail)
	})
}

// publicInviteRoutes let someone with a link see what it's for and use it, without a session.
func (s *Server) publicInviteRoutes(r chi.Router) {
	r.Get("/invites/{token}", s.handleGetInvite)
	r.Post("/invites/{token}/accept", s.handleAcceptInvite)
}

type memberDTO struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	IsAdmin  bool   `json:"is_admin"`
	JoinedAt int64  `json:"joined_at"`
	IsYou    bool   `json:"is_you"`
}

type inviteDTO struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"` // join | reset
	Label     string `json:"label"`
	UserID    *int64 `json:"user_id"`
	UserName  string `json:"user_name"`
	CreatedBy string `json:"created_by"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt int64  `json:"expires_at"`
}

func inviteKind(userID int64) string {
	if userID != 0 {
		return "reset"
	}
	return "join"
}

func (s *Server) handleGetHousehold(w http.ResponseWriter, r *http.Request) {
	ctx, hh, me, q := r.Context(), HouseholdID(r), CurrentUser(r), db.New(s.db)
	h, err := q.GetUserHousehold(ctx, me.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	rows, err := q.ListHouseholdMembers(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	members := make([]memberDTO, len(rows))
	for i, m := range rows {
		members[i] = memberDTO{m.ID, m.Name, m.Email, m.IsAdmin == 1, m.JoinedAt, m.ID == me.ID}
	}
	admin := me.IsAdmin == 1
	invites := []inviteDTO{}
	if admin {
		open, err := q.ListOpenInvites(ctx, db.ListOpenInvitesParams{HouseholdID: hh, ExpiresAt: s.auth.Now().Unix()})
		if err != nil {
			s.internalError(w, err)
			return
		}
		for _, i := range open {
			invites = append(invites, inviteDTO{i.ID, inviteKind(i.UserID.Int64), i.Label, ptr(i.UserID), i.UserName, i.CreatedByName, i.CreatedAt, i.ExpiresAt})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": h.ID, "name": h.Name, "members": members, "invites": invites, "can_manage": admin,
	})
}

func householdAdmin(w http.ResponseWriter, r *http.Request) bool {
	if CurrentUser(r).IsAdmin != 1 {
		writeError(w, http.StatusForbidden, "Only an admin can manage the household.")
		return false
	}
	return true
}

func (s *Server) handleRenameHousehold(w http.ResponseWriter, r *http.Request) {
	if !householdAdmin(w, r) {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 80 {
		writeError(w, http.StatusBadRequest, "Enter a household name (up to 80 characters).")
		return
	}
	if err := db.New(s.db).RenameHousehold(r.Context(), db.RenameHouseholdParams{Name: in.Name, ID: HouseholdID(r)}); err != nil {
		s.internalError(w, err)
		return
	}
	s.handleGetHousehold(w, r)
}

func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	if !householdAdmin(w, r) {
		return
	}
	var in struct {
		Label  string `json:"label"`
		UserID int64  `json:"user_id"` // set = password reset link for this member
	}
	if !readJSON(w, r, &in) {
		return
	}
	inv, err := s.auth.CreateInvite(r.Context(), HouseholdID(r), CurrentUser(r).ID, in.UserID, in.Label)
	if errors.Is(err, auth.ErrNotMember) {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	url := "" // the app builds it from its own address when public_url isn't set
	if s.cfg.PublicURL != "" {
		url = strings.TrimRight(s.cfg.PublicURL, "/") + "/join/" + inv.Token
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": inv.Row.ID, "kind": inviteKind(in.UserID), "token": inv.Token, "url": url, "expires_at": inv.Row.ExpiresAt,
	})
}

func (s *Server) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	if !householdAdmin(w, r) {
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	n, err := db.New(s.db).RevokeInvite(r.Context(), db.RevokeInviteParams{
		RevokedAt: sql.NullInt64{Int64: s.auth.Now().Unix(), Valid: true}, ID: id, HouseholdID: HouseholdID(r),
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "link not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) memberError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrNotMember):
		writeError(w, http.StatusNotFound, "member not found")
	case errors.Is(err, auth.ErrLastAdmin):
		writeError(w, http.StatusConflict, "The household needs at least one admin. Make someone else an admin first.")
	default:
		s.internalError(w, err)
	}
}

func (s *Server) handleUpdateMember(w http.ResponseWriter, r *http.Request) {
	if !householdAdmin(w, r) {
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var in struct {
		IsAdmin bool `json:"is_admin"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.auth.SetAdmin(r.Context(), HouseholdID(r), id, in.IsAdmin); err != nil {
		s.memberError(w, err)
		return
	}
	s.handleGetHousehold(w, r)
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	if !householdAdmin(w, r) {
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id == CurrentUser(r).ID {
		writeError(w, http.StatusBadRequest, "You can't remove yourself.")
		return
	}
	if err := s.auth.RemoveMember(r.Context(), HouseholdID(r), id); err != nil {
		s.memberError(w, err)
		return
	}
	s.handleGetHousehold(w, r)
}

// lookupInvite resolves the {token} URL param, counting bad tokens toward the login limiter
// so links can't be guessed.
func (s *Server) lookupInvite(w http.ResponseWriter, r *http.Request) (db.GetOpenInviteByHashRow, bool) {
	ip := ClientIP(r).String()
	if !s.limiter.allow(ip) {
		writeError(w, http.StatusTooManyRequests, "Too many attempts. Try again in a few minutes.")
		return db.GetOpenInviteByHashRow{}, false
	}
	inv, err := s.auth.LookupInvite(r.Context(), chi.URLParam(r, "token"))
	if errors.Is(err, auth.ErrInviteInvalid) {
		s.limiter.fail(ip)
		writeError(w, http.StatusNotFound, "This link is invalid, has expired or was already used. Ask an admin for a new one.")
		return inv, false
	}
	if err != nil {
		s.internalError(w, err)
		return inv, false
	}
	return inv, true
}

func (s *Server) handleGetInvite(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.lookupInvite(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind": inviteKind(inv.UserID.Int64), "household_name": inv.HouseholdName,
		"invited_by": inv.CreatedByName, "email": inv.UserEmail, "expires_at": inv.ExpiresAt,
	})
}

func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupInvite(w, r); !ok {
		return
	}
	var in auth.AcceptInput
	if !readJSON(w, r, &in) {
		return
	}
	u, err := s.auth.AcceptInvite(r.Context(), chi.URLParam(r, "token"), in)
	var ve auth.ValidationError
	switch {
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, ve.Msg)
		return
	case errors.Is(err, auth.ErrEmailTaken):
		writeError(w, http.StatusConflict, "An account with this email already exists. Sign in instead, or use another email.")
		return
	case errors.Is(err, auth.ErrInviteInvalid):
		writeError(w, http.StatusNotFound, "This link is invalid, has expired or was already used. Ask an admin for a new one.")
		return
	case err != nil:
		s.internalError(w, err)
		return
	}
	s.log.Info("invite used", "user", u.Email)
	s.startSession(w, r, u)
}

// handleUpdateMe changes the signed-in user's display name.
func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 80 {
		writeError(w, http.StatusBadRequest, "Enter your name (up to 80 characters).")
		return
	}
	u := CurrentUser(r)
	if err := db.New(s.db).SetUserName(r.Context(), db.SetUserNameParams{Name: in.Name, ID: u.ID}); err != nil {
		s.internalError(w, err)
		return
	}
	u.Name = in.Name
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserDTO(u)})
}

// handleChangePassword needs the current password. Wrong ones count toward the login
// limiter. Other sessions are signed out; this one stays.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ip := ClientIP(r).String()
	if !s.limiter.allow(ip) {
		writeError(w, http.StatusTooManyRequests, "Too many attempts. Try again in a few minutes.")
		return
	}
	c, _ := r.Cookie(sessionCookie) // present: sessionOnly ran
	err := s.auth.ChangePassword(r.Context(), CurrentUser(r).ID, in.Current, in.New, c.Value)
	var ve auth.ValidationError
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		s.limiter.fail(ip)
		writeError(w, http.StatusBadRequest, "Your current password isn't right.")
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, ve.Msg)
	case err != nil:
		s.internalError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleChangeEmail needs the current password, like handleChangePassword. There's no
// confirmation mail (Viceroy can't send email), so the change applies right away.
func (s *Server) handleChangeEmail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current_password"`
		Email   string `json:"email"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ip := ClientIP(r).String()
	if !s.limiter.allow(ip) {
		writeError(w, http.StatusTooManyRequests, "Too many attempts. Try again in a few minutes.")
		return
	}
	u := CurrentUser(r)
	err := s.auth.ChangeEmail(r.Context(), u.ID, in.Current, in.Email)
	var ve auth.ValidationError
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		s.limiter.fail(ip)
		writeError(w, http.StatusBadRequest, "Your current password isn't right.")
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, ve.Msg)
	case errors.Is(err, auth.ErrEmailTaken):
		writeError(w, http.StatusConflict, "That email is already in use.")
	case err != nil:
		s.internalError(w, err)
	default:
		s.log.Info("email changed", "user", u.ID, "from", u.Email, "to", strings.TrimSpace(in.Email))
		u.Email = strings.TrimSpace(in.Email)
		writeJSON(w, http.StatusOK, map[string]any{"user": toUserDTO(u)})
	}
}
