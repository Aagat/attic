package capture

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	stdhtml "html"
	"net/url"
	"strings"

	"attic/internal/acquisition"
	"golang.org/x/net/html"
)

// ReadingView derives a clean, offline article from a saved capture. It makes no
// network, AI or PDF calls and leaves the original capture unchanged. Pages that
// lack sufficient article content return an error; their static copy stays usable.
func ReadingView(savedHTML []byte, sourceURL string) ([]byte, error) {
	if len(savedHTML) == 0 || len(savedHTML) > maxArchiveBytes {
		return nil, errors.New("saved page exceeds reading limits")
	}
	doc, err := html.Parse(bytes.NewReader(savedHTML))
	if err != nil {
		return nil, err
	}
	images := map[string]string{}
	var prepare func(*html.Node)
	prepare = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "svg" {
			// SVG is safe in an image context and remains crisp without requiring a
			// browser or rasterizer to derive a reading view.
			if attribute(n, "xmlns") == "" {
				n.Attr = append(n.Attr, html.Attribute{Key: "xmlns", Val: "http://www.w3.org/2000/svg"})
			}
			var svg bytes.Buffer
			html.Render(&svg, n)
			n.Data = "img"
			n.DataAtom = 0
			n.Namespace = ""
			n.Attr = []html.Attribute{{Key: "src", Val: "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(svg.Bytes())}}
			for n.FirstChild != nil {
				n.RemoveChild(n.FirstChild)
			}
		}
		if n.Type == html.ElementNode && n.Data == "img" {
			for i, a := range n.Attr {
				if a.Key == "src" && strings.HasPrefix(a.Val, "data:image/") {
					// Extraction shares the PDF sanitizer's image-size and raster restrictions.
					// Keep archive images outside that transformation, then restore their exact
					// data URLs only into surviving img elements after extraction.
					token := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("attic-reading-image-%d", len(images))))
					images[token] = a.Val
					n.Attr[i].Val = token
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			prepare(child)
		}
	}
	prepare(doc)
	var prepared bytes.Buffer
	if err := html.Render(&prepared, doc); err != nil {
		return nil, err
	}
	candidate, err := acquisition.Extract(acquisition.Page{FinalURL: sourceURL, HTML: prepared.Bytes()})
	if err != nil {
		return nil, err
	}
	content, err := html.Parse(strings.NewReader(candidate.SemanticHTML))
	if err != nil {
		return nil, err
	}
	var restore func(*html.Node)
	restore = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			for i, a := range n.Attr {
				if a.Key == "src" {
					if original, ok := images[a.Val]; ok {
						n.Attr[i].Val = original
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			restore(c)
		}
	}
	restore(content)
	var body bytes.Buffer
	var renderBody func(*html.Node)
	renderBody = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "body" {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				html.Render(&body, c)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			renderBody(c)
		}
	}
	renderBody(content)
	source := ""
	if u, err := url.Parse(sourceURL); err == nil && acquisition.ValidateURL(u) == nil {
		source = `<p class="source"><a href="` + stdhtml.EscapeString(sourceURL) + `" rel="noreferrer">` + stdhtml.EscapeString(u.Hostname()) + `</a></p>`
	}
	title := stdhtml.EscapeString(candidate.Title)
	output := []byte(`<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="` + stdhtml.EscapeString(ReplayCSP) + `"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` + title + `</title><style>` + readingCSS + `</style></head><body><main><header><h1>` + title + `</h1>` + source + `</header>` + body.String() + `</main></body></html>`)
	if len(output) > maxArchiveBytes {
		return nil, errors.New("reading view exceeds size limit")
	}
	return output, nil
}

const readingCSS = `:root{color-scheme:light}body{margin:0;background:#fff;color:#181818;font:1.125rem/1.65 Georgia,"Times New Roman",serif}main{max-width:72ch;margin:auto;padding:2rem 1.25rem 4rem}h1,h2,h3{line-height:1.2}h1{font-size:2rem}a{color:inherit;text-decoration:underline}.source{font:0.9rem/1.5 system-ui,sans-serif;color:#555}img,svg{max-width:100%;height:auto}figure{margin:1.5rem 0}figcaption{font-size:0.9rem}pre{overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere;padding:1rem;background:#f3f3f3;border:1px solid #ddd;tab-size:4}code,kbd,samp{font:0.85em/1.5 ui-monospace,monospace}table{display:block;max-width:100%;overflow:auto;border-collapse:collapse;font-size:0.95rem}td,th{padding:.4rem .6rem;border:1px solid #bbb;text-align:left}blockquote{margin-left:0;padding-left:1rem;border-left:3px solid #aaa}@media(max-width:600px){main{padding:1rem}h1{font-size:1.6rem}}`
