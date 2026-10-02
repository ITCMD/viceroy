package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/db"
	"viceroy/internal/email"
)

func (s *Server) emailRoutes(r chi.Router) {
	r.Get("/email/mailboxes", s.handleListMailboxes)
	r.Post("/email/mailboxes", s.handleCreateMailbox)
	r.Patch("/email/mailboxes/{id}", s.handleUpdateMailbox)
	r.Delete("/email/mailboxes/{id}", s.handleDeleteMailbox)
	r.Post("/email/mailboxes/{id}/check", s.handleCheckMailbox)
	r.Get("/email/templates", s.handleListTemplates)
	r.Get("/email/ai", s.handleEmailAIInfo)
	r.Get("/email/filters", s.handleListEmailFilters)
	r.Post("/email/filters", s.handleCreateEmailFilter)
	r.Post("/email/filters/preview", s.handlePreviewEmailFilter)
	r.Patch("/email/filters/{id}", s.handleUpdateEmailFilter)
	r.Delete("/email/filters/{id}", s.handleDeleteEmailFilter)
	r.Get("/email/messages", s.handleListEmailMessages)
	r.Get("/email/messages/{id}", s.handleGetEmailMessage)
	r.Post("/email/messages/{id}/ignore", s.handleIgnoreEmailMessage)
	r.Post("/email/messages/{id}/retry", s.handleRetryEmailMessage)
}

// ---- Mailboxes ----

type mailboxDTO struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Host          string `json:"host"`
	Port          int64  `json:"port"`
	Security      string `json:"security"`
	Username      string `json:"username"`
	Folder        string `json:"folder"`
	Enabled       bool   `json:"enabled"`
	Status        string `json:"status"`
	LastError     string `json:"last_error"`
	LastCheckedAt *int64 `json:"last_checked_at"`
	AIRead        bool   `json:"ai_read"`    // read emails no filter caught with AI
	AISenders     string `json:"ai_senders"` // only from these senders (one per line; empty = any)
}

func toMailboxDTO(m db.EmailMailbox) mailboxDTO {
	return mailboxDTO{
		ID: m.ID, Name: m.Name, Host: m.Host, Port: m.Port, Security: m.Security, Username: m.Username,
		Folder: m.Folder, Enabled: m.Enabled == 1, Status: m.Status, LastError: m.LastError, LastCheckedAt: ptr(m.LastCheckedAt),
		AIRead: m.AiRead == 1, AISenders: m.AiSenders,
	}
}

type mailboxIn struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int64  `json:"port"`
	Security string `json:"security"`
	Username string `json:"username"`
	Password string `json:"password"` // empty on update = keep
	Folder   string `json:"folder"`
	Enabled  *bool  `json:"enabled"`
	// AI reading; nil = leave as is.
	AIRead    *bool   `json:"ai_read"`
	AISenders *string `json:"ai_senders"`
}

// saveMailboxAI applies the AI settings of in to mailbox id; turning reading on queues the
// last week's unmatched emails. msg is a user-facing error.
func (s *Server) saveMailboxAI(ctx context.Context, q *db.Queries, hh int64, cur db.EmailMailbox, in mailboxIn) (msg string, err error) {
	if in.AIRead == nil && in.AISenders == nil {
		return "", nil
	}
	on, senders := cur.AiRead == 1, cur.AiSenders
	if in.AIRead != nil {
		on = *in.AIRead
	}
	if in.AISenders != nil {
		var lines []string
		for _, l := range strings.FieldsFunc(*in.AISenders, func(r rune) bool { return r == '\n' || r == ',' }) {
			if l = strings.ToLower(strings.TrimSpace(l)); l != "" {
				if strings.ContainsAny(l, " <>") {
					return "Senders are email addresses or domains, one per line.", nil
				}
				lines = append(lines, l)
			}
		}
		senders = strings.Join(lines, "\n")
	}
	if on && !s.mail.AIEnabled(ctx, hh) {
		return "AI email reading isn't set up yet: add an OpenRouter key in Settings → AI.", nil
	}
	if err := q.SetMailboxAI(ctx, db.SetMailboxAIParams{AiRead: b2i(on), AiSenders: senders, ID: cur.ID, HouseholdID: hh}); err != nil {
		return "", err
	}
	if on && cur.AiRead == 0 {
		if _, err := s.mail.QueueRecentForAI(ctx, hh); err != nil {
			return "", err
		}
	}
	return "", nil
}

// GET /email/ai: whether AI email reading is available and with which model.
func (s *Server) handleEmailAIInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.mail.AIInfo(r.Context(), HouseholdID(r)))
}

func (in *mailboxIn) normalize() error {
	in.Name, in.Host, in.Username = strings.TrimSpace(in.Name), strings.TrimSpace(in.Host), strings.TrimSpace(in.Username)
	in.Folder = strings.TrimSpace(in.Folder)
	if in.Folder == "" {
		in.Folder = "INBOX"
	}
	switch in.Security {
	case "":
		in.Security = "tls"
	case "tls", "starttls", "none":
	default:
		return errors.New("Choose a security mode.")
	}
	if in.Port == 0 {
		in.Port = map[string]int64{"tls": 993, "starttls": 143, "none": 143}[in.Security]
	}
	if in.Host == "" || in.Username == "" {
		return errors.New("Enter the IMAP server and username.")
	}
	if in.Port < 1 || in.Port > 65535 {
		return errors.New("Enter a valid port.")
	}
	if in.Name == "" {
		in.Name = in.Username
	}
	return nil
}

func (in mailboxIn) conn(password string) email.Conn {
	return email.Conn{Host: in.Host, Port: int(in.Port), Security: in.Security, Username: in.Username, Password: password, Folder: in.Folder}
}

func (s *Server) handleListMailboxes(w http.ResponseWriter, r *http.Request) {
	rows, err := db.New(s.db).ListMailboxes(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := make([]mailboxDTO, len(rows))
	for i, m := range rows {
		out[i] = toMailboxDTO(m)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateMailbox checks the login before saving, so a typo shows up right away.
func (s *Server) handleCreateMailbox(w http.ResponseWriter, r *http.Request) {
	var in mailboxIn
	if !readJSON(w, r, &in) {
		return
	}
	if err := in.normalize(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Password == "" {
		writeError(w, http.StatusBadRequest, "Enter the app password.")
		return
	}
	if _, err := email.Test(in.conn(in.Password)); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't connect: "+err.Error())
		return
	}
	sealed, err := s.mail.Box.Seal(in.Password)
	if err != nil {
		s.internalError(w, err)
		return
	}
	m, err := db.New(s.db).InsertMailbox(r.Context(), db.InsertMailboxParams{
		HouseholdID: HouseholdID(r), Name: in.Name, Host: in.Host, Port: in.Port, Security: in.Security,
		Username: in.Username, PasswordEnc: sealed, Folder: in.Folder, Enabled: 1, CreatedAt: time.Now().Unix(),
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	if msg, err := s.saveMailboxAI(r.Context(), db.New(s.db), HouseholdID(r), m, in); err != nil {
		s.internalError(w, err)
		return
	} else if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	s.mail.Refresh(m.ID)
	if m, err = db.New(s.db).GetMailbox(r.Context(), db.GetMailboxParams{ID: m.ID, HouseholdID: HouseholdID(r)}); err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toMailboxDTO(m))
}

func (s *Server) handleUpdateMailbox(w http.ResponseWriter, r *http.Request) {
	var in mailboxIn
	if !readJSON(w, r, &in) {
		return
	}
	ctx, hh, id := r.Context(), HouseholdID(r), txnID(r)
	q := db.New(s.db)
	cur, err := q.GetMailbox(ctx, db.GetMailboxParams{ID: id, HouseholdID: hh})
	if err != nil {
		writeError(w, http.StatusNotFound, "Mailbox not found.")
		return
	}
	if err := in.normalize(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	enabled := cur.Enabled == 1
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	sealed, password := cur.PasswordEnc, in.Password
	if password == "" {
		if password, err = s.mail.Box.Open(cur.PasswordEnc); err != nil {
			writeError(w, http.StatusBadRequest, "Re-enter the app password.")
			return
		}
	} else if sealed, err = s.mail.Box.Seal(password); err != nil {
		s.internalError(w, err)
		return
	}
	moved := in.Host != cur.Host || in.Port != cur.Port || in.Username != cur.Username || in.Folder != cur.Folder
	if enabled && (moved || in.Password != "" || in.Security != cur.Security) {
		if _, err := email.Test(in.conn(password)); err != nil {
			writeError(w, http.StatusBadRequest, "Couldn't connect: "+err.Error())
			return
		}
	}
	if err := q.UpdateMailbox(ctx, db.UpdateMailboxParams{
		Name: in.Name, Host: in.Host, Port: in.Port, Security: in.Security, Username: in.Username,
		PasswordEnc: sealed, Folder: in.Folder, Enabled: b2i(enabled), ID: id, HouseholdID: hh,
	}); err != nil {
		s.internalError(w, err)
		return
	}
	if moved {
		if err := q.ResetMailboxCursor(ctx, id); err != nil {
			s.internalError(w, err)
			return
		}
	}
	if msg, err := s.saveMailboxAI(ctx, q, hh, cur, in); err != nil {
		s.internalError(w, err)
		return
	} else if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	s.mail.Refresh(id)
	m, err := q.GetMailbox(ctx, db.GetMailboxParams{ID: id, HouseholdID: hh})
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toMailboxDTO(m))
}

func (s *Server) handleDeleteMailbox(w http.ResponseWriter, r *http.Request) {
	if err := db.New(s.db).DeleteMailbox(r.Context(), db.DeleteMailboxParams{ID: txnID(r), HouseholdID: HouseholdID(r)}); err != nil {
		s.internalError(w, err)
		return
	}
	s.mail.Refresh(0)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCheckMailbox(w http.ResponseWriter, r *http.Request) {
	if _, err := db.New(s.db).GetMailbox(r.Context(), db.GetMailboxParams{ID: txnID(r), HouseholdID: HouseholdID(r)}); err != nil {
		writeError(w, http.StatusNotFound, "Mailbox not found.")
		return
	}
	s.mail.Refresh(txnID(r))
	w.WriteHeader(http.StatusAccepted)
}

// ---- Filters ----

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, email.Templates)
}

type emailFilterDTO struct {
	ID           int64               `json:"id"`
	Name         string              `json:"name"`
	Priority     int64               `json:"priority"`
	Enabled      bool                `json:"enabled"`
	Sender       string              `json:"sender"`
	SubjectMatch string              `json:"subject_match"`
	BodyMatch    string              `json:"body_match"`
	UseRegex     bool                `json:"use_regex"`
	AccountID    int64               `json:"account_id"`
	Parser       string              `json:"parser"`
	CustomParser *email.CustomParser `json:"custom_parser"`
	Sign         string              `json:"sign"`
	Source       string              `json:"source"` // user | ai (written by the email AI, checked by Viceroy)
}

func toEmailFilterDTO(f db.EmailFilter) emailFilterDTO {
	out := emailFilterDTO{
		ID: f.ID, Name: f.Name, Priority: f.Priority, Enabled: f.Enabled == 1, Sender: f.Sender, SubjectMatch: f.SubjectMatch,
		BodyMatch: f.BodyMatch, UseRegex: f.UseRegex == 1, AccountID: f.AccountID, Parser: f.Parser, Sign: f.Sign, Source: f.Source,
	}
	if f.CustomParser != "" {
		var c email.CustomParser
		if json.Unmarshal([]byte(f.CustomParser), &c) == nil {
			out.CustomParser = &c
		}
	}
	return out
}

type emailFilterIn struct {
	Name         string              `json:"name"`
	Priority     *int64              `json:"priority"`
	Enabled      *bool               `json:"enabled"`
	Sender       string              `json:"sender"`
	SubjectMatch string              `json:"subject_match"`
	BodyMatch    string              `json:"body_match"`
	UseRegex     bool                `json:"use_regex"`
	AccountID    int64               `json:"account_id"`
	Parser       string              `json:"parser"`
	CustomParser *email.CustomParser `json:"custom_parser"`
	Sign         string              `json:"sign"`
}

// row validates the body and converts it to a filter row (ID/priority/created left to the caller).
func (in emailFilterIn) row(hh int64) (db.EmailFilter, error) {
	f := db.EmailFilter{
		HouseholdID: hh, Name: strings.TrimSpace(in.Name), Enabled: 1, Sender: strings.TrimSpace(in.Sender),
		SubjectMatch: strings.TrimSpace(in.SubjectMatch), BodyMatch: strings.TrimSpace(in.BodyMatch),
		UseRegex: b2i(in.UseRegex), AccountID: in.AccountID, Parser: in.Parser, Sign: in.Sign,
	}
	if in.Enabled != nil {
		f.Enabled = b2i(*in.Enabled)
	}
	if f.Parser == "" {
		f.Parser = "generic"
	}
	if f.Sign == "" {
		f.Sign = "debit"
	}
	if f.Sign != "debit" && f.Sign != "credit" {
		return f, errors.New("Choose whether alerts are purchases or deposits.")
	}
	if err := email.FilterOf(f).Validate(); err != nil {
		return f, err
	}
	if f.Parser == "custom" && in.CustomParser != nil {
		b, _ := json.Marshal(in.CustomParser)
		f.CustomParser = string(b)
	}
	if err := email.ValidateParser(f.Parser, f.CustomParser); err != nil {
		return f, err
	}
	if f.Name == "" {
		f.Name = firstNonEmpty(f.Sender, f.SubjectMatch, f.BodyMatch)
	}
	return f, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func (s *Server) handleListEmailFilters(w http.ResponseWriter, r *http.Request) {
	rows, err := db.New(s.db).ListEmailFilters(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := make([]emailFilterDTO, len(rows))
	for i, f := range rows {
		out[i] = toEmailFilterDTO(f)
	}
	writeJSON(w, http.StatusOK, out)
}

// filterResult is returned after saving a filter: the filter plus how many waiting emails it
// turned into transactions.
type filterResult struct {
	Filter emailFilterDTO `json:"filter"`
	Routed int            `json:"routed"`
}

func (s *Server) handleCreateEmailFilter(w http.ResponseWriter, r *http.Request) {
	var in emailFilterIn
	if !readJSON(w, r, &in) {
		return
	}
	ctx, hh := r.Context(), HouseholdID(r)
	q := db.New(s.db)
	f, err := in.row(hh)
	if err == nil {
		_, err = q.GetAccount(ctx, db.GetAccountParams{ID: f.AccountID, HouseholdID: hh})
		if err != nil {
			err = errors.New("Choose an account.")
		}
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Priority != nil {
		f.Priority = *in.Priority
	} else if f.Priority, err = q.NextEmailFilterPriority(ctx, hh); err != nil {
		s.internalError(w, err)
		return
	}
	row, err := q.InsertEmailFilter(ctx, db.InsertEmailFilterParams{
		HouseholdID: hh, Name: f.Name, Priority: f.Priority, Enabled: f.Enabled, Sender: f.Sender,
		SubjectMatch: f.SubjectMatch, BodyMatch: f.BodyMatch, UseRegex: f.UseRegex, AccountID: f.AccountID,
		Parser: f.Parser, CustomParser: f.CustomParser, Sign: f.Sign, CreatedAt: time.Now().Unix(),
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	s.respondFilter(w, r, http.StatusCreated, row)
}

func (s *Server) handleUpdateEmailFilter(w http.ResponseWriter, r *http.Request) {
	var in emailFilterIn
	if !readJSON(w, r, &in) {
		return
	}
	ctx, hh, id := r.Context(), HouseholdID(r), txnID(r)
	q := db.New(s.db)
	cur, err := q.GetEmailFilter(ctx, db.GetEmailFilterParams{ID: id, HouseholdID: hh})
	if err != nil {
		writeError(w, http.StatusNotFound, "Filter not found.")
		return
	}
	f, err := in.row(hh)
	if err == nil {
		if _, err = q.GetAccount(ctx, db.GetAccountParams{ID: f.AccountID, HouseholdID: hh}); err != nil {
			err = errors.New("Choose an account.")
		}
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f.Priority = cur.Priority
	if in.Priority != nil {
		f.Priority = *in.Priority
	}
	if err := q.UpdateEmailFilter(ctx, db.UpdateEmailFilterParams{
		Name: f.Name, Priority: f.Priority, Enabled: f.Enabled, Sender: f.Sender, SubjectMatch: f.SubjectMatch,
		BodyMatch: f.BodyMatch, UseRegex: f.UseRegex, AccountID: f.AccountID, Parser: f.Parser,
		CustomParser: f.CustomParser, Sign: f.Sign, ID: id, HouseholdID: hh,
	}); err != nil {
		s.internalError(w, err)
		return
	}
	row, err := q.GetEmailFilter(ctx, db.GetEmailFilterParams{ID: id, HouseholdID: hh})
	if err != nil {
		s.internalError(w, err)
		return
	}
	s.respondFilter(w, r, http.StatusOK, row)
}

// respondFilter re-routes waiting emails through the saved filters and returns the result.
func (s *Server) respondFilter(w http.ResponseWriter, r *http.Request, status int, f db.EmailFilter) {
	n, err := s.mail.Reroute(r.Context(), HouseholdID(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, status, filterResult{Filter: toEmailFilterDTO(f), Routed: n})
}

func (s *Server) handleDeleteEmailFilter(w http.ResponseWriter, r *http.Request) {
	if err := db.New(s.db).DeleteEmailFilter(r.Context(), db.DeleteEmailFilterParams{ID: txnID(r), HouseholdID: HouseholdID(r)}); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type previewMatch struct {
	ID         int64         `json:"id"`
	Subject    string        `json:"subject"`
	FromAddr   string        `json:"from_addr"`
	ReceivedAt int64         `json:"received_at"`
	Status     string        `json:"status"`
	Parsed     *email.Parsed `json:"parsed"`
	Error      string        `json:"error"`
	Matches    bool          `json:"matches"` // the draft filter's conditions catch this email
}

// handlePreviewEmailFilter shows which recent emails a draft filter would match and what its
// parser extracts from each, so the user can build the filter against real alerts. The
// message given by message_id is always parsed, even if the draft's conditions miss it.
func (s *Server) handlePreviewEmailFilter(w http.ResponseWriter, r *http.Request) {
	var in struct {
		emailFilterIn
		MessageID int64 `json:"message_id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx, hh := r.Context(), HouseholdID(r)
	f := db.EmailFilter{
		Sender: strings.TrimSpace(in.Sender), SubjectMatch: strings.TrimSpace(in.SubjectMatch),
		BodyMatch: strings.TrimSpace(in.BodyMatch), UseRegex: b2i(in.UseRegex), Parser: in.Parser,
	}
	if f.Parser == "" {
		f.Parser = "generic"
	}
	if f.Parser == "custom" && in.CustomParser != nil {
		b, _ := json.Marshal(in.CustomParser)
		f.CustomParser = string(b)
	}
	out := struct {
		FilterError string         `json:"filter_error"`
		ParserError string         `json:"parser_error"`
		Matches     []previewMatch `json:"matches"`
		Sample      *previewMatch  `json:"sample"`
	}{Matches: []previewMatch{}}
	filterOK := email.FilterOf(f).Validate() == nil
	if !filterOK {
		out.FilterError = email.FilterOf(f).Validate().Error()
	}
	if err := email.ValidateParser(f.Parser, f.CustomParser); err != nil {
		out.ParserError = err.Error()
	}
	parse := func(m db.EmailMessage) previewMatch {
		pm := previewMatch{ID: m.ID, Subject: m.Subject, FromAddr: m.FromAddr, ReceivedAt: m.ReceivedAt, Status: m.Status,
			Matches: filterOK && email.FilterOf(f).Matches(email.FromRow(m))}
		if out.ParserError != "" {
			return pm
		}
		if p, err := email.Parse(f.Parser, f.CustomParser, email.FromRow(m)); err != nil {
			pm.Error = err.Error()
		} else {
			pm.Parsed = &p
		}
		return pm
	}
	q := db.New(s.db)
	if in.MessageID != 0 {
		m, err := q.GetEmailMessage(ctx, db.GetEmailMessageParams{ID: in.MessageID, HouseholdID: hh})
		if err != nil {
			writeError(w, http.StatusNotFound, "Email not found.")
			return
		}
		pm := parse(m)
		out.Sample = &pm
	}
	if filterOK {
		msgs, err := q.ListRecentEmailBodies(ctx, db.ListRecentEmailBodiesParams{HouseholdID: hh, OnlyOpen: 0, Lim: 300})
		if err != nil {
			s.internalError(w, err)
			return
		}
		mf := email.FilterOf(f)
		for _, m := range msgs {
			if mf.Matches(email.FromRow(m)) {
				out.Matches = append(out.Matches, parse(m))
				if len(out.Matches) == 25 {
					break
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- Messages ----

func (s *Server) handleListEmailMessages(w http.ResponseWriter, r *http.Request) {
	ctx, hh := r.Context(), HouseholdID(r)
	q := db.New(s.db)
	status := r.URL.Query().Get("status")
	var rows []db.ListEmailMessagesRow
	var err error
	if status == "open" { // what needs attention: unrouted and parse_failed
		for _, st := range []string{email.StatusParseFailed, email.StatusUnrouted} {
			part, e := q.ListEmailMessages(ctx, db.ListEmailMessagesParams{HouseholdID: hh, Status: st, Lim: 200})
			rows, err = append(rows, part...), errors.Join(err, e)
		}
	} else {
		rows, err = q.ListEmailMessages(ctx, db.ListEmailMessagesParams{HouseholdID: hh, Status: status, Lim: 200})
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	counts, err := q.CountOpenEmailMessages(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	if rows == nil {
		rows = []db.ListEmailMessagesRow{}
	}
	type msgDTO struct {
		db.ListEmailMessagesRow
		MailboxID     *int64 `json:"mailbox_id"`
		FilterID      *int64 `json:"filter_id"`
		TransactionID *int64 `json:"transaction_id"`
		AiRecipe      string `json:"-"`
		// Why no filter could be built from the AI's reading of a transaction email.
		AIProblem string `json:"ai_problem"`
	}
	out := make([]msgDTO, len(rows))
	for i, m := range rows {
		out[i] = msgDTO{ListEmailMessagesRow: m, MailboxID: ptr(m.MailboxID), FilterID: ptr(m.FilterID), TransactionID: ptr(m.TransactionID)}
		var rc email.RecipeCheck
		if m.AiRecipe != "" && json.Unmarshal([]byte(m.AiRecipe), &rc) == nil {
			out[i].AIProblem = rc.Problem
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out, "counts": counts})
}

type emailMessageDTO struct {
	ID            int64  `json:"id"`
	FromAddr      string `json:"from_addr"`
	FromName      string `json:"from_name"`
	Subject       string `json:"subject"`
	ReceivedAt    int64  `json:"received_at"`
	BodyText      string `json:"body_text"`
	Status        string `json:"status"`
	FilterID      *int64 `json:"filter_id"`
	TransactionID *int64 `json:"transaction_id"`
	Error         string `json:"error"`
	AIKind        string `json:"ai_kind"`
	AISummary     string `json:"ai_summary"`
	// What the AI read from a transaction email and Viceroy's verdict (null when it wasn't read).
	AIRecipe *email.RecipeCheck `json:"ai_recipe"`
	// For the filter editor: the one account whose last 4 digits the email mentions, the phrase
	// that mentions them, and whether money went out or came in.
	SuggestedAccountID *int64 `json:"suggested_account_id"`
	AccountPhrase      string `json:"account_phrase"`
	SuggestedSign      string `json:"suggested_sign"`
}

func toEmailMessageDTO(m db.EmailMessage) emailMessageDTO {
	out := emailMessageDTO{
		ID: m.ID, FromAddr: m.FromAddr, FromName: m.FromName, Subject: m.Subject, ReceivedAt: m.ReceivedAt,
		BodyText: m.BodyText, Status: m.Status, FilterID: ptr(m.FilterID), TransactionID: ptr(m.TransactionID), Error: m.Error,
		AIKind: m.AiKind, AISummary: m.AiSummary, SuggestedSign: email.GuessSign(email.FromRow(m)),
	}
	if m.AiRecipe != "" {
		var rc email.RecipeCheck
		if json.Unmarshal([]byte(m.AiRecipe), &rc) == nil {
			out.AIRecipe = &rc
			if rc.Sign != "" {
				out.SuggestedSign = rc.Sign
			}
		}
	}
	return out
}

func (s *Server) handleGetEmailMessage(w http.ResponseWriter, r *http.Request) {
	m, err := db.New(s.db).GetEmailMessage(r.Context(), db.GetEmailMessageParams{ID: txnID(r), HouseholdID: HouseholdID(r)})
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Email not found.")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := toEmailMessageDTO(m)
	if id, phrase, err := email.SuggestAccount(r.Context(), db.New(s.db), HouseholdID(r), email.FromRow(m)); err != nil {
		s.internalError(w, err)
		return
	} else if id != 0 {
		out.SuggestedAccountID, out.AccountPhrase = &id, phrase
	}
	if out.AIRecipe != nil && out.AIRecipe.AccountID != 0 {
		out.SuggestedAccountID = &out.AIRecipe.AccountID
		if out.AIRecipe.Recipe.AccountText != "" {
			out.AccountPhrase = out.AIRecipe.Recipe.AccountText
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleIgnoreEmailMessage(w http.ResponseWriter, r *http.Request) {
	q := db.New(s.db)
	m, err := q.GetEmailMessage(r.Context(), db.GetEmailMessageParams{ID: txnID(r), HouseholdID: HouseholdID(r)})
	if err != nil {
		writeError(w, http.StatusNotFound, "Email not found.")
		return
	}
	if m.Status == email.StatusParsed {
		writeError(w, http.StatusConflict, "This email already created a transaction.")
		return
	}
	if err := q.SetEmailMessageResult(r.Context(), db.SetEmailMessageResultParams{Status: email.StatusIgnored, FilterID: m.FilterID, ID: m.ID}); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRetryEmailMessage(w http.ResponseWriter, r *http.Request) {
	m, err := s.mail.Retry(r.Context(), HouseholdID(r), txnID(r))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Email not found.")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toEmailMessageDTO(m))
}
