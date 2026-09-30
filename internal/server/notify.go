package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/db"
	"viceroy/internal/notify"
)

func (s *Server) notifyRoutes(r chi.Router) {
	r.Get("/notifications", s.handleListNotifications)
	r.Post("/notifications/read", s.handleReadNotifications)
	r.Get("/notifications/settings", s.handleGetNotifySettings)
	r.Put("/notifications/settings", s.handleSetNotifySettings)
	r.Post("/notifications/subscriptions", s.handleSubscribe)
	r.Delete("/notifications/subscriptions/{id}", s.handleUnsubscribe)
	r.Post("/notifications/test", s.handleTestNotification)
}

type notificationDTO struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	URL       string `json:"url"`
	CreatedAt int64  `json:"created_at"`
	Read      bool   `json:"read"`
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	ctx, u, q := r.Context(), CurrentUser(r), db.New(s.db)
	rows, err := q.ListNotifications(ctx, db.ListNotificationsParams{UserID: u.ID, Limit: 50})
	if err != nil {
		s.internalError(w, err)
		return
	}
	unread, err := q.CountUnreadNotifications(ctx, u.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := make([]notificationDTO, len(rows))
	for i, n := range rows {
		out[i] = notificationDTO{ID: n.ID, Kind: n.Kind, Title: n.Title, Body: n.Body, URL: n.Url, CreatedAt: n.CreatedAt, Read: n.ReadAt.Valid}
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": out, "unread": unread})
}

func (s *Server) handleReadNotifications(w http.ResponseWriter, r *http.Request) {
	if err := db.New(s.db).MarkNotificationsRead(r.Context(), db.MarkNotificationsReadParams{
		ReadAt: sql.NullInt64{Int64: time.Now().Unix(), Valid: true}, UserID: CurrentUser(r).ID,
	}); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type deviceDTO struct {
	ID        int64  `json:"id"`
	Endpoint  string `json:"endpoint"`
	UserAgent string `json:"user_agent"`
	CreatedAt int64  `json:"created_at"`
}

type notifySettingsDTO struct {
	PublicKey string       `json:"public_key"`
	Prefs     notify.Prefs `json:"prefs"`
	Devices   []deviceDTO  `json:"devices"`
}

func (s *Server) handleGetNotifySettings(w http.ResponseWriter, r *http.Request) {
	ctx, u, q := r.Context(), CurrentUser(r), db.New(s.db)
	p, err := notify.LoadPrefs(ctx, q, u.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	subs, err := q.ListPushSubscriptions(ctx, u.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := notifySettingsDTO{PublicKey: s.notify.Keys.Public, Prefs: p, Devices: []deviceDTO{}}
	for _, d := range subs {
		out.Devices = append(out.Devices, deviceDTO{ID: d.ID, Endpoint: d.Endpoint, UserAgent: d.UserAgent, CreatedAt: d.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSetNotifySettings(w http.ResponseWriter, r *http.Request) {
	var p notify.Prefs
	if !readJSON(w, r, &p) {
		return
	}
	if err := p.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, strings.ToUpper(err.Error()[:1])+err.Error()[1:]+".")
		return
	}
	b, _ := json.Marshal(p)
	if err := db.New(s.db).SetNotificationPrefs(r.Context(), db.SetNotificationPrefsParams{UserID: CurrentUser(r).ID, Prefs: string(b)}); err != nil {
		s.internalError(w, err)
		return
	}
	s.notify.Changed(HouseholdID(r))
	s.handleGetNotifySettings(w, r)
}

func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
		ExpirationTime *float64 `json:"expirationTime"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !strings.HasPrefix(in.Endpoint, "https://") || in.Keys.P256dh == "" || in.Keys.Auth == "" {
		writeError(w, http.StatusBadRequest, "Invalid push subscription.")
		return
	}
	ua := r.UserAgent()
	if len(ua) > 300 {
		ua = ua[:300]
	}
	sub, err := db.New(s.db).UpsertPushSubscription(r.Context(), db.UpsertPushSubscriptionParams{
		UserID: CurrentUser(r).ID, Endpoint: in.Endpoint, P256dh: in.Keys.P256dh, Auth: in.Keys.Auth,
		UserAgent: ua, CreatedAt: time.Now().Unix(),
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, deviceDTO{ID: sub.ID, Endpoint: sub.Endpoint, UserAgent: sub.UserAgent, CreatedAt: sub.CreatedAt})
}

func (s *Server) handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if err := db.New(s.db).DeletePushSubscription(r.Context(), db.DeletePushSubscriptionParams{ID: txnID(r), UserID: CurrentUser(r).ID}); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /notifications/test: adds a test notification and pushes it to the user's devices.
func (s *Server) handleTestNotification(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), CurrentUser(r)
	a := notify.Alert{
		Kind: "test", Key: fmt.Sprintf("test:%d", time.Now().UnixNano()), URL: "/settings",
		Title: "Test notification", Body: "Notifications from Viceroy are working.",
	}
	_, sent, err := s.notify.Notify(ctx, u, a)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"sent": sent})
}
