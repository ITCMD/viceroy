// Command fakeimap runs an in-memory IMAP server (plaintext, user test@example.com /
// app-pass) seeded with the alert fixtures, for local development and e2e tests.
//
//	curl -XPOST localhost:18432/deliver --data-binary @alert.eml   # deliver a new message
package main

import (
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"viceroy/internal/email/fakeimap"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:18431", "IMAP listen address")
	control := flag.String("control", "127.0.0.1:18432", "HTTP control address")
	seed := flag.String("seed", "internal/email/testdata", "directory of .eml files delivered at start")
	flag.Parse()
	srv, err := fakeimap.Start(*addr, "test@example.com", "app-pass")
	if err != nil {
		log.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(*seed, "*.eml"))
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			log.Fatal(err)
		}
		if err := srv.Deliver(raw); err != nil {
			log.Fatal(err)
		}
	}
	log.Printf("fake IMAP on %s with %d messages; control on http://%s", srv.Addr, len(files), *control)
	http.HandleFunc("POST /deliver", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := srv.Deliver(raw); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	log.Fatal(http.ListenAndServe(*control, nil))
}
