// Package aisettings resolves a household's AI setup: the OpenRouter key and models saved in
// Settings, falling back to [ai] in viceroy.toml. Clients are built per call, so changes apply
// without a restart.
package aisettings

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"

	"viceroy/internal/ai"
	"viceroy/internal/config"
	"viceroy/internal/db"
	"viceroy/internal/secrets"
)

// Household setting keys. The key is stored sealed (base64 of secrets.Box output).
const (
	keyAPIKey       = "ai.openrouter_key"
	keyChatModel    = "ai.chat_model"
	keyEmailModel   = "ai.email_model"
	keyEmailBaseURL = "ai.email_base_url"
	keyVisionModel  = "ai.vision_model"
	keyCategorize   = "ai.categorize" // "off" turns automatic categorization off
)

type Store struct {
	DB      *sql.DB
	Box     *secrets.Box
	Config  config.AIConfig // fallbacks from viceroy.toml
	Referer string
}

// Settings is the effective setup. Source says where the key came from.
type Settings struct {
	APIKey       string
	KeySource    string // settings | config | ""
	ChatModel    string
	EmailModel   string // "" = ChatModel
	EmailBaseURL string // self-hosted endpoint for email reading; "" = OpenRouter
	VisionModel  string // multimodal model for budget imports; "" = ChatModel
	Categorize   bool   // categorize new transactions with the email (light) model

	// What viceroy.toml would give, shown as placeholders.
	ConfigChatModel string
}

func (s *Store) Load(ctx context.Context, hh int64) (Settings, error) {
	out := Settings{
		APIKey: s.Config.OpenRouterKey, ChatModel: s.Config.ChatModel, EmailModel: s.Config.EmailModel,
		EmailBaseURL: s.Config.EmailBaseURL, VisionModel: s.Config.VisionModel, ConfigChatModel: s.Config.ChatModel,
		Categorize: true,
	}
	if out.APIKey != "" {
		out.KeySource = "config"
	}
	rows, err := db.New(s.DB).ListHouseholdSettings(ctx, hh)
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		switch r.Key {
		case keyAPIKey:
			sealed, err := base64.StdEncoding.DecodeString(r.Value)
			if err != nil {
				continue
			}
			if k, err := s.Box.Open(sealed); err == nil && k != "" {
				out.APIKey, out.KeySource = k, "settings"
			}
		case keyChatModel:
			if r.Value != "" {
				out.ChatModel = r.Value
			}
		case keyEmailModel:
			out.EmailModel = r.Value
		case keyEmailBaseURL:
			out.EmailBaseURL = r.Value
		case keyVisionModel:
			out.VisionModel = r.Value
		case keyCategorize:
			out.Categorize = r.Value != "off"
		}
	}
	return out, nil
}

// Patch changes settings; nil fields stay. An empty APIKey removes the saved key (the
// viceroy.toml key, if any, applies again); empty models fall back to viceroy.toml.
type Patch struct {
	APIKey       *string `json:"openrouter_key"`
	ChatModel    *string `json:"chat_model"`
	EmailModel   *string `json:"email_model"`
	EmailBaseURL *string `json:"email_base_url"`
	VisionModel  *string `json:"vision_model"`
	Categorize   *bool   `json:"categorize"`
}

var ErrBadURL = errors.New("the self-hosted endpoint must start with http:// or https://")

func (s *Store) Save(ctx context.Context, hh int64, p Patch) error {
	q := db.New(s.DB)
	set := func(k, v string) error {
		return q.SetHouseholdSetting(ctx, db.SetHouseholdSettingParams{HouseholdID: hh, Key: k, Value: v})
	}
	if p.EmailBaseURL != nil {
		u := strings.TrimRight(strings.TrimSpace(*p.EmailBaseURL), "/")
		if u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return ErrBadURL
		}
		if err := set(keyEmailBaseURL, u); err != nil {
			return err
		}
	}
	if p.APIKey != nil {
		v := ""
		if k := strings.TrimSpace(*p.APIKey); k != "" {
			sealed, err := s.Box.Seal(k)
			if err != nil {
				return err
			}
			v = base64.StdEncoding.EncodeToString(sealed)
		}
		if err := set(keyAPIKey, v); err != nil {
			return err
		}
	}
	if p.Categorize != nil {
		v := "on"
		if !*p.Categorize {
			v = "off"
		}
		if err := set(keyCategorize, v); err != nil {
			return err
		}
	}
	for k, v := range map[string]*string{keyChatModel: p.ChatModel, keyEmailModel: p.EmailModel, keyVisionModel: p.VisionModel} {
		if v != nil {
			if err := set(k, strings.TrimSpace(*v)); err != nil {
				return err
			}
		}
	}
	return nil
}

// Chat is the client for "chat with your budget".
func (st Settings) Chat(baseURL, referer string) *ai.Client {
	c := ai.New(baseURL, st.APIKey, st.ChatModel)
	c.Referer = referer
	return c
}

// Email is the client that reads unmatched bank emails.
func (st Settings) Email(baseURL, referer string) *ai.Client {
	model := st.EmailModel
	if model == "" {
		model = st.ChatModel
	}
	if st.EmailBaseURL != "" {
		c := ai.New(st.EmailBaseURL, "", model)
		c.Local = true
		return c
	}
	c := ai.New(baseURL, st.APIKey, model)
	c.Referer = referer
	return c
}

// Vision is the multimodal client that reads pasted or screenshotted budgets.
func (st Settings) Vision(baseURL, referer string) *ai.Client {
	model := st.VisionModel
	if model == "" {
		model = st.ChatModel
	}
	c := ai.New(baseURL, st.APIKey, model)
	c.Referer = referer
	return c
}

// VisionClient loads the household's settings and returns its multimodal client.
func (s *Store) VisionClient(ctx context.Context, hh int64) (*ai.Client, error) {
	st, err := s.Load(ctx, hh)
	if err != nil {
		return nil, err
	}
	return st.Vision(s.Config.BaseURL, s.Referer), nil
}

// ChatClient loads the household's settings and returns its chat client.
func (s *Store) ChatClient(ctx context.Context, hh int64) (*ai.Client, error) {
	st, err := s.Load(ctx, hh)
	if err != nil {
		return nil, err
	}
	return st.Chat(s.Config.BaseURL, s.Referer), nil
}

// EmailClient loads the household's settings and returns its email-reading client.
func (s *Store) EmailClient(ctx context.Context, hh int64) (*ai.Client, error) {
	st, err := s.Load(ctx, hh)
	if err != nil {
		return nil, err
	}
	return st.Email(s.Config.BaseURL, s.Referer), nil
}
