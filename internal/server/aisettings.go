package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/ai"
	"viceroy/internal/aisettings"
)

func (s *Server) aiSettingsRoutes(r chi.Router) {
	r.Get("/settings/ai", s.handleGetAISettings)
	r.Patch("/settings/ai", s.handleSaveAISettings)
	r.Post("/settings/ai/test", s.handleTestAI)
}

type aiSettingsDTO struct {
	CanEdit         bool   `json:"can_edit"`
	KeySet          bool   `json:"key_set"`
	KeyHint         string `json:"key_hint"`   // last 4 characters
	KeySource       string `json:"key_source"` // settings | config | ""
	ChatModel       string `json:"chat_model"`
	EmailModel      string `json:"email_model"`
	EmailBaseURL    string `json:"email_base_url"`
	ConfigChatModel string `json:"config_chat_model"`
	ChatReady       bool   `json:"chat_ready"`
	EmailReady      bool   `json:"email_ready"`
}

func (s *Server) handleGetAISettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.ai.Load(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := aiSettingsDTO{
		CanEdit: CurrentUser(r).IsAdmin == 1, KeySet: st.APIKey != "", KeySource: st.KeySource,
		ChatModel: st.ChatModel, EmailModel: st.EmailModel, EmailBaseURL: st.EmailBaseURL, ConfigChatModel: st.ConfigChatModel,
		ChatReady:  st.Chat(s.ai.Config.BaseURL, "").Configured(),
		EmailReady: st.Email(s.ai.Config.BaseURL, "").Configured(),
	}
	if k := st.APIKey; len(k) >= 8 {
		out.KeyHint = k[len(k)-4:]
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSaveAISettings(w http.ResponseWriter, r *http.Request) {
	if CurrentUser(r).IsAdmin != 1 {
		writeError(w, http.StatusForbidden, "Only an admin can change AI settings.")
		return
	}
	var in aisettings.Patch
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.ai.Save(r.Context(), HouseholdID(r), in); errors.Is(err, aisettings.ErrBadURL) {
		writeError(w, http.StatusBadRequest, "The self-hosted endpoint must start with http:// or https://.")
		return
	} else if err != nil {
		s.internalError(w, err)
		return
	}
	s.handleGetAISettings(w, r)
}

// POST /settings/ai/test {target: chat|email}: one tiny request to check the key and model.
func (s *Server) handleTestAI(w http.ResponseWriter, r *http.Request) {
	if CurrentUser(r).IsAdmin != 1 {
		writeError(w, http.StatusForbidden, "Only an admin can test AI settings.")
		return
	}
	var in struct {
		Target string `json:"target"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	var (
		client *ai.Client
		err    error
	)
	if in.Target == "email" {
		client, err = s.ai.EmailClient(r.Context(), HouseholdID(r))
	} else {
		client, err = s.ai.ChatClient(r.Context(), HouseholdID(r))
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	if !client.Configured() {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "Add an OpenRouter API key first."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	_, err = client.CompleteJSON(ctx, []ai.Message{{Role: "user", Content: `Reply with exactly {"ok": true}`}})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "model": client.Model, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "model": client.Model})
}
