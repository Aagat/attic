package capture

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"attic/internal/acquisition"
	"github.com/chromedp/chromedp"
)

func readingFixture() []byte {
	return []byte(`<html><head><title>Useful article</title><style>body{background:black}</style></head><body><nav>Unwanted navigation</nav><article><h1>Useful article</h1><p>A useful article with enough content to qualify for deterministic extraction. Its tables, code and embedded diagrams should remain available without contacting the original website.</p><figure><img alt="GIF diagram" src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"><figcaption>A small diagram</figcaption></figure><svg xmlns="http://www.w3.org/2000/svg" width="2" height="2" viewBox="0 0 2 2"><rect width="2" height="2" fill="red"></rect></svg><table><tr><th>Metric</th><th>Value</th></tr><tr><td>Useful value</td><td>42</td></tr></table><pre><code><span>if</span> (a &lt; b) {
  return 42;
}</code></pre><script>window.attacked=true</script></article><footer>Unwanted footer</footer></body></html>`)
}
func TestReadingViewPreservesRichArchivedContent(t *testing.T) {
	input := readingFixture()
	original := bytes.Clone(input)
	output, err := ReadingView(input, "https://source.invalid/article")
	if err != nil {
		t.Fatal(err)
	}
	body := string(output)
	for _, wanted := range []string{"data:image/gif;base64,", "data:image/svg+xml;base64,", "A small diagram", "<table>", "Useful value", "if (a &lt; b) {\n  return 42;", "Content-Security-Policy"} {
		if !strings.Contains(body, wanted) {
			t.Errorf("missing %q", wanted)
		}
	}
	for _, bad := range []string{"Unwanted navigation", "Unwanted footer", "background:black", "<script", "attic-reading-image-"} {
		if strings.Contains(body, bad) {
			t.Errorf("unexpected %q", bad)
		}
	}
	if strings.Count(body, "<h1>") != 1 {
		t.Fatalf("repeated title: %s", body)
	}
	if !bytes.Equal(input, original) {
		t.Fatal("reader changed saved capture")
	}
	if _, err := ReadingView([]byte(`<h1>Short bookmark</h1>`), "https://source.invalid"); err == nil {
		t.Fatal("short non-article was reported as a reading view")
	}
}
func TestEmptyAssetReferencesDoNotReportThePageAsMissing(t *testing.T) {
	result, err := convert(acquisition.RenderedPage{Status: 200, FinalURL: "https://source.invalid/article", DOM: []byte(`<html><body style="background:url('')"><p>Saved text</p><img src=""><style>@font-face{font-family:missing;src:url('/missing.woff2')}</style></body></html>`)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "partial" || len(result.MissingResources) != 1 || result.MissingResources[0] != "https://source.invalid/missing.woff2" {
		t.Fatalf("missing resources: %+v", result.MissingResources)
	}
}
func TestReadingViewOfflineBrowser(t *testing.T) {
	if os.Getenv("ATTIC_WEB_INTEGRATION") != "1" {
		t.Skip("set ATTIC_WEB_INTEGRATION=1 to verify reading view with Chromium")
	}
	output, err := ReadingView(readingFixture(), "https://source.invalid/article")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", ReplayCSP)
		w.Write(output)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	allocator, stop := chromedp.NewExecAllocator(ctx, append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath("/usr/bin/chromium"), chromedp.NoSandbox)...)
	defer stop()
	browser, closeBrowser := chromedp.NewContext(allocator)
	defer closeBrowser()
	var state struct {
		Images   bool
		Attacked bool
		Code     string
	}
	err = chromedp.Run(browser, chromedp.Navigate(server.URL), chromedp.Evaluate(`({Images:document.images.length===2&&[...document.images].every(img=>img.naturalWidth>0),Attacked:!!window.attacked,Code:document.querySelector('pre').textContent})`, &state))
	if err != nil {
		t.Fatal(err)
	}
	if !state.Images || state.Attacked || !strings.Contains(state.Code, "  return 42;") {
		t.Fatalf("reader: %+v", state)
	}
}
