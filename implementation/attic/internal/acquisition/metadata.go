package acquisition

import (
	"encoding/json"
	"strings"

	"golang.org/x/net/html"
)

// supplementalMetadata reads publisher-provided attribution before article
// headers and scripts are removed. It never follows JSON-LD URLs or contexts.
func supplementalMetadata(doc *html.Node) (author, publisher, date string) {
	var authors, visibleAuthors []string
	var visibleDate string
	add := func(list *[]string, value string) {
		value = boundedMetadata(value, maxMetadataAuthorRunes)
		if value == "" {
			return
		}
		for _, old := range *list {
			if old == value {
				return
			}
		}
		*list = append(*list, value)
	}
	var names func(any)
	names = func(v any) {
		switch v := v.(type) {
		case string:
			add(&authors, v)
		case []any:
			for _, item := range v {
				names(item)
			}
		case map[string]any:
			if name, ok := v["name"].(string); ok {
				add(&authors, name)
			}
		}
	}
	var structured func(any, int)
	structured = func(v any, depth int) {
		if depth > 16 {
			return
		}
		switch v := v.(type) {
		case []any:
			for _, item := range v {
				structured(item, depth+1)
			}
		case map[string]any:
			types := []string{}
			switch t := v["@type"].(type) {
			case string:
				types = append(types, t)
			case []any:
				for _, x := range t {
					if t, ok := x.(string); ok {
						types = append(types, t)
					}
				}
			}
			for _, t := range types {
				if t != "Article" && t != "NewsArticle" && t != "BlogPosting" && t != "TechArticle" {
					continue
				}
				names(v["author"])
				if value, ok := v["datePublished"].(string); ok {
					setFirst(&date, value)
				}
				if p, ok := v["publisher"].(map[string]any); ok {
					if name, ok := p["name"].(string); ok {
						setFirst(&publisher, name)
					}
				}
				break
			}
			structured(v["@graph"], depth+1)
			structured(v["mainEntity"], depth+1)
		}
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "script" && strings.EqualFold(attr(n, "type"), "application/ld+json") {
				raw := nodeText(n)
				if len(raw) <= 1<<20 {
					var value any
					if json.Unmarshal([]byte(raw), &value) == nil {
						structured(value, 0)
					}
				}
			}
			if relContains(attr(n, "rel"), "author") || relContains(attr(n, "itemprop"), "author") {
				value := firstElementProperty(n, "name")
				if value == "" {
					value = nodeText(n)
				}
				add(&visibleAuthors, value)
			}
			if n.Data == "time" && relContains(attr(n, "itemprop"), "datePublished") {
				setFirst(&visibleDate, attr(n, "datetime"))
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return firstNonEmpty(strings.Join(authors, ", "), strings.Join(visibleAuthors, ", ")), publisher, firstNonEmpty(date, visibleDate)
}

func firstElementProperty(n *html.Node, property string) string {
	if n.Type == html.ElementNode && relContains(attr(n, "itemprop"), property) {
		return firstNonEmpty(attr(n, "content"), nodeText(n))
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if v := firstElementProperty(c, property); v != "" {
			return v
		}
	}
	return ""
}
