package acquisition

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
)

// Run in the runtime image with a virtual display. This crosses the real browser
// lifecycle: cancel the activity, gracefully close, reopen the same private profile.
func TestBrowserSessionRetainsCookiesAfterCancellation(t *testing.T) {
	if os.Getenv("ATTIC_SESSION_INTEGRATION") != "1" {
		t.Skip("requires headed Chromium and network")
	}
	profiles := t.TempDir()
	executable := os.Getenv("BROWSER_EXECUTABLE")
	if executable == "" {
		executable = "/usr/bin/chromium"
	}
	parent, cancel := context.WithCancel(context.Background())
	s, err := OpenBrowserSession(parent, executable, profiles, "https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	expires := cdp.TimeSinceEpoch(time.Now().Add(time.Hour))
	if err = s.run(context.Background(), network.SetCookie("attic_recovery_probe", "retained").WithURL("https://example.com/").WithExpires(&expires)); err != nil {
		s.Close()
		t.Fatal(err)
	}
	cancel()
	s.Close()
	reopened, err := OpenBrowserSession(context.Background(), executable, profiles, "https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var cookies []*network.Cookie
	if err = reopened.run(context.Background(), chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		cookies, err = storage.GetCookies().Do(ctx)
		return err
	})); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range cookies {
		found = found || c.Name == "attic_recovery_probe" && c.Value == "retained"
	}
	if !found {
		t.Fatal("verification cookie did not survive activity cancellation and reopen")
	}
	frame, err := reopened.Frame(context.Background())
	if err != nil || len(frame.Screenshot) == 0 {
		t.Fatal("headed session does not render a takeover frame", err)
	}
	if _, err = OpenBrowserSession(context.Background(), executable, profiles, "http://127.0.0.1/private"); err == nil {
		t.Fatal("private network target accepted")
	}
}
