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

func TestExtractMetadataPrecedenceAndRelativeCanonical(t *testing.T) {
	body := strings.Repeat("A complete primary article sentence. ", 5)
	markup := `<html lang=" en-US "><head>
		<title>Document title</title>
		<meta name="title" content="Standard title">
		<meta name="twitter:title" content="Twitter title">
		<meta property="og:title" content=" OpenGraph   title ">
		<meta name="author" content="Standard Author">
		<meta property="article:author" content="Article Author">
		<meta name="application-name" content="Example Application">
		<meta property="og:site_name" content="Example Publication">
		<meta name="description" content="Standard description">
		<meta property="og:description" content="OpenGraph description">
		<meta itemprop="datePublished" content="2026-08-31T12:00:00Z">
		<meta property="article:published_time" content="2026-08-30T10:00:00Z">
		<meta property="og:locale" content="fr_FR">
		<link rel="alternate CANONICAL" href="../canonical/story?view=reader#fragment">
	</head><body><article><p>` + body + `</p></article></body></html>`
	candidate, err := Extract(Page{FinalURL: "https://example.com/news/2026/source", HTML: []byte(markup)})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Title != "OpenGraph title" || candidate.Author != "Article Author" || candidate.SiteName != "Example Publication" {
		t.Fatalf("metadata precedence = %+v", candidate)
	}
	if candidate.Description != "OpenGraph description" || candidate.PublicationDate != "2026-08-30T10:00:00Z" || candidate.Language != "en-US" {
		t.Fatalf("metadata values = %+v", candidate)
	}
	if candidate.CanonicalURL != "https://example.com/news/canonical/story?view=reader" {
		t.Fatalf("canonical URL = %q", candidate.CanonicalURL)
	}
}

func TestExtractCanonicalRejectsUnsafeValuesAndKeepsFinalURLFallback(t *testing.T) {
	body := strings.Repeat("A complete primary article sentence. ", 5)
	for _, unsafe := range []string{
		"javascript:alert(1)",
		"file:///etc/passwd",
		"https://user:password@example.net/private",
		"//user@example.net/private",
	} {
		t.Run(url.QueryEscape(unsafe), func(t *testing.T) {
			finalURL := "https://example.com/original?safe=1"
			markup := `<html><head><link rel="canonical" href="` + unsafe + `"></head><body><article><p>` + body + `</p></article></body></html>`
			candidate, err := Extract(Page{FinalURL: finalURL, HTML: []byte(markup)})
			if err != nil {
				t.Fatal(err)
			}
			if candidate.CanonicalURL != finalURL {
				t.Fatalf("unsafe canonical %q produced %q", unsafe, candidate.CanonicalURL)
			}
		})
	}
}

func TestExtractMetadataIsBoundedTrimmedAndControlFree(t *testing.T) {
	body := strings.Repeat("A complete primary article sentence. ", 5)
	longTitle := " \n\t" + strings.Repeat("T", maxMetadataTitleRunes+20) + "\x00 "
	longDescription := " start\n\t" + strings.Repeat("D", maxMetadataDescriptionRunes+20)
	markup := `<html><head><meta property="og:title" content="` + longTitle + `"><meta property="og:description" content="` + longDescription + `"><meta name="author" content="  Ada` + "\n\t" + `Lovelace  "><meta property="og:locale" content="` + strings.Repeat("e", maxMetadataLanguageRunes+20) + `"></head><body><article><p>` + body + `</p></article></body></html>`
	candidate, err := Extract(Page{FinalURL: "https://example.com/story", HTML: []byte(markup)})
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(candidate.Title)) != maxMetadataTitleRunes || len([]rune(candidate.Description)) != maxMetadataDescriptionRunes || len([]rune(candidate.Language)) != maxMetadataLanguageRunes {
		t.Fatalf("unbounded metadata lengths: title=%d description=%d language=%d", len([]rune(candidate.Title)), len([]rune(candidate.Description)), len([]rune(candidate.Language)))
	}
	if candidate.Author != "Ada Lovelace" || strings.ContainsAny(candidate.Title+candidate.Description+candidate.Language, "\x00\n\r\t") {
		t.Fatalf("metadata was not normalized: %+v", candidate)
	}
}

func TestExtractUsesFirstValidCanonical(t *testing.T) {
	body := strings.Repeat("A complete primary article sentence. ", 5)
	markup := `<html><head>
		<link rel="canonical" href="javascript:bad()">
		<link rel="canonical" href="/first">
		<link rel="canonical" href="https://attacker.example/second">
	</head><body><article><p>` + body + `</p></article></body></html>`
	candidate, err := Extract(Page{FinalURL: "https://example.com/original", HTML: []byte(markup)})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.CanonicalURL != "https://example.com/first" {
		t.Fatalf("canonical URL = %q, want first valid canonical", candidate.CanonicalURL)
	}
}

func TestExtractFallsBackToPrimaryHeadingAndHTTPContentLanguage(t *testing.T) {
	body := strings.Repeat("A complete primary article sentence. ", 5)
	markup := `<html><head><meta http-equiv="content-language" content="pt_BR"></head><body><article><h1>Primary heading</h1><p>` + body + `</p></article></body></html>`
	candidate, err := Extract(Page{FinalURL: "https://example.com/story", HTML: []byte(markup)})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Title != "Primary heading" || candidate.Language != "pt-BR" {
		t.Fatalf("fallback metadata = %+v", candidate)
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

func TestRendererRequestBudgetCountsMultiplexedBrowserRequests(t *testing.T) {
	budget := requestBudget{maximum: 2}
	if budget.exceeded() || budget.exceeded() {
		t.Fatal("budget rejected an allowed request")
	}
	if !budget.exceeded() || budget.count.Load() != 3 {
		t.Fatalf("budget count = %d", budget.count.Load())
	}
}

func TestExtractPlainTextCannotBeInflatedByActiveContent(t *testing.T) {
	active := strings.Repeat("hidden script words ", 100)
	_, err := Extract(Page{FinalURL: "https://example.com/article", HTML: []byte("<article><p>short</p><script>" + active + "</script></article>")})
	if err == nil {
		t.Fatal("active-only length passed extraction")
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

func TestExtractStructuredBylineBeforeHeaderCleanup(t *testing.T) {
	body := strings.Repeat("A complete primary article sentence. ", 5)
	for _, markup := range []string{
		`<script type="application/ld+json">{"@graph":[{"@type":"WebSite","author":{"name":"Wrong author"}},{"@type":"BlogPosting","author":[{"name":"Ada Lovelace"},{"name":"Grace Hopper"}],"publisher":{"name":"Example Journal"},"datePublished":"2026-08-23"}]}</script><article><h1>Title</h1><p>` + body + `</p></article>`,
		`<article><header><h1>Title</h1><a rel="author">Ada Lovelace</a><a rel="author">Grace Hopper</a><time itemprop="datePublished" datetime="2026-08-23">August 23</time></header><p>` + body + `</p></article>`,
	} {
		c, err := Extract(Page{FinalURL: "https://example.test/article", HTML: []byte(markup)})
		if err != nil {
			t.Fatal(err)
		}
		if c.Author != "Ada Lovelace, Grace Hopper" || c.PublicationDate != "2026-08-23" {
			t.Fatalf("lost byline: author=%q date=%q", c.Author, c.PublicationDate)
		}
	}
}
