package acquisition

import (
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

var (
	ErrRenderTimeout      = errors.New("browser rendering exceeded its deadline")
	ErrDOMTooLarge        = errors.New("rendered DOM exceeds configured limit")
	ErrScreenshotTooLarge = errors.New("screenshot exceeds configured limit")
	ErrRequestLimit       = errors.New("browser request count exceeds configured limit")
)

// Renderer is the acquisition boundary consumed by workers. Implementations
// own all browser resources for one call and must honor ctx cancellation.
type Renderer interface {
	Render(context.Context, string) (RenderedPage, error)
}

type RenderedPage struct {
	FinalURL    string
	Status      int
	Title       string
	DOM         []byte
	MHTML       []byte
	Screenshot  []byte
	Diagnostics RenderDiagnostics
}

type RenderDiagnostics struct {
	Requests         int64
	BlockedRequests  int64
	TransferredBytes int64
}

type ChromiumConfig struct {
	// Concurrency bounds browser processes across both Render and Snapshot calls.
	Concurrency         int
	Executable          string
	NavigationTimeout   time.Duration
	RenderTimeout       time.Duration
	SettleTime          time.Duration
	DialTimeout         time.Duration
	MaxDOMBytes         int
	MaxDOMNodes         int
	MaxScreenshotBytes  int
	ScreenshotWidth     int64
	ScreenshotHeight    int64
	MaxTransferredBytes int64
	MaxRequests         int64
	MaxRedirects        int64
}

// ChromiumRenderer starts a fresh browser process and profile per Render call.
// This is intentionally more isolated than a shared browser and makes cleanup
// and cancellation ownership unambiguous.
type ChromiumRenderer struct {
	config  ChromiumConfig
	slots   chan struct{}
	resolve func(context.Context, string) ([]net.IP, error)
}

func NewChromiumRenderer(cfg ChromiumConfig) (*ChromiumRenderer, error) {
	if cfg.Concurrency < 0 {
		return nil, errors.New("browser concurrency must not be negative")
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = 1
	}
	if cfg.Executable == "" {
		cfg.Executable = "/usr/bin/chromium"
	}
	if _, err := os.Stat(cfg.Executable); err != nil {
		return nil, fmt.Errorf("chromium executable: %w", err)
	}
	if cfg.NavigationTimeout <= 0 {
		cfg.NavigationTimeout = 20 * time.Second
	}
	if cfg.RenderTimeout <= 0 {
		cfg.RenderTimeout = 30 * time.Second
	}
	if cfg.SettleTime < 0 {
		return nil, errors.New("settle time must not be negative")
	}
	if cfg.SettleTime == 0 {
		cfg.SettleTime = 500 * time.Millisecond
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 5 * time.Second
	}
	if cfg.MaxDOMBytes <= 0 {
		cfg.MaxDOMBytes = 4 << 20
	}
	if cfg.MaxDOMNodes <= 0 {
		cfg.MaxDOMNodes = 100000
	}
	if cfg.MaxScreenshotBytes <= 0 {
		cfg.MaxScreenshotBytes = 8 << 20
	}
	if cfg.ScreenshotWidth <= 0 {
		cfg.ScreenshotWidth = 1280
	}
	if cfg.ScreenshotHeight <= 0 {
		cfg.ScreenshotHeight = 1600
	}
	if cfg.ScreenshotWidth > 4096 || cfg.ScreenshotHeight > 4096 {
		return nil, errors.New("screenshot dimensions exceed hard limit")
	}
	if cfg.MaxTransferredBytes <= 0 {
		cfg.MaxTransferredBytes = 32 << 20
	}
	if cfg.MaxRequests <= 0 {
		cfg.MaxRequests = 256
	}
	if cfg.MaxRedirects <= 0 {
		cfg.MaxRedirects = 5
	}
	return &ChromiumRenderer{config: cfg, slots: make(chan struct{}, cfg.Concurrency), resolve: func(ctx context.Context, host string) ([]net.IP, error) {
		return net.DefaultResolver.LookupIP(ctx, "ip", host)
	}}, nil
}

func (r *ChromiumRenderer) Render(ctx context.Context, raw string) (RenderedPage, error) {
	return r.render(ctx, raw, false, nil)
}

// Snapshot preserves loaded cross-origin images and styles within the same protected browser.
func (r *ChromiumRenderer) Snapshot(ctx context.Context, raw string) (RenderedPage, error) {
	return r.render(ctx, raw, true, nil)
}

// RenderSaved renders an already sanitized snapshot without contacting its website.
// It retains the original URL for extraction and blocks all external resources.
func (r *ChromiumRenderer) RenderSaved(ctx context.Context, raw string, html []byte) (RenderedPage, error) {
	if len(html) == 0 || len(html) > 64<<20 {
		return RenderedPage{}, ErrDOMTooLarge
	}
	return r.render(ctx, raw, false, html)
}

func (r *ChromiumRenderer) render(ctx context.Context, raw string, snapshot bool, savedHTML []byte) (result RenderedPage, err error) {
	if r == nil {
		return result, errors.New("nil renderer")
	}
	u, parseErr := url.Parse(raw)
	if parseErr != nil || ValidateURL(u) != nil {
		return result, ErrBlockedTarget
	}
	if savedHTML != nil {
		u.Fragment = ""
		if u.Path == "" {
			u.Path = "/"
		}
	}
	// Waiting callers remain cancellable and do not consume browser resources.
	if err := ctx.Err(); err != nil {
		return result, err
	}
	select {
	case r.slots <- struct{}{}:
	case <-ctx.Done():
		return result, ctx.Err()
	}
	defer func() { <-r.slots }()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	resolve := r.resolve
	if savedHTML != nil {
		// Chromium also makes process-level requests outside target interception.
		// Deny them before DNS resolution when replaying a saved document.
		resolve = func(context.Context, string) ([]net.IP, error) { return nil, ErrBlockedTarget }
	}
	proxy, err := newPolicyProxy(proxyConfig{resolve: resolve, dialTimeout: r.config.DialTimeout, maxBytes: r.config.MaxTransferredBytes, maxRequests: r.config.MaxRequests})
	if err != nil {
		return result, fmt.Errorf("start browser policy proxy: %w", err)
	}
	budget := requestBudget{maximum: r.config.MaxRequests}
	var interceptionBlocked atomic.Int64
	defer func() {
		result.Diagnostics = proxy.diagnostics()
		if intercepted := budget.count.Load(); intercepted > result.Diagnostics.Requests {
			result.Diagnostics.Requests = intercepted
		}
		result.Diagnostics.BlockedRequests += interceptionBlocked.Load()
		if closeErr := proxy.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close browser policy proxy: %w", closeErr)
		}
	}()

	operationCtx, operationCancel := context.WithTimeout(ctx, r.config.RenderTimeout)
	defer operationCancel()
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.ExecPath(r.config.Executable),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.ProxyServer(proxy.URL()),
		chromedp.Flag("proxy-bypass-list", "<-loopback>"),
		chromedp.Flag("disable-quic", true),
		chromedp.Flag("disable-popup-blocking", false),
		chromedp.Flag("force-webrtc-ip-handling-policy", "disable_non_proxied_udp"),
		chromedp.Flag("disable-features", "WebRtcHideLocalIpsWithMdns,MediaRouter,OptimizationHints,AutofillServerCommunication"),
		chromedp.WindowSize(int(r.config.ScreenshotWidth), int(r.config.ScreenshotHeight)),
	)
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(operationCtx, opts...)
	// allocatorCancel cancels the exec.Cmd context, waits for Chromium, and
	// removes its temporary profile even after timeout or caller cancellation.
	defer allocatorCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()

	var responseMu sync.Mutex
	responseStatuses := make(map[string]int)
	var policyMu sync.Mutex
	var policyErr error
	var redirects int64
	var servedSaved atomic.Bool
	chromedp.ListenTarget(browserCtx, func(event any) {
		switch e := event.(type) {
		case *network.EventResponseReceived:
			if e.Type == network.ResourceTypeDocument {
				responseMu.Lock()
				responseStatuses[e.Response.URL] = int(e.Response.Status)
				responseMu.Unlock()
			}
		case *fetch.EventRequestPaused:
			go func() {
				targetContext := chromedp.FromContext(browserCtx)
				if targetContext == nil || targetContext.Target == nil {
					return
				}
				executorCtx := cdp.WithExecutor(browserCtx, targetContext.Target)

				if savedHTML != nil {
					if e.ResourceType == network.ResourceTypeDocument && e.Request.URL == u.String() && servedSaved.CompareAndSwap(false, true) {
						_ = fetch.FulfillRequest(e.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{
							{Name: "Content-Type", Value: "text/html; charset=utf-8"},
							{Name: "Content-Security-Policy", Value: "default-src 'none'; img-src data:; style-src 'unsafe-inline' data:; font-src data:; media-src data:; base-uri 'none'; form-action 'none'; frame-src 'none'; sandbox"},
						}).WithBody(base64.StdEncoding.EncodeToString(savedHTML)).Do(executorCtx)
					} else {
						interceptionBlocked.Add(1)
						_ = fetch.FailRequest(e.RequestID, network.ErrorReasonBlockedByClient).Do(executorCtx)
					}
					return
				}
				target, parseErr := url.Parse(e.Request.URL)
				policyMu.Lock()
				if budget.exceeded() && policyErr == nil {
					policyErr = ErrRequestLimit
				}
				if e.RedirectedRequestID != "" {
					redirects++
					if redirects > r.config.MaxRedirects && policyErr == nil {
						policyErr = ErrTooManyRedirects
					}
				}
				blocked := policyErr != nil || parseErr != nil || ValidateURL(target) != nil
				if blocked && policyErr == nil {
					policyErr = ErrBlockedTarget
				}
				policyMu.Unlock()
				if blocked {
					interceptionBlocked.Add(1)
					_ = fetch.FailRequest(e.RequestID, network.ErrorReasonBlockedByClient).Do(executorCtx)
					return
				}
				_ = fetch.ContinueRequest(e.RequestID).Do(executorCtx)
			}()
		}
	})

	// Initialize the target on browserCtx. The context used for a target's first
	// Run owns its event loop, so using a short-lived navigation context here
	// would tear the target down as soon as navigation completed.
	err = chromedp.Run(browserCtx,
		network.Enable(),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*", RequestStage: fetch.RequestStageRequest}}),
		browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorDeny),
	)
	if err != nil {
		return result, r.renderError(ctx, operationCtx, err)
	}
	navigationCtx, navigationCancel := context.WithTimeout(browserCtx, r.config.NavigationTimeout)
	err = chromedp.Run(navigationCtx, chromedp.Navigate(u.String()))
	navigationCancel()
	if err != nil {
		policyMu.Lock()
		blockedErr := policyErr
		policyMu.Unlock()
		if blockedErr != nil {
			return result, blockedErr
		}
		return result, r.renderError(ctx, operationCtx, err)
	}
	if r.config.SettleTime > 0 {
		if err = chromedp.Run(browserCtx, chromedp.Sleep(r.config.SettleTime)); err != nil {
			return result, r.renderError(ctx, operationCtx, err)
		}
	}

	if !snapshot {
		type capture struct {
			HTML     string `json:"html"`
			Nodes    int    `json:"nodes"`
			TooLarge bool   `json:"tooLarge"`
		}
		var dom capture
		expression := fmt.Sprintf(captureScript, r.config.MaxDOMNodes, r.config.MaxDOMBytes)
		if err = chromedp.Run(browserCtx, chromedp.Evaluate(expression, &dom, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
			return result, r.renderError(ctx, operationCtx, err)
		}
		if dom.TooLarge || len([]byte(dom.HTML)) > r.config.MaxDOMBytes {
			return result, ErrDOMTooLarge
		}
		if err = chromedp.Run(browserCtx, chromedp.CaptureScreenshot(&result.Screenshot)); err != nil {
			return result, r.renderError(ctx, operationCtx, err)
		}
		if len(result.Screenshot) > r.config.MaxScreenshotBytes {
			return result, ErrScreenshotTooLarge
		}
		result.DOM = []byte(dom.HTML)
	}
	if err = chromedp.Run(browserCtx, chromedp.Title(&result.Title), chromedp.Location(&result.FinalURL)); err != nil {
		return result, r.renderError(ctx, operationCtx, err)
	}
	finalURL, finalErr := url.Parse(result.FinalURL)
	if finalErr != nil || ValidateURL(finalURL) != nil {
		return result, ErrBlockedTarget
	}
	if snapshot {
		var tooLarge bool
		expression := fmt.Sprintf(`(()=>{let count=0;const walker=document.createTreeWalker(document,NodeFilter.SHOW_ALL);while(walker.nextNode()){if(++count>%d)return true;}return new TextEncoder().encode(document.documentElement.outerHTML).length>%d})()`, r.config.MaxDOMNodes, r.config.MaxDOMBytes)
		if err = chromedp.Run(browserCtx, chromedp.Evaluate(expression, &tooLarge)); err != nil {
			return result, r.renderError(ctx, operationCtx, err)
		}
		if tooLarge {
			return result, ErrDOMTooLarge
		}
		// Trigger ordinary lazy image loading through the existing network policy.
		if err = chromedp.Run(browserCtx, chromedp.Evaluate(`(async()=>{await Promise.all([...document.images].slice(0,256).map(img=>{img.loading="eager";return Promise.race([img.decode().catch(()=>{}),new Promise(resolve=>setTimeout(resolve,4000))])}));return true})()`, nil, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
			return result, r.renderError(ctx, operationCtx, err)
		}
		var archive string
		if err = chromedp.Run(browserCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			var e error
			archive, e = page.CaptureSnapshot().Do(ctx)
			return e
		})); err != nil {
			return result, r.renderError(ctx, operationCtx, err)
		}
		if int64(len(archive)) > r.config.MaxTransferredBytes*2 {
			return result, ErrResponseTooLarge
		}
		result.MHTML = []byte(archive)
	}
	responseMu.Lock()
	result.Status = responseStatuses[result.FinalURL]
	if result.Status == 0 {
		for responseURL, status := range responseStatuses {
			if strings.TrimSuffix(responseURL, "/") == strings.TrimSuffix(result.FinalURL, "/") {
				result.Status = status
				break
			}
		}
	}
	responseMu.Unlock()
	policyMu.Lock()
	blockedErr := policyErr
	policyMu.Unlock()
	// Blocking an unsafe subresource does not invalidate an otherwise complete
	// public document. Exhausting a configured resource/redirect budget does:
	// continuing would send a knowingly truncated render to the AI stage.
	if errors.Is(blockedErr, ErrRequestLimit) || errors.Is(blockedErr, ErrTooManyRedirects) {
		return result, blockedErr
	}
	if blockedErr != nil && result.FinalURL == "" {
		return result, blockedErr
	}
	return result, nil
}

type requestBudget struct {
	maximum int64
	count   atomic.Int64
}

func (b *requestBudget) exceeded() bool {
	return b != nil && b.count.Add(1) > b.maximum
}

func (r *ChromiumRenderer) renderError(parent, operation context.Context, err error) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	if operation.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %v", ErrRenderTimeout, err)
	}
	return fmt.Errorf("render page: %w", err)
}

var _ Renderer = (*ChromiumRenderer)(nil)

//go:embed capture.js
var captureScript string
