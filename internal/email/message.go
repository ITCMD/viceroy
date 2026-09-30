// Package email turns bank transaction-alert emails into transactions: it watches an IMAP
// folder (read-only), routes each message through the household's filters, and parses the
// amount, merchant and date with a built-in template or a user-built custom parser.
package email

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"regexp"
	"strings"
	"time"

	_ "github.com/emersion/go-message/charset" // decode non-UTF-8 alerts
	"github.com/emersion/go-message/mail"
	"golang.org/x/net/html"
)

// Message is the part of an email Viceroy keeps: headers and a plain-text body.
type Message struct {
	MessageID string
	FromAddr  string
	FromName  string
	Subject   string
	Date      time.Time
	Text      string
}

const maxBody = 64 << 10 // alerts are short; cap what gets stored

// ParseMessage reads a raw RFC 5322 message. The body is the text/plain part, or the HTML
// part converted to text when there is no usable plain part.
func ParseMessage(raw []byte) (Message, error) {
	r, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil && r == nil {
		return Message{}, err
	}
	defer r.Close()
	var m Message
	m.MessageID, _ = r.Header.MessageID()
	if m.MessageID == "" {
		sum := sha256.Sum256(raw)
		m.MessageID = "sha256:" + hex.EncodeToString(sum[:16])
	}
	if addrs, _ := r.Header.AddressList("From"); len(addrs) > 0 {
		m.FromAddr, m.FromName = strings.ToLower(addrs[0].Address), addrs[0].Name
	}
	m.Subject, _ = r.Header.Subject()
	m.Date, _ = r.Header.Date()

	var plain, htmlText string
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			if plain != "" || htmlText != "" {
				break // keep what decoded
			}
			return m, fmt.Errorf("reading message body: %w", err)
		}
		h, ok := p.Header.(*mail.InlineHeader)
		if !ok {
			continue // attachment
		}
		ct, _, _ := mime.ParseMediaType(h.Get("Content-Type"))
		if ct == "" {
			ct = "text/plain"
		}
		b, _ := io.ReadAll(io.LimitReader(p.Body, 4*maxBody))
		switch {
		case ct == "text/plain" && plain == "":
			plain = string(b)
		case ct == "text/html" && htmlText == "":
			htmlText = HTMLToText(string(b))
		}
	}
	body := plain
	if strings.TrimSpace(plain) == "" || looksLikeHTML(plain) {
		body = htmlText
	}
	if body == "" {
		body = plain
	}
	m.Text = tidy(body)
	if len(m.Text) > maxBody {
		m.Text = m.Text[:maxBody]
	}
	return m, nil
}

var htmlTag = regexp.MustCompile(`(?i)<(html|body|div|table|p|br)\b`)

func looksLikeHTML(s string) bool { return len(htmlTag.FindAllStringIndex(s, 3)) >= 2 }

// blockTags end a line in the text rendering.
var blockTags = map[string]bool{
	"p": true, "div": true, "br": true, "tr": true, "li": true, "table": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "section": true, "article": true, "header": true,
	"footer": true, "ul": true, "ol": true, "hr": true, "center": true, "blockquote": true,
}

// HTMLToText renders HTML as plain text: block elements and table cells become line breaks,
// scripts, styles and comments are dropped. The result is only ever shown as text.
func HTMLToText(s string) string {
	z := html.NewTokenizer(strings.NewReader(s))
	var b strings.Builder
	skip := 0
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			// Cells become one value per line with no blank lines, so a label and its value
			// sit on adjacent lines.
			return blankLines.ReplaceAllString(tidy(b.String()), "\n")
		case html.TextToken:
			if skip == 0 {
				b.WriteString(string(z.Text()))
			}
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			tag := string(name)
			switch tag {
			case "script", "style", "head", "title":
				if tt == html.StartTagToken {
					skip++
				} else if tt == html.EndTagToken && skip > 0 {
					skip--
				}
				continue
			case "td", "th":
				// Label and value cells usually sit side by side; a line break keeps the
				// "Merchant" / "STARBUCKS" pairing readable for parsers.
				b.WriteString("\n")
				continue
			}
			if blockTags[tag] {
				b.WriteString("\n")
			}
		}
	}
}

var (
	spaces     = regexp.MustCompile(`[ \t\x{a0}\x{200b}\x{200c}\x{feff}]+`)
	manyLines  = regexp.MustCompile(`\n{3,}`)
	blankLines = regexp.MustCompile(`\n{2,}`)
)

// tidy normalizes whitespace: CRLF to LF, runs of spaces collapsed, lines trimmed, and at
// most one blank line in a row.
func tidy(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(spaces.ReplaceAllString(l, " "))
	}
	s = strings.Join(lines, "\n")
	return strings.TrimSpace(manyLines.ReplaceAllString(s, "\n\n"))
}
