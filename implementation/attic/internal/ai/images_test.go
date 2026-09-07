package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEmbeddedImagesSurviveApprovalWithoutBase64InPrompt(t *testing.T) {
	original := `<p>Article</p><img src="data:image/png;base64,YWJj" alt="Diagram">`
	request := AnalyzeRequest{SourceURL: "https://example.test", CandidateHTML: original}
	client := mustClient(t, Config{BaseURL: "https://example.test/v1", APIKey: "test"})
	body, err := client.buildRequestBody(request, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "base64,YWJj") || !strings.Contains(string(body), "attic-image:") {
		t.Fatal("prompt did not replace embedded image bytes")
	}
	references, _ := imageReferences(original)
	for _, decision := range []string{"accept_candidate", "replace_candidate"} {
		encoded, _ := json.Marshal(map[string]any{"classification": "article", "decision": decision, "title": "Article", "completeness": 1, "confidence": 1, "decision_reason": "Complete article", "cleaned_html": references})
		approved, _, err := parseApproved(string(encoded), request)
		if err != nil {
			t.Fatal(err)
		}
		if approved.ContentHTML != original {
			t.Fatalf("%s lost image: %s", decision, approved.ContentHTML)
		}
	}
}
