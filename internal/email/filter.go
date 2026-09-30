package email

import (
	"fmt"
	"regexp"
	"strings"
)

// Filter decides whether an email is an alert for one account. Empty conditions match anything,
// but a filter must set at least one.
type Filter struct {
	Sender  string // "alerts@chase.com", or a domain like "chase.com" / "@chase.com"
	Subject string
	Body    string
	Regex   bool // Subject/Body are regexes instead of case-insensitive "contains"
}

// Validate reports a user-facing problem with f, or nil.
func (f Filter) Validate() error {
	if strings.TrimSpace(f.Sender+f.Subject+f.Body) == "" {
		return fmt.Errorf("Set a sender, subject or body text to match.")
	}
	if f.Regex {
		for _, p := range []string{f.Subject, f.Body} {
			if p == "" {
				continue
			}
			if _, err := regexp.Compile("(?i)" + p); err != nil {
				return fmt.Errorf("Invalid pattern %q.", p)
			}
		}
	}
	return nil
}

// Matches reports whether m satisfies every condition of f.
func (f Filter) Matches(m Message) bool {
	if s := strings.ToLower(strings.TrimSpace(f.Sender)); s != "" {
		addr := strings.ToLower(m.FromAddr)
		if strings.Contains(s, "@") && !strings.HasPrefix(s, "@") {
			if addr != s {
				return false
			}
		} else {
			domain := strings.TrimPrefix(s, "@")
			_, at, _ := strings.Cut(addr, "@")
			if at != domain && !strings.HasSuffix(at, "."+domain) {
				return false
			}
		}
	}
	return f.textMatches(f.Subject, m.Subject) && f.textMatches(f.Body, m.Text)
}

func (f Filter) textMatches(pattern, text string) bool {
	if pattern == "" {
		return true
	}
	if f.Regex {
		re, err := regexp.Compile("(?i)" + pattern)
		return err == nil && re.MatchString(text)
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(pattern))
}
