package application

import (
	"errors"
	"strings"
	"testing"

	"attic/internal/domain"
)

func TestApproveArticleStoresNormalizedSanitizedHTML(t *testing.T) {
	archive := &Archive{minContent: 1}
	draft := ArticleDraft{
		Classification: "article",
		Decision:       "accept_candidate",
		Title:          "Safe article",
		PlainText:      "Readable text",
		SemanticHTML:   `<ARTICLE CLASS=drop><P ONCLICK=alert(1)>Keep <STRONG>this</STRONG>.</P><SCRIPT>bad()</SCRIPT></ARTICLE>`,
		AIConfidence:   0.9,
		AIAttemptID:    "attempt-1",
	}

	approved, err := archive.approveArticle(draft)
	if err != nil {
		t.Fatalf("approveArticle() error = %v", err)
	}
	if got, want := approved.draft.SemanticHTML, `<article><p>Keep <strong>this</strong>.</p></article>`; got != want {
		t.Fatalf("approved semantic HTML = %q, want %q", got, want)
	}
}

func TestApproveArticleRejectsMarkupWithNoSemanticContent(t *testing.T) {
	archive := &Archive{minContent: 1}
	_, err := archive.approveArticle(ArticleDraft{
		Classification: "article",
		Decision:       "accept_candidate",
		Title:          "No content",
		PlainText:      "Readable text",
		SemanticHTML:   `<svg><script>alert(1)</script></svg>`,
		AIConfidence:   0.9,
		AIAttemptID:    "attempt-1",
	})
	var processingErr *ProcessingError
	if !errors.As(err, &processingErr) || processingErr.Category != string(domain.FailureInsufficientContent) {
		t.Fatalf("approveArticle() error = %#v, want insufficient-content processing error", err)
	}
	if strings.Contains(err.Error(), "alert") {
		t.Fatalf("approveArticle() error leaked active content: %v", err)
	}
}
