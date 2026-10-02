package server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/ai"
	"viceroy/internal/aisettings"
)

func (s *Server) aiSettingsRoutes(r chi.Router) {
	r.Get("/settings/ai", s.handleGetAISettings)
	r.Patch("/settings/ai", s.handleSaveAISettings)
	r.Post("/settings/ai/test", s.handleTestAI)
	r.Get("/settings/ai/models", s.handleListModels)
}

// modelCache keeps each endpoint's model list for an hour (OpenRouter's is ~1 MB).
type modelCache struct {
	mu      sync.Mutex
	entries map[string]modelCacheEntry
}

type modelCacheEntry struct {
	at     time.Time
	models []ai.ModelInfo
}

func (m *modelCache) get(ctx context.Context, c *ai.Client) ([]ai.ModelInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.entries[c.BaseURL]; ok && time.Since(e.at) < time.Hour {
		return e.models, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	models, err := c.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	if m.entries == nil {
		m.entries = map[string]modelCacheEntry{}
	}
	m.entries[c.BaseURL] = modelCacheEntry{time.Now(), models}
	return models, nil
}

// GET /settings/ai/models?endpoint=openrouter|email: the models the chat/vision endpoint
// (or the self-hosted email endpoint, when set) offers.
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	st, err := s.ai.Load(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	c := st.Chat(s.ai.Config.BaseURL, "")
	if r.URL.Query().Get("endpoint") == "email" && st.EmailBaseURL != "" {
		c = st.Email(s.ai.Config.BaseURL, "")
	}
	models, err := s.models.get(r.Context(), c)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Couldn't load the model list: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

type aiSettingsDTO struct {
	CanEdit         bool   `json:"can_edit"`
	KeySet          bool   `json:"key_set"`
	KeyHint         string `json:"key_hint"`   // last 4 characters
	KeySource       string `json:"key_source"` // settings | config | ""
	ChatModel       string `json:"chat_model"`
	EmailModel      string `json:"email_model"`
	EmailBaseURL    string `json:"email_base_url"`
	VisionModel     string `json:"vision_model"`
	Categorize      bool   `json:"categorize"`
	CatReview       bool   `json:"categorize_review"`
	ConfigChatModel string `json:"config_chat_model"`
	ChatReady       bool   `json:"chat_ready"`
	EmailReady      bool   `json:"email_ready"`
	VisionReady     bool   `json:"vision_ready"`
}

func (s *Server) handleGetAISettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.ai.Load(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := aiSettingsDTO{
		CanEdit: CurrentUser(r).IsAdmin == 1, KeySet: st.APIKey != "", KeySource: st.KeySource,
		ChatModel: st.ChatModel, EmailModel: st.EmailModel, EmailBaseURL: st.EmailBaseURL, VisionModel: st.VisionModel, ConfigChatModel: st.ConfigChatModel,
		Categorize:  st.Categorize, CatReview: st.CatReview,
		ChatReady:   st.Chat(s.ai.Config.BaseURL, "").Configured(),
		EmailReady:  st.Email(s.ai.Config.BaseURL, "").Configured(),
		VisionReady: st.Vision(s.ai.Config.BaseURL, "").Configured(),
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

// testImage is a 1×1 PNG: the vision test only passes when the model accepts images.
const testImage = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

// POST /settings/ai/test {target: chat|email|vision}: one tiny request to check the key and model.
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
	switch in.Target {
	case "email":
		client, err = s.ai.EmailClient(r.Context(), HouseholdID(r))
	case "vision":
		client, err = s.ai.VisionClient(r.Context(), HouseholdID(r))
	default:
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
	msg := ai.Message{Role: "user", Content: `Reply with exactly {"ok": true}`}
	if in.Target == "vision" {
		msg.Parts = []ai.Part{ai.TextPart(`This is a test image. Reply with exactly {"ok": true}`), ai.ImagePart(testImage)}
	}
	_, err = client.CompleteJSON(ctx, []ai.Message{msg})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "model": client.Model, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "model": client.Model})
}
