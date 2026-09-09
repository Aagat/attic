package capture

import (
	"bytes"
	"context"
	"errors"
	stdhtml "html"
	"net/url"
	"strings"
	"time"

	"attic/internal/acquisition"
	"golang.org/x/net/html"
)

const maxMirrorBytes = 2 << 20

// Only derive a mirror URL from a recognized post permalink. Tracking parameters,
// fragments and media navigation are never forwarded to the mirror.
func xMirrorURL(original string) string {
	id := xMirrorPostID(original)
	if id == "" {
		return ""
	}
	u, _ := url.Parse(original)
	user := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")[0]
	return "https://xcancel.com/" + user + "/status/" + id
}

// Mirror recognition is separate so oEmbed response validation still accepts only X.
func xMirrorPostID(raw string) string {
	if id := xPostID(raw); id != "" {
		return id
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "xcancel.com") || u.User != nil {
		return ""
	}
	u.Host = "x.com"
	return xPostID(u.String())
}

func (c *Capturer) xMirror(ctx context.Context, original, endpoint string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	fetch := c.fetchMirror
	if fetch == nil {
		fetch = acquisition.NewFetcherWithConfig(acquisition.FetchConfig{Timeout: 10 * time.Second, MaxBytes: maxMirrorBytes, MaxRedirects: 2}).Fetch
	}
	page, err := fetch(ctx, endpoint)
	if err != nil {
		return Result{}, err
	}
	if len(page.HTML) > maxMirrorBytes {
		return Result{}, acquisition.ErrResponseTooLarge
	}
	final, err := url.Parse(page.FinalURL)
	if err != nil || final.Scheme != "https" || final.Host != "xcancel.com" || final.User != nil || page.Status != 200 {
		return Result{}, errors.New("X mirror returned an unexpected page")
	}
	doc, err := html.Parse(bytes.NewReader(page.HTML))
	if err != nil {
		return Result{}, err
	}
	main := mirrorClass(doc, "main-tweet")
	if main == nil {
		return Result{}, errors.New("X mirror has no main post")
	}
	// Check the main post's own date permalink, not an arbitrary matching link in
	// quoted content or replies. Their identifiers must never validate another post.
	header := mirrorClass(main, "tweet-header")
	date := mirrorClass(header, "tweet-date")
	link := mirrorElement(date, "a")
	if link == nil {
		return Result{}, errors.New("X mirror has no post permalink")
	}
	permalink, err := url.Parse(absolute(page.FinalURL, attribute(link, "href")))
	if err != nil || permalink.Scheme != "https" || permalink.Host != "xcancel.com" || permalink.User != nil {
		return Result{}, errors.New("X mirror has an invalid post permalink")
	}
	permalink.Host = "x.com"
	if xPostID(permalink.String()) != xMirrorPostID(original) {
		return Result{}, errors.New("X mirror does not match requested post")
	}
	content := mirrorClass(main, "tweet-content")
	if content == nil || strings.TrimSpace(mirrorText(content)) == "" {
		return Result{}, errors.New("X mirror has no post text")
	}
	author := strings.TrimSpace(mirrorText(mirrorClass(header, "fullname")))
	username := strings.TrimSpace(mirrorText(mirrorClass(header, "username")))
	title := "Post on X"
	if author != "" {
		title = "Post by " + author
	}
	var body bytes.Buffer
	body.WriteString("<!doctype html><html><head><title>" + stdhtml.EscapeString(title) + "</title></head><body><article><header><p rel=author>" + stdhtml.EscapeString(author+" "+username) + "</p><p>")
	published := attribute(link, "title")
	if published == "" {
		published = mirrorText(link)
	}
	body.WriteString(stdhtml.EscapeString(published) + "</p></header>")
	if err := html.Render(&body, content); err != nil {
		return Result{}, err
	}
	if attachments := mirrorClass(main, "attachments"); attachments != nil {
		if err := html.Render(&body, attachments); err != nil {
			return Result{}, err
		}
	}
	body.WriteString("</article></body></html>")
	result, err := convert(acquisition.RenderedPage{DOM: body.Bytes(), Title: title, FinalURL: page.FinalURL, Status: 200})
	if err != nil {
		return Result{}, err
	}
	if result.Status == "blocked" {
		return Result{}, errors.New("X mirror returned a challenge")
	}
	result.OriginalURL = original
	result.Status = "partial"
	result.MissingResources = append(result.MissingResources, "X mirror: main post preserved; media and thread context are not verified")
	return result, nil
}

func mirrorClass(n *html.Node, class string) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && strings.Contains(" "+attribute(n, "class")+" ", " "+class+" ") {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := mirrorClass(child, class); found != nil {
			return found
		}
	}
	return nil
}
func mirrorElement(n *html.Node, tag string) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := mirrorElement(child, tag); found != nil {
			return found
		}
	}
	return nil
}
func mirrorText(n *html.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	if n.Data == "script" || n.Data == "style" {
		return ""
	}
	var text strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		text.WriteString(mirrorText(child))
	}
	return text.String()
}
