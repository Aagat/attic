package acquisition

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestChromiumSavedPageMakesNoNetworkRequests(t *testing.T) {
	if os.Getenv("ATTIC_CHROMIUM_INTEGRATION") != "1" {
		t.Skip("set ATTIC_CHROMIUM_INTEGRATION=1 to use installed Chromium")
	}
	renderer, err := NewChromiumRenderer(ChromiumConfig{Executable: "/usr/bin/chromium", RenderTimeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	renderer.resolve = func(context.Context, string) ([]net.IP, error) {
		requests.Add(1)
		return nil, errors.New("network unavailable")
	}
	body := `<html><head><title>Saved article</title><link rel="stylesheet" href="https://missing.invalid/style.css"></head><body><article><h1>Saved article</h1><p>` + strings.Repeat("An archived article remains readable without its website. ", 15) + `</p><img src="https://missing.invalid/image.png"><script>document.title='Script executed'</script></article></body></html>`
	page, err := renderer.RenderSaved(context.Background(), "https://gone.invalid/article", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "Saved article" || len(page.Screenshot) == 0 || !strings.Contains(string(page.DOM), "archived article") {
		t.Fatalf("invalid saved render: title=%q screenshot=%d DOM=%d", page.Title, len(page.Screenshot), len(page.DOM))
	}
	if requests.Load() != 0 {
		t.Fatalf("saved render made %d network requests", requests.Load())
	}
	if page.FinalURL != "https://gone.invalid/article" {
		t.Fatal("lost saved source URL")
	}
}
