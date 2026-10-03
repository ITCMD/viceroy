package server

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"viceroy/internal/ai"
	"viceroy/internal/branding"
	"viceroy/internal/db"
)

// maxLogoBytes caps an uploaded account logo (the app shrinks images before sending).
const maxLogoBytes = 256 << 10

// handleSuggestAccountColor asks the light AI model for the account's bank color without
// saving it.
func (s *Server) handleSuggestAccountColor(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadAccount(w, r)
	if !ok {
		return
	}
	client, err := s.ai.EmailClient(r.Context(), a.HouseholdID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	name := strings.TrimSpace(a.InstitutionName)
	if name == "" {
		name = a.Name
	}
	c, err := branding.Suggest(r.Context(), client, branding.Bank{Name: name})
	switch {
	case errors.Is(err, ai.ErrNotConfigured):
		writeError(w, http.StatusBadRequest, "Set up AI in Settings → AI first.")
		return
	case err != nil:
		writeError(w, http.StatusBadGateway, err.Error())
		return
	case c == "":
		writeError(w, http.StatusUnprocessableEntity, "The AI doesn't know "+name+"'s color. Pick one instead.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"color": c})
}

func (s *Server) handleGetAccountLogo(w http.ResponseWriter, r *http.Request) {
	l, err := db.New(s.db).GetAccountLogo(r.Context(), db.GetAccountLogoParams{AccountID: txnID(r), HouseholdID: HouseholdID(r)})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", l.Mime)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable") // the URL carries ?v=
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Write(l.Data)
}

// handlePutAccountLogo stores an image given as a data URL (PNG, JPEG, WebP or GIF; no SVG,
// which could carry script).
func (s *Server) handlePutAccountLogo(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadAccount(w, r)
	if !ok {
		return
	}
	data, sniffed, ok := readImageUpload(w, r)
	if !ok {
		return
	}
	if err := db.New(s.db).SetAccountLogo(r.Context(), db.SetAccountLogoParams{AccountID: a.ID, Mime: sniffed, Data: data, UpdatedAt: time.Now().UnixNano()}); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteAccountLogo(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadAccount(w, r)
	if !ok {
		return
	}
	if err := db.New(s.db).DeleteAccountLogo(r.Context(), a.ID); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// readImageUpload reads {image: <data URL>} and checks it's a PNG, JPEG, WebP or GIF (by its
// bytes, whatever the data URL claimed; no SVG, which could carry script) of at most
// maxLogoBytes. It writes the error response when it returns false.
func readImageUpload(w http.ResponseWriter, r *http.Request) (data []byte, mime string, ok bool) {
	var in struct {
		Image string `json:"image"`
	}
	if !readJSONLimit(w, r, &in, maxLogoBytes*2) {
		return nil, "", false
	}
	_, b64, found := strings.Cut(in.Image, ";base64,")
	data, err := base64.StdEncoding.DecodeString(b64)
	if !found || err != nil || len(data) == 0 {
		writeError(w, http.StatusBadRequest, "Upload a PNG, JPEG, WebP or GIF image.")
		return nil, "", false
	}
	if len(data) > maxLogoBytes {
		writeError(w, http.StatusBadRequest, "That image is too large (256 KB at most).")
		return nil, "", false
	}
	mime = http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return data, mime, true
	}
	writeError(w, http.StatusBadRequest, "Upload a PNG, JPEG, WebP or GIF image.")
	return nil, "", false
}
