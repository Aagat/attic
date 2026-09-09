package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"attic/internal/application"
	"attic/internal/recovery"
)

func (s *Server) serveRecovery(w http.ResponseWriter, r *http.Request, correlation string) bool {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 5 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "items" || parts[4] != "recovery" {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	fail := func(code string, status int, message string) {
		writeError(w, application.NewSafeError(code, status, message), correlation)
	}
	if r.Method != "GET" && r.Method != "POST" {
		fail("method_not_allowed", 405, "Use GET or POST")
		return true
	}
	if s.library == nil || s.recovery == nil {
		fail("recovery_unavailable", 503, "Browser recovery is not configured")
		return true
	}
	item, err := s.library.Get(r.Context(), parts[3])
	if err != nil {
		fail("not_found", 404, "Saved item was not found")
		return true
	}
	if item.Kind != "bookmark" {
		fail("invalid_item", 400, "Only web bookmarks use browser recovery")
		return true
	}
	state := s.recovery.State(item.URL)
	if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		var action recovery.Action
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&action) != nil || decoder.Decode(new(any)) != io.EOF {
			fail("invalid_action", 400, "Invalid browser action")
			return true
		}
		state, err = s.recovery.Command(item.URL, action)
		if err != nil {
			if err == recovery.ErrBusy {
				fail("recovery_busy", 409, err.Error())
			} else {
				fail("invalid_action", 400, "The browser action is unavailable. Refresh the session and try again.")
			}
			return true
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(state)
	return true
}
