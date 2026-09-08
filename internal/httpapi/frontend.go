package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:frontend
var frontendAssets embed.FS

func serveFrontend(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	shell := p == "/" || p == "/index.html" || p == "/settings" || p == "/setup" || p == "/connect" || p == "/share" || (strings.HasPrefix(p, "/items/") && len(strings.Split(strings.Trim(p, "/"), "/")) == 2)
	if !shell {
		if path.Clean(p) != p || strings.HasSuffix(p, "/") {
			return false
		}
		if _, err := frontendAssets.ReadFile("frontend" + p); err != nil {
			return false
		}
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		methodNotAllowed(w, "GET, HEAD", "")
		return true
	}
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self'; style-src-attr 'unsafe-inline'; worker-src 'self' blob:; img-src 'self' data: blob:; frame-src 'self' blob:; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	if strings.HasPrefix(p, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	if shell {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		b, err := frontendAssets.ReadFile("frontend/index.html")
		if err != nil {
			b = []byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Attic</title></head><body><div id="root">Build the browser UI with pnpm build:embed, then rebuild Attic.</div></body></html>`)
		}
		if r.Method == "GET" {
			w.Write(b)
		}
		return true
	}
	if strings.HasSuffix(p, ".mjs") {
		w.Header().Set("Content-Type", "text/javascript")
	}
	if strings.HasSuffix(p, ".webmanifest") {
		w.Header().Set("Content-Type", "application/manifest+json")
	}
	files, _ := fs.Sub(frontendAssets, "frontend")
	http.FileServer(http.FS(files)).ServeHTTP(w, r)
	return true
}
