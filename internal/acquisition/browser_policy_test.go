package acquisition

import (
	"context"
	"errors"
	"fmt"
	"github.com/chromedp/chromedp"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBrowserPolicyCountsRequestsInsideOneHTTPSConnection(t *testing.T) {
	p, err := newBrowserPolicy(proxyConfig{maxRequests: 8}, 5, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	var accepted atomic.Int64
	var requests sync.WaitGroup
	for range 20 {
		requests.Add(1)
		go func() {
			defer requests.Done()
			if p.allow("https://example.com/resource", false) {
				accepted.Add(1)
			}
		}()
	}
	requests.Wait()
	if accepted.Load() != 8 {
		t.Fatalf("allowed %d requests, want 8", accepted.Load())
	}
	if !errors.Is(p.exhausted(), ErrRequestLimit) {
		t.Fatalf("budget failure = %v", p.exhausted())
	}
	if got := p.diagnostics().Requests; got != 20 {
		t.Fatalf("target requests = %d, want 20 despite no new proxy connections", got)
	}
}

func TestBrowserPolicyStopsRedirectsAcrossNavigation(t *testing.T) {
	p, err := newBrowserPolicy(proxyConfig{maxRequests: 20}, 2, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for i, redirected := range []bool{false, true, true, false} {
		if !p.allow("https://example.com/article", redirected) {
			t.Fatalf("request %d blocked before redirect limit", i)
		}
	}
	if p.allow("https://example.com/third-redirect", true) {
		t.Fatal("third redirect allowed")
	}
	if !errors.Is(p.exhausted(), ErrTooManyRedirects) {
		t.Fatalf("budget failure = %v", p.exhausted())
	}
	if p.allow("https://example.com/another-document", false) {
		t.Fatal("navigation reset exhausted session budget")
	}
}

func TestBrowserPolicyUnsafeResourceDoesNotPoisonPublicRequests(t *testing.T) {
	p, err := newBrowserPolicy(proxyConfig{maxRequests: 4}, 2, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.allow("http://127.0.0.1/private", false) {
		t.Fatal("private request allowed")
	}
	if p.exhausted() != nil {
		t.Fatal("unsafe resource exhausted budget")
	}
	if !p.allow("https://example.com/image.png", false) {
		t.Fatal("unsafe resource blocked later public resource")
	}
	if !p.allow("https://example.com/other.png", false) {
		t.Fatal("public resource blocked")
	}
	if !p.allow("https://example.com/last.png", false) {
		t.Fatal("last permitted request blocked")
	}
	if p.allow("https://example.com/over-limit.png", false) {
		t.Fatal("request budget ignored after unsafe resource")
	}
	if !errors.Is(p.exhausted(), ErrRequestLimit) {
		t.Fatalf("unsafe resource masked request exhaustion: %v", p.exhausted())
	}
}

// The fixture proxy serves public URLs locally. No external site is contacted;
// the target policy must stop requests even though this proxy has no limits.
func TestBrowserPolicyChromiumEnforcement(t *testing.T) {
	if os.Getenv("ATTIC_CHROMIUM_INTEGRATION") != "1" {
		t.Skip("requires installed Chromium")
	}
	for _, tc := range []struct {
		name, path          string
		requests, redirects int64
		want                error
	}{
		{"requests", "/requests", 3, 20, ErrRequestLimit},
		{"redirects", "/redirect", 20, 2, ErrTooManyRedirects},
		{"private_document", "/private", 20, 2, ErrBlockedTarget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var privateRequests atomic.Int64
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Hostname() == "127.0.0.1" {
					privateRequests.Add(1)
				}
				switch r.URL.Path {
				case "/redirect":
					http.Redirect(w, r, "http://example.test/redirect", http.StatusFound)
				case "/private":
					http.Redirect(w, r, "http://127.0.0.1/private", http.StatusFound)
				default:
					w.Header().Set("Content-Type", "text/html")
					fmt.Fprint(w, `<html><body>Article<script>for(let i=0;i<10;i++)fetch('/resource?'+i).catch(()=>{});</script></body></html>`)
				}
			}))
			defer fixture.Close()
			p, err := newBrowserPolicy(proxyConfig{maxRequests: tc.requests}, tc.redirects, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
			opts = append(opts, p.options()...)
			opts = append(opts, chromedp.ExecPath("/usr/bin/chromium"), chromedp.ProxyServer(fixture.URL))
			parent, stop := context.WithTimeout(context.Background(), 15*time.Second)
			defer stop()
			alloc, closeAlloc := chromedp.NewExecAllocator(parent, opts...)
			defer closeAlloc()
			ctx, closeTab := chromedp.NewContext(alloc)
			defer closeTab()
			if err := p.install(ctx); err != nil {
				t.Fatal(err)
			}
			_ = chromedp.Run(ctx, chromedp.Navigate("http://example.test"+tc.path))
			deadline := time.Now().Add(3 * time.Second)
			for p.failure() == nil && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if !errors.Is(p.failure(), tc.want) {
				t.Fatalf("policy failure = %v, want %v", p.failure(), tc.want)
			}
			if p.diagnostics().BlockedRequests == 0 {
				t.Fatal("missing blocked-request diagnostics")
			}
			if privateRequests.Load() != 0 {
				t.Fatal("private document reached proxy")
			}
		})
	}
}
