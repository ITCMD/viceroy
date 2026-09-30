// Command fakesimplefin runs a fake SimpleFIN Bridge for local development and e2e tests.
//
//	curl localhost:18430/_control/token            # a fresh setup token
//	curl -XPOST localhost:18430/_control/scenario -d '{"name":"relinked"}'
package main

import (
	"flag"
	"log"
	"net/http"

	"viceroy/internal/providers/simplefin/fake"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:18430", "listen address")
	flag.Parse()
	log.Printf("fake SimpleFIN Bridge on http://%s (scenarios: %v)", *addr, fake.Scenarios)
	log.Fatal(http.ListenAndServe(*addr, fake.New()))
}
