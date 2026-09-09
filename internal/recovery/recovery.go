// Package recovery owns bounded automated browsing and human takeover of the
// same persistent session. Neither HTTP clients nor capture workers own Chrome.
package recovery

import (
	"context"
	"encoding/base64"
	"errors"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"attic/internal/acquisition"
	"attic/internal/ai"
	"attic/internal/capture"
)

var ErrBusy = errors.New("Another capture browser is open. Close it before starting this one")
var ErrInput = errors.New("Invalid browser action")

type Browser interface {
	Frame(context.Context) (acquisition.BrowserFrame, error)
	Input(context.Context, string, float64, float64, string, float64) error
	Snapshot(context.Context) (acquisition.RenderedPage, error)
	Close()
}
type Navigator interface {
	BrowserStep(context.Context, ai.BrowserInput) (ai.BrowserAction, error)
}
type Session struct {
	Status     string `json:"status"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	Message    string `json:"message"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Screenshot string `json:"screenshot"`
}
type Action struct {
	Action string  `json:"action"`
	X      float64 `json:"x,omitempty"`
	Y      float64 `json:"y,omitempty"`
	Text   string  `json:"text,omitempty"`
	DeltaY float64 `json:"delta_y,omitempty"`
}
type active struct {
	raw        string
	state      Session
	cancel     context.CancelFunc
	stepCancel context.CancelFunc
	human      bool
	inputs     chan Action
	done       chan struct{}
	result     capture.Result
}
type Manager struct {
	ctx     context.Context
	cancel  context.CancelFunc
	open    func(context.Context, string) (Browser, error)
	model   Navigator
	save    func(context.Context, string, capture.Result) error
	mu      sync.Mutex
	current *active
	wg      sync.WaitGroup
}

func New(open func(context.Context, string) (Browser, error), model Navigator, save func(context.Context, string, capture.Result) error) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{ctx: ctx, cancel: cancel, open: open, model: model, save: save}
}
func (m *Manager) Close() { m.cancel(); m.wg.Wait() }
func idle() Session {
	return Session{Status: "idle", Width: acquisition.RecoveryWidth, Height: acquisition.RecoveryHeight}
}
func (m *Manager) State(raw string) Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil || m.current.raw != raw {
		return idle()
	}
	return m.current.state
}
func (m *Manager) Command(raw string, action Action) (Session, error) {
	if !validAction(action) {
		return Session{}, ErrInput
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.current
	if action.Action == "start" {
		a, err := m.startLocked(raw, true)
		if err != nil {
			return Session{}, err
		}
		return a.state, nil
	}
	if a == nil || a.raw != raw {
		return idle(), ErrInput
	}
	if action.Action == "close" {
		a.state.Status = "idle"
		a.state.Screenshot = ""
		a.state.Message = "Browser closed. Its session is retained."
		a.cancel()
		return a.state, nil
	}
	select {
	case <-a.done:
		return a.state, ErrInput
	default:
	}
	a.human = true
	if a.stepCancel != nil {
		a.stepCancel()
	}
	a.state.Status = "needs_input"
	a.state.Message = "You control this browser. Complete verification; Attic will save the readable page."
	if action.Action != "takeover" {
		select {
		case a.inputs <- action:
		default:
			return a.state, ErrBusy
		}
	}
	return a.state, nil
}
func (m *Manager) startLocked(raw string, retry bool) (*active, error) {
	a := m.current
	if a != nil {
		if a.raw == raw && (!retry || a.state.Status == "saved") {
			return a, nil
		}
		select {
		case <-a.done:
		default:
			if a.raw != raw {
				return nil, ErrBusy
			}
			return a, nil
		}
	}
	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Minute)
	a = &active{raw: raw, state: idle(), cancel: cancel, inputs: make(chan Action, 16), done: make(chan struct{})}
	a.state.Status = "starting"
	a.state.URL = raw
	a.state.Message = "Opening the capture browser…"
	m.current = a
	m.wg.Add(1)
	go func() { defer m.wg.Done(); m.run(ctx, a) }()
	return a, nil
}
func (m *Manager) begin(raw string) (*active, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startLocked(raw, false)
}

func validAction(a Action) bool {
	finite := func(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }
	if !finite(a.X) || !finite(a.Y) || !finite(a.DeltaY) {
		return false
	}
	switch a.Action {
	case "start", "takeover", "capture", "close":
		return a.Text == "" && a.X == 0 && a.Y == 0 && a.DeltaY == 0
	case "click":
		return a.X >= 0 && a.X < acquisition.RecoveryWidth && a.Y >= 0 && a.Y < acquisition.RecoveryHeight && a.Text == "" && a.DeltaY == 0
	case "scroll":
		return a.DeltaY != 0 && math.Abs(a.DeltaY) <= 768 && a.X == 0 && a.Y == 0 && a.Text == ""
	case "key":
		return (a.Text == "Enter" || a.Text == "Tab" || a.Text == "Backspace" || a.Text == "Escape") && a.X == 0 && a.Y == 0 && a.DeltaY == 0
	case "type":
		if !utf8.ValidString(a.Text) || a.Text == "" || utf8.RuneCountInString(a.Text) > 500 || a.X != 0 || a.Y != 0 || a.DeltaY != 0 {
			return false
		}
		for _, r := range a.Text {
			if unicode.IsControl(r) {
				return false
			}
		}
		return true
	}
	return false
}
func (m *Manager) update(a *active, f func(*Session)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.state.Status != "idle" {
		f(&a.state)
	}
}
func (m *Manager) human(a *active) bool { m.mu.Lock(); defer m.mu.Unlock(); return a.human }
func (m *Manager) handoff(a *active, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a.human = true
	if a.state.Status != "idle" {
		a.state.Status = "needs_input"
		a.state.Message = message
	}
}
func (m *Manager) run(ctx context.Context, a *active) {
	defer close(a.done)
	defer func() {
		if ctx.Err() != nil {
			m.update(a, func(s *Session) {
				s.Status = "failed"
				s.Screenshot = ""
				s.Message = "Capture browser expired. Start again to reuse its stored session."
			})
		}
	}()
	b, err := m.open(ctx, a.raw)
	if err != nil {
		m.update(a, func(s *Session) { s.Status = "failed"; s.Message = "The capture browser could not start. Try again." })
		return
	}
	defer b.Close()
	automated, stop := context.WithTimeout(ctx, 90*time.Second)
	defer stop()
	for step := 0; step < 4 && !m.human(a) && automated.Err() == nil; step++ {
		frame, err := m.observe(ctx, a, b)
		if err != nil {
			break
		}
		m.update(a, func(s *Session) { s.Status = "working"; s.Message = "Trying to recover the page…" })
		if m.model == nil {
			break
		}
		decisionCtx, decisionCancel := context.WithCancel(automated)
		m.mu.Lock()
		a.stepCancel = decisionCancel
		human := a.human
		m.mu.Unlock()
		if human {
			decisionCancel()
			break
		}
		action, err := m.model.BrowserStep(decisionCtx, ai.BrowserInput{URL: frame.URL, Text: frame.Text, ScreenshotDataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(frame.Screenshot), Step: step})
		defer decisionCancel()
		if m.human(a) {
			break
		}
		if err != nil {
			break
		}
		if action.Action == "done" {
			if m.capture(ctx, a, b, false) {
				return
			}
			break
		}
		if action.Action == "handoff" {
			break
		}
		if action.Action != "wait" && !validAction(Action{Action: action.Action, X: action.X, Y: action.Y, Text: action.Text, DeltaY: action.DeltaY}) {
			break
		}
		if err = b.Input(decisionCtx, action.Action, action.X, action.Y, action.Text, action.DeltaY); err != nil {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(1500 * time.Millisecond):
		}
	}
	m.handoff(a, "Verification needs your help. Take control here; the readable page will be saved automatically.")
	_, _ = m.observe(ctx, a, b)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	// A changing page must be stable across two observations before auto-saving.
	var previous string
	for {
		select {
		case <-ctx.Done():
			return
		case command := <-a.inputs:
			if command.Action == "capture" {
				if m.capture(ctx, a, b, true) {
					return
				}
			} else if err = b.Input(ctx, command.Action, command.X, command.Y, command.Text, command.DeltaY); err != nil {
				m.update(a, func(s *Session) { s.Message = "That browser action failed. Try again." })
			}
			_, _ = m.observe(ctx, a, b)
			previous = ""
		case <-ticker.C:
			frame, err := m.observe(ctx, a, b)
			if err != nil {
				continue
			}
			if readable(frame.Text) && frame.Text == previous {
				if m.capture(ctx, a, b, false) {
					return
				}
			}
			previous = frame.Text
		}
	}
}
func (m *Manager) observe(ctx context.Context, a *active, b Browser) (acquisition.BrowserFrame, error) {
	f, err := b.Frame(ctx)
	if err == nil {
		m.update(a, func(s *Session) {
			s.URL = f.URL
			s.Title = f.Title
			s.Screenshot = "data:image/png;base64," + base64.StdEncoding.EncodeToString(f.Screenshot)
		})
	}
	return f, err
}
func clearPage(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if len(lower) == 0 {
		return false
	}
	// Short interstitials are different from articles discussing challenges or
	// ordinary navigation labels at the foot of a long page.
	if len(lower) < 2000 {
		for _, marker := range []string{"verify you are human", "verify that you are human", "just a moment", "sign in to x", "log in to x", "something went wrong", "enable javascript and cookies", "access denied", "too many requests", "subscribe to continue", "complete the captcha", "solve the captcha"} {
			if strings.Contains(lower, marker) {
				return false
			}
		}
		if len(lower) < 600 && (strings.Contains(lower, "captcha") || strings.Contains(lower, "show more")) {
			return false
		}
	}
	return true
}
func readable(text string) bool { return len(strings.TrimSpace(text)) >= 300 && clearPage(text) }

func (m *Manager) capture(ctx context.Context, a *active, b Browser, explicit bool) bool {
	frame, err := b.Frame(ctx)
	if err != nil || !clearPage(frame.Text) || (!explicit && !readable(frame.Text)) {
		m.update(a, func(s *Session) {
			s.Message = "This is still a blocked or incomplete page. Complete verification before saving."
		})
		return false
	}
	page, err := b.Snapshot(ctx)
	if err != nil || page.FinalURL != frame.URL {
		return false
	}
	if !explicit && !samePage(a.raw, page.FinalURL) {
		m.update(a, func(s *Session) {
			s.Message = "The browser opened a different address. Check the page, then choose Save page to preserve it."
		})
		return false
	}
	result, err := capture.FromPage(page)
	if err != nil || result.Status == "blocked" || strings.TrimSpace(result.PlainText) == "" {
		m.update(a, func(s *Session) { s.Message = "This is still a blocked page. Complete verification before saving." })
		return false
	}
	if len(strings.TrimSpace(result.PlainText)) < min(300, len(strings.TrimSpace(frame.Text))/4) {
		m.update(a, func(s *Session) {
			s.Message = "The browser copy lost visible article text. Try again or save with the extension."
		})
		return false
	}
	result.OriginalURL = a.raw
	if m.save != nil {
		if err = m.save(ctx, a.raw, result); err != nil {
			m.update(a, func(s *Session) { s.Message = "The page opened, but saving failed. Try Save page again." })
			return false
		}
	}
	m.mu.Lock()
	a.result = result
	m.mu.Unlock()
	m.update(a, func(s *Session) {
		s.Status = "saved"
		s.Message = "Page preserved. Any failed document request will resume."
		s.Screenshot = ""
	})
	return true
}

// Capture tries ordinary/API/archive capture first, then shares the same browser
// recovery session with mobile takeover. Its caller remains the durable worker.
type Capturer struct {
	Base interface {
		Capture(context.Context, string) (capture.Result, error)
	}
	Browser *Manager
}

func (c Capturer) Capture(ctx context.Context, raw string) (capture.Result, error) {
	result, err := c.Base.Capture(ctx, raw)
	needs := err != nil || result.Status == "blocked"
	for _, missing := range result.MissingResources {
		needs = needs || strings.Contains(missing, "truncated")
	}
	if !needs || ctx.Err() != nil {
		return result, err
	}
	a, e := c.Browser.begin(raw)
	if e != nil {
		return result, err
	}
	timer := time.NewTimer(100 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-timer.C:
			return result, err
		case <-a.done:
			c.Browser.mu.Lock()
			saved := a.result
			c.Browser.mu.Unlock()
			if len(saved.HTML) > 0 {
				return saved, nil
			}
			return result, err
		case <-ticker.C:
			if c.Browser.State(raw).Status == "needs_input" {
				return result, err
			}
		}
	}
}

// Automatic capture must stay on the requested document. Human Save page
// explicitly accepts the currently displayed address, including archive redirects.
func samePage(original, current string) bool {
	a, e := url.Parse(original)
	b, f := url.Parse(current)
	if e != nil || f != nil {
		return false
	}
	xhost := func(host string) bool {
		return host == "x.com" || host == "www.x.com" || host == "twitter.com" || host == "www.twitter.com" || host == "mobile.twitter.com"
	}
	if xhost(a.Hostname()) && xhost(b.Hostname()) {
		post := func(u *url.URL) string {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			for i, p := range parts {
				if p == "status" && i+1 < len(parts) {
					return parts[i+1]
				}
			}
			return ""
		}
		return post(a) != "" && post(a) == post(b)
	}
	return strings.EqualFold(a.Host, b.Host) && strings.TrimSuffix(a.EscapedPath(), "/") == strings.TrimSuffix(b.EscapedPath(), "/") && a.RawQuery == b.RawQuery
}
