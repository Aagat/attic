package capture

import (
	"context"
	"strings"
	"testing"

	"attic/internal/acquisition"
)

func TestCapturePreservesArticleInsideLegacyAndFormWrappers(t *testing.T) {
	page := fixture()
	page.MHTML = nil
	page.DOM = []byte(`<html><head><title>Neutral article</title></head><body><center><form action="https://source.invalid/submit" method="post" onsubmit="evil()"><article><h1>Preserved heading</h1><font color="blue"><p>Public article content inside legacy layout.</p></font><table><tr><td>Preserved table</td></tr></table><pre><code>a &lt; b</code></pre><img src="data:image/png;base64,aW1hZ2U=" onerror="evil()"><input name="password" value="not-article"><textarea>Inert form content</textarea><button>Submit</button><script>evil()</script><iframe><p>Embedded content</p></iframe><template><p>Inert template content</p></template></article></form></center></body></html>`)
	result, err := New(&fakeRenderer{pages: map[string]acquisition.RenderedPage{page.FinalURL: page}}, nil).Capture(context.Background(), page.FinalURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Preserved heading", "Public article content", "Preserved table", "a < b"} {
		if !strings.Contains(result.PlainText, text) {
			t.Errorf("missing text %q in %q", text, result.PlainText)
		}
	}
	for _, bad := range []string{"<center", "<form", "<font", "<template", "evil()", "Inert template", "Inert form", "<input", "<button", "action=", "method=", "not-article", "<iframe"} {
		if strings.Contains(string(result.HTML), bad) {
			t.Errorf("unexpected %q in output", bad)
		}
	}
	if !strings.Contains(string(result.HTML), "data:image/png;base64,") {
		t.Fatal("image missing")
	}
}
