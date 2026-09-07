package sanitize

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
)

// ArticleHTML separates article content from page navigation and presentation.
// Unlike the security sanitizer, it deliberately removes publication chrome.
// Code text is joined without inserting spaces between syntax-highlight spans.
func ArticleHTML(input, title string) (string, error) {
	root, err := html.Parse(strings.NewReader(input))
	if err != nil {
		return "", ErrMalformedHTML
	}
	var best *html.Node
	var choose func(*html.Node)
	choose = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "article" && (best == nil || len(articleText(n)) > len(articleText(best))) {
			best = n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			choose(c)
		}
	}
	choose(root)
	if best != nil {
		root = best
	}
	var clean func(*html.Node)
	clean = func(n *html.Node) {
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling
			drop := false
			if c.Type == html.ElementNode {
				switch c.Data {
				case "nav", "footer":
					drop = true
				case "header":
					drop = hasHeading(c)
				case "h1":
					drop = normalizeTitle(articleText(c)) == normalizeTitle(title)
				case "details":
					for child := c.FirstChild; child != nil; child = child.NextSibling {
						if child.Data == "summary" {
							label := normalizeTitle(articleText(child))
							drop = label == "table of contents" || label == "contents"
						}
					}
				case "a":
					label := strings.TrimSpace(articleText(c))
					drop = label == "¶" || label == "#" || label == "§" || label == ""
				}
				for _, a := range c.Attr {
					if a.Key == "role" && (a.Val == "navigation" || a.Val == "doc-toc") {
						drop = true
					}
					if a.Key == "class" || a.Key == "id" {
						for _, token := range strings.Fields(strings.ToLower(a.Val)) {
							switch token {
							case "toc", "table-of-contents", "post-meta", "post-metadata", "article-meta", "share-buttons", "social-share", "related-posts", "comments", "sidebar":
								drop = true
							}
						}
					}
				}
				if c.Data == "figure" && !hasElement(c, "img") {
					c.Data = "div"
					c.DataAtom = 0
				}
				if c.Data == "pre" {
					value := articleText(c)
					for child := c.FirstChild; child != nil; {
						after := child.NextSibling
						c.RemoveChild(child)
						child = after
					}
					c.AppendChild(&html.Node{Type: html.TextNode, Data: strings.Trim(value, "\n")})
				}
			}
			if drop {
				n.RemoveChild(c)
			} else {
				clean(c)
			}
			c = next
		}
	}
	clean(root)
	var b bytes.Buffer
	if html.Render(&b, root) != nil {
		return "", ErrMalformedHTML
	}
	return SanitizeHTML(b.String())
}

func articleText(n *html.Node) string {
	if n.Type == html.ElementNode {
		if _, drop := droppedElements[n.Data]; drop {
			return ""
		}
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(articleText(c))
	}
	return b.String()
}
func normalizeTitle(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
func hasHeading(n *html.Node) bool {
	if n.Type == html.ElementNode && n.Data == "h1" {
		return true
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if hasHeading(c) {
			return true
		}
	}
	return false
}

func hasElement(n *html.Node, tag string) bool {
	if n.Type == html.ElementNode && n.Data == tag {
		return true
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if hasElement(c, tag) {
			return true
		}
	}
	return false
}
