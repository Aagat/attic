// Package setup owns private, persistent owner configuration. Deployment SMTP
// configuration is authoritative as a whole to avoid mixing credential sets.
package setup

import (
	"attic/internal/delivery"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var ErrManaged = errors.New("SMTP is deployment-managed; update deployment values and recreate Attic")
var ErrStorage = errors.New("private settings storage unavailable; check state volume permissions")

type Mail struct {
	BeforeEnable func() error
	mu           sync.RWMutex
	path         string
	managed      bool
	config       delivery.Config
}
type MailView struct {
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	TLSMode     string `json:"tls_mode"`
	Username    string `json:"username"`
	Sender      string `json:"sender"`
	Destination string `json:"destination"`
	PasswordSet bool   `json:"password_set"`
	Managed     bool   `json:"managed"`
}
type MailInput struct {
	Enabled          bool   `json:"enabled"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	TLSMode          string `json:"tls_mode"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	Sender           string `json:"sender"`
	Destination      string `json:"destination"`
	ClearCredentials bool   `json:"clear_credentials"`
}

func NewMail(path string, c delivery.Config, managed bool) (*Mail, error) {
	m := &Mail{path: path, config: c, managed: managed}
	if managed {
		return m, nil
	}
	dirInfo, dirErr := os.Lstat(filepath.Dir(path))
	if dirErr == nil && (!dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 || dirInfo.Mode().Perm()&0022 != 0) {
		return nil, ErrStorage
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16384 {
		return nil, ErrStorage
	}
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &m.config) != nil {
		return nil, ErrStorage
	}
	if m.config.Validate() != nil {
		return nil, ErrStorage
	}
	return m, nil
}
func (m *Mail) Config() delivery.Config { m.mu.RLock(); defer m.mu.RUnlock(); return m.config }
func (m *Mail) View() MailView {
	c := m.Config()
	return MailView{c.Enabled, c.Host, c.Port, c.TLSMode, c.Username, c.Sender, c.Destination, c.Password != "", m.managed}
}
func (m *Mail) Destination() string {
	c := m.Config()
	if !c.Enabled {
		return ""
	}
	return c.Destination
}
func (m *Mail) Update(in MailInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.managed {
		return ErrManaged
	}
	c := delivery.Config{Enabled: in.Enabled, Host: in.Host, Port: in.Port, TLSMode: in.TLSMode, Username: in.Username, Password: in.Password, Sender: in.Sender, Destination: in.Destination, Timeout: 30 * time.Second}
	if c.Password == "" {
		c.Password = m.config.Password
	}
	if in.ClearCredentials {
		c.Username = ""
		c.Password = ""
	}
	// Validate even disabled configurations that contain values.
	check := c
	check.Enabled = true
	if err := check.Validate(); err != nil {
		return err
	}
	if c.Enabled && !m.config.Enabled && m.BeforeEnable != nil {
		if err := m.BeforeEnable(); err != nil {
			return err
		}
	}
	dir := filepath.Dir(m.path)
	if os.MkdirAll(dir, 0700) != nil {
		return ErrStorage
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
		return ErrStorage
	}
	b, _ := json.Marshal(c)
	f, err := os.CreateTemp(dir, ".mail-*")
	if err != nil {
		return ErrStorage
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil || os.Rename(f.Name(), m.path) != nil {
		return ErrStorage
	}
	m.config = c
	return nil
}
func (m *Mail) Send(ctx context.Context, claim *delivery.Claim, pdf io.Reader) delivery.Result {
	c := m.Config()
	if !c.Enabled {
		return delivery.Result{Outcome: "rejected"}
	}
	s, err := delivery.NewSMTP(c)
	if err != nil {
		return delivery.Result{Outcome: "rejected"}
	}
	return s.Send(ctx, claim, pdf)
}
