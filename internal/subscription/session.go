package subscription

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"
)

// Status never reads or exposes tokens to callers. Expiry requires an explicit
// compatibility check because a refresh token may still be valid.
func (s *Store) Status() string {
	c, err := s.read()
	if errors.Is(err, ErrLoginRequired) {
		return "not_connected"
	}
	if err != nil {
		return "storage_error"
	}
	if !c.ExpiresAt.After(time.Now()) {
		return "refresh_required"
	}
	return "connected"
}
func (s *Store) Disconnect(ctx context.Context) error {
	return s.locked(ctx, func() error {
		err := os.Remove(s.path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return ErrStorage
		}
		return nil
	})
}

type LoginState struct {
	State     string    `json:"state"`
	URL       string    `json:"url,omitempty"`
	Code      string    `json:"code,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

// Sessions owns one bounded login per application process. Browser reloads only
// read this state; they never start additional provider polling loops.
type Sessions struct {
	lifetime time.Duration
	op       sync.Mutex
	mu       sync.Mutex
	store    *Store
	state    LoginState
	cancel   context.CancelFunc
	done     chan struct{}
}

func NewSessions(s *Store) *Sessions {
	return &Sessions{lifetime: 15 * time.Minute, store: s, state: LoginState{State: "idle"}}
}
func (s *Sessions) State() LoginState { s.mu.Lock(); defer s.mu.Unlock(); return s.state }
func (s *Sessions) Start() LoginState {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return s.state
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.lifetime)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.state = LoginState{State: "starting", ExpiresAt: time.Now().Add(s.lifetime)}
	go func() {
		err := s.store.Login(ctx, func(url, code string) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.state.State = "waiting"
			s.state.URL = url
			s.state.Code = code
		})
		s.mu.Lock()
		defer s.mu.Unlock()
		state := "connected"
		switch {
		case errors.Is(err, context.Canceled):
			state = "cancelled"
		case errors.Is(err, context.DeadlineExceeded):
			state = "expired"
		case err != nil:
			state = "failed"
		}
		s.state = LoginState{State: state}
		cancel()
		s.cancel = nil
		close(s.done)
	}()
	return s.state
}
func (s *Sessions) Cancel() { s.op.Lock(); defer s.op.Unlock(); s.cancelLogin() }
func (s *Sessions) cancelLogin() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	if cancel != nil {
		cancel()
	}
	s.mu.Unlock()
	if cancel != nil {
		<-done
	}
}

// Stop polling and wait for any credential commit before removing credentials.
func (s *Sessions) Disconnect(ctx context.Context) error { s.Cancel(); return s.store.Disconnect(ctx) }
