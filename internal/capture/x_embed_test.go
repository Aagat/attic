package capture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"attic/internal/acquisition"
)

const testXURL = "https://x.com/sample/status/12345?s=20"

func embedFixture(text string) acquisition.Page {
	data, _ := json.Marshal(map[string]string{"url": "https://twitter.com/sample/status/12345", "type": "rich", "author_name": "Example Author", "html": `<blockquote class="twitter-tweet"><p lang="en">` + text + `</p> &mdash; Example Author <a href="https://twitter.com/sample/status/12345?ref_src=embed">September 9</a></blockquote><script>evil()</script>`})
	return acquisition.Page{HTML: data}
}
func TestXEmbedShortPostIsSanitizedAndHonest(t *testing.T) {
	renderer := &fakeRenderer{}
	c := New(renderer, nil)
	c.fetchMirror = unavailableMirror
	calls := 0
	c.fetchJSON = func(ctx context.Context, raw string) (acquisition.Page, error) {
		calls++
		u, _ := url.Parse(raw)
		if u.Scheme != "https" || u.Host != "publish.x.com" || u.Path != "/oembed" || u.Query().Get("url") != "https://x.com/i/status/12345" || u.Query().Get("omit_script") != "true" {
			t.Fatalf("unexpected request %s", raw)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			t.Fatal("missing bounded request")
		}
		return embedFixture(`A short public note. <img src="https://media.example/image" onerror="evil()">`), nil
	}
	result, err := c.Capture(context.Background(), testXURL)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(renderer.calls) != 1 || result.Status != "partial" || !strings.Contains(result.PlainText, "A short public note.") || result.Title != "Post by Example Author" || result.OriginalURL != testXURL {
		t.Fatalf("%+v calls=%v", result, renderer.calls)
	}
	if strings.Contains(string(result.HTML), "evil()") || strings.Contains(string(result.HTML), "<script") {
		t.Fatal("active content preserved")
	}
	if !strings.Contains(strings.Join(result.MissingResources, " "), "media and thread context") {
		t.Fatal(result.MissingResources)
	}
}
func TestXEmbedTruncationStillAttemptsBrowserAndArchives(t *testing.T) {
	for _, recover := range []bool{false, true} {
		t.Run(fmt.Sprint(recover), func(t *testing.T) {
			renderer := &fakeRenderer{pages: map[string]acquisition.RenderedPage{testXURL: {FinalURL: testXURL, Status: 403, DOM: []byte("<p>Access denied</p>")}}}
			archive := "https://archive.example/post"
			if recover {
				renderer.pages[archive] = acquisition.RenderedPage{FinalURL: archive, Status: 200, DOM: []byte("<p>Full post recovered from archive.</p>")}
			}
			c := New(renderer, fakeArchives{archive})
			c.fetchMirror = unavailableMirror
			c.fetchJSON = func(context.Context, string) (acquisition.Page, error) {
				return embedFixture("This post continues…"), nil
			}
			result, err := c.Capture(context.Background(), testXURL)
			if err != nil {
				t.Fatal(err)
			}
			if len(renderer.calls) != 2 || len(result.Attempts) != 4 {
				t.Fatalf("attempts %+v", result.Attempts)
			}
			if recover {
				if result.Status != "complete" || !strings.Contains(result.PlainText, "Full post recovered") {
					t.Fatalf("%+v", result)
				}
			} else if result.Status != "partial" || !strings.Contains(result.PlainText, "This post continues") || !strings.Contains(strings.Join(result.MissingResources, " "), "truncated") {
				t.Fatalf("%+v", result)
			}
		})
	}
}
func TestXEmbedFailuresFallThrough(t *testing.T) {
	cases := map[string]acquisition.Page{
		"invalid JSON": {HTML: []byte("not JSON")},
		"wrong post":   {HTML: []byte(`{"url":"https://x.com/sample/status/6789","type":"rich","html":"<p>Wrong post</p>"}`)},
		"empty text":   embedFixture(""),
		"oversized":    {HTML: []byte(strings.Repeat(" ", maxEmbedBytes+1))},
		"wrong anchor": {HTML: []byte(strings.Replace(string(embedFixture("Public note").HTML), "12345?ref_src", "54321?ref_src", 1))},
		"unavailable":  {},
	}
	for name, page := range cases {
		t.Run(name, func(t *testing.T) {
			renderer := &fakeRenderer{pages: map[string]acquisition.RenderedPage{testXURL: {FinalURL: testXURL, Status: 200, DOM: []byte("<p>Browser recovered the post.</p>")}}}
			c := New(renderer, nil)
			c.fetchMirror = unavailableMirror
			c.fetchJSON = func(context.Context, string) (acquisition.Page, error) {
				if name == "unavailable" {
					return page, errors.New("rate limited")
				}
				return page, nil
			}
			result, err := c.Capture(context.Background(), testXURL)
			if err != nil || !strings.Contains(result.PlainText, "Browser recovered") || len(renderer.calls) != 1 || result.Attempts[0].Status != "failed" {
				t.Fatalf("%+v %v", result, err)
			}
		})
	}
}
func TestXURLRecognition(t *testing.T) {
	for _, raw := range []string{testXURL, "https://twitter.com/sample/status/12345/photo/1", "https://mobile.twitter.com/i/web/status/12345#part", "http://www.x.com/i/status/12345"} {
		if id := xPostID(raw); id != "12345" {
			t.Errorf("%s: %s", raw, id)
		}
	}
	for _, raw := range []string{"https://evil.example/sample/status/12345", "https://x.com.evil.example/sample/status/12345", "https://x.com:444/sample/status/12345", "https://a@x.com/sample/status/12345", "https://x.com/sample", "https://x.com/sample/status/abc", "https://x.com/sample/status/12345/other"} {
		if xEmbedURL(raw) != "" {
			t.Errorf("accepted %s", raw)
		}
	}
	renderer := &fakeRenderer{pages: map[string]acquisition.RenderedPage{"https://example.com": {FinalURL: "https://example.com", Status: 200, DOM: []byte("<p>Ordinary page</p>")}}}
	c := New(renderer, nil)
	c.fetchMirror = unavailableMirror
	c.fetchJSON = func(context.Context, string) (acquisition.Page, error) {
		t.Fatal("non-X URL queried embed")
		return acquisition.Page{}, nil
	}
	if _, err := c.Capture(context.Background(), "https://example.com"); err != nil {
		t.Fatal(err)
	}
}
