package acquisition

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type proxyConfig struct {
	resolve     func(context.Context, string) ([]net.IP, error)
	dialTimeout time.Duration
	maxBytes    int64
	maxRequests int64
}

type policyProxy struct {
	listener net.Listener
	server   *http.Server
	config   proxyConfig
	bytes    atomic.Int64
	requests atomic.Int64
	blocked  atomic.Int64
	close    sync.Once
}

func newPolicyProxy(cfg proxyConfig) (*policyProxy, error) {
	if cfg.resolve == nil {
		cfg.resolve = func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
	}
	if cfg.dialTimeout <= 0 {
		cfg.dialTimeout = 5 * time.Second
	}
	if cfg.maxBytes <= 0 {
		cfg.maxBytes = 32 << 20
	}
	if cfg.maxRequests <= 0 {
		cfg.maxRequests = 256
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &policyProxy{listener: listener, config: cfg}
	p.server = &http.Server{
		Handler:           p,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       10 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	go func() { _ = p.server.Serve(listener) }()
	return p, nil
}

func (p *policyProxy) URL() string { return "http://" + p.listener.Addr().String() }

func (p *policyProxy) Close() error {
	var err error
	p.close.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err = p.server.Shutdown(ctx)
		if err != nil {
			_ = p.server.Close()
		}
	})
	return err
}

func (p *policyProxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if p.requests.Add(1) > p.config.maxRequests {
		p.blocked.Add(1)
		http.Error(w, "request limit exceeded", http.StatusTooManyRequests)
		return
	}
	if req.Method == http.MethodConnect {
		p.connect(w, req)
		return
	}
	if req.URL == nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
		p.blocked.Add(1)
		http.Error(w, "blocked target", http.StatusForbidden)
		return
	}
	if err := ValidateURL(req.URL); err != nil {
		p.blocked.Add(1)
		http.Error(w, "blocked target", http.StatusForbidden)
		return
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           p.dialContext,
		DisableKeepAlives:     true,
		TLSHandshakeTimeout:   p.config.dialTimeout,
		ResponseHeaderTimeout: 15 * time.Second,
	}
	defer transport.CloseIdleConnections()
	out := req.Clone(req.Context())
	out.RequestURI = ""
	out.Header = req.Header.Clone()
	removeHopHeaders(out.Header)
	resp, err := transport.RoundTrip(out)
	if err != nil {
		if errors.Is(err, ErrBlockedTarget) {
			p.blocked.Add(1)
		}
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	removeHopHeaders(resp.Header)
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(&budgetWriter{proxy: p, writer: w}, resp.Body)
}

func (p *policyProxy) connect(w http.ResponseWriter, req *http.Request) {
	address := req.Host
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, "443")
	}
	u := &url.URL{Scheme: "https", Host: address}
	if err := ValidateURL(u); err != nil {
		p.blocked.Add(1)
		http.Error(w, "blocked target", http.StatusForbidden)
		return
	}
	upstream, err := p.dialContext(req.Context(), "tcp", address)
	if err != nil {
		if errors.Is(err, ErrBlockedTarget) {
			p.blocked.Add(1)
		}
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "proxy unavailable", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	defer client.Close()
	defer upstream.Close()
	if _, err = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err = buffered.Flush(); err != nil {
		return
	}
	if buffered.Reader.Buffered() > 0 {
		if _, err = io.CopyN(&budgetWriter{proxy: p, writer: upstream}, buffered, int64(buffered.Reader.Buffered())); err != nil {
			return
		}
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(&budgetWriter{proxy: p, writer: upstream}, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(&budgetWriter{proxy: p, writer: client}, upstream); done <- struct{}{} }()
	<-done
}

func (p *policyProxy) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrBlockedTarget
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return nil, ErrBlockedTarget
	}
	ips, err := p.config.resolve(ctx, strings.TrimSuffix(host, "."))
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, ErrBlockedTarget
	}
	for _, ip := range ips {
		if !isPublic(ip) {
			return nil, ErrBlockedTarget
		}
	}
	dialer := &net.Dialer{Timeout: p.config.dialTimeout}
	var lastErr error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrBlockedTarget
}

type budgetWriter struct {
	proxy  *policyProxy
	writer io.Writer
}

func (w *budgetWriter) Write(data []byte) (int, error) {
	for {
		used := w.proxy.bytes.Load()
		remaining := w.proxy.config.maxBytes - used
		if remaining <= 0 {
			return 0, ErrResponseTooLarge
		}
		want := int64(len(data))
		if want > remaining {
			want = remaining
		}
		if !w.proxy.bytes.CompareAndSwap(used, used+want) {
			continue
		}
		n, err := w.writer.Write(data[:want])
		if int64(n) < want {
			w.proxy.bytes.Add(int64(n) - want)
		}
		if err == nil && n < len(data) {
			err = ErrResponseTooLarge
		}
		return n, err
	}
}

func removeHopHeaders(header http.Header) {
	for _, key := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		header.Del(key)
	}
}

func (p *policyProxy) diagnostics() RenderDiagnostics {
	return RenderDiagnostics{Requests: p.requests.Load(), BlockedRequests: p.blocked.Load(), TransferredBytes: p.bytes.Load()}
}
