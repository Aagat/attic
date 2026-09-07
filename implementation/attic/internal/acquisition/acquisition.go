// Package acquisition retrieves public pages and produces bounded article candidates.
package acquisition

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode"

	"attic/internal/sanitize"
	"golang.org/x/net/html"
)

var ErrBlockedTarget = errors.New("target is not a public HTTP(S) address")
var ErrResponseTooLarge = errors.New("response exceeds configured limit")
var ErrTooManyRedirects = errors.New("too many redirects")

const (
	defaultMaxResponseBytes = int64(4 << 20)
	defaultFetchTimeout     = 30 * time.Second
	defaultMaxRedirects     = 5
)

// FetchConfig contains immutable limits for Fetcher. Resolve is intended for
// controlled deployments and tests; every address it returns is still checked.
type FetchConfig struct {
	MaxBytes     int64
	Timeout      time.Duration
	DialTimeout  time.Duration
	Resolve      func(context.Context, string) ([]net.IP, error)
	MaxRedirects int
}

// Fetcher is safe for concurrent use. It deliberately does not accept an
// arbitrary http.Client, because doing so could bypass its address policy.
type Fetcher struct {
	client   *http.Client
	maxBytes int64
}

func NewFetcher() *Fetcher                          { return newFetcher(FetchConfig{}, nil) }
func NewFetcherWithConfig(cfg FetchConfig) *Fetcher { return newFetcher(cfg, nil) }

// dialOverride is an in-package deterministic test seam. Public constructors
// always connect to the validated IP address themselves.
func newFetcher(cfg FetchConfig, dialOverride func(context.Context, string, string) (net.Conn, error)) *Fetcher {
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = defaultMaxResponseBytes
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultFetchTimeout
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 5 * time.Second
	}
	if cfg.MaxRedirects <= 0 {
		cfg.MaxRedirects = defaultMaxRedirects
	}
	resolve := cfg.Resolve
	if resolve == nil {
		resolve = func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
	}
	dialer := &net.Dialer{Timeout: cfg.DialTimeout}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := resolve(ctx, host)
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
		var lastErr error
		for _, ip := range ips {
			validated := net.JoinHostPort(ip.String(), port)
			if dialOverride != nil {
				if conn, e := dialOverride(ctx, network, validated); e == nil {
					return conn, nil
				} else {
					lastErr = e
				}
				continue
			}
			if conn, e := dialer.DialContext(ctx, network, validated); e == nil {
				return conn, nil
			} else {
				lastErr = e
			}
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, ErrBlockedTarget
	}
	transport := &http.Transport{Proxy: nil, DialContext: dial, ForceAttemptHTTP2: true, TLSHandshakeTimeout: cfg.DialTimeout, ResponseHeaderTimeout: cfg.Timeout}
	client := &http.Client{
		Timeout:   cfg.Timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > cfg.MaxRedirects {
				return ErrTooManyRedirects
			}
			return ValidateURL(req.URL)
		},
	}
	return &Fetcher{client: client, maxBytes: cfg.MaxBytes}
}

func ValidateURL(u *url.URL) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Hostname() == "" {
		return ErrBlockedTarget
	}
	h := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return ErrBlockedTarget
	}
	if ip, err := netip.ParseAddr(h); err == nil && !isPublic(ip.AsSlice()) {
		return ErrBlockedTarget
	}
	return nil
}

func isPublic(b []byte) bool {
	ip, ok := netip.AddrFromSlice(b)
	if !ok || !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	blocked := []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("2001:2::/48"), netip.MustParsePrefix("2001:10::/28"), netip.MustParsePrefix("2001:20::/28"),
		netip.MustParsePrefix("2002::/16"),
	}
	for _, prefix := range blocked {
		if prefix.Contains(ip.Unmap()) {
			return false
		}
	}
	return true
}

func (f *Fetcher) Fetch(ctx context.Context, raw string) (Page, error) {
	return f.fetch(ctx, raw, false)
}

// FetchJSON uses the same address, redirect, time and size limits as page fetching.
func (f *Fetcher) FetchJSON(ctx context.Context, raw string) (Page, error) {
	return f.fetch(ctx, raw, true)
}

func (f *Fetcher) fetch(ctx context.Context, raw string, jsonResponse bool) (Page, error) {
	if f == nil || f.client == nil {
		return Page{}, errors.New("nil fetcher")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Page{}, ErrBlockedTarget
	}
	if err = ValidateURL(u); err != nil {
		return Page{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Page{}, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	if jsonResponse {
		req.Header.Set("Accept", "application/json")
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return Page{}, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return Page{}, fmt.Errorf("fetch status %d", resp.StatusCode)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if (jsonResponse && !strings.Contains(ct, "application/json")) || (!jsonResponse && ct != "" && !strings.Contains(ct, "text/html") && !strings.Contains(ct, "application/xhtml+xml")) {
		return Page{}, errors.New("unsupported content type")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return Page{}, err
	}
	if int64(len(b)) > f.maxBytes {
		return Page{}, ErrResponseTooLarge
	}
	return Page{FinalURL: resp.Request.URL.String(), Status: resp.StatusCode, HTML: b}, nil
}

type Page struct {
	FinalURL string
	Status   int
	HTML     []byte
}

type Candidate struct {
	Title, Author, SiteName, PublicationDate, Description, Language string
	CanonicalURL, SemanticHTML, PlainText                           string
	ExtractionMethod                                                string
}

const (
	maxMetadataTitleRunes       = 500
	maxMetadataAuthorRunes      = 300
	maxMetadataSiteNameRunes    = 300
	maxMetadataDateRunes        = 128
	maxMetadataDescriptionRunes = 2000
	maxMetadataLanguageRunes    = 32
	maxCanonicalURLBytes        = 8192
)

func Extract(p Page) (Candidate, error) {
	doc, err := html.Parse(strings.NewReader(string(p.HTML)))
	if err != nil {
		return Candidate{}, err
	}
	if nodes, depth := treeSize(doc, 0); nodes > 100000 || depth > 256 {
		return Candidate{}, errors.New("document exceeds DOM limits")
	}
	c := Candidate{CanonicalURL: boundedCanonicalFallback(p.FinalURL), ExtractionMethod: "deterministic"}
	var documentTitle, htmlLanguage, openGraphLanguage string
	var openGraphTitle, twitterTitle, standardMetaTitle string
	var articleAuthor, standardAuthor, openGraphSiteName, standardSiteName string
	var openGraphDescription, standardDescription string
	var articlePublicationDate, standardPublicationDate string
	canonicalSet := false
	var best *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "article" || n.Data == "main") && (best == nil || textLen(n) > textLen(best)) {
			best = n
		}
		for x := n.FirstChild; x != nil; x = x.NextSibling {
			walk(x)
		}
	}
	walk(doc)
	if best == nil {
		best = doc
	}
	var title func(*html.Node)
	title = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "title" && documentTitle == "" {
			documentTitle = nodeText(n)
		}
		for x := n.FirstChild; x != nil; x = x.NextSibling {
			title(x)
		}
	}
	title(doc)
	var metadata func(*html.Node)
	metadata = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "html" && htmlLanguage == "" {
				htmlLanguage = attr(n, "lang")
			}
			if n.Data == "link" && !canonicalSet && relContains(attr(n, "rel"), "canonical") {
				if value := absoluteHTTPURL(p.FinalURL, attr(n, "href")); value != "" {
					c.CanonicalURL = value
					canonicalSet = true
				}
			}
			if n.Data == "meta" {
				value := attr(n, "content")
				for _, key := range metadataKeys(n) {
					switch key {
					case "article:author":
						setFirst(&articleAuthor, value)
					case "author":
						setFirst(&standardAuthor, value)
					case "og:site_name":
						setFirst(&openGraphSiteName, value)
					case "application-name":
						setFirst(&standardSiteName, value)
					case "og:description":
						setFirst(&openGraphDescription, value)
					case "description":
						setFirst(&standardDescription, value)
					case "article:published_time":
						setFirst(&articlePublicationDate, value)
					case "date", "datepublished", "date-published", "pubdate":
						setFirst(&standardPublicationDate, value)
					case "og:locale", "content-language", "language":
						setFirst(&openGraphLanguage, strings.ReplaceAll(value, "_", "-"))
					case "og:title":
						setFirst(&openGraphTitle, value)
					case "twitter:title":
						setFirst(&twitterTitle, value)
					case "title":
						setFirst(&standardMetaTitle, value)
					}
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			metadata(child)
		}
	}
	metadata(doc)
	extraAuthor, extraPublisher, extraDate := supplementalMetadata(doc)
	headingTitle := firstElementText(best, "h1")
	c.Title = boundedMetadata(firstNonEmpty(openGraphTitle, twitterTitle, standardMetaTitle, documentTitle, headingTitle), maxMetadataTitleRunes)
	c.Author = boundedMetadata(firstNonEmpty(articleAuthor, extraAuthor, standardAuthor), maxMetadataAuthorRunes)
	c.SiteName = boundedMetadata(firstNonEmpty(openGraphSiteName, extraPublisher, standardSiteName), maxMetadataSiteNameRunes)
	c.PublicationDate = boundedMetadata(firstNonEmpty(articlePublicationDate, extraDate, standardPublicationDate), maxMetadataDateRunes)
	c.Description = boundedMetadata(firstNonEmpty(openGraphDescription, standardDescription), maxMetadataDescriptionRunes)
	c.Language = boundedMetadata(firstNonEmpty(htmlLanguage, openGraphLanguage), maxMetadataLanguageRunes)
	safe, err := sanitize.ArticleHTML(render(best), c.Title)
	if err != nil {
		return Candidate{}, err
	}
	safeDocument, err := html.Parse(strings.NewReader(safe))
	if err != nil {
		return Candidate{}, err
	}
	plain := strings.TrimSpace(nodeText(safeDocument))
	if len([]rune(plain)) < 80 {
		return Candidate{}, errors.New("insufficient article content")
	}
	c.SemanticHTML, c.PlainText = safe, plain
	return c, nil
}

func attr(n *html.Node, name string) string {
	for _, attribute := range n.Attr {
		if strings.EqualFold(attribute.Key, name) {
			return strings.TrimSpace(attribute.Val)
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func setFirst(target *string, value string) {
	if target != nil && *target == "" && strings.TrimSpace(value) != "" {
		*target = value
	}
}

func metadataKeys(n *html.Node) []string {
	keys := make([]string, 0, 4)
	for _, attribute := range []string{"property", "name", "itemprop", "http-equiv"} {
		if value := strings.ToLower(strings.TrimSpace(attr(n, attribute))); value != "" {
			keys = append(keys, value)
		}
	}
	return keys
}

func firstElementText(root *html.Node, element string) string {
	if root == nil {
		return ""
	}
	if root.Type == html.ElementNode && root.Data == element {
		return nodeText(root)
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if value := firstElementText(child, element); strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func relContains(value, wanted string) bool {
	for _, relation := range strings.Fields(value) {
		if strings.EqualFold(relation, wanted) {
			return true
		}
	}
	return false
}

func absoluteHTTPURL(base, value string) string {
	baseURL, baseErr := url.Parse(base)
	reference, refErr := url.Parse(strings.TrimSpace(value))
	if baseErr != nil || refErr != nil || baseURL == nil || reference == nil {
		return ""
	}
	resolved := baseURL.ResolveReference(reference)
	if (!strings.EqualFold(resolved.Scheme, "http") && !strings.EqualFold(resolved.Scheme, "https")) || resolved.User != nil || resolved.Hostname() == "" {
		return ""
	}
	resolved.Fragment = ""
	value = resolved.String()
	if len(value) > maxCanonicalURLBytes {
		return ""
	}
	return value
}

func boundedCanonicalFallback(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > maxCanonicalURLBytes {
		return ""
	}
	return value
}

func boundedMetadata(value string, limit int) string {
	var b strings.Builder
	spacePending := false
	for _, r := range strings.TrimSpace(value) {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			spacePending = b.Len() > 0
			continue
		}
		if spacePending {
			b.WriteByte(' ')
			spacePending = false
		}
		b.WriteRune(r)
	}
	runes := []rune(b.String())
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

func treeSize(n *html.Node, depth int) (int, int) {
	count, maxDepth := 1, depth
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		c, d := treeSize(child, depth+1)
		count += c
		if d > maxDepth {
			maxDepth = d
		}
	}
	return count, maxDepth
}
func textLen(n *html.Node) int { return len([]rune(nodeText(n))) }
func nodeText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for x := n.FirstChild; x != nil; x = x.NextSibling {
		if text := nodeText(x); text != "" {
			b.WriteString(text)
			b.WriteByte(' ')
		}
	}
	return b.String()
}
func render(n *html.Node) string { var b strings.Builder; html.Render(&b, n); return b.String() }
