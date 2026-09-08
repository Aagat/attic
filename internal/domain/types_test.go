package domain

import "testing"

func TestStatusLifecycleClassification(t *testing.T) {
	for _, status := range []Status{StatusReady, StatusDelivered, StatusDeliveryFailed, StatusFailed, StatusCancelled} {
		if !status.Valid() || !status.Terminal() {
			t.Fatalf("%q should be a valid terminal status", status)
		}
	}
	for _, status := range []Status{StatusQueued, StatusProcessing, StatusDelivering} {
		if !status.Valid() || status.Terminal() {
			t.Fatalf("%q should be a valid non-terminal status", status)
		}
	}
}

func TestStageTransitions(t *testing.T) {
	allowed := map[Stage][]Stage{
		StageFetching:    {StageFetching, StageExtracting},
		StageExtracting:  {StageExtracting, StageAIAnalyzing, StageFetching},
		StageAIAnalyzing: {StageAIAnalyzing, StageFormatting, StageFetching},
		StageFormatting:  {StageFormatting, StagePersisting, StageFetching},
		StagePersisting:  {StagePersisting},
	}
	stages := []Stage{"", "unknown", StageFetching, StageExtracting, StageAIAnalyzing, StageFormatting, StagePersisting}
	for _, from := range stages {
		for _, to := range stages {
			want := false
			for _, permitted := range allowed[from] {
				want = want || permitted == to
			}
			if got := from.CanTransitionTo(to); got != want {
				t.Errorf("%q -> %q: %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestFailurePolicy(t *testing.T) {
	categories := []FailureCategory{FailureInvalidInput, FailureBlockedTarget, FailureFetchFailed, FailureRenderTimeout,
		FailureAccessDenied, FailurePaywallDetected, FailureUnsupportedContent, FailureInsufficientContent,
		FailureAIUnavailable, FailureAIAuthFailed, FailureAIModelUnsupported, FailureAIInvalidResponse,
		FailureFormatFailed, FailurePDFQualityFailed, FailureStorageFailed, FailureDeliveryRejected,
		FailureDeliveryTimeout, FailureInternalError}
	for _, category := range categories {
		if category.Normalized() != category || category.Message() == "" {
			t.Errorf("unrecognized declared category %q", category)
		}
	}
	unknown := FailureCategory("secret provider details")
	if unknown.Normalized() != FailureInternalError || unknown.Message() != "The job could not be completed" {
		t.Fatal("unknown failure leaked")
	}
}
