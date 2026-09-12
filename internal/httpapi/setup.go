package httpapi

import (
	"attic/internal/application"
	"attic/internal/delivery"
	"attic/internal/search"
	"attic/internal/setup"
	"attic/internal/subscription"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Setup contains only server-owned integrations; credential objects are never encoded.
type Setup struct {
	Mail          *setup.Mail
	Auth          *subscription.Store
	Login         *subscription.Sessions
	Provider      string
	CheckAI       func(context.Context) string
	Runtime       func(context.Context) map[string]string
	mu            sync.Mutex
	check         string
	checking      bool
	runtimeMu     sync.Mutex
	runtimeResult map[string]string
}

func (s *Server) serveSetup(w http.ResponseWriter, r *http.Request, id string) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/v1/setup") {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	respond := func(v any) { json.NewEncoder(w).Encode(v) }
	fail := func(code int, message string) {
		writeError(w, application.NewSafeError("setup_failed", code, message), id)
	}
	if s.setup == nil {
		fail(503, "Setup controls are unavailable")
		return true
	}
	u := s.setup
	switch r.URL.Path {
	case "/api/v1/setup":
		if r.Method != "GET" {
			fail(405, "Use GET")
			break
		}
		u.mu.Lock()
		check := u.check
		runtimeResult := u.runtimeResult
		u.mu.Unlock()
		respond(map[string]any{"provider": u.Provider, "ai": u.Auth.Status(), "login": u.Login.State(), "mail": u.Mail.View(), "compatibility": check, "runtime": runtimeResult, "search_configured": s.search != nil})
	case "/api/v1/setup/chatgpt":
		if u.Provider != "chatgpt" {
			fail(409, "Set AI_PROVIDER=chatgpt in deployment configuration to use subscription login")
			break
		}
		switch r.Method {
		case "POST":
			respond(u.Login.Start())
		case "DELETE":
			if err := u.Login.Disconnect(r.Context()); err != nil {
				fail(503, "Unable to remove local credentials")
			} else {
				respond(map[string]string{"message": "Local credentials removed. Provider access is not revoked; manage it in your ChatGPT account."})
			}
		default:
			fail(405, "Use POST or DELETE")
		}
	case "/api/v1/setup/chatgpt/cancel":
		if r.Method != "POST" {
			fail(405, "Use POST")
			break
		}
		u.Login.Cancel()
		respond(u.Login.State())
	case "/api/v1/setup/check-ai":
		if r.Method != "POST" {
			fail(405, "Use POST")
			break
		}
		u.mu.Lock()
		if u.checking {
			u.mu.Unlock()
			fail(409, "A compatibility check is already running")
			break
		}
		u.checking = true
		u.mu.Unlock()
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		result := u.CheckAI(ctx)
		u.mu.Lock()
		u.check = result
		u.checking = false
		u.mu.Unlock()
		check := result
		respond(map[string]string{"message": check})
	case "/api/v1/setup/mail":
		if r.Method != "PUT" {
			fail(405, "Use PUT")
			break
		}
		var in setup.MailInput
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
		d.DisallowUnknownFields()
		if d.Decode(&in) != nil {
			fail(400, "Invalid mail settings")
			break
		}
		if err := u.Mail.Update(in); err != nil {
			fail(400, err.Error())
			break
		}
		respond(u.Mail.View())
	case "/api/v1/setup/mail/test":
		if r.Method != "POST" {
			fail(405, "Use POST")
			break
		}
		smtp, err := delivery.NewSMTP(u.Mail.Config())
		if err != nil {
			fail(400, "Enable and save valid mail configuration first")
			break
		}
		if err := smtp.TestConnection(r.Context()); err != nil {
			fail(422, err.Error())
			break
		}
		message := "SMTP connection and authentication succeeded. No email was sent; Kindle receipt is untested."
		if u.Mail.Config().Username == "" {
			message = "SMTP connection succeeded without configured authentication. No email was sent; Kindle receipt is untested."
		}
		respond(map[string]string{"message": message})
	case "/api/v1/setup/mail/send-test":
		if r.Method != "POST" {
			fail(405, "Use POST")
			break
		}
		config := u.Mail.Config()
		var in struct {
			Destination string `json:"destination"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil || in.Destination != config.Destination || !config.Enabled || in.Destination == "" {
			fail(409, "Confirm the current recipient before sending")
			break
		}
		smtp, err := delivery.NewSMTP(config)
		if err != nil {
			fail(400, "Save mail settings first")
			break
		}
		result := smtp.SendTest(r.Context())
		message := "SMTP submission failed. Check mail settings before explicitly retrying."
		if result.Outcome == "accepted" {
			message = "SMTP accepted the test email. Device receipt is not confirmed."
		} else if result.Outcome == "uncertain" {
			message = "SMTP acceptance is uncertain. Check the inbox before retrying to avoid a duplicate."
		}
		respond(map[string]string{"message": message, "diagnostic_id": id})
	case "/api/v1/setup/runtime":
		if r.Method != "POST" {
			fail(405, "Use POST")
			break
		}
		if !u.runtimeMu.TryLock() {
			fail(409, "A runtime check is already running")
			break
		}
		result := u.Runtime(r.Context())
		u.mu.Lock()
		u.runtimeResult = result
		u.mu.Unlock()
		u.runtimeMu.Unlock()
		respond(result)
	case "/api/v1/setup/search":
		if r.Method != "GET" && r.Method != "POST" {
			fail(405, "Use GET or POST")
			break
		}
		if s.search == nil || s.library == nil {
			fail(409, "Configure SEARCH_URL and an Attic index-scoped SEARCH_API_KEY in deployment settings")
			break
		}
		if r.Method == "POST" {
			if err := s.library.Reindex(r.Context()); err != nil {
				fail(503, "Cannot queue reindex")
				break
			}
		}
		pending, failed, err := s.library.IndexProgress(r.Context())
		if err != nil {
			fail(503, "Cannot read indexing progress")
			break
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		_, err = s.search.Search(ctx, search.Query{Limit: 1})
		state := "indexing"
		if err != nil {
			state = "unavailable"
		} else if failed > 0 {
			state = "failed"
		} else if pending == 0 {
			state = "ready"
		}
		respond(map[string]any{"state": state, "pending": pending, "failed": failed})
	default:
		fail(404, "Setup route not found")
	}
	return true
}
