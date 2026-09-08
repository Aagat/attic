package subscription

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fakeID() string {
	return "a." + base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"account"}}`)) + ".b"
}
func TestRefreshSerializesAcrossStoresAndPersistsRotation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = r.ParseForm()
		if r.Form.Get("refresh_token") != "old-refresh" || r.Form.Get("grant_type") != "refresh_token" {
			t.Error("wrong refresh request")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-access", "refresh_token": "rotated-refresh", "id_token": fakeID(), "expires_in": 3600})
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "auth.json")
	s := NewStore(path)
	s.issuer = server.URL
	if err := s.save(Credentials{AccessToken: "old-access", RefreshToken: "old-refresh", AccountID: "account", ExpiresAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other := NewStore(path)
			other.issuer = server.URL
			token, id, err := other.Token(context.Background())
			if err != nil || token != "new-access" || id != "account" {
				t.Errorf("refresh failed: %v", err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("refreshes=%d", calls.Load())
	}
	c, err := s.read()
	if err != nil || c.RefreshToken != "rotated-refresh" {
		t.Fatal("rotation not persisted")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("credential permissions")
	}
}

func TestDeviceLoginExchangesAndSavesOwnCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			_, _ = w.Write([]byte(`{"device_auth_id":"device","user_code":"ABCD","interval":"1"}`))
		case "/api/accounts/deviceauth/token":
			_, _ = w.Write([]byte(`{"authorization_code":"code","code_verifier":"verifier"}`))
		case "/oauth/token":
			_ = r.ParseForm()
			if r.Form.Get("code") != "code" || r.Form.Get("code_verifier") != "verifier" || r.Form.Get("client_id") != ClientID {
				t.Error("bad code exchange")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "refresh_token": "refresh", "id_token": fakeID(), "expires_in": 3600})
		default:
			t.Error("unexpected request")
		}
	}))
	defer server.Close()
	s := NewStore(filepath.Join(t.TempDir(), "auth.json"))
	s.issuer = server.URL
	shown := false
	err := s.Login(context.Background(), func(url, code string) {
		shown = true
		if url != server.URL+"/codex/device" || code != "ABCD" {
			t.Error("wrong verification instructions")
		}
	})
	if err != nil || !shown {
		t.Fatalf("login: %v", err)
	}
	token, id, err := s.Token(context.Background())
	if err != nil || token != "access" || id != "account" {
		t.Fatal("login not persisted")
	}
}

func TestCredentialSafetyAndProviderErrors(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "auth.json"))
	if _, _, err := s.Token(context.Background()); !errors.Is(err, ErrLoginRequired) {
		t.Fatal(err)
	}
	_ = os.WriteFile(s.path, []byte(`{}`), 0644)
	if _, _, err := s.Token(context.Background()); !errors.Is(err, ErrStorage) {
		t.Fatal(err)
	}
	_ = os.Remove(s.path)
	_ = os.Symlink("/does/not/exist", s.path)
	if _, _, err := s.Token(context.Background()); !errors.Is(err, ErrStorage) {
		t.Fatal(err)
	}
	_ = os.Remove(s.path)
	_ = s.save(Credentials{AccessToken: "access", RefreshToken: "refresh", AccountID: "account"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"secret-provider-body"}`))
	}))
	defer server.Close()
	s.issuer = server.URL
	_, _, err := s.Token(context.Background())
	if !errors.Is(err, ErrLoginRequired) || strings.Contains(err.Error(), "secret-provider-body") {
		t.Fatal("unsafe error")
	}
}

func TestLoginCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := NewStore(filepath.Join(t.TempDir(), "auth.json"))
	if err := s.Login(ctx, func(string, string) {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}
