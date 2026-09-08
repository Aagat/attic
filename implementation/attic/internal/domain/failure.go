package domain

// Normalized returns the durable category, rejecting unrecognized provider values.
func (category FailureCategory) Normalized() FailureCategory {
	if category.message() == "" {
		return FailureInternalError
	}
	return category
}

// Message returns fixed, public-safe text; provider and page errors never enter it.
func (category FailureCategory) Message() string {
	return category.Normalized().message()
}

func (category FailureCategory) message() string {
	switch category {
	case FailureAIUnavailable:
		return "The AI provider is temporarily unavailable"
	case FailureAIAuthFailed:
		return "The AI provider rejected the configured credentials"
	case FailureAIModelUnsupported:
		return "The configured AI model is unsupported"
	case FailureAIInvalidResponse:
		return "The AI provider returned an invalid response"
	case FailureUnsupportedContent:
		return "The page is not a supported article"
	case FailureInsufficientContent:
		return "The page did not contain enough readable content"
	case FailurePDFQualityFailed:
		return "The PDF failed automatic quality checks and was not published"
	case FailureFormatFailed:
		return "The PDF could not be generated"
	case FailureStorageFailed:
		return "Artifact storage is temporarily unavailable"
	case FailureFetchFailed:
		return "The page could not be retrieved"
	case FailureBlockedTarget:
		return "The destination was blocked by network policy"
	case FailureRenderTimeout:
		return "Page rendering exceeded its deadline"
	case FailureAccessDenied:
		return "The page denied access"
	case FailurePaywallDetected:
		return "The page appears to be behind a paywall"
	case FailureDeliveryRejected:
		return "Email delivery was rejected"
	case FailureDeliveryTimeout:
		return "Email delivery could not be confirmed"
	case FailureInvalidInput:
		return "The submitted input is invalid"
	case FailureInternalError:
		return "The job could not be completed"
	default:
		return ""
	}
}
