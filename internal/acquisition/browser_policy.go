package acquisition

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"sync"
	"sync/atomic"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// browserPolicy installs both process-level network protection and target-level
// request accounting. HTTPS tunnels hide individual requests from the proxy, so
// neither layer alone enforces the complete policy.
type browserPolicy struct {
	proxy        *policyProxy
	maxRedirects int64
	savedURL     string
	savedHTML    []byte
	servedSaved  atomic.Bool
	budget       requestBudget
	blocked      atomic.Int64
	mu           sync.Mutex
	redirects    int64
	err          error
}

func newBrowserPolicy(cfg proxyConfig, maxRedirects int64, savedURL string, savedHTML []byte) (*browserPolicy, error) {
	if savedHTML != nil {
		// Deny background Chromium traffic before DNS during offline replay too.
		cfg.resolve = func(context.Context, string) ([]net.IP, error) { return nil, ErrBlockedTarget }
	}
	proxy, err := newPolicyProxy(cfg)
	if err != nil {
		return nil, err
	}
	return &browserPolicy{proxy: proxy, maxRedirects: maxRedirects, savedURL: savedURL, savedHTML: savedHTML, budget: requestBudget{maximum: proxy.config.maxRequests}}, nil
}

func (p *browserPolicy) options() []chromedp.ExecAllocatorOption {
	return []chromedp.ExecAllocatorOption{
		chromedp.ProxyServer(p.proxy.URL()),
		chromedp.Flag("proxy-bypass-list", "<-loopback>"),
		chromedp.Flag("disable-quic", true),
		chromedp.Flag("disable-popup-blocking", false),
		chromedp.Flag("force-webrtc-ip-handling-policy", "disable_non_proxied_udp"),
		chromedp.Flag("disable-features", "WebRtcHideLocalIpsWithMdns,MediaRouter,OptimizationHints,AutofillServerCommunication"),
	}
}

// install must run on the context that owns the target's entire lifetime.
func (p *browserPolicy) install(ctx context.Context) error {
	chromedp.ListenTarget(ctx, func(event any) {
		if e, ok := event.(*fetch.EventRequestPaused); ok {
			go func() {
				target := chromedp.FromContext(ctx)
				if target == nil || target.Target == nil {
					return
				}
				exec := cdp.WithExecutor(ctx, target.Target)
				if p.savedHTML != nil {
					if e.ResourceType == network.ResourceTypeDocument && e.Request.URL == p.savedURL && p.servedSaved.CompareAndSwap(false, true) {
						_ = fetch.FulfillRequest(e.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{
							{Name: "Content-Type", Value: "text/html; charset=utf-8"},
							{Name: "Content-Security-Policy", Value: "default-src 'none'; img-src data:; style-src 'unsafe-inline' data:; font-src data:; media-src data:; base-uri 'none'; form-action 'none'; frame-src 'none'; sandbox"},
						}).WithBody(base64.StdEncoding.EncodeToString(p.savedHTML)).Do(exec)
						return
					}
				} else if p.allow(e.Request.URL, e.RedirectedRequestID != "") {
					_ = fetch.ContinueRequest(e.RequestID).Do(exec)
					return
				}
				p.blocked.Add(1)
				_ = fetch.FailRequest(e.RequestID, network.ErrorReasonBlockedByClient).Do(exec)
			}()
		}
	})
	return chromedp.Run(ctx, network.Enable(), fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*", RequestStage: fetch.RequestStageRequest}}), browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorDeny))
}

func (p *browserPolicy) allow(raw string, redirected bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.budget.exceeded() {
		p.err = ErrRequestLimit
	}
	if redirected {
		p.redirects++
		if p.redirects > p.maxRedirects && !errors.Is(p.err, ErrRequestLimit) {
			p.err = ErrTooManyRedirects
		}
	}
	if errors.Is(p.err, ErrRequestLimit) || errors.Is(p.err, ErrTooManyRedirects) {
		return false
	}
	target, err := url.Parse(raw)
	if err != nil || ValidateURL(target) != nil {
		p.err = ErrBlockedTarget
		return false
	}
	// An unsafe subresource must not prevent unrelated public resources loading.
	return true
}

func (p *browserPolicy) failure() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *browserPolicy) exhausted() error {
	err := p.failure()
	if errors.Is(err, ErrRequestLimit) || errors.Is(err, ErrTooManyRedirects) {
		return err
	}
	return nil
}

func (p *browserPolicy) diagnostics() RenderDiagnostics {
	d := p.proxy.diagnostics()
	if requests := p.budget.count.Load(); requests > d.Requests {
		d.Requests = requests
	}
	d.BlockedRequests += p.blocked.Load()
	return d
}

func (p *browserPolicy) Close() error { return p.proxy.Close() }

type requestBudget struct {
	maximum int64
	count   atomic.Int64
}

func (b *requestBudget) exceeded() bool { return b != nil && b.count.Add(1) > b.maximum }
