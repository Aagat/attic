package recovery

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"attic/internal/acquisition"
	"attic/internal/ai"
	"attic/internal/capture"
)

type fakeBrowser struct {
	mu     sync.Mutex
	text   string
	inputs int
	closed bool
}

func (b *fakeBrowser) Frame(context.Context) (acquisition.BrowserFrame, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return acquisition.BrowserFrame{URL: "https://example.com/article", Title: "Article", Text: b.text, Screenshot: []byte("png")}, nil
}
func (b *fakeBrowser) Input(_ context.Context, a string, _, _ float64, text string, _ float64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.inputs++
	if a == "type" {
		b.text = text
	}
	return nil
}
func (b *fakeBrowser) Snapshot(context.Context) (acquisition.RenderedPage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return acquisition.RenderedPage{FinalURL: "https://example.com/article", Title: "Article", Status: 200, DOM: []byte("<html><title>Article</title><article>" + b.text + "</article></html>")}, nil
}
func (b *fakeBrowser) Close() { b.mu.Lock(); defer b.mu.Unlock(); b.closed = true }

type blockedModel struct {
	entered  chan struct{}
	canceled chan struct{}
}

func (m *blockedModel) BrowserStep(ctx context.Context, _ ai.BrowserInput) (ai.BrowserAction, error) {
	close(m.entered)
	<-ctx.Done()
	close(m.canceled)
	return ai.BrowserAction{Action: "click", X: 1, Y: 1}, nil
}
func await(t *testing.T, f func() bool) {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for !f() {
		if time.Now().After(until) {
			t.Fatal("condition did not become true")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
func TestTakeoverCancelsAIAndPreservesHumanCapture(t *testing.T) {
	b := &fakeBrowser{text: "Please verify you are human"}
	model := &blockedModel{entered: make(chan struct{}), canceled: make(chan struct{})}
	saved := make(chan capture.Result, 1)
	m := New(func(context.Context, string) (Browser, error) { return b, nil }, model, func(_ context.Context, raw string, r capture.Result) error {
		if raw != "https://example.com/article" {
			t.Error("source changed")
		}
		saved <- r
		return nil
	})
	defer m.Close()
	raw := "https://example.com/article"
	if _, err := m.Command(raw, Action{Action: "start"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-model.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("model did not start")
	}
	if _, err := m.Command("https://other.example/article", Action{Action: "start"}); err != ErrBusy {
		t.Fatal("parallel browser was allowed")
	}
	if _, err := m.Command(raw, Action{Action: "takeover"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-model.canceled:
	case <-time.After(time.Second):
		t.Fatal("takeover did not cancel AI")
	}
	if _, err := m.Command(raw, Action{Action: "type", Text: strings.Repeat("A neutral preserved article about libraries. ", 8)}); err != nil {
		t.Fatal(err)
	}
	await(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.inputs == 1 })
	if _, err := m.Command(raw, Action{Action: "capture"}); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-saved:
		if r.Status == "blocked" || r.OriginalURL != raw {
			t.Fatal("bad preserved result")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("capture not saved")
	}
	await(t, func() bool { return m.State(raw).Status == "saved" })
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.inputs != 1 {
		t.Fatal("AI acted after takeover")
	}
}
func TestClosingSessionNeverSavesChallenge(t *testing.T) {
	b := &fakeBrowser{text: "Please verify you are human"}
	m := New(func(context.Context, string) (Browser, error) { return b, nil }, nil, func(context.Context, string, capture.Result) error { t.Error("challenge was saved"); return nil })
	defer m.Close()
	raw := "https://example.com/article"
	m.Command(raw, Action{Action: "start"})
	await(t, func() bool { return m.State(raw).Status == "needs_input" })
	m.Command(raw, Action{Action: "capture"})
	await(t, func() bool { return strings.Contains(m.State(raw).Message, "still a blocked") })
	m.Command(raw, Action{Action: "close"})
	await(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.closed })
	if m.State(raw).Status != "idle" {
		t.Fatal("close did not stop session")
	}
}
func TestRecoveryRejectsArbitraryActions(t *testing.T) {
	for _, a := range []Action{{Action: "navigate", Text: "file:///etc/passwd"}, {Action: "type", Text: "secret\n"}, {Action: "key", Text: "Ctrl+L"}, {Action: "click", X: 1024}, {Action: "scroll", DeltaY: 9000}, {Action: "start", Text: "ignored"}} {
		if validAction(a) {
			t.Errorf("accepted %+v", a)
		}
	}
}

func TestAutomaticCaptureStaysOnRequestedDocument(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"https://x.com/author/status/123?s=46", "https://x.com/Author/status/123", true},
		{"https://x.com/author/status/123", "https://x.com/author/status/456", false},
		{"https://archive.is/abc", "https://archive.is/privacy", false},
		{"https://example.com/article#intro", "https://example.com/article", true},
		{"https://example.com/article", "https://other.example/article", false},
	} {
		if samePage(tc.a, tc.b) != tc.want {
			t.Errorf("identity check %s -> %s", tc.a, tc.b)
		}
	}
	for _, text := range []string{"Too many requests", "Something went wrong. Try reloading.", "Sign in to X", "Please complete the captcha"} {
		if clearPage(text) {
			t.Errorf("challenge accepted: %s", text)
		}
	}
}
