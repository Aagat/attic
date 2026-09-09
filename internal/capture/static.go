package capture

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"attic/internal/acquisition"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ReplayCSP must also be sent as a response header. The embedded policy protects
// exported snapshots; sandbox additionally isolates snapshots served by Attic.
const ReplayCSP = "default-src 'none'; img-src data:; style-src 'unsafe-inline' data:; font-src data:; media-src data:; base-uri 'none'; form-action 'none'; frame-src 'none'; sandbox"
const maxArchiveBytes = 64 << 20

var cssURL = regexp.MustCompile(`(?i)url\(\s*(?:"([^"]*)"|'([^']*)'|([^)]*))\s*\)`)
var cssImport = regexp.MustCompile(`(?i)@import\s+(?:url\(\s*["']?([^"')]+)["']?\s*\)|"([^"]*)"|'([^']*)')([^;]*);?`)

type resource struct {
	kind string
	body []byte
}
type rewriter struct {
	assets  map[string]resource
	missing map[string]bool
	used    int
}

func convert(page acquisition.RenderedPage) (Result, error) {
	r := rewriter{assets: map[string]resource{}, missing: map[string]bool{}}
	root := page.DOM
	if len(page.MHTML) > maxArchiveBytes {
		return Result{}, errors.New("snapshot exceeds size limit")
	}
	if len(page.MHTML) > 0 {
		message, err := mail.ReadMessage(bytes.NewReader(page.MHTML))
		if err != nil {
			return Result{}, err
		}
		_, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
		if err != nil {
			return Result{}, err
		}
		if params["boundary"] == "" {
			return Result{}, errors.New("snapshot has no MIME boundary")
		}
		parts := multipart.NewReader(message.Body, params["boundary"])
		first := true
		total := 0
		for {
			part, err := parts.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return Result{}, err
			}
			var reader io.Reader = part
			if strings.EqualFold(part.Header.Get("Content-Transfer-Encoding"), "base64") {
				reader = base64.NewDecoder(base64.StdEncoding, reader)
			}
			body, err := io.ReadAll(io.LimitReader(reader, maxArchiveBytes+1))
			part.Close()
			if err != nil {
				return Result{}, err
			}
			total += len(body)
			if total > maxArchiveBytes {
				return Result{}, errors.New("snapshot resources exceed size limit")
			}
			kind, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
			location := absolute(page.FinalURL, part.Header.Get("Content-Location"))
			if kind == "text/html" && first {
				root = body
				if page.Title == "" {
					page.Title = documentTitle(body)
				}
				first = false
			}
			r.assets[location] = resource{kind, body}
		}
	}
	if len(root) == 0 {
		return Result{}, errors.New("empty snapshot")
	}
	doc, err := html.Parse(bytes.NewReader(root))
	if err != nil {
		return Result{}, err
	}
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			// Legacy layout and form wrappers can contain the whole article.
			// Neutralize the wrapper; dropping its subtree would lose that content.
			switch n.Data {
			case "center", "form":
				if n.Data == "center" {
					style := "text-align:center;" + attribute(n, "style")
					attrs := n.Attr[:0]
					for _, a := range n.Attr {
						if a.Key != "style" {
							attrs = append(attrs, a)
						}
					}
					n.Attr = append(attrs, html.Attribute{Key: "style", Val: style})
				}
				n.Data, n.DataAtom = "div", atom.Div
			case "font":
				n.Data, n.DataAtom = "span", atom.Span
			}
			if !allowedTag(n.Data) {
				if n.Data == "iframe" || n.Data == "object" || n.Data == "embed" {
					r.missing["unsupported embedded content"] = true
				}
				if n.Parent != nil {
					n.Parent.RemoveChild(n)
				}
				return
			}
			if n.Data == "link" {
				href := attribute(n, "href")
				if strings.EqualFold(attribute(n, "rel"), "stylesheet") {
					location := absolute(page.FinalURL, href)
					if asset, ok := r.assets[location]; ok && asset.kind == "text/css" {
						n.Data = "style"
						n.Attr = nil
						n.AppendChild(&html.Node{Type: html.TextNode, Data: r.css(string(asset.body), location)})
					} else {
						r.missing[location] = true
						n.Parent.RemoveChild(n)
						return
					}
				} else {
					n.Parent.RemoveChild(n)
					return
				}
			}
			attrs := n.Attr[:0]
			for _, a := range n.Attr {
				key := strings.ToLower(a.Key)
				switch {
				case key == "style":
					a.Val = r.css(a.Val, page.FinalURL)
				case key == "src" && n.Data == "img":
					a.Val = r.asset(a.Val, page.FinalURL)
				case key == "href" && n.Data == "a":
					if !strings.HasPrefix(a.Val, "#") {
						a.Val = absolute(page.FinalURL, a.Val)
						u, e := url.Parse(a.Val)
						if e != nil || (u.Scheme != "http" && u.Scheme != "https") {
							continue
						}
					}
				case strings.Contains(" id class title alt width height colspan rowspan scope lang dir role viewbox d fill stroke stroke-width points x y x1 x2 y1 y2 cx cy r rx ry transform xmlns preserveaspectratio ", " "+key+" "):
				default:
					continue
				}
				attrs = append(attrs, a)
			}
			n.Attr = attrs
			if n.Data == "style" {
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					if child.Type == html.TextNode {
						child.Data = r.css(child.Data, page.FinalURL)
					}
				}
				return
			}
		}
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
			text.WriteByte(' ')
		}
		for child := n.FirstChild; child != nil; {
			next := child.NextSibling
			walk(child)
			child = next
		}
	}
	walk(doc)
	// Insert before source styles/content, so the policy is effective immediately.
	var head *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "head" {
			head = n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	if head != nil {
		head.InsertBefore(&html.Node{Type: html.ElementNode, Data: "meta", Attr: []html.Attribute{{Key: "charset", Val: "utf-8"}}}, head.FirstChild)
		head.InsertBefore(&html.Node{Type: html.ElementNode, Data: "meta", Attr: []html.Attribute{{Key: "http-equiv", Val: "Content-Security-Policy"}, {Key: "content", Val: ReplayCSP}}}, head.FirstChild)
	}
	var out bytes.Buffer
	if err := html.Render(&out, doc); err != nil {
		return Result{}, err
	}
	if out.Len() > maxArchiveBytes {
		return Result{}, errors.New("inlined snapshot exceeds size limit")
	}
	result := Result{HTML: out.Bytes(), PlainText: strings.Join(strings.Fields(text.String()), " "), Title: page.Title, FinalURL: page.FinalURL, Status: "complete"}
	for missing := range r.missing {
		result.MissingResources = append(result.MissingResources, missing)
	}
	sort.Strings(result.MissingResources)
	if len(r.missing) > 0 || page.Diagnostics.BlockedRequests > 0 {
		result.Status = "partial"
	}
	if page.Truncated {
		result.Status = "partial"
		result.MissingResources = append(result.MissingResources, "Post text is truncated; expansion is still required")
	}
	if blocked(page, result.PlainText) {
		result.Status = "blocked"
	}
	return result, nil
}
func allowedTag(tag string) bool {
	return strings.Contains(" html head title body article main section nav aside header footer div span p h1 h2 h3 h4 h5 h6 br hr pre code kbd samp var b strong i em u s del ins sub sup small mark blockquote q cite abbr address time ul ol li dl dt dd table caption colgroup col thead tbody tfoot tr th td figure figcaption img picture a style link details summary svg g path rect circle ellipse line polyline polygon text tspan defs linearGradient radialGradient stop ", " "+tag+" ")
}
func attribute(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func absolute(base, raw string) string {
	b, e := url.Parse(base)
	u, e2 := url.Parse(strings.TrimSpace(raw))
	if e != nil || e2 != nil {
		return raw
	}
	return b.ResolveReference(u).String()
}
func (r *rewriter) asset(raw, base string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "data:") {
		if strings.HasPrefix(raw, "data:image/") || strings.HasPrefix(raw, "data:font/") {
			return raw
		}
		return ""
	}
	location := absolute(base, raw)
	a, ok := r.assets[location]
	if !ok || !(strings.HasPrefix(a.kind, "image/") || strings.HasPrefix(a.kind, "font/") || a.kind == "application/font-woff" || a.kind == "application/octet-stream") {
		r.missing[location] = true
		return ""
	}
	encoded := "data:" + a.kind + ";base64," + base64.StdEncoding.EncodeToString(a.body)
	r.used += len(encoded)
	if r.used > maxArchiveBytes {
		r.missing[location] = true
		return ""
	}
	return encoded
}
func (r *rewriter) css(css, base string) string { return r.stylesheet(css, base, 0) }
func (r *rewriter) stylesheet(css, base string, depth int) string {
	r.used += len(css)
	if r.used > maxArchiveBytes {
		r.missing[base] = true
		return ""
	}
	css = cssImport.ReplaceAllStringFunc(css, func(s string) string {
		match := cssImport.FindStringSubmatch(s)
		location := absolute(base, strings.TrimSpace(match[1]+match[2]+match[3]))
		asset, ok := r.assets[location]
		if !ok || asset.kind != "text/css" || depth >= 8 {
			r.missing[location] = true
			return ""
		}
		content := r.stylesheet(string(asset.body), location, depth+1)
		if media := strings.TrimSpace(match[4]); media != "" {
			content = "@media " + media + " {" + content + "}"
		}
		return content
	})
	css = cssURL.ReplaceAllStringFunc(css, func(s string) string {
		m := cssURL.FindStringSubmatch(s)
		raw := strings.TrimSpace(m[1] + m[2] + m[3])
		if strings.HasPrefix(raw, "#") {
			return s
		}
		return `url("` + r.asset(raw, base) + `")`
	})
	return strings.ReplaceAll(css, "<", `\3c `)
}

func documentTitle(body []byte) string {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	var title string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "title" && n.FirstChild != nil {
			title = strings.TrimSpace(n.FirstChild.Data)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return title
}
