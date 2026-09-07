package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebSessionsProtectLibraryAndMutations(t *testing.T) {
	server, _, _ := testServer(t)
	home := request(server, "GET", "/", "", "")
	if home.Code != 200 || !strings.Contains(home.Body.String(), "Your reading library") {
		t.Fatal("web entry point unavailable")
	}
	if home.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing browser policy")
	}
	denied := request(server, "GET", "/api/v1/jobs", "", "")
	if denied.Code != 401 {
		t.Fatal("library is public")
	}
	login := request(server, "POST", "/api/v1/session", "", "secret-token")
	cookies := login.Result().Cookies()
	if login.Code != 200 || len(cookies) != 1 {
		t.Fatalf("login = %d", login.Code)
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("session cookie lacks protections")
	}
	do := func(method, path, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.AddCookie(cookie)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	if got := do("GET", "/api/v1/jobs", ""); got.Code != 200 {
		t.Fatalf("cookie cannot read library: %d", got.Code)
	}
	if got := do("DELETE", "/api/v1/session", "https://other.test"); got.Code != 401 {
		t.Fatal("cross-origin mutation allowed")
	}
	if got := do("DELETE", "/api/v1/session", "http://example.com"); got.Code != 204 {
		t.Fatalf("logout = %d %s", got.Code, got.Body.String())
	}
	if got := do("GET", "/api/v1/jobs", ""); got.Code != 401 {
		t.Fatal("logged-out session remains valid")
	}
}
