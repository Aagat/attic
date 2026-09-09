package capture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	stdhtml "html"
	"net/url"
	"regexp"
	"strings"
	"time"

	"attic/internal/acquisition"
	"golang.org/x/net/html"
)

const maxEmbedBytes = 128 << 10

var xStatusPath = regexp.MustCompile(`^/(?:[A-Za-z0-9_]{1,15}/status|i/status|i/web/status)/([0-9]{1,20})(?:/(?:photo|video)/[0-9]+)?/?$`)

func xPostID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Port() != "" {
		return ""
	}
	switch strings.ToLower(u.Hostname()) {
	case "x.com", "www.x.com", "twitter.com", "www.twitter.com", "mobile.twitter.com", "mobile.x.com":
	default:
		return ""
	}
	match := xStatusPath.FindStringSubmatch(u.Path)
	if match == nil {
		return ""
	}
	return match[1]
}

func xEmbedURL(original string) string {
	id := xPostID(original)
	if id == "" {
		return ""
	}
	q := url.Values{"url": {"https://x.com/i/status/" + id}, "omit_script": {"true"}, "dnt": {"true"}}
	return "https://publish.x.com/oembed?" + q.Encode()
}

// xEmbed preserves the static fallback supplied by X, never the live widget.
// Even an apparently short post cannot prove that media or thread context is complete.
func (c *Capturer) xEmbed(ctx context.Context, original, endpoint string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	fetch := c.fetchJSON
	if fetch == nil {
		fetch = acquisition.NewFetcherWithConfig(acquisition.FetchConfig{Timeout: 10 * time.Second, MaxBytes: maxEmbedBytes, MaxRedirects: 2}).FetchJSON
	}
	page, err := fetch(ctx, endpoint)
	if err != nil {
		return Result{}, err
	}
	if len(page.HTML) > maxEmbedBytes {
		return Result{}, acquisition.ErrResponseTooLarge
	}
	var embed struct {
		URL    string `json:"url"`
		HTML   string `json:"html"`
		Author string `json:"author_name"`
		Type   string `json:"type"`
	}
	if err := json.Unmarshal(page.HTML, &embed); err != nil {
		return Result{}, err
	}
	if embed.Type != "rich" || xPostID(embed.URL) != xPostID(original) {
		return Result{}, errors.New("X embed does not match requested post")
	}
	doc, err := html.Parse(strings.NewReader(embed.HTML))
	if err != nil {
		return Result{}, err
	}
	var quote *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if quote != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "blockquote" && strings.Contains(" "+attribute(n, "class")+" ", " twitter-tweet ") {
			quote = n
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			find(child)
		}
	}
	find(doc)
	if quote == nil {
		return Result{}, errors.New("X embed has no post")
	}
	var prose strings.Builder
	var matchingLink bool
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, paragraph bool) {
		if n.Type == html.ElementNode {
			if n.Data == "script" || n.Data == "style" {
				return
			}
			if n.Data == "p" {
				paragraph = true
			}
			if n.Data == "a" && xPostID(attribute(n, "href")) == xPostID(original) {
				matchingLink = true
			}
		}
		if n.Type == html.TextNode && paragraph {
			prose.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child, paragraph)
		}
	}
	walk(quote, false)
	if !matchingLink || strings.TrimSpace(prose.String()) == "" {
		return Result{}, errors.New("X embed has no matching post text")
	}
	var body bytes.Buffer
	if err := html.Render(&body, quote); err != nil {
		return Result{}, err
	}
	title := "Post on X"
	if author := strings.TrimSpace(embed.Author); author != "" {
		title = "Post by " + author
	}
	dom := "<!doctype html><html><head><title>" + stdhtml.EscapeString(title) + "</title></head><body><article>" + body.String() + "</article></body></html>"
	result, err := convert(acquisition.RenderedPage{DOM: []byte(dom), Title: title, FinalURL: embed.URL, Status: 200})
	if err != nil {
		return Result{}, err
	}
	result.OriginalURL = original
	result.Status = "partial"
	result.MissingResources = append(result.MissingResources, "X embed only: full post, media and thread context are not verified")
	truncated := strings.Contains(prose.String(), "…") || strings.Contains(prose.String(), "...")
	if truncated {
		result.MissingResources = append(result.MissingResources, "X embed text may be truncated; full post is missing")
	}
	return result, nil
}
