package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebSessionsProtectLibraryAndMutations(t *testing.T) {
	server, _, _ := testServer(t)
	home := request(server, "GET", "/", "", "")
	if home.Code != 200 || !strings.Contains(home.Body.String(), "Things you might need later") {
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

func TestSharingAssetsArePublicButCannotSubmitJobs(t *testing.T) {
	server, _, _ := testServer(t)
	for _, path := range []string{"/share.js", "/connect.html", "/connect.js", "/manifest.webmanifest", "/sw.js", "/offline.html", "/icon-192.png", "/icon-512.png"} {
		response := request(server, "GET", path, "", "")
		if response.Code != 200 {
			t.Fatalf("%s: %d", path, response.Code)
		}
	}
	manifest := request(server, "GET", "/manifest.webmanifest", "", "")
	var config struct {
		Share struct{ Action, Method string } `json:"share_target"`
	}
	if err := json.Unmarshal(manifest.Body.Bytes(), &config); err != nil || config.Share.Action != "/" || config.Share.Method != "GET" {
		t.Fatalf("invalid share manifest: %v", err)
	}
	// A shared URL only renders the public application shell. It never creates a job.
	response := request(server, "GET", "/?text=Read+https%3A%2F%2Fexample.com%2Farticle", "", "")
	if response.Code != 200 {
		t.Fatal("share landing unavailable")
	}
	page := request(server, "GET", "/api/v1/jobs", "", "secret-token")
	if !strings.Contains(page.Body.String(), `"items":[]`) {
		t.Fatalf("share mutated library: %s", page.Body.String())
	}
	if response := request(server, "POST", "/", "url=https://example.com", ""); response.Code != 405 {
		t.Fatal("share bypassed authenticated API")
	}
}
