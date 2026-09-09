package acquisition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const RecoveryWidth, RecoveryHeight = 1024, 768

// BrowserSession owns one persistent, visible Chromium tab. Profiles are private
// per source host; Close releases processes but never removes stored profiles.
// Every network connection crosses the same public-address policy as captures.
type BrowserSession struct {
	responseMu      sync.Mutex
	statuses        map[string]int
	ctx             context.Context
	cancel          context.CancelFunc
	allocatorCancel context.CancelFunc
	proxy           *policyProxy
	mu              sync.Mutex
	closeOnce       sync.Once
}

type BrowserFrame struct {
	URL, Title, Text string
	Screenshot       []byte
}

func OpenBrowserSession(parent context.Context, executable, profiles, raw string) (*BrowserSession, error) {
	u, err := url.Parse(raw)
	if err != nil || ValidateURL(u) != nil {
		return nil, ErrBlockedTarget
	}
	hash := sha256.Sum256([]byte(u.Hostname()))
	profile := filepath.Join(profiles, hex.EncodeToString(hash[:16]))
	if err = os.MkdirAll(profile, 0700); err != nil {
		return nil, err
	}
	if err = os.Chmod(profile, 0700); err != nil {
		return nil, err
	}
	proxy, err := newPolicyProxy(proxyConfig{maxBytes: 128 << 20, maxRequests: 2048})
	if err != nil {
		return nil, err
	}
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts, chromedp.ExecPath(executable), chromedp.UserDataDir(profile),
		chromedp.Flag("headless", false), chromedp.ProxyServer(proxy.URL()), chromedp.Flag("proxy-bypass-list", "<-loopback>"),
		chromedp.Flag("disable-quic", true), chromedp.Flag("disable-dev-shm-usage", true), chromedp.Flag("disable-popup-blocking", false),
		chromedp.Flag("force-webrtc-ip-handling-policy", "disable_non_proxied_udp"), chromedp.WindowSize(RecoveryWidth, RecoveryHeight))
	allocator, ac := chromedp.NewExecAllocator(context.WithoutCancel(parent), opts...)
	ctx, cancel := chromedp.NewContext(allocator)
	initCancel := context.AfterFunc(parent, cancel)
	defer initCancel()
	initTimer := time.AfterFunc(45*time.Second, cancel)
	defer initTimer.Stop()
	s := &BrowserSession{ctx: ctx, cancel: cancel, allocatorCancel: ac, proxy: proxy, statuses: make(map[string]int)}
	chromedp.ListenTarget(ctx, func(event any) {
		if e, ok := event.(*network.EventResponseReceived); ok && e.Type == network.ResourceTypeDocument {
			s.responseMu.Lock()
			s.statuses[e.Response.URL] = int(e.Response.Status)
			s.responseMu.Unlock()
		}
		if e, ok := event.(*fetch.EventRequestPaused); ok {
			go func() {
				c := chromedp.FromContext(ctx)
				if c == nil || c.Target == nil {
					return
				}
				exec := cdp.WithExecutor(ctx, c.Target)
				target, parseErr := url.Parse(e.Request.URL)
				if parseErr != nil || ValidateURL(target) != nil {
					_ = fetch.FailRequest(e.RequestID, network.ErrorReasonBlockedByClient).Do(exec)
					return
				}
				_ = fetch.ContinueRequest(e.RequestID).Do(exec)
			}()
		}
	})
	if err = chromedp.Run(ctx, network.Enable(), fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*", RequestStage: fetch.RequestStageRequest}}), browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorDeny), chromedp.EmulateViewport(RecoveryWidth, RecoveryHeight)); err != nil {
		s.Close()
		return nil, err
	}
	nav, stop := context.WithTimeout(ctx, 35*time.Second)
	err = chromedp.Run(nav, chromedp.Navigate(raw))
	stop()
	// A timed out load can still contain an interactive verification page.
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *BrowserSession) Close() {
	s.closeOnce.Do(func() {
		graceful, stop := context.WithTimeout(s.ctx, 3*time.Second)
		_ = chromedp.Cancel(graceful)
		stop()
		s.cancel()
		s.allocatorCancel()
		_ = s.proxy.Close()
	})
}
func (s *BrowserSession) run(parent context.Context, actions ...chromedp.Action) error {
	if err := parent.Err(); err != nil {
		return err
	}
	ctx, stop := context.WithTimeout(s.ctx, 15*time.Second)
	defer stop()
	end := context.AfterFunc(parent, stop)
	defer end()
	return chromedp.Run(ctx, actions...)
}
func (s *BrowserSession) Frame(ctx context.Context) (BrowserFrame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var f BrowserFrame
	err := s.run(ctx, chromedp.Location(&f.URL), chromedp.Title(&f.Title), chromedp.Evaluate(`(document.body?.innerText || '').slice(0,20000)`, &f.Text), chromedp.CaptureScreenshot(&f.Screenshot))
	u, e := url.Parse(f.URL)
	if e != nil || ValidateURL(u) != nil {
		return f, ErrBlockedTarget
	}
	if len(f.Screenshot) > 5<<20 {
		return BrowserFrame{}, ErrScreenshotTooLarge
	}
	return f, err
}
func (s *BrowserSession) Input(ctx context.Context, action string, x, y float64, text string, delta float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var actions []chromedp.Action
	switch action {
	case "click":
		actions = []chromedp.Action{input.DispatchMouseEvent(input.MousePressed, x, y).WithButton(input.Left).WithClickCount(1), input.DispatchMouseEvent(input.MouseReleased, x, y).WithButton(input.Left).WithClickCount(1)}
	case "type":
		actions = []chromedp.Action{input.InsertText(text)}
	case "scroll":
		actions = []chromedp.Action{input.DispatchMouseEvent(input.MouseWheel, RecoveryWidth/2, RecoveryHeight/2).WithDeltaY(delta).WithDeltaX(0)}
	case "key":
		keys := map[string]string{"Enter": "\r", "Backspace": "\b", "Tab": "\t", "Escape": "\u001b"}
		key, ok := keys[text]
		if !ok {
			return errors.New("unsupported key")
		}
		actions = []chromedp.Action{chromedp.KeyEvent(key)}
	case "wait":
		return nil
	default:
		return errors.New("unsupported browser input")
	}
	return s.run(ctx, actions...)
}
func (s *BrowserSession) Snapshot(ctx context.Context) (RenderedPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result RenderedPage
	var snapshot string
	err := s.run(ctx, chromedp.Title(&result.Title), chromedp.Location(&result.FinalURL), chromedp.ActionFunc(func(ctx context.Context) error { var e error; snapshot, e = page.CaptureSnapshot().Do(ctx); return e }))
	if err != nil {
		return result, err
	}
	if len(snapshot) > 32<<20 {
		return result, ErrResponseTooLarge
	}
	u, e := url.Parse(result.FinalURL)
	if e != nil || ValidateURL(u) != nil {
		return result, ErrBlockedTarget
	}
	result.MHTML = []byte(snapshot)
	s.responseMu.Lock()
	result.Status = s.statuses[result.FinalURL]
	s.responseMu.Unlock()
	if result.Status == 0 {
		result.Status = 200
	}
	return result, nil
}
