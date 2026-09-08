package sanitize_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"attic/internal/sanitize"
)

func TestSanitizeHTMLPreservesSafeArticleSemantics(t *testing.T) {
	input := `<ARTICLE><HEADER><H1>Title</H1></HEADER><P>Intro <STRONG>text</STRONG> and <A HREF="HTTPS://example.test/read?x=1&amp;y=2" TITLE="Read">a link</A>.</P><UL><LI>One</LI><LI>Two</LI></UL></ARTICLE>`

	got, err := sanitize.SanitizeHTML(input)
	if err != nil {
		t.Fatalf("SanitizeHTML() error = %v", err)
	}
	for _, want := range []string{
		`<article>`,
		`<header><h1>Title</h1></header>`,
		`<p>Intro <strong>text</strong> and <a href="https://example.test/read?x=1&amp;y=2" title="Read">a link</a>.</p>`,
		`<ul><li>One</li><li>Two</li></ul>`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("sanitized HTML = %q, want fragment %q", got, want)
		}
	}
}

func TestSanitizeHTMLStripsMixedCaseHandlersAndUnsafeAttributes(t *testing.T) {
	input := `<ARTICLE CLASS="drop" ID=article STYLE="background:url(javascript:alert(1))" OnClick=alert(1)><P data-test="drop" ONMOUSEOVER="alert(2)" style="color:red">Keep <EM>meaning</EM>.</P></ARTICLE>`

	got, err := sanitize.SanitizeHTML(input)
	if err != nil {
		t.Fatalf("SanitizeHTML() error = %v", err)
	}
	for _, forbidden := range []string{"class=", "id=", "style=", "data-test", "onclick", "onmouseover", "javascript:"} {
		if strings.Contains(strings.ToLower(got), forbidden) {
			t.Fatalf("sanitized HTML = %q, contains forbidden %q", got, forbidden)
		}
	}
	if !strings.Contains(got, `<p>Keep <em>meaning</em>.</p>`) {
		t.Fatalf("sanitized HTML = %q, lost safe text semantics", got)
	}
}

func TestSanitizeHTMLDropsActiveAndMediaSubtrees(t *testing.T) {
	input := `<script>alert(1)</script><STYLE>.x{background:url(javascript:alert(2))}</STYLE><FORM><input value="discard">form text</FORM><IFRAME SRC="https://evil.test"></IFRAME><SVG><A HREF="javascript:alert(3)">svg text</A></SVG><VIDEO><SOURCE SRC="https://evil.test/movie"></VIDEO><ARTICLE><P>Keep this article.</P></ARTICLE>`

	got, err := sanitize.SanitizeHTML(input)
	if err != nil {
		t.Fatalf("SanitizeHTML() error = %v", err)
	}
	if !strings.Contains(got, `<article><p>Keep this article.</p></article>`) {
		t.Fatalf("sanitized HTML = %q, lost safe article", got)
	}
	for _, forbidden := range []string{"alert", "form text", "svg text", "evil.test", "<script", "<style", "<iframe", "<video", "<source"} {
		if strings.Contains(strings.ToLower(got), strings.ToLower(forbidden)) {
			t.Fatalf("sanitized HTML = %q, contains dropped content %q", got, forbidden)
		}
	}
}

func TestSanitizeHTMLStripsUnsafeLinksButKeepsLinkText(t *testing.T) {
	input := `<article><A HREF="HTTPS://example.test/safe">safe</A><A HREF="javascript:alert(1)">javascript text</A><A HREF="data:text/html;base64,PHNjcmlwdD4=">data text</A><A HREF="//example.test/relative">relative text</A></article>`

	got, err := sanitize.SanitizeHTML(input)
	if err != nil {
		t.Fatalf("SanitizeHTML() error = %v", err)
	}
	if !strings.Contains(got, `<a href="https://example.test/safe">safe</a>`) {
		t.Fatalf("sanitized HTML = %q, lost safe link", got)
	}
	for _, forbidden := range []string{"javascript:", "data:text/html", `href="//example.test/relative"`} {
		if strings.Contains(strings.ToLower(got), strings.ToLower(forbidden)) {
			t.Fatalf("sanitized HTML = %q, contains unsafe link %q", got, forbidden)
		}
	}
	for _, text := range []string{"javascript text", "data text", "relative text"} {
		if !strings.Contains(got, text) {
			t.Fatalf("sanitized HTML = %q, lost safe link text %q", got, text)
		}
	}
}

func TestSanitizeHTMLOnlyAllowsBoundedDataImages(t *testing.T) {
	validPNG := base64.StdEncoding.EncodeToString([]byte("not an actual image, but valid bounded base64"))
	input := `<article><P>Images:</P><IMG SRC="data:image/png;base64,` + validPNG + `" ALT="inline"><IMG SRC="https://remote.test/image.png" ALT="remote"><IMG SRC="data:image/svg+xml;base64,PHN2Zz4=" ALT="svg"><IMG SRC="data:image/jpeg;base64,%%%" ALT="invalid"><IMG SRCSET="data:image/png;base64,` + validPNG + ` 1x" SRC="data:image/webp;base64,` + validPNG + `" ALT="webp"></article>`

	got, err := sanitize.SanitizeHTML(input)
	if err != nil {
		t.Fatalf("SanitizeHTML() error = %v", err)
	}
	if !strings.Contains(got, `src="data:image/png;base64,`+validPNG+`"`) || !strings.Contains(got, `alt="inline"`) {
		t.Fatalf("sanitized HTML = %q, lost valid inline image", got)
	}
	if !strings.Contains(got, `src="data:image/webp;base64,`+validPNG+`"`) {
		t.Fatalf("sanitized HTML = %q, lost valid webp image", got)
	}
	for _, forbidden := range []string{"remote.test", "image/svg+xml", "%%%", "srcset", "alt=\"remote\"", "alt=\"svg\"", "alt=\"invalid\""} {
		if strings.Contains(strings.ToLower(got), strings.ToLower(forbidden)) {
			t.Fatalf("sanitized HTML = %q, contains forbidden image data %q", got, forbidden)
		}
	}
}

func TestSanitizeHTMLStripsCommentsAndRecoversMalformedMarkup(t *testing.T) {
	input := `<article><!-- secret comment --><p>First <EM>second</article><unknown title="drop">Third<!-- hidden --><script>bad()</script></unknown>`

	got, err := sanitize.SanitizeHTML(input)
	if err != nil {
		t.Fatalf("SanitizeHTML() error = %v", err)
	}
	if strings.Contains(got, "secret comment") || strings.Contains(got, "hidden") || strings.Contains(got, "bad()") {
		t.Fatalf("sanitized HTML = %q, retained comment or active content", got)
	}
	for _, text := range []string{"First", "second", "Third"} {
		if !strings.Contains(got, text) {
			t.Fatalf("sanitized HTML = %q, lost recovered text %q", got, text)
		}
	}
	if strings.Contains(got, "<unknown") {
		t.Fatalf("sanitized HTML = %q, retained unknown tag", got)
	}
}

func TestSanitizeHTMLRejectsNoSemanticContent(t *testing.T) {
	validPNG := base64.StdEncoding.EncodeToString([]byte("valid bounded base64"))
	for _, input := range []string{
		"",
		"   <!-- comment -->   ",
		"<script>alert(1)</script>",
		"<article>   </article>",
		"<article>\u200b</article>",
		`<img src="https://remote.test/image.png">`,
		`<img src="data:image/png;base64,` + validPNG + `">`,
	} {
		_, err := sanitize.SanitizeHTML(input)
		if !errors.Is(err, sanitize.ErrNoSemanticContent) {
			t.Errorf("SanitizeHTML(%q) error = %v, want ErrNoSemanticContent", input, err)
		}
	}
}
