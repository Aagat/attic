package acquisition

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestRenderingAndSnapshotsShareCancellableBrowserCapacity(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := NewChromiumRenderer(ChromiumConfig{Executable: executable})
	if err != nil {
		t.Fatal(err)
	}
	if cap(renderer.slots) != 1 {
		t.Fatal("default renderer permits concurrent Chromium processes")
	}
	// Simulate an occupied browser. Both public operations must stop waiting on
	// cancellation, before allocating a proxy, browser process, or profile.
	renderer.slots <- struct{}{}
	for name, call := range map[string]func(context.Context, string) (RenderedPage, error){"render": renderer.Render, "snapshot": renderer.Snapshot} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := call(ctx, "https://example.com"); done <- err }()
			select {
			case err := <-done:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("waiting operation returned %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("browser capacity wait ignored cancellation")
			}
			if len(renderer.slots) != 1 {
				t.Fatal("cancelled waiter consumed or released another operation's slot")
			}
		})
	}
	<-renderer.slots
	// Cancellation must win even when a slot is immediately available.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := renderer.Snapshot(ctx, "https://example.com"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(renderer.slots) != 0 {
		t.Fatal("cancelled caller leaked browser capacity")
	}
	configured, err := NewChromiumRenderer(ChromiumConfig{Executable: executable, Concurrency: 2})
	if err != nil || cap(configured.slots) != 2 {
		t.Fatalf("configured capacity: %v", err)
	}
}
