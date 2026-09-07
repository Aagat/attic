package httpapi

import (
	"archive/zip"
	"bytes"
	"embed"
	"net/http"
	"time"
)

//go:embed extension/*
var extensionAssets embed.FS

func (s *Server) serveExtension(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/attic-chromium.zip" {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	entries, err := extensionAssets.ReadDir("extension")
	if err != nil {
		http.Error(w, "Extension unavailable", 500)
		return true
	}
	for _, entry := range entries {
		body, err := extensionAssets.ReadFile("extension/" + entry.Name())
		if err != nil {
			http.Error(w, "Extension unavailable", 500)
			return true
		}
		file, err := archive.Create("attic/" + entry.Name())
		if err != nil {
			http.Error(w, "Extension unavailable", 500)
			return true
		}
		if _, err := file.Write(body); err != nil {
			http.Error(w, "Extension unavailable", 500)
			return true
		}
	}
	if err := archive.Close(); err != nil {
		http.Error(w, "Extension unavailable", 500)
		return true
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="attic-chromium.zip"`)
	http.ServeContent(w, r, "attic-chromium.zip", time.Time{}, bytes.NewReader(data.Bytes()))
	return true
}
