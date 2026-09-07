package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"attic/internal/application"
)

//go:embed web/*
var webAssets embed.FS

func (s *Server) serveWeb(w http.ResponseWriter, r *http.Request) bool {
	if s.serveExtension(w, r) {
		return true
	}
	path := r.URL.Path
	switch path {
	case "/", "/share.js", "/app.js", "/app.css", "/connect.html", "/connect.js", "/manifest.webmanifest", "/sw.js", "/offline.html", "/icon-192.png", "/icon-512.png":
	default:
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, "GET, HEAD", "")
		return true
	}
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; frame-src 'self' blob:; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	if path == "/manifest.webmanifest" {
		w.Header().Set("Content-Type", "application/manifest+json")
	}
	files, _ := fs.Sub(webAssets, "web")
	http.FileServer(http.FS(files)).ServeHTTP(w, r)
	return true
}

func (s *Server) authorizedSession(r *http.Request) bool {
	cookie, err := r.Cookie("attic_session")
	if err != nil {
		return false
	}
	// SameSite protects cookies; explicit origin matching also rejects forms or
	// scripts hosted on another port of this machine.
	if r.Method != "GET" && r.Method != "HEAD" {
		origin, err := url.Parse(r.Header.Get("Origin"))
		if err != nil || origin.Host != r.Host || (origin.Scheme != "http" && origin.Scheme != "https") {
			return false
		}
	}
	hash := sha256.Sum256([]byte(cookie.Value))
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	expires, ok := s.sessions[hash]
	if !ok || !s.now().Before(expires) {
		delete(s.sessions, hash)
		return false
	}
	return true
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request, correlationID string) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, map[string]bool{"authenticated": true})
	case http.MethodPost:
		// A cookie cannot mint another session: a login needs the operator's key.
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			writeError(w, application.NewSafeError("unauthorized", 401, "Enter your access key"), correlationID)
			return
		}
		token := make([]byte, 32)
		if _, err := rand.Read(token); err != nil {
			writeError(w, application.NewSafeError("internal_error", 500, "Could not sign in"), correlationID)
			return
		}
		value := hex.EncodeToString(token)
		hash := sha256.Sum256([]byte(value))
		expires := s.now().Add(30 * 24 * time.Hour)
		s.sessionsMu.Lock()
		for key, until := range s.sessions {
			if !s.now().Before(until) {
				delete(s.sessions, key)
			}
		}
		if len(s.sessions) >= 256 {
			s.sessionsMu.Unlock()
			writeError(w, application.NewSafeError("rate_limited", 429, "Too many active sessions"), correlationID)
			return
		}
		s.sessions[hash] = expires
		s.sessionsMu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "attic_session", Value: value, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 30 * 24 * 60 * 60, Expires: expires})
		writeJSON(w, 200, map[string]bool{"authenticated": true})
	case http.MethodDelete:
		if cookie, err := r.Cookie("attic_session"); err == nil {
			s.sessionsMu.Lock()
			delete(s.sessions, sha256.Sum256([]byte(cookie.Value)))
			s.sessionsMu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: "attic_session", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w, "GET, POST, DELETE", correlationID)
	}
}
