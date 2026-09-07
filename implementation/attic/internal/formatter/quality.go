package formatter

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"attic/internal/sanitize"
	"golang.org/x/net/html"
	"golang.org/x/text/unicode/norm"
)

// QualityError contains only a fixed failure reason, never article text.
type QualityError struct{ Reason string }

func (e *QualityError) Error() string { return "PDF quality check failed: " + e.Reason }

// Inspector checks the rendered artifact against the approved semantic article.
// It detects mechanical losses; it does not claim to verify an extraction's
// accuracy against content that was unavailable on the original website.
type Inspector struct{ Timeout time.Duration }

type qualityEvidence struct{ Info, Raw, Layout, Bounds, Images string }

func (q Inspector) Check(ctx context.Context, article Article, pdf []byte) error {
	timeout := q.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "attic-check-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "article.pdf")
	if err := os.WriteFile(path, pdf, 0600); err != nil {
		return err
	}
	var e qualityEvidence
	for _, task := range []struct {
		tool   string
		args   []string
		target *string
	}{
		{"pdfinfo", []string{path}, &e.Info},
		{"pdftotext", []string{"-raw", path, "-"}, &e.Raw},
		{"pdftotext", []string{"-layout", path, "-"}, &e.Layout},
		{"pdftotext", []string{"-bbox", path, "-"}, &e.Bounds},
		{"pdfimages", []string{"-list", path}, &e.Images},
	} {
		var output limitedOutput
		cmd := exec.CommandContext(ctx, task.tool, task.args...)
		cmd.Stdout = &output
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return &QualityError{Reason: "inspection_unavailable"}
		}
		*task.target = output.String()
	}
	return checkEvidence(article, e)
}

type limitedOutput struct{ bytes.Buffer }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 32<<20 {
		return 0, errors.New("inspection output limit")
	}
	return b.Buffer.Write(p)
}

var pageNumber = regexp.MustCompile(`\n[ \t]*[0-9]+[ \t]*\n?\f`)

func comparable(s string) string {
	var b strings.Builder
	for _, r := range norm.NFKC.String(s) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func checkEvidence(article Article, e qualityEvidence) error {
	fail := func(reason string) error { return &QualityError{Reason: reason} }
	metadata := map[string]string{}
	for _, line := range strings.Split(e.Info, "\n") {
		if key, value, ok := strings.Cut(line, ":"); ok {
			metadata[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if comparable(metadata["Title"]) != comparable(article.Title) || metadata["Creator"] != "Attic" {
		return fail("missing_metadata")
	}
	if article.Author != "" && comparable(metadata["Author"]) != comparable(article.Author) {
		return fail("missing_author")
	}
	for _, v := range []string{article.SiteName, article.PublicationDate, article.SourceURL} {
		if v != "" && !strings.Contains(comparable(metadata["Subject"]), comparable(v)) {
			return fail("missing_metadata")
		}
	}
	imagePages := map[int]bool{}
	imageCount := 0
	for _, line := range strings.Split(e.Images, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 3 && fields[2] == "image" {
			n, _ := strconv.Atoi(fields[0])
			imagePages[n] = true
			imageCount++
		}
	}
	decoder := xml.NewDecoder(strings.NewReader(e.Bounds))
	pages, words := 0, 0
	width, height := 0.0, 0.0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fail("invalid_geometry")
		}
		switch token := token.(type) {
		case xml.StartElement:
			if token.Name.Local == "page" {
				pages++
				words = 0
				for _, a := range token.Attr {
					if a.Name.Local == "width" {
						width, _ = strconv.ParseFloat(a.Value, 64)
					}
					if a.Name.Local == "height" {
						height, _ = strconv.ParseFloat(a.Value, 64)
					}
				}
				expected := 419.53
				if article.Profile == "kindle-scribe" {
					expected = 446.46
				}
				if width < expected-1 || width > expected+1 || height < 594 || height > 597 {
					return fail("wrong_page_size")
				}
			}
			if token.Name.Local == "word" {
				var word struct {
					XMin float64 `xml:"xMin,attr"`
					XMax float64 `xml:"xMax,attr"`
					YMin float64 `xml:"yMin,attr"`
					YMax float64 `xml:"yMax,attr"`
					Text string  `xml:",chardata"`
				}
				if decoder.DecodeElement(&word, &token) != nil {
					return fail("invalid_geometry")
				}
				if word.XMin < 10 || word.XMax > width-10 || word.YMin < 5 || word.YMax > height-5 {
					return fail("text_overflow")
				}
				if !(strings.TrimSpace(word.Text) == strconv.Itoa(pages) && word.YMin > height-70) {
					words++
				}
			}
		case xml.EndElement:
			if token.Name.Local == "page" && words == 0 && !imagePages[pages] {
				return fail("blank_page")
			}
		}
	}
	if pages == 0 {
		return fail("blank_pdf")
	}
	clean, err := sanitize.ArticleHTML(article.SemanticHTML, article.Title)
	if err != nil {
		return fail("invalid_article")
	}
	root, err := html.Parse(strings.NewReader(clean))
	if err != nil {
		return err
	}
	raw := comparable(pageNumber.ReplaceAllString(e.Raw, "\n"))
	layout := comparable(pageNumber.ReplaceAllString(e.Layout, "\n"))
	if len(raw) < 20 {
		return fail("missing_text")
	}
	expectedImages := 0
	var inspect func(*html.Node) error
	inspect = func(n *html.Node) error {
		if n.Type == html.ElementNode {
			if n.Data == "img" {
				expectedImages++
			}
			switch n.Data {
			case "h1", "h2", "h3", "h4", "h5", "h6", "p", "pre", "td", "th":
				value := comparable(semanticText(n))
				if len(value) >= 8 && !strings.Contains(raw, value) && !strings.Contains(layout, value) {
					return fail("missing_content")
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := inspect(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := inspect(root); err != nil {
		return err
	}
	if expectedImages > imageCount {
		return fail("missing_images")
	}
	return nil
}
func semanticText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(semanticText(c))
	}
	return b.String()
}

// Checked composes rendering with mandatory inspection before artifact storage.
type Checked struct {
	Renderer  Formatter
	Inspector Inspector
}

func (c Checked) Format(ctx context.Context, a Article) (Result, error) {
	if c.Renderer == nil {
		return Result{}, fmt.Errorf("PDF renderer is not configured")
	}
	result, err := c.Renderer.Format(ctx, a)
	if err != nil {
		return Result{}, err
	}
	if err := c.Inspector.Check(ctx, a, result.PDF); err != nil {
		return Result{}, err
	}
	return result, nil
}
