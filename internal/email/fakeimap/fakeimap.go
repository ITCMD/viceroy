// Package fakeimap is an in-memory IMAP server for tests and the e2e suite.
package fakeimap

import (
	"bytes"
	"io"
	"log"
	"net"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

type Server struct {
	Addr string
	user *imapmemserver.User
	srv  *imapserver.Server
	ln   net.Listener
}

// Start serves a plaintext IMAP server on addr ("127.0.0.1:0" for a random port) with one
// user and an INBOX.
func Start(addr, username, password string) (*Server, error) {
	mem := imapmemserver.New()
	u := imapmemserver.NewUser(username, password)
	if err := u.Create("INBOX", nil); err != nil {
		return nil, err
	}
	mem.AddUser(u)
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}, imap.CapIdle: {}},
		InsecureAuth: true,
		Logger:       log.New(io.Discard, "", 0),
	})
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	go srv.Serve(ln)
	return &Server{Addr: ln.Addr().String(), user: u, srv: srv, ln: ln}, nil
}

// Deliver appends a raw message to INBOX.
func (s *Server) Deliver(raw []byte) error {
	_, err := s.user.Append("INBOX", bytes.NewReader(raw), &imap.AppendOptions{Time: time.Now()})
	return err
}

func (s *Server) Close() error { return s.srv.Close() }
