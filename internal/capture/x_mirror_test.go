package capture

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"attic/internal/acquisition"
)

func unavailableMirror(context.Context, string) (acquisition.Page, error) {
	return acquisition.Page{}, errors.New("mirror unavailable")
}

func mirrorFixture() acquisition.Page {
	return acquisition.Page{FinalURL: "https://xcancel.com/sample/status/12345", Status: 200, HTML: []byte(`<div class="main-tweet"><div class="tweet-header"><a class="fullname">Example Author</a><a class="username">@sample</a><span class="tweet-date"><a href="/sample/status/12345#m" title="Sep 9, 2026 · 12:00 PM UTC">Sep 9</a></span></div><div class="tweet-content media-body">An opening sentence. ` + strings.Repeat("A longer explanation. ", 100) + `<br>The final sentence.<script>evil()</script></div><div class="attachments"><img src="/pic/test.jpg" onerror="evil()"></div></div><div class="timeline-item"><a href="/sample/status/12345">Unrelated reply</a><div class="tweet-content">Reply text must be excluded.</div></div>`)}
}

func TestXMirrorPreservesFullMainPostBeforeEmbed(t *testing.T) {
	c := New(&fakeRenderer{}, nil)
	c.fetchJSON = func(context.Context, string) (acquisition.Page, error) {
		t.Fatal("mirror success should not use truncated embed")
		return acquisition.Page{}, nil
	}
	c.fetchMirror = func(ctx context.Context, raw string) (acquisition.Page, error) {
		if raw != "https://xcancel.com/sample/status/12345" {
			t.Fatalf("unexpected mirror URL %s", raw)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			t.Fatal("unbounded mirror request")
		}
		return mirrorFixture(), nil
	}
	got, err := c.Capture(context.Background(), testXURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.PlainText, "The final sentence.") || !strings.Contains(got.PlainText, "Example Author") || !strings.Contains(got.PlainText, "Sep 9, 2026") || got.OriginalURL != testXURL || got.FinalURL != mirrorFixture().FinalURL || got.Status != "partial" || len(got.Attempts) != 1 {
		t.Fatalf("unexpected capture: %+v", got)
	}
	if strings.Contains(got.PlainText, "Reply text") || strings.Contains(string(got.HTML), "evil()") || strings.Contains(string(got.HTML), "<script") {
		t.Fatal("unrelated or active content preserved")
	}
	if !strings.Contains(strings.Join(got.MissingResources, " "), "/pic/test.jpg") {
		t.Fatal("missing attachment not reported")
	}
}
func TestXMirrorRejectsWrongPostAndFallsBack(t *testing.T) {
	cases := map[string]acquisition.Page{
		"challenge":                      {Status: 200, FinalURL: mirrorFixture().FinalURL, HTML: []byte("<title>Verify you are human</title>")},
		"redirect":                       {Status: 200, FinalURL: "https://evil.example", HTML: mirrorFixture().HTML},
		"large":                          {Status: 200, FinalURL: mirrorFixture().FinalURL, HTML: []byte(strings.Repeat(" ", maxMirrorBytes+1))},
		"rate limit":                     {Status: 429, FinalURL: mirrorFixture().FinalURL, HTML: mirrorFixture().HTML},
		"wrong main with matching reply": {Status: 200, FinalURL: mirrorFixture().FinalURL, HTML: []byte(strings.Replace(string(mirrorFixture().HTML), "/sample/status/12345#m", "/sample/status/999#m", 1))},
	}
	for name, page := range cases {
		t.Run(name, func(t *testing.T) {
			c := New(&fakeRenderer{}, nil)
			c.fetchMirror = func(context.Context, string) (acquisition.Page, error) { return page, nil }
			c.fetchJSON = func(context.Context, string) (acquisition.Page, error) { return embedFixture("Fallback post…"), nil }
			got, err := c.Capture(context.Background(), testXURL)
			if err != nil || !strings.Contains(got.PlainText, "Fallback post") || got.Attempts[0].Status != "failed" {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}
func TestXMirrorURL(t *testing.T) {
	for _, raw := range []string{testXURL, "https://twitter.com/sample/status/12345/photo/1?tracking=yes#part", "https://xcancel.com/sample/status/12345#part"} {
		if got := xMirrorURL(raw); got != "https://xcancel.com/sample/status/12345" {
			t.Fatal(got)
		}
	}
	for _, raw := range []string{"https://x.com.evil.example/sample/status/12345", "https://user@x.com/sample/status/12345", "https://example.com"} {
		if got := xMirrorURL(raw); got != "" {
			t.Fatal(got)
		}
	}
}

func TestUnexpandedPostIsPartial(t *testing.T) {
	got, err := FromPage(acquisition.RenderedPage{FinalURL: testXURL, Status: 200, DOM: []byte("<p>The beginning of a long post.</p>"), Truncated: true})
	if err != nil || got.Status != "partial" || !strings.Contains(strings.Join(got.MissingResources, " "), "truncated") {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestDirectMirrorPostUsesTargetedCapture(t *testing.T) {
	c := New(&fakeRenderer{}, nil)
	c.fetchMirror = func(context.Context, string) (acquisition.Page, error) { return mirrorFixture(), nil }
	raw := "https://xcancel.com/sample/status/12345"
	got, err := c.Capture(context.Background(), raw)
	if err != nil || got.OriginalURL != raw || !strings.Contains(got.PlainText, "The final sentence.") || strings.Contains(got.PlainText, "Reply text") {
		t.Fatalf("unexpected direct mirror result: %v", err)
	}
	if xEmbedURL(raw) != "" {
		t.Fatal("mirror accepted as an X API post")
	}
}

func TestMirrorRetriesCanonicalUsernameFromX(t *testing.T) {
	c := New(&fakeRenderer{}, nil)
	c.fetchJSON = func(context.Context, string) (acquisition.Page, error) {
		p := embedFixture("The opening…")
		p.HTML = []byte(strings.ReplaceAll(string(p.HTML), "sample", "Sample"))
		return p, nil
	}
	var calls []string
	c.fetchMirror = func(_ context.Context, raw string) (acquisition.Page, error) {
		calls = append(calls, raw)
		if raw == "https://xcancel.com/sample/status/12345" {
			return acquisition.Page{}, errors.New("noncanonical account not found")
		}
		if raw != "https://xcancel.com/Sample/status/12345" {
			t.Fatalf("unexpected mirror %s", raw)
		}
		p := mirrorFixture()
		p.FinalURL = raw
		return p, nil
	}
	result, err := c.Capture(context.Background(), testXURL)
	if err != nil || len(calls) != 2 || !strings.Contains(result.PlainText, "The final sentence.") || result.OriginalURL != testXURL || len(result.Attempts) != 3 {
		t.Fatalf("canonical recovery failed: %v attempts=%v", err, result.Attempts)
	}
}
