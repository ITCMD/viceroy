package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/backup"
)

// Backups are instance-wide (database + keys), so only admins see or make them, and
// only from the app: the archive holds the key that decrypts bank tokens.
func (s *Server) backupRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(sessionOnly)
		r.Get("/settings/backups", s.handleListBackups)
		r.Post("/settings/backups", s.handleCreateBackup)
	})
}

func (s *Server) backups() *backup.Service {
	return &backup.Service{DB: s.db, DataDir: s.cfg.DataDir, Dir: s.cfg.Backup.Dir, Config: s.ConfigPath, Keep: s.cfg.Backup.Keep}
}

type backupDTO struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	At   int64  `json:"at"`
}

func (s *Server) backupList(w http.ResponseWriter) {
	list, err := s.backups().List()
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := make([]backupDTO, 0, len(list))
	for _, b := range list {
		out = append(out, backupDTO{b.Name, b.Size, b.At.Unix()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"dir": s.cfg.Backup.Dir, "keep": s.cfg.Backup.Keep, "backups": out})
}

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	if CurrentUser(r).IsAdmin != 1 {
		writeError(w, http.StatusForbidden, "Only an admin can manage backups.")
		return
	}
	s.backupList(w)
}

func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	if CurrentUser(r).IsAdmin != 1 {
		writeError(w, http.StatusForbidden, "Only an admin can manage backups.")
		return
	}
	b := s.backups()
	info, err := b.Create(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	s.log.Info("backup written", "file", info.Path, "by", CurrentUser(r).ID)
	if b.Keep > 0 {
		if err := b.Prune(b.Keep); err != nil {
			s.log.Warn("pruning backups", "err", err)
		}
	}
	s.backupList(w)
}
