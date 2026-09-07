package httpapi

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// This opt-in check uses a real browser against an isolated in-memory server.
// No external articles, AI provider, account credentials or running deployment.
func TestPWABrowserIntegration(t *testing.T) {
	if os.Getenv("ATTIC_WEB_INTEGRATION") != "1" {
		t.Skip("ATTIC_WEB_INTEGRATION is not set")
	}
	server, _, _ := testServer(t)
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath("/usr/bin/chromium"))
	allocator, cancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer cancel()
	ctx, cancel := chromedp.NewContext(allocator, chromedp.WithErrorf(func(string, ...any) {}))
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	article := "https://example.com/article?x=1&y=2"
	sharedURL := httpServer.URL + "/?text=" + url.QueryEscape("A good read\n"+article)
	var value string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(sharedURL),
		chromedp.WaitVisible("#login"),
		chromedp.Value("#article-url", &value),
	); err != nil {
		t.Fatal(err)
	}
	if value != article {
		t.Fatalf("shared URL = %q", value)
	}
	if err := chromedp.Run(ctx,
		chromedp.SendKeys("#access-key", "secret-token"),
		chromedp.Click("#login-form button"),
		chromedp.WaitVisible("#library"),
		chromedp.Poll(`document.querySelector('.empty') !== null`, nil),
		chromedp.Value("#article-url", &value),
	); err != nil {
		t.Fatal(err)
	}
	if value != article {
		t.Fatal("sign-in lost the shared link")
	}
	if err := chromedp.Run(ctx,
		chromedp.Click("#submit-form button"),
		chromedp.Poll(`document.querySelectorAll('.article').length===1`, nil),
		chromedp.Poll(`location.search === ''`, nil),
		chromedp.Navigate(httpServer.URL+"/connect.html"),
		chromedp.WaitVisible("#server-address"),
		chromedp.Value("#server-address", &value),
	); err != nil {
		t.Fatal(err)
	}
	if value != httpServer.URL {
		t.Fatalf("setup assumes another host: %s", value)
	}
	var overflow bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.documentElement.scrollWidth>innerWidth`, &overflow)); err != nil {
		t.Fatal(err)
	}
	if overflow {
		t.Fatal("mobile setup overflows viewport")
	}
	if path := os.Getenv("ATTIC_WEB_SCREENSHOT"); path != "" {
		var png []byte
		if err := chromedp.Run(ctx, chromedp.FullScreenshot(&png, 90)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, png, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := chromedp.Run(ctx, chromedp.Poll(`navigator.serviceWorker.controller!==null`, nil)); err != nil {
		t.Fatal("service worker did not take control:", err)
	}
	httpServer.Close()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(sharedURL),
		chromedp.Poll(`document.body.textContent.includes('The article has not been saved yet.')`, nil),
	); err != nil {
		t.Fatal(err)
	}
	t.Log("Shared link survives sign-in, saves once on confirmation, setup fits mobile, and offline navigation reports unsaved state")
}
