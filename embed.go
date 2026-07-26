// Package geet embeds the built web UI into the binary.
//
// This file lives at the module root because go:embed can only reach files at or
// below its own package directory, and web/dist is a sibling of cmd/ and
// internal/. Embedding is what makes the whole app one file and therefore one
// tiny container.
package geet

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:web/dist
var distFS embed.FS

const placeholder = `<!doctype html><html><head><meta charset="utf-8">
<title>geet</title></head><body style="font-family:sans-serif;padding:2rem">
<h1>geet</h1><p>The web UI has not been built into this binary.</p>
<p>Run <code>npm --prefix web run build</code> and rebuild, or use the Vite dev
server on port 5173 during development. The JSON API is live at
<code>/api/boards</code>.</p></body></html>`

// WebHandler serves the SPA, falling back to index.html for unknown paths so
// client-side routes survive a refresh or a pasted link.
func WebHandler() http.Handler {
	sub, err := fs.Sub(distFS, "web/dist")
	if err != nil {
		return placeholderHandler()
	}
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		// Built without a frontend (a plain `go build` before npm run build).
		return placeholderHandler()
	}
	files := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			serveIndex(w, index)
			return
		}
		if _, err := fs.Stat(sub, p); err != nil {
			serveIndex(w, index)
			return
		}
		// Hashed asset filenames are safe to cache hard; index.html is not.
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, index []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(index)
}

func placeholderHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(placeholder))
	})
}
