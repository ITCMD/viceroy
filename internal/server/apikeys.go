package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/db"
)

// The REST API is the same /api the app uses, reachable with an API key
// (Authorization: Bearer vk_...) once an admin turns it on. Keys act as the user who made them;
// read keys may only GET.

const (
	setAPIEnabled = "api.enabled"
	keyPrefix     = "vk_"
	scopeRead     = "read"
	scopeWrite    = "write"
)

type apiKeyKey struct{}

// APIKey returns the key a request authenticated with, if any.
func APIKey(r *http.Request) (db.ApiKey, bool) {
	k, ok := r.Context().Value(apiKeyKey{}).(db.ApiKey)
	return k, ok
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:]), true
	}
	return "", false
}

func hashKey(k string) []byte {
	h := sha256.Sum256([]byte(k))
	return h[:]
}

func apiEnabled(ctx context.Context, q *db.Queries, hh int64) (bool, error) {
	rows, err := q.ListHouseholdSettings(ctx, hh)
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		if r.Key == setAPIEnabled {
			return r.Value == "1", nil
		}
	}
	return false, nil
}

// authAPIKey authenticates a bearer key. status/msg describe a rejection.
func (s *Server) authAPIKey(r *http.Request, token string) (u db.User, key db.ApiKey, status int, msg string, err error) {
	ip := ClientIP(r).String()
	if !s.limiter.allow(ip) {
		return u, key, http.StatusTooManyRequests, "Too many bad API keys. Try again in a few minutes.", nil
	}
	ctx, q := r.Context(), db.New(s.db)
	key, err = q.GetAPIKeyByHash(ctx, hashKey(token))
	if errors.Is(err, sql.ErrNoRows) || !strings.HasPrefix(token, keyPrefix) {
		s.limiter.fail(ip)
		return u, key, http.StatusUnauthorized, "Invalid API key.", nil
	}
	if err != nil {
		return u, key, 0, "", err
	}
	on, err := apiEnabled(ctx, q, key.HouseholdID)
	if err != nil {
		return u, key, 0, "", err
	}
	if !on {
		return u, key, http.StatusForbidden, "The REST API is turned off (Settings → API).", nil
	}
	if u, err = q.GetUser(ctx, key.UserID); err != nil {
		return u, key, 0, "", err
	}
	if key.Scope != scopeWrite && r.Method != http.MethodGet && r.Method != http.MethodHead {
		return u, key, http.StatusForbidden, "This API key is read-only.", nil
	}
	now := time.Now().Unix()
	if !key.LastUsedAt.Valid || now-key.LastUsedAt.Int64 > 60 {
		q.TouchAPIKey(ctx, db.TouchAPIKeyParams{LastUsedAt: sql.NullInt64{Int64: now, Valid: true}, ID: key.ID})
	}
	return u, key, 0, "", nil
}

// sessionOnly keeps API keys away from routes that manage access itself.
func sessionOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := APIKey(r); ok {
			writeError(w, http.StatusForbidden, "This endpoint is only available in the app, not with an API key.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) apiKeyRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(sessionOnly)
		r.Get("/settings/api", s.handleGetAPISettings)
		r.Patch("/settings/api", s.handleSetAPIEnabled)
		r.Post("/settings/api/keys", s.handleCreateAPIKey)
		r.Delete("/settings/api/keys/{id}", s.handleRevokeAPIKey)
	})
	r.Get("/docs", s.handleAPIDocs)
	r.Get("/openapi.json", s.handleOpenAPI)
}

type apiKeyDTO struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	Scope      string `json:"scope"`
	CreatedBy  string `json:"created_by"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt *int64 `json:"last_used_at"`
}

func (s *Server) handleGetAPISettings(w http.ResponseWriter, r *http.Request) {
	ctx, hh, q := r.Context(), HouseholdID(r), db.New(s.db)
	on, err := apiEnabled(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	admin := CurrentUser(r).IsAdmin == 1
	keys := []apiKeyDTO{}
	if admin {
		rows, err := q.ListAPIKeys(ctx, hh)
		if err != nil {
			s.internalError(w, err)
			return
		}
		for _, k := range rows {
			keys = append(keys, apiKeyDTO{k.ID, k.Name, k.Prefix, k.Scope, k.UserName, k.CreatedAt, ptr(k.LastUsedAt)})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": on, "can_edit": admin, "keys": keys})
}

func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if CurrentUser(r).IsAdmin != 1 {
		writeError(w, http.StatusForbidden, "Only an admin can manage API access.")
		return false
	}
	return true
}

func (s *Server) handleSetAPIEnabled(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := db.New(s.db).SetHouseholdSetting(r.Context(), db.SetHouseholdSettingParams{
		HouseholdID: HouseholdID(r), Key: setAPIEnabled, Value: map[bool]string{true: "1", false: "0"}[in.Enabled],
	}); err != nil {
		s.internalError(w, err)
		return
	}
	s.handleGetAPISettings(w, r)
}

const keyAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newAPIKey() (string, error) {
	var b strings.Builder
	b.WriteString(keyPrefix)
	for range 40 {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(keyAlphabet))))
		if err != nil {
			return "", err
		}
		b.WriteByte(keyAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// POST /settings/api/keys {name, scope}: the key is in the response once and never again.
func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var in struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 80 {
		writeError(w, http.StatusBadRequest, "Give the key a name (what will use it).")
		return
	}
	if in.Scope != scopeRead && in.Scope != scopeWrite {
		writeError(w, http.StatusBadRequest, "Access must be read or write.")
		return
	}
	token, err := newAPIKey()
	if err != nil {
		s.internalError(w, err)
		return
	}
	u := CurrentUser(r)
	k, err := db.New(s.db).InsertAPIKey(r.Context(), db.InsertAPIKeyParams{
		HouseholdID: HouseholdID(r), UserID: u.ID, Name: in.Name, Prefix: token[:len(keyPrefix)+6],
		TokenHash: hashKey(token), Scope: in.Scope, CreatedAt: time.Now().Unix(),
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"key": token, "api_key": apiKeyDTO{k.ID, k.Name, k.Prefix, k.Scope, u.Name, k.CreatedAt, nil},
	})
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	n, err := db.New(s.db).RevokeAPIKey(r.Context(), db.RevokeAPIKeyParams{
		RevokedAt: sql.NullInt64{Int64: time.Now().Unix(), Valid: true}, ID: txnID(r), HouseholdID: HouseholdID(r),
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "API key not found.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
