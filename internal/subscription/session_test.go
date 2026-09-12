package subscription

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentLoginCancellationRetainsWorkingCredentials(t *testing.T) {
	var calls atomic.Int32
	ready := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/accounts/deviceauth/usercode" {
			if calls.Add(1) == 1 {
				close(ready)
			}
			w.Write([]byte(`{"device_auth_id":"d","user_code":"CODE","interval":60}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	store := NewStore(filepath.Join(t.TempDir(), "auth"))
	store.issuer = server.URL
	if err := store.save(Credentials{AccessToken: "old", RefreshToken: "refresh", AccountID: "account", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	sessions := NewSessions(store)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); sessions.Start() }()
	}
	wg.Wait()
	<-ready
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if token, _, err := store.Token(ctx); err != nil || token != "old" {
		t.Fatal("reconnect blocked existing credentials")
	}
	sessions.Cancel()
	if calls.Load() != 1 || sessions.State().State != "cancelled" || store.Status() != "connected" {
		t.Fatal("concurrent or cancelled login lost state")
	}
	if err := sessions.Disconnect(ctx); err != nil || store.Status() != "not_connected" {
		t.Fatal("disconnect failed")
	}
}

func TestLoginExpiryClearsVerificationCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"device_auth_id":"d","user_code":"CODE","interval":60}`))
	}))
	defer server.Close()
	store := NewStore(filepath.Join(t.TempDir(), "auth"))
	store.issuer = server.URL
	sessions := NewSessions(store)
	sessions.lifetime = 20 * time.Millisecond
	sessions.Start()
	sessions.mu.Lock()
	done := sessions.done
	sessions.mu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unbounded login")
	}
	state := sessions.State()
	if state.State != "expired" || state.Code != "" || store.Status() != "not_connected" {
		t.Fatal("expiry state incorrect")
	}
}
