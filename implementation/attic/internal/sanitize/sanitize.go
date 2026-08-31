// Package sanitize turns untrusted article markup into a small, inert HTML
// subset suitable for persistence and later rendering.
package sanitize

import (
	"bytes"
	"encoding/base64"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var (
	// ErrNoSemanticContent means that the input contained no text after
	// dangerous and non-semantic nodes were removed.
	ErrNoSemanticContent = errors.New("sanitized HTML has no semantic content")
	// ErrMalformedHTML is reserved for tokenizer errors. The HTML parser
	// intentionally recovers ordinary malformed markup before this boundary.
	ErrMalformedHTML = errors.New("HTML could not be parsed")
)

const (
	maxInlineImageBytes = 1 << 20
	maxAttributeRunes   = 1000
	maxDimension        = 4096
	maxSpan             = 1000
)

var allowedElements = map[string]struct{}{
	"a":          {},
	"abbr":       {},
	"address":    {},
	"article":    {},
	"aside":      {},
	"b":          {},
	"bdi":        {},
	"bdo":        {},
	"blockquote": {},
	"br":         {},
	"caption":    {},
	"cite":       {},
	"code":       {},
	"col":        {},
	"colgroup":   {},
	"dd":         {},
	"del":        {},
	"details":    {},
	"div":        {},
	"dl":         {},
	"dt":         {},
	"em":         {},
	"figcaption": {},
	"figure":     {},
	"footer":     {},
	"h1":         {},
	"h2":         {},
	"h3":         {},
	"h4":         {},
	"h5":         {},
	"h6":         {},
	"header":     {},
	"hgroup":     {},
	"hr":         {},
	"i":          {},
	"img":        {},
	"ins":        {},
	"kbd":        {},
	"li":         {},
	"main":       {},
	"mark":       {},
	"nav":        {},
	"ol":         {},
	"p":          {},
	"pre":        {},
	"q":          {},
	"rp":         {},
	"rt":         {},
	"ruby":       {},
	"s":          {},
	"samp":       {},
	"section":    {},
	"small":      {},
	"span":       {},
	"strong":     {},
	"sub":        {},
	"summary":    {},
	"sup":        {},
	"table":      {},
	"tbody":      {},
	"td":         {},
	"tfoot":      {},
	"th":         {},
	"thead":      {},
	"time":       {},
	"tr":         {},
	"u":          {},
	"ul":         {},
	"var":        {},
	"wbr":        {},
}

// These elements are removed with their complete subtree. Keeping text from
// an active/raw-text element would make it possible for parser recovery to
// turn an unsafe payload into apparently ordinary article content.
var droppedElements = map[string]struct{}{
	"applet":   {},
	"audio":    {},
	"base":     {},
	"button":   {},
	"canvas":   {},
	"embed":    {},
	"fieldset": {},
	"form":     {},
	"frame":    {},
	"frameset": {},
	"head":     {},
	"iframe":   {},
	"input":    {},
	"link":     {},
	"map":      {},
	"math":     {},
	"meta":     {},
	"noscript": {},
	"object":   {},
	"optgroup": {},
	"option":   {},
	"param":    {},
	"picture":  {},
	"portal":   {},
	"script":   {},
	"select":   {},
	"slot":     {},
	"source":   {},
	"style":    {},
	"svg":      {},
	"template": {},
	"textarea": {},
	"track":    {},
	"video":    {},
	"xmp":      {},
}

var voidElements = map[string]struct{}{
	"br":  {},
	"col": {},
	"hr":  {},
	"img": {},
	"wbr": {},
}

// SanitizeHTML parses input as an HTML fragment, removes active and unknown
// markup, validates the small set of URL-bearing attributes, and renders a
// normalized fragment. Text under unknown presentation wrappers is retained;
// text under active/raw or media elements is discarded with its subtree.
func SanitizeHTML(input string) (string, error) {
	context := &html.Node{Type: html.ElementNode, Data: "article", DataAtom: atom.Article}
	nodes, err := html.ParseFragment(strings.NewReader(input), context)
	if err != nil {
		return "", errors.Join(ErrMalformedHTML, err)
	}

	clean := make([]*html.Node, 0, len(nodes))
	for _, node := range nodes {
		clean = append(clean, sanitizeNode(node)...)
	}
	if !hasSemanticText(clean) {
		return "", ErrNoSemanticContent
	}

	var output bytes.Buffer
	for _, node := range clean {
		if err := html.Render(&output, node); err != nil {
			return "", errors.Join(ErrMalformedHTML, err)
		}
	}
	return strings.TrimSpace(output.String()), nil
}

func sanitizeNode(node *html.Node) []*html.Node {
	if node == nil {
		return nil
	}
	switch node.Type {
	case html.TextNode:
		return []*html.Node{{Type: html.TextNode, Data: node.Data}}
	case html.ElementNode:
		tag := strings.ToLower(node.Data)
		// Namespace-bearing nodes are SVG/MathML or another non-HTML
		// namespace. Rejecting the subtree also prevents namespace
		// confusion when an element happens to share an HTML tag name.
		if node.Namespace != "" {
			return nil
		}
		if _, drop := droppedElements[tag]; drop {
			return nil
		}
		if _, allowed := allowedElements[tag]; !allowed {
			return sanitizeChildren(node)
		}

		attrs, keep := sanitizeAttributes(tag, node.Attr)
		if !keep {
			return nil
		}
		clean := &html.Node{
			Type:      html.ElementNode,
			Data:      tag,
			DataAtom:  atom.Lookup([]byte(tag)),
			Namespace: "",
			Attr:      attrs,
		}
		if _, isVoid := voidElements[tag]; !isVoid {
			for _, child := range sanitizeChildren(node) {
				clean.AppendChild(child)
			}
		}
		return []*html.Node{clean}
	default:
		// Comments, doctypes and raw nodes never belong in persisted article
		// content. Parser-produced raw nodes are not expected, but dropping
		// them also keeps this function safe if callers change parser options.
		return nil
	}
}

func sanitizeChildren(node *html.Node) []*html.Node {
	clean := make([]*html.Node, 0)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		clean = append(clean, sanitizeNode(child)...)
	}
	return clean
}

func sanitizeAttributes(tag string, attrs []html.Attribute) ([]html.Attribute, bool) {
	clean := make([]html.Attribute, 0, len(attrs))
	seen := make(map[string]struct{}, len(attrs))
	imageSource := ""
	for _, attr := range attrs {
		key := strings.ToLower(strings.TrimSpace(attr.Key))
		if key == "" {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		value := strings.TrimSpace(attr.Val)

		switch key {
		case "href":
			if tag != "a" {
				continue
			}
			if safeURL, ok := absoluteHTTPURL(value); ok {
				clean = append(clean, html.Attribute{Key: key, Val: safeURL})
			}
		case "src":
			if tag != "img" {
				continue
			}
			if safeImage, ok := inlineImageURL(value); ok {
				imageSource = safeImage
			}
		case "alt":
			if tag == "img" {
				if safeValue, ok := boundedAttribute(value, maxAttributeRunes); ok {
					clean = append(clean, html.Attribute{Key: key, Val: safeValue})
				}
			}
		case "title":
			if safeValue, ok := boundedAttribute(value, maxAttributeRunes); ok {
				clean = append(clean, html.Attribute{Key: key, Val: safeValue})
			}
		case "lang":
			if safeLanguage(value) {
				clean = append(clean, html.Attribute{Key: key, Val: strings.ToLower(value)})
			}
		case "dir":
			value = strings.ToLower(value)
			if value == "ltr" || value == "rtl" || value == "auto" {
				clean = append(clean, html.Attribute{Key: key, Val: value})
			}
		case "rel":
			if tag == "a" {
				if safeValue, ok := safeRel(value); ok {
					clean = append(clean, html.Attribute{Key: key, Val: safeValue})
				}
			}
		case "width", "height":
			if tag == "img" {
				if safeValue, ok := boundedInteger(value, 0, maxDimension); ok {
					clean = append(clean, html.Attribute{Key: key, Val: safeValue})
				}
			}
		case "datetime":
			if tag == "time" {
				if safeValue, ok := boundedAttribute(value, 100); ok {
					clean = append(clean, html.Attribute{Key: key, Val: safeValue})
				}
			}
		case "start":
			if tag == "ol" {
				if safeValue, ok := boundedInteger(value, -maxSpan, maxSpan); ok {
					clean = append(clean, html.Attribute{Key: key, Val: safeValue})
				}
			}
		case "value":
			if tag == "li" {
				if safeValue, ok := boundedInteger(value, -maxSpan, maxSpan); ok {
					clean = append(clean, html.Attribute{Key: key, Val: safeValue})
				}
			}
		case "colspan", "rowspan":
			if tag == "td" || tag == "th" {
				if safeValue, ok := boundedInteger(value, 1, maxSpan); ok {
					clean = append(clean, html.Attribute{Key: key, Val: safeValue})
				}
			}
		case "scope":
			if tag == "th" {
				value = strings.ToLower(value)
				if value == "row" || value == "col" || value == "rowgroup" || value == "colgroup" {
					clean = append(clean, html.Attribute{Key: key, Val: value})
				}
			}
		case "reversed":
			if tag == "ol" && value == "" {
				clean = append(clean, html.Attribute{Key: key, Val: ""})
			}
		}
	}

	if tag == "img" {
		if imageSource == "" {
			return nil, false
		}
		clean = append(clean, html.Attribute{Key: "src", Val: imageSource})
	}
	sort.SliceStable(clean, func(i, j int) bool { return clean[i].Key < clean[j].Key })
	return clean, true
}

func absoluteHTTPURL(value string) (string, bool) {
	if value == "" || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil {
		return "", false
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	return parsed.String(), true
}

func inlineImageURL(value string) (string, bool) {
	value = strings.TrimSpace(value)
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 {
		return "", false
	}
	var mediaType string
	switch strings.ToLower(parts[0]) {
	case "data:image/png;base64":
		mediaType = "png"
	case "data:image/jpeg;base64":
		mediaType = "jpeg"
	case "data:image/webp;base64":
		mediaType = "webp"
	default:
		return "", false
	}
	payload := parts[1]
	if payload == "" || len(payload) > ((maxInlineImageBytes+2)/3)*4+4 || !base64Payload(payload) {
		return "", false
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(payload)
	if err != nil {
		decoded, err = base64.RawStdEncoding.Strict().DecodeString(payload)
	}
	if err != nil || len(decoded) == 0 || len(decoded) > maxInlineImageBytes {
		return "", false
	}
	return "data:image/" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(decoded), true
}

func base64Payload(value string) bool {
	padding := 0
	for i, r := range value {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '+', r == '/':
			if padding != 0 {
				return false
			}
		case r == '=':
			padding++
			if padding > 2 || i < len(value)-2 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func boundedAttribute(value string, maxRunes int) (string, bool) {
	if utf8.RuneCountInString(value) > maxRunes || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", false
	}
	return value, true
}

func safeLanguage(value string) bool {
	if value == "" || utf8.RuneCountInString(value) > 35 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func safeRel(value string) (string, bool) {
	allowed := map[string]struct{}{
		"nofollow": {}, "noopener": {}, "noreferrer": {}, "sponsored": {}, "ugc": {},
	}
	words := strings.Fields(strings.ToLower(value))
	if len(words) == 0 {
		return "", false
	}
	clean := make([]string, 0, len(words))
	seen := make(map[string]struct{}, len(words))
	for _, word := range words {
		if _, ok := allowed[word]; !ok {
			return "", false
		}
		if _, ok := seen[word]; ok {
			continue
		}
		seen[word] = struct{}{}
		clean = append(clean, word)
	}
	return strings.Join(clean, " "), true
}

func boundedInteger(value string, min, max int) (string, bool) {
	if value == "" || strings.TrimSpace(value) != value {
		return "", false
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < min || number > max {
		return "", false
	}
	return strconv.Itoa(number), true
}

func hasSemanticText(nodes []*html.Node) bool {
	for _, node := range nodes {
		if node.Type == html.TextNode && hasVisibleText(node.Data) {
			return true
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if hasSemanticText([]*html.Node{child}) {
				return true
			}
		}
	}
	return false
}

func hasVisibleText(value string) bool {
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		return true
	}
	return false
}
