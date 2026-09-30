// Command fakeopenrouter serves internal/ai/fakeai for the e2e suite. Point viceroy at it with
// [ai] base_url = "http://127.0.0.1:18433" and openrouter_key = "test-key".
package main

import (
	"flag"
	"log"
	"net/http"

	"viceroy/internal/ai/fakeai"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:18433", "address to listen on")
	flag.Parse()
	log.Printf("fake OpenRouter on http://%s", *listen)
	log.Fatal(http.ListenAndServe(*listen, &fakeai.Server{}))
}
