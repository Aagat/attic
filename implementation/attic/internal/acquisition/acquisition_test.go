package acquisition

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestValidateURLRejectsPrivateAndNonHTTP(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1/x", "http://10.0.0.1", "http://100.64.0.1", "http://192.0.2.1", "http://198.18.0.1", "http://[::1]", "http://[2001:db8::1]", "file:///etc/passwd", "https://user:pass@example.com"} {
		u, _ := url.Parse(raw)
		if err := ValidateURL(u); err == nil {
			t.Errorf("ValidateURL(%q) accepted unsafe target", raw)
		}
	}
}

func testFetcher(t *testing.T, handler http.Handler, cfg FetchConfig) *Fetcher {
	t.Helper()
	cfg.Resolve = func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("93.184.216.34")}, nil }
	return newFetcher(cfg, func(ctx context.Context, network, _ string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			req, err := http.ReadRequest(bufio.NewReader(server))
			if err != nil {
				return
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			_ = recorder.Result().Write(server)
		}()
		return client, nil
	})
}

func TestFetcherEnforcesResponseTypeAndSize(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		want                    error
	}{
		{name: "html", contentType: "text/html; charset=utf-8", body: "valid", want: nil},
		{name: "type", contentType: "application/json", body: `{}`, want: errors.New("unsupported content type")},
		{name: "size", contentType: "text/html", body: "12345", want: ErrResponseTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			maxBytes := int64(32)
			if tc.name == "size" {
				maxBytes = 4
			}
			f := testFetcher(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				_, _ = io.WriteString(w, tc.body)
			}), FetchConfig{MaxBytes: maxBytes})
			page, err := f.Fetch(context.Background(), "http://article.example/story")
			if tc.want == nil && (err != nil || string(page.HTML) != tc.body) {
				t.Fatalf("Fetch() = (%q, %v)", page.HTML, err)
			}
			if tc.want != nil && (err == nil || !errors.Is(err, tc.want) && !strings.Contains(err.Error(), tc.want.Error())) {
				t.Fatalf("Fetch() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestFetcherChecksRedirectsAndCapsCount(t *testing.T) {
	f := testFetcher(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/private":
			http.Redirect(w, req, "http://127.0.0.1/secret", http.StatusFound)
		default:
			http.Redirect(w, req, "/again", http.StatusFound)
		}
	}), FetchConfig{MaxRedirects: 2})
	if _, err := f.Fetch(context.Background(), "http://article.example/private"); !errors.Is(err, ErrBlockedTarget) {
		t.Fatalf("private redirect error = %v", err)
	}
	if _, err := f.Fetch(context.Background(), "http://article.example/loop"); err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("redirect cap error = %v", err)
	}
}

func TestFetcherRejectsMixedDNSAnswersWithoutDialing(t *testing.T) {
	var dialed bool
	f := newFetcher(FetchConfig{Resolve: func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("127.0.0.1")}, nil
	}}, func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, errors.New("unexpected")
	})
	if _, err := f.Fetch(context.Background(), "http://article.example/"); !errors.Is(err, ErrBlockedTarget) {
		t.Fatalf("Fetch() error = %v", err)
	}
	if dialed {
		t.Fatal("dial attempted for prohibited DNS answer")
	}
}

func TestFetcherIsConcurrentSafe(t *testing.T) {
	f := testFetcher(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "ok")
	}), FetchConfig{})
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.Fetch(context.Background(), "http://article.example/"); err != nil {
				t.Errorf("Fetch: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestExtractChoosesArticleAndSanitizes(t *testing.T) {
	body := strings.Repeat("A long article sentence with useful information. ", 5)
	p := Page{FinalURL: "https://example.com/a", HTML: []byte("<html><head><title>Story</title><script>alert(1)</script></head><body><nav>Noise</nav><article><h1>Heading</h1><p>" + body + "</p><img src=\"javascript:alert(1)\"></article></body></html>")}
	c, err := Extract(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Title != "Story" || !strings.Contains(c.SemanticHTML, "Heading") || strings.Contains(c.SemanticHTML, "script") || strings.Contains(c.SemanticHTML, "javascript:") {
		t.Fatalf("unsafe or incomplete candidate: %+v", c)
	}
}

func TestExtractRejectsTinyPages(t *testing.T) {
	if _, err := Extract(Page{FinalURL: "https://example.com", HTML: []byte("<p>short</p>")}); err == nil {
		t.Fatal("expected insufficient content")
	}
}

func TestPolicyProxyRejectsLoopbackRequest(t *testing.T) {
	p := &policyProxy{config: proxyConfig{maxRequests: 5}}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/private", nil)
	recorder := httptest.NewRecorder()
	p.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden || p.diagnostics().BlockedRequests != 1 {
		t.Fatalf("status=%d diagnostics=%+v", recorder.Code, p.diagnostics())
	}
}

func TestProxyBudgetWriterIsBounded(t *testing.T) {
	p := &policyProxy{config: proxyConfig{maxBytes: 3}}
	var out strings.Builder
	n, err := (&budgetWriter{proxy: p, writer: &out}).Write([]byte("abcdef"))
	if n != 3 || !errors.Is(err, ErrResponseTooLarge) || out.String() != "abc" {
		t.Fatalf("Write() = %d, %v, %q", n, err, out.String())
	}
}

func TestChromiumRendererIntegration(t *testing.T) {
	if os.Getenv("ATTIC_CHROMIUM_INTEGRATION") != "1" {
		t.Skip("set ATTIC_CHROMIUM_INTEGRATION=1 to use installed Chromium and external network")
	}
	r, err := NewChromiumRenderer(ChromiumConfig{Executable: "/usr/bin/chromium", RenderTimeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	page, err := r.Render(context.Background(), "https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	if page.FinalURL == "" || len(page.DOM) == 0 || len(page.Screenshot) == 0 {
		t.Fatalf("incomplete render: %+v", page)
	}
}
