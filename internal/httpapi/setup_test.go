package httpapi

import (
	"attic/internal/delivery"
	"attic/internal/setup"
	"attic/internal/subscription"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupRequiresOwnerAndNeverReturnsPassword(t *testing.T) {
	dir := t.TempDir()
	mail, _ := setup.NewMail(filepath.Join(dir, "mail"), delivery.Config{Password: "production-secret"}, true)
	auth := subscription.NewStore(filepath.Join(dir, "chatgpt"))
	controls := &Setup{Mail: mail, Auth: auth, Login: subscription.NewSessions(auth), Provider: "chatgpt", CheckAI: func(context.Context) string { return "fixture compatible" }}
	s := NewServerWithOptions(nil, nil, "owner", Options{Setup: controls})
	for _, path := range []string{"/api/v1/setup", "/api/v1/setup/chatgpt", "/api/v1/setup/mail", "/api/v1/setup/mail/test", "/api/v1/setup/mail/send-test", "/api/v1/setup/check-ai", "/api/v1/setup/runtime", "/api/v1/setup/search"} {
		r := httptest.NewRequest("POST", path, nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 401 && w.Code != 429 {
			t.Fatalf("unauthorized %s: %d", path, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/setup", nil)
	r.Header.Set("Authorization", "Bearer owner")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "production-secret") || !strings.Contains(w.Body.String(), "not_connected") {
		t.Fatalf("unsafe status: %s", w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("setup cacheable")
	}
	r = httptest.NewRequest(http.MethodPut, "/api/v1/setup/mail", strings.NewReader(`{"host":"override"}`))
	r.Header.Set("Authorization", "Bearer owner")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "deployment-managed") {
		t.Fatal("managed settings editable")
	}
}

func TestSetupCookieMutationRejectsCrossOrigin(t *testing.T) {
	s := NewServerWithOptions(nil, nil, "owner", Options{})
	r := httptest.NewRequest("POST", "http://attic.test/api/v1/session", nil)
	r.Header.Set("Authorization", "Bearer owner")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("session missing")
	}
	r = httptest.NewRequest("POST", "http://attic.test/api/v1/setup/chatgpt", nil)
	r.AddCookie(cookies[0])
	r.Header.Set("Origin", "https://attacker.test")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("cross origin mutation authorized")
	}
}
