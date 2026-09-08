package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Exercises the browser-facing saved-item interface without an external index or AI.
func TestLibraryBrowserIntegration(t *testing.T) {
	if os.Getenv("ATTIC_WEB_INTEGRATION") != "1" {
		t.Skip("ATTIC_WEB_INTEGRATION is not set")
	}
	server, _, _ := testServer(t)
	var mu sync.Mutex
	var intents []string
	var searched bool
	item := map[string]any{"id": "saved-one", "kind": "web", "url": "https://example.com/article", "title": "Saved article", "notes": "", "tags": []string{}, "saved_at": "2026-09-08T12:00:00Z", "capture_status": "complete", "index_status": "indexed", "pdf_status": "not_requested", "delivery_status": "not_requested", "suggested_tags": []string{"Computing"}, "captures": []map[string]any{{"id": "capture-one", "created_at": "2026-09-08T12:00:00Z", "status": "complete"}}}
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.URL.Path != "/api/v1/items" && r.URL.Path != "/api/v1/items/saved-one") || !server.authorized(r) {
			server.ServeHTTP(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			intents = append(intents, body["action"])
		}
		if r.Method == http.MethodPut {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			for key, value := range body {
				item[key] = value
			}
		}
		if r.URL.Path == "/api/v1/items" && r.Method == http.MethodGet {
			if r.URL.Query().Get("q") == "captured phrase" {
				searched = true
			}
			json.NewEncoder(w).Encode(map[string]any{"items": []any{item}, "total": 1, "index_status": "ready", "storage_bytes": 1048576})
			return
		}
		json.NewEncoder(w).Encode(item)
	}))
	defer app.Close()
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath("/usr/bin/chromium"))
	allocator, cancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer cancel()
	ctx, cancel := chromedp.NewContext(allocator, chromedp.WithErrorf(func(string, ...any) {}))
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844), chromedp.Navigate(app.URL), chromedp.WaitVisible("#login"), chromedp.SendKeys("#access-key", "secret-token"), chromedp.Click("#login-form button"), chromedp.WaitVisible(".article"),
		chromedp.SendKeys("#article-url", "https://example.com/bookmark"), chromedp.Click(`#submit-form button[value="bookmark"]`), chromedp.Poll(`document.getElementById('article-url').value === ''`, nil),
		chromedp.SendKeys("#article-url", "https://example.com/kindle"), chromedp.Click(`#submit-form button[value="kindle"]`), chromedp.Poll(`document.getElementById('article-url').value === ''`, nil),
		chromedp.SendKeys("#search", "captured phrase"), chromedp.Sleep(400*time.Millisecond),
		chromedp.Click(".article .actions button"), chromedp.WaitVisible("#item-details"), chromedp.Click("#accept-tags"), chromedp.SendKeys("#edit-notes", "Remember this"), chromedp.Click(`#edit-form button[type="submit"]`), chromedp.Poll(`document.getElementById('edit-status').textContent === 'Changes saved.'`, nil),
	); err != nil {
		t.Fatal(err)
	}
	var href string
	var overflow bool
	if err := chromedp.Run(ctx, chromedp.AttributeValue("#capture-list a", "href", &href, nil), chromedp.Evaluate(`document.documentElement.scrollWidth > innerWidth`, &overflow)); err != nil {
		t.Fatal(err)
	}
	if href != "/api/v1/items/saved-one/captures/capture-one" {
		t.Fatalf("capture href: %q", href)
	}
	if overflow {
		t.Fatal("mobile library overflows")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(intents) != 2 || intents[0] != "bookmark" || intents[1] != "kindle" {
		t.Fatalf("intents: %v", intents)
	}
	if !searched {
		t.Fatal("search did not reach collection interface")
	}
	if item["notes"] != "Remember this" {
		t.Fatalf("notes: %v", item["notes"])
	}
	tags, _ := item["tags"].([]any)
	if len(tags) != 1 || tags[0] != "Computing" {
		t.Fatalf("tags: %v", item["tags"])
	}
}
