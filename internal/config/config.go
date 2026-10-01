// Package config loads and validates viceroy.toml.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Listen         string   `toml:"listen"`
	AllowedCIDRs   []string `toml:"allowed_cidrs"`
	TrustedProxies []string `toml:"trusted_proxies"`
	DataDir        string   `toml:"data_dir"`
	PublicURL      string   `toml:"public_url"`

	TLS TLSConfig `toml:"tls"`
	AI  AIConfig  `toml:"ai"`

	// Parsed forms, filled by Validate.
	Allowed []netip.Prefix `toml:"-"`
	Proxies []netip.Prefix `toml:"-"`
}

type TLSConfig struct {
	Cert string `toml:"cert"`
	Key  string `toml:"key"`
}

type AIConfig struct {
	OpenRouterKey    string `toml:"openrouter_key"`
	ChatModel        string `toml:"chat_model"`
	BaseURL          string `toml:"base_url"`       // OpenAI-compatible API root; default OpenRouter
	EmailModel       string `toml:"email_model"`    // model that reads unmatched bank emails; "" = chat_model
	EmailBaseURL     string `toml:"email_base_url"` // e.g. a local Ollama; "" = base_url with openrouter_key
	LocalCategorizer bool   `toml:"local_categorizer"`
}

func Default() Config {
	return Config{
		Listen:         "127.0.0.1:8420",
		AllowedCIDRs:   []string{"127.0.0.1/32", "::1/128"},
		TrustedProxies: []string{},
		DataDir:        "./data",
		AI:             AIConfig{ChatModel: "anthropic/claude-sonnet-5.5", LocalCategorizer: true},
	}
}

// Load reads path on top of Default() and validates the result.
// Relative data_dir is resolved against the config file's directory.
func Load(path string) (Config, error) {
	cfg := Default()
	md, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return cfg, fmt.Errorf("reading %s: %w", path, err)
	}
	if undec := md.Undecoded(); len(undec) > 0 {
		keys := make([]string, len(undec))
		for i, k := range undec {
			keys[i] = k.String()
		}
		return cfg, fmt.Errorf("%s: unknown keys: %s", path, strings.Join(keys, ", "))
	}
	if !filepath.IsAbs(cfg.DataDir) {
		cfg.DataDir = filepath.Join(filepath.Dir(path), cfg.DataDir)
	}
	return cfg, cfg.Validate()
}

func (c *Config) Validate() error {
	var errs []error
	if c.Listen == "" {
		errs = append(errs, errors.New("listen must be set"))
	}
	if c.DataDir == "" {
		errs = append(errs, errors.New("data_dir must be set"))
	}
	var err error
	if c.Allowed, err = parsePrefixes(c.AllowedCIDRs); err != nil {
		errs = append(errs, fmt.Errorf("allowed_cidrs: %w", err))
	}
	if len(c.Allowed) == 0 {
		errs = append(errs, errors.New("allowed_cidrs must contain at least one entry"))
	}
	if c.Proxies, err = parsePrefixes(c.TrustedProxies); err != nil {
		errs = append(errs, fmt.Errorf("trusted_proxies: %w", err))
	}
	if (c.TLS.Cert == "") != (c.TLS.Key == "") {
		errs = append(errs, errors.New("tls.cert and tls.key must be set together"))
	}
	return errors.Join(errs...)
}

// parsePrefixes accepts CIDRs or bare IPs. The special value "0.0.0.0"
// (as used in the config docs) means "any IPv4 and IPv6 address".
func parsePrefixes(in []string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, s := range in {
		s = strings.TrimSpace(s)
		switch s {
		case "0.0.0.0", "0.0.0.0/0", "*":
			out = append(out, netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0"))
			continue
		}
		if strings.Contains(s, "/") {
			p, err := netip.ParsePrefix(s)
			if err != nil {
				return nil, err
			}
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(s)
		if err != nil {
			return nil, err
		}
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

const Template = `# Viceroy configuration. Edit, then run: viceroy serve

# Address and port to listen on. Use "0.0.0.0:8420" to listen on all interfaces.
listen = "127.0.0.1:8420"

# Which client IPs may connect. CIDRs or single IPs; "0.0.0.0" allows everyone.
# Example for a home LAN: ["127.0.0.1", "192.168.0.0/24"]
allowed_cidrs = ["127.0.0.1", "::1"]

# Reverse proxies (Caddy, nginx, Tailscale) in front of Viceroy. Requests from
# these addresses use X-Forwarded-For as the client IP for allowed_cidrs.
trusted_proxies = []

# Where the database, generated keys and model files live (relative to this file).
data_dir = "./data"

# Public HTTPS URL, e.g. "https://viceroy.example.com". Needed for PWA install and push.
public_url = ""

[tls]
# Optional built-in TLS. Leave empty when using a reverse proxy.
cert = ""
key = ""

[ai]
# The easiest place for these is Settings → AI in the app (saved there, it wins over this file).
# OpenRouter API key (https://openrouter.ai/keys) for "Chat with your budget". Empty = chat off.
openrouter_key = ""
# Any OpenRouter model id that supports tool calling.
chat_model = "anthropic/claude-sonnet-5.5"
# Optional: another OpenAI-compatible endpoint instead of OpenRouter.
base_url = ""
# Model that reads bank emails no filter caught (turn it on per mailbox in Settings → Email).
# A cheap, fast model is plenty. Empty = chat_model.
email_model = ""
# Optional: read those emails with a self-hosted OpenAI-compatible endpoint instead, so they
# never leave this machine, e.g. Ollama: "http://127.0.0.1:11434/v1" (no API key is sent).
email_base_url = ""
local_categorizer = true
`

// WriteTemplate writes the default config to path, refusing to overwrite.
func WriteTemplate(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(Template)
	return err
}
