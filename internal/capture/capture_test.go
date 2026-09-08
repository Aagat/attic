package capture

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"attic/internal/acquisition"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

type fakeRenderer struct {
	pages map[string]acquisition.RenderedPage
	calls []string
}

func (f *fakeRenderer) Snapshot(ctx context.Context, url string) (acquisition.RenderedPage, error) {
	f.calls = append(f.calls, url)
	p, ok := f.pages[url]
	if !ok {
		return p, fmt.Errorf("unavailable")
	}
	return p, nil
}

type fakeArchives []string

func (f fakeArchives) Candidates(context.Context, string) []string { return f }
func fixture() acquisition.RenderedPage {
	var archive strings.Builder
	archive.WriteString("MIME-Version: 1.0\r\nContent-Type: multipart/related; boundary=attic\r\n\r\n")
	parts := []struct{ location, kind, body string }{
		{"https://source.invalid/story", "text/html", `<html><head><title>Saved fixture</title><link rel="stylesheet" href="/style.css"><meta http-equiv="refresh" content="0;url=https://source.invalid/escape"></head><body onload="window.attacked=true"><article><h1>Saved fixture</h1><p>Needle that only exists in archived content.</p><img id="photo" src="/photo.png" srcset="https://source.invalid/large.png 2x"><table><tr><td>Table value</td></tr></table><pre><code>one &lt; two</code></pre><script>window.attacked=true</script><a href="javascript:alert(1)" ping="https://source.invalid/ping">bad</a><a href="/next">next</a></article></body></html>`},
		{"https://source.invalid/style.css", "text/css", `@import "/extra.css"; article {color: rgb(12, 34, 56); background-image:url('/photo.png')}`},
		{"https://source.invalid/extra.css", "text/css", `pre {white-space: pre-wrap}`},
		{"https://source.invalid/photo.png", "image/png", mustDecode("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aBl8AAAAASUVORK5CYII=")},
	}
	for _, p := range parts {
		fmt.Fprintf(&archive, "--attic\r\nContent-Type: %s\r\nContent-Location: %s\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n", p.kind, p.location, base64.StdEncoding.EncodeToString([]byte(p.body)))
	}
	archive.WriteString("--attic--\r\n")
	return acquisition.RenderedPage{FinalURL: "https://source.invalid/story", Title: "Saved fixture", Status: 200, MHTML: []byte(archive.String())}
}
func mustDecode(s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
func TestCapturePreservesAssetsAndRemovesActiveContent(t *testing.T) {
	page := fixture()
	renderer := &fakeRenderer{pages: map[string]acquisition.RenderedPage{page.FinalURL: page}}
	result, err := New(renderer, nil).Capture(context.Background(), page.FinalURL)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "complete" {
		t.Fatalf("%+v", result)
	}
	body := string(result.HTML)
	for _, wanted := range []string{"data:image/png;base64,", "rgb(12, 34, 56)", "white-space: pre-wrap", "Table value", "one &lt; two", "Content-Security-Policy", "https://source.invalid/next"} {
		if !strings.Contains(body, wanted) {
			t.Errorf("missing %q", wanted)
		}
	}
	for _, bad := range []string{"<script", "onload=", "srcset=", "javascript:", "http-equiv=\"refresh\"", "ping=", "@import"} {
		if strings.Contains(body, bad) {
			t.Errorf("unsafe %q remains", bad)
		}
	}
	if !strings.Contains(result.PlainText, "Needle that only exists") || strings.Contains(result.PlainText, "rgb(") {
		t.Fatal(result.PlainText)
	}
}
func TestCaptureRecoversBlockedOriginalAndReportsMissingAssets(t *testing.T) {
	original := "https://source.invalid/blocked"
	archived := "https://archive.invalid/snapshot"
	renderer := &fakeRenderer{pages: map[string]acquisition.RenderedPage{
		original: {FinalURL: original, Status: 403, DOM: []byte(`<h1>Access denied</h1>`)},
		archived: {FinalURL: archived, Status: 200, DOM: []byte(`<h1>Recovered</h1><p>Preserved prose</p><img src="/missing.png">`)},
	}}
	result, err := New(renderer, fakeArchives{archived}).Capture(context.Background(), original)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "partial" || result.OriginalURL != original || result.FinalURL != archived || len(result.Attempts) != 2 || len(result.MissingResources) != 1 {
		t.Fatalf("%+v", result)
	}
}
func TestCaptureOfflineReplayBrowser(t *testing.T) {
	if os.Getenv("ATTIC_WEB_INTEGRATION") != "1" {
		t.Skip("set ATTIC_WEB_INTEGRATION=1 to verify replay with Chromium")
	}
	page := fixture()
	result, err := New(&fakeRenderer{pages: map[string]acquisition.RenderedPage{page.FinalURL: page}}, nil).Capture(context.Background(), page.FinalURL)
	if err != nil {
		t.Fatal(err)
	}
	var extraRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			extraRequests.Add(1)
			http.Error(w, "unexpected request", 404)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", ReplayCSP)
		w.Write(result.HTML)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	allocator, stop := chromedp.NewExecAllocator(ctx, append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath("/usr/bin/chromium"), chromedp.NoSandbox)...)
	defer stop()
	browser, closeBrowser := chromedp.NewContext(allocator)
	defer closeBrowser()
	chromedp.ListenTarget(browser, func(event any) {
		if e, ok := event.(*network.EventRequestWillBeSent); ok && e.Request.URL != server.URL+"/" && !strings.HasPrefix(e.Request.URL, "data:") {
			extraRequests.Add(1)
		}
	})
	var state struct {
		Image    bool
		Color    string
		Attacked bool
		Text     string
	}
	err = chromedp.Run(browser, chromedp.Navigate(server.URL), chromedp.Sleep(250*time.Millisecond), chromedp.Evaluate(`({Image:document.querySelector('#photo').naturalWidth===1,Color:getComputedStyle(document.querySelector('article')).color,Attacked:!!window.attacked,Text:document.body.innerText})`, &state))
	if err != nil {
		t.Fatal(err)
	}
	if !state.Image || state.Color != "rgb(12, 34, 56)" || state.Attacked || !strings.Contains(state.Text, "Table value") || extraRequests.Load() != 0 {
		t.Fatalf("replay: %+v, extra requests: %d", state, extraRequests.Load())
	}
}

func TestCapturePublicPageIntegration(t *testing.T) {
	if os.Getenv("ATTIC_CHROMIUM_INTEGRATION") != "1" {
		t.Skip("set ATTIC_CHROMIUM_INTEGRATION=1 for protected Chromium capture")
	}
	renderer, err := acquisition.NewChromiumRenderer(acquisition.ChromiumConfig{RenderTimeout: 25 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	result, err := New(renderer, nil).Capture(context.Background(), "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status == "blocked" || !strings.Contains(result.PlainText, "Example Domain") || !strings.Contains(string(result.HTML), "Content-Security-Policy") {
		t.Fatalf("capture status=%s, text=%s", result.Status, result.PlainText)
	}
}
