package sanitize

import (
	"strings"
	"testing"
)

func TestArticleHTMLKeepsArticleAndCodeWithoutPageChrome(t *testing.T) {
	input := `<main><article><header><h1>Example article</h1><p><time>2026-09-07</time> • audience—programmers • tags</p></header><details><summary>Table of contents</summary><ol><li>Navigation</li></ol></details><blockquote><p>An opening quotation.</p></blockquote><h2>Code<a href="#code">¶</a></h2><pre><code class="language-sh"><span>sort</span><span> words</span>
  | uniq -c
</code></pre><p>Last paragraph.</p></article><small>Discuss on social media</small></main>`
	output, err := ArticleHTML(input, "Example article")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"Example article", "audience", "Table of contents", "Navigation", "Discuss on", "¶", "<span"} {
		if strings.Contains(output, bad) {
			t.Errorf("retained chrome %q", bad)
		}
	}
	for _, want := range []string{"An opening quotation.", "<h2>Code</h2>", "sort words\n  | uniq -c", "Last paragraph."} {
		if !strings.Contains(output, want) {
			t.Errorf("lost content %q", want)
		}
	}
	again, err := ArticleHTML(output, "Example article")
	if err != nil || output != again {
		t.Fatal("cleanup must be idempotent")
	}
}

func TestArticleHTMLKeepsContentDisclosuresAndDistinctHeadings(t *testing.T) {
	output, err := ArticleHTML(`<article><h1>Distinct heading</h1><details><summary>Proof</summary><p>A useful derivation.</p></details><p>See <a href="https://example.com">this explanation</a>.</p></article>`, "Document title")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Distinct heading", "Proof", "A useful derivation.", "https://example.com"} {
		if !strings.Contains(output, want) {
			t.Errorf("lost %q", want)
		}
	}
}

func TestCodeFiguresStayInArticleFlow(t *testing.T) {
	clean, err := ArticleHTML(`<article><p>Before</p><figure><pre>print(1)</pre></figure><p>After</p></article>`, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(clean, "<figure") || !strings.Contains(clean, "<pre>print(1)</pre>") {
		t.Fatalf("code became a floating figure: %s", clean)
	}
}
