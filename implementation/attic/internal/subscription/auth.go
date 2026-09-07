// Package subscription owns Attic's ChatGPT device login and private tokens.
// It uses the direct OAuth flow documented in docs/chatgpt-subscription-access.md.
package subscription

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	ClientID      = "app_EMoamEEZ73f0CkXaXp7hrann"
	Issuer        = "https://auth.openai.com"
	DefaultPath   = "/data/auth/chatgpt.json"
	maxTokenBytes = 128 << 10
)

var ErrLoginRequired = errors.New("ChatGPT login required; run attic login-chatgpt")
var ErrUnavailable = errors.New("ChatGPT authentication service unavailable")
var ErrStorage = errors.New("ChatGPT credentials could not be read or saved securely")

type Credentials struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	AccountID    string    `json:"account_id"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type Store struct {
	path, issuer string
	client       *http.Client
}

func NewStore(path string) *Store {
	if path == "" {
		path = DefaultPath
	}
	return &Store{path: path, issuer: Issuer, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// Token serializes refresh across processes sharing this credential file.
// The inference request itself does not hold the file lock.
func (s *Store) Token(ctx context.Context) (string, string, error) {
	var result Credentials
	err := s.locked(ctx, func() error {
		current, err := s.read()
		if err != nil {
			return err
		}
		if !current.ExpiresAt.After(time.Now().Add(time.Minute)) {
			next, err := s.exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {current.RefreshToken}, "client_id": {ClientID}})
			if err != nil {
				return err
			}
			if next.RefreshToken == "" {
				next.RefreshToken = current.RefreshToken
			}
			if next.AccountID == "" {
				next.AccountID = current.AccountID
			}
			if err := s.save(next); err != nil {
				return err
			}
			current = next
		}
		result = current
		return nil
	})
	return result.AccessToken, result.AccountID, err
}

func (s *Store) read() (Credentials, error) {
	var c Credentials
	info, err := os.Lstat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return c, ErrLoginRequired
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > maxTokenBytes {
		return c, ErrStorage
	}
	f, err := os.Open(s.path)
	if err != nil {
		return c, ErrStorage
	}
	defer f.Close()
	if json.NewDecoder(io.LimitReader(f, maxTokenBytes)).Decode(&c) != nil || c.AccessToken == "" || c.RefreshToken == "" || c.AccountID == "" || strings.ContainsAny(c.AccessToken+c.AccountID, "\r\n") {
		return Credentials{}, ErrLoginRequired
	}
	return c, nil
}

func (s *Store) save(c Credentials) error {
	if c.AccessToken == "" || c.RefreshToken == "" || c.AccountID == "" || strings.ContainsAny(c.AccessToken+c.AccountID, "\r\n") {
		return ErrLoginRequired
	}
	data, err := json.Marshal(c)
	if err != nil || len(data) > maxTokenBytes {
		return ErrStorage
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".chatgpt-*")
	if err != nil {
		return ErrStorage
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return ErrStorage
	}
	if os.Rename(f.Name(), s.path) != nil {
		return ErrStorage
	}
	return nil
}

func (s *Store) locked(ctx context.Context, fn func() error) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return ErrStorage
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
		return ErrStorage
	}
	fd, err := syscall.Open(s.path+".lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return ErrStorage
	}
	defer syscall.Close(fd)
	for {
		err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			return ErrStorage
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	return fn()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func accountID(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		AccountID string `json:"chatgpt_account_id"`
		Auth      struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(data, &claims) != nil {
		return ""
	}
	if claims.Auth.AccountID != "" {
		return claims.Auth.AccountID
	}
	return claims.AccountID
}

func (s *Store) exchange(ctx context.Context, values url.Values) (Credentials, error) {
	var t tokenResponse
	status, err := s.post(ctx, "/oauth/token", "application/x-www-form-urlencoded", values.Encode(), &t)
	if err != nil {
		return Credentials{}, err
	}
	if status == 400 || status == 401 || status == 403 {
		return Credentials{}, ErrLoginRequired
	}
	if status != 200 {
		return Credentials{}, ErrUnavailable
	}
	id := accountID(t.IDToken)
	if id == "" {
		id = accountID(t.AccessToken)
	}
	if t.AccessToken == "" {
		return Credentials{}, ErrLoginRequired
	}
	if t.ExpiresIn <= 0 {
		t.ExpiresIn = 3600
	}
	return Credentials{AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, AccountID: id, ExpiresAt: time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)}, nil
}

func (s *Store) post(ctx context.Context, path, contentType, body string, result any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.issuer+path, strings.NewReader(body))
	if err != nil {
		return 0, ErrUnavailable
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "attic/1.0")
	res, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode == 200 {
		data, err := io.ReadAll(io.LimitReader(res.Body, maxTokenBytes+1))
		if err != nil || len(data) > maxTokenBytes || json.Unmarshal(data, result) != nil {
			return res.StatusCode, ErrUnavailable
		}
	}
	return res.StatusCode, nil
}

// Login shows only the verification URL and short-lived user code. Tokens stay
// in the private credential store; no browser cookies or other clients are read.
func (s *Store) Login(ctx context.Context, show func(string, string)) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	return s.locked(ctx, func() error {
		var device struct {
			ID       string          `json:"device_auth_id"`
			Code     string          `json:"user_code"`
			Interval json.RawMessage `json:"interval"`
		}
		status, err := s.post(ctx, "/api/accounts/deviceauth/usercode", "application/json", fmt.Sprintf(`{"client_id":%q}`, ClientID), &device)
		if err != nil {
			return err
		}
		if status != 200 || device.ID == "" || device.Code == "" {
			return ErrUnavailable
		}
		show(s.issuer+"/codex/device", device.Code)
		var seconds int
		_ = json.Unmarshal(device.Interval, &seconds)
		if seconds == 0 {
			var str string
			_ = json.Unmarshal(device.Interval, &str)
			_, _ = fmt.Sscanf(str, "%d", &seconds)
		}
		if seconds < 1 {
			seconds = 5
		}
		if seconds > 60 {
			seconds = 60
		}
		body, _ := json.Marshal(map[string]string{"device_auth_id": device.ID, "user_code": device.Code})
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(seconds) * time.Second):
			}
			var approved struct {
				Code     string `json:"authorization_code"`
				Verifier string `json:"code_verifier"`
			}
			status, err = s.post(ctx, "/api/accounts/deviceauth/token", "application/json", string(body), &approved)
			if err != nil {
				return err
			}
			if status == 403 || status == 404 {
				continue
			}
			if status != 200 || approved.Code == "" || approved.Verifier == "" {
				return ErrLoginRequired
			}
			c, err := s.exchange(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {approved.Code}, "code_verifier": {approved.Verifier}, "redirect_uri": {s.issuer + "/deviceauth/callback"}, "client_id": {ClientID}})
			if err != nil {
				return err
			}
			return s.save(c)
		}
	})
}
