// Package server wires the HTTP API and the embedded web app.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"viceroy/internal/ai"
	"viceroy/internal/auth"
	"viceroy/internal/config"
	"viceroy/internal/db"
	"viceroy/internal/email"
	"viceroy/internal/notify"
	"viceroy/internal/syncer"
)

const sessionCookie = "viceroy_session"

type Server struct {
	cfg     config.Config
	db      *sql.DB
	auth    *auth.Service
	web     fs.FS
	limiter *loginLimiter
	log     *slog.Logger
	sync    *syncer.Service
	mail    *email.Service
	notify  *notify.Service
	ai      *ai.Client
}

func New(cfg config.Config, conn *sql.DB, web fs.FS, log *slog.Logger, sync *syncer.Service, mail *email.Service, nt *notify.Service, chat *ai.Client) *Server {
	return &Server{
		cfg: cfg, db: conn, auth: auth.New(conn), web: web,
		limiter: newLoginLimiter(10, 15*time.Minute), log: log, sync: sync, mail: mail, notify: nt, ai: chat,
	}
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(IPAllow(s.cfg.Allowed, s.cfg.Proxies))
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)

	r.Route("/api", func(r chi.Router) {
		r.Use(requireCSRFHeader)
		r.Use(middleware.NoCache)
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
		r.Get("/session", s.handleSession)
		r.Post("/setup", s.handleSetup)
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/logout", s.handleLogout)

		r.Group(func(r chi.Router) {
			r.Use(s.requireUser)
			s.accountRoutes(r)
			s.transactionRoutes(r)
			s.settingsRoutes(r)
			s.emailRoutes(r)
			s.budgetRoutes(r)
			s.reportRoutes(r)
			s.notifyRoutes(r)
			s.chatRoutes(r)
		})
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusNotFound, "not found")
		})
	})
	r.Handle("/*", spaHandler(s.web))
	return r
}

// ---- middleware ----

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// requireCSRFHeader forces unsafe API requests to carry a custom header. Browsers
// cannot send one cross-origin without a CORS preflight, which we never approve.
func requireCSRFHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if r.Header.Get("X-Viceroy-CSRF") != "1" {
				writeError(w, http.StatusForbidden, "missing CSRF header")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type userKey struct{}
type householdKey struct{}

// CurrentUser returns the authenticated user set by requireUser.
func CurrentUser(r *http.Request) db.User {
	u, _ := r.Context().Value(userKey{}).(db.User)
	return u
}

// HouseholdID returns the current user's household, set by requireUser. All feature data is
// scoped by it.
func HouseholdID(r *http.Request) int64 {
	h, _ := r.Context().Value(householdKey{}).(int64)
	return h
}

func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.userFromRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "not signed in")
			return
		}
		h, err := db.New(s.db).GetUserHousehold(r.Context(), u.ID)
		if err != nil {
			s.internalError(w, err)
			return
		}
		ctx := context.WithValue(r.Context(), userKey{}, u)
		ctx = context.WithValue(ctx, householdKey{}, h.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) userFromRequest(r *http.Request) (db.User, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return db.User{}, auth.ErrNoSession
	}
	return s.auth.Authenticate(r.Context(), c.Value)
}

// ---- handlers ----

type userDTO struct {
	ID      int64  `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	IsAdmin bool   `json:"is_admin"`
}

type householdDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type sessionDTO struct {
	NeedsSetup bool          `json:"needs_setup"`
	User       *userDTO      `json:"user"`
	Household  *householdDTO `json:"household"`
}

func toUserDTO(u db.User) *userDTO {
	return &userDTO{ID: u.ID, Email: u.Email, Name: u.Name, IsAdmin: u.IsAdmin == 1}
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	needs, err := s.auth.NeedsSetup(ctx)
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := sessionDTO{NeedsSetup: needs}
	if u, err := s.userFromRequest(r); err == nil {
		out.User = toUserDTO(u)
		if h, err := db.New(s.db).GetUserHousehold(ctx, u.ID); err == nil {
			out.Household = &householdDTO{ID: h.ID, Name: h.Name}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var in auth.SetupInput
	if !readJSON(w, r, &in) {
		return
	}
	u, err := s.auth.Setup(r.Context(), in)
	var ve auth.ValidationError
	switch {
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, ve.Msg)
		return
	case errors.Is(err, auth.ErrAlreadySetUp):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		s.internalError(w, err)
		return
	}
	s.log.Info("initial setup complete", "user", u.Email)
	s.startSession(w, r, u)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ip := ClientIP(r).String()
	if !s.limiter.allow(ip) {
		writeError(w, http.StatusTooManyRequests, "Too many attempts. Try again in a few minutes.")
		return
	}
	u, err := s.auth.Login(r.Context(), in.Email, in.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		s.limiter.fail(ip)
		writeError(w, http.StatusUnauthorized, "Invalid email or password.")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	s.limiter.reset(ip)
	s.startSession(w, r, u)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u db.User) {
	token, exp, err := s.auth.CreateSession(r.Context(), u.ID, r.UserAgent(), ClientIP(r).String())
	if err != nil {
		s.internalError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: exp,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.secureCookies(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserDTO(u)})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.auth.Logout(r.Context(), c.Value); err != nil {
			s.internalError(w, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.secureCookies(r),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) secureCookies(r *http.Request) bool {
	return r.TLS != nil || strings.HasPrefix(s.cfg.PublicURL, "https://")
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func (s *Server) internalError(w http.ResponseWriter, err error) {
	s.log.Error("request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
