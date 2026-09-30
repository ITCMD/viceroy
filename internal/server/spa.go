package server

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

func init() {
	// Not in Go's builtin table; browsers want this exact type for PWA install.
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// spaHandler serves the built web app, falling back to index.html for client-side routes.
func spaHandler(web fs.FS) http.Handler {
	files := http.FileServerFS(web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(web, p); errors.Is(err, fs.ErrNotExist) {
			if path.Ext(p) != "" {
				http.NotFound(w, r)
				return
			}
			p = "index.html"
		}
		switch {
		case strings.HasPrefix(p, "assets/"):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		default:
			// index.html, sw.js and the manifest must revalidate so updates roll out.
			w.Header().Set("Cache-Control", "no-cache")
		}
		if p == "index.html" {
			index, err := fs.ReadFile(web, "index.html")
			if err != nil {
				http.Error(w, "Web app not built. Run `make web` (or `npm run build` in web/).", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(index)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + p
		files.ServeHTTP(w, r2)
	})
}
