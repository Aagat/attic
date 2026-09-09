package application

import (
	"strings"
	"testing"
)

func TestOwnerReviewedContentIsSanitizedWithoutFabricatedAIApproval(t *testing.T) {
	approved, err := ApproveReadingEdit(ArticleDraft{Title: "Edited", SemanticHTML: `<p onclick="bad()">Keep <strong>this</strong>.</p><script>bad()</script>`, AIConfidence: 1, AIAttemptID: "not-an-ai-review"})
	if err != nil {
		t.Fatal(err)
	}
	draft, ok := approved.Snapshot()
	if !ok || strings.Contains(draft.SemanticHTML, "bad") || !strings.Contains(draft.SemanticHTML, "<strong>this</strong>") || draft.AIAttemptID != "" || draft.AIConfidence != 0 || draft.ExtractionMethod != "owner_edit" {
		t.Fatalf("review: %+v", draft)
	}
	if _, err := ApproveReadingEdit(ArticleDraft{Title: "Empty", SemanticHTML: `<script>discard()</script>`}); err == nil {
		t.Fatal("empty edit accepted")
	}
}
