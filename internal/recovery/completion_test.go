package recovery

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"attic/internal/ai"
	"attic/internal/capture"
)

type completionModel struct{ release chan struct{} }

func (m completionModel) BrowserStep(ctx context.Context, _ ai.BrowserInput) (ai.BrowserAction, error) {
	select {
	case <-ctx.Done():
		return ai.BrowserAction{}, ctx.Err()
	case <-m.release:
		return ai.BrowserAction{Action: "done"}, nil
	}
}

func TestRecoverSharesCompletionAndCallerCancellation(t *testing.T) {
	raw := "https://example.com/article"
	b := &fakeBrowser{text: strings.Repeat("A neutral preserved article about libraries. ", 12)}
	var opens, saves atomic.Int32
	model := completionModel{release: make(chan struct{})}
	m := New(func(context.Context, string) (Browser, error) { opens.Add(1); return b, nil }, model, func(context.Context, string, capture.Result) error { saves.Add(1); return nil })
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := m.Recover(ctx, raw); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting deadline: %v", err)
	}
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		t.Fatal("caller deadline closed shared browser")
	}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			result, err := m.Recover(ctx, raw)
			if err == nil && !strings.Contains(result.PlainText, "neutral preserved article") {
				err = errors.New("missing shared result")
			}
			results <- err
		}()
	}
	close(model.release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if opens.Load() != 1 || saves.Load() != 1 {
		t.Fatalf("opens=%d saves=%d", opens.Load(), saves.Load())
	}
}

func TestRecoverHandoffRetainsBrowser(t *testing.T) {
	raw := "https://example.com/article"
	b := &fakeBrowser{text: "Please verify you are human"}
	m := New(func(context.Context, string) (Browser, error) { return b, nil }, nil, nil)
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := m.Recover(ctx, raw); !errors.Is(err, ErrNeedsInput) {
		t.Fatalf("handoff: %v", err)
	}
	if m.State(raw).Status != "needs_input" {
		t.Fatal("handoff session lost")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		t.Fatal("handoff closed browser")
	}
}

func TestRecoverReportsStartupFailure(t *testing.T) {
	m := New(func(context.Context, string) (Browser, error) { return nil, errors.New("launch failed") }, nil, nil)
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := m.Recover(ctx, "https://example.com/article"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("startup failure: %v", err)
	}
}

type partialCapturer struct{ result capture.Result }

func (c partialCapturer) Capture(context.Context, string) (capture.Result, error) {
	return c.result, nil
}

func TestCaptureRetainsPartialWhenRecoveryUnavailable(t *testing.T) {
	m := New(func(context.Context, string) (Browser, error) { return nil, errors.New("launch failed") }, nil, nil)
	defer m.Close()
	partial := capture.Result{HTML: []byte("<p>Partial text</p>"), PlainText: "Partial text", Status: "partial", MissingResources: []string{"post truncated"}}
	result, err := (Capturer{Base: partialCapturer{partial}, Browser: m}).Capture(context.Background(), "https://example.com/article")
	if err != nil || result.PlainText != partial.PlainText || result.Status != "partial" {
		t.Fatalf("partial fallback lost: %+v, %v", result, err)
	}
}
