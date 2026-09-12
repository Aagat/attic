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
		return "The AI connection is missing or its credentials were rejected"
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

// NextAction is fixed public guidance. It never contains upstream error text.
func (category FailureCategory) NextAction() string {
	switch category {
	case FailureAIAuthFailed:
		return "Open Settings, connect or reconnect ChatGPT, then check AI compatibility and retry approval."
	case FailureAIModelUnsupported:
		return "Check the configured model and account permissions, then run the Settings compatibility check."
	case FailureAIUnavailable:
		return "Check provider availability or account quota, then retry approval."
	case FailureUnsupportedContent, FailureInsufficientContent:
		return "Open a specific article URL or upload your own PDF. The saved copy remains available."
	case FailureFormatFailed, FailurePDFQualityFailed:
		return "Run the PDF/browser check in Settings, then retry document preparation."
	case FailureDeliveryRejected:
		return "Check SMTP settings and approved sender, then explicitly retry delivery of the saved PDF."
	case FailureDeliveryTimeout:
		return "Check the recipient inbox before retrying: the relay may already have accepted the email."
	case FailureFetchFailed, FailureAccessDenied, FailurePaywallDetected, FailureRenderTimeout, FailureBlockedTarget:
		return "Check the source URL or save a browser capture; retry capture only when needed."
	default:
		return "Retry the failed stage; use the diagnostic ID when reporting a persistent failure."
	}
}
func (category FailureCategory) StageName() string {
	switch category {
	case FailureAIAuthFailed, FailureAIUnavailable, FailureAIModelUnsupported, FailureAIInvalidResponse, FailureUnsupportedContent, FailureInsufficientContent:
		return "AI approval"
	case FailureFormatFailed:
		return "PDF formatting"
	case FailurePDFQualityFailed:
		return "PDF validation"
	case FailureDeliveryRejected, FailureDeliveryTimeout:
		return "SMTP submission"
	case FailureStorageFailed:
		return "Artifact storage"
	default:
		return "Capture"
	}
}
