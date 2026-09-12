package domain

import (
	"strings"
	"testing"
)

func TestSetupFailuresHaveDistinctActionableStages(t *testing.T) {
	for _, tc := range []struct {
		category FailureCategory
		stage    string
	}{{FailureAIAuthFailed, "AI approval"}, {FailureUnsupportedContent, "AI approval"}, {FailureFormatFailed, "PDF formatting"}, {FailurePDFQualityFailed, "PDF validation"}, {FailureDeliveryRejected, "SMTP submission"}} {
		if tc.category.StageName() != tc.stage || tc.category.NextAction() == "" {
			t.Fatal(tc.category)
		}
	}
	if strings.Contains(FailureUnsupportedContent.Message(), "PDF") {
		t.Fatal("article rejection mislabels formatter")
	}
}
