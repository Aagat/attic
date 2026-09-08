package formatter

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestQualityChecksRejectBrokenArtifacts(t *testing.T) {
	a := Article{Profile: "kindle-scribe", Title: "Article title", Author: "Ada", SourceURL: "https://example.test/story", SemanticHTML: "<p>Complete preserved article text.</p>"}
	good := qualityEvidence{Info: "Title: Article title\nAuthor: Ada\nCreator: Attic\nSubject: https://example.test/story", Raw: "Complete preserved article text.", Layout: "Complete preserved article text.", Bounds: `<html><body><doc><page width="446.46" height="595.28"><word xMin="34" xMax="200" yMin="40" yMax="55">Complete preserved article text.</word></page></doc></body></html>`}
	if err := checkEvidence(a, good); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, reason string
		change       func(*Article, *qualityEvidence)
	}{
		{"metadata", "missing_metadata", func(a *Article, e *qualityEvidence) { e.Info = "Title: Wrong title" }},
		{"author", "missing_author", func(a *Article, e *qualityEvidence) {
			e.Info = strings.ReplaceAll(e.Info, "Author: Ada", "Author: Grace")
		}},
		{"content", "missing_content", func(a *Article, e *qualityEvidence) { a.SemanticHTML += "<pre>critical_missing_function()</pre>" }},
		{"images", "missing_images", func(a *Article, e *qualityEvidence) { a.SemanticHTML += `<img src="data:image/png;base64,YWJj">` }},
		{"overflow", "text_overflow", func(a *Article, e *qualityEvidence) {
			e.Bounds = strings.ReplaceAll(e.Bounds, `xMax="200"`, `xMax="500"`)
		}},
		{"blank", "blank_page", func(a *Article, e *qualityEvidence) {
			e.Bounds = `<html><page width="446.46" height="595.28"></page></html>`
		}},
		{"geometry", "wrong_page_size", func(a *Article, e *qualityEvidence) {
			e.Bounds = strings.ReplaceAll(e.Bounds, `width="446.46"`, `width="800"`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, e := a, good
			tc.change(&a, &e)
			var got *QualityError
			if err := checkEvidence(a, e); !errors.As(err, &got) || got.Reason != tc.reason {
				t.Fatalf("got %v; want %s", err, tc.reason)
			}
		})
	}
}

func TestQualityInspectionOfSavedArticle(t *testing.T) {
	// Optional replay of a real approved article; source and PDF stay untracked.
	source, pdf := os.Getenv("ATTIC_QUALITY_HTML"), os.Getenv("ATTIC_QUALITY_PDF")
	if source == "" || pdf == "" {
		t.Skip("set ATTIC_QUALITY_HTML and ATTIC_QUALITY_PDF for a real article audit")
	}
	html, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(pdf)
	if err != nil {
		t.Fatal(err)
	}
	a := Article{Profile: "kindle-scribe", Title: os.Getenv("ATTIC_QUALITY_TITLE"), Author: os.Getenv("ATTIC_QUALITY_AUTHOR"), SemanticHTML: string(html)}
	if err := (Inspector{}).Check(context.Background(), a, data); err != nil {
		t.Fatal(err)
	}
}
