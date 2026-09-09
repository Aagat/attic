package ai

import (
	"context"
	"errors"
	"strings"
	"time"

	"attic/internal/application"
	"attic/internal/domain"
	"attic/internal/sanitize"
	"golang.org/x/net/html"
)

// Analyzer is the true-external provider seam. Client is the production
// adapter; tests use a local fake-compatible adapter or server.
type Analyzer interface {
	Analyze(context.Context, AnalyzeRequest) (Approved, []Attempt, error)
}

// ApprovalInput is the complete, bounded handoff from deterministic
// extraction. ScreenshotDataURL is required even when Candidate is empty.
type ApprovalInput struct {
	TargetLanguage    string
	RetrievedURL      string
	SourceURL         string
	CandidateText     string
	CandidateHTML     string
	Title             string
	Author            string
	SiteName          string
	PublicationDate   string
	Description       string
	Language          string
	ExtractionMethod  string
	ScreenshotDataURL string
}

// ArticleApprover is a deep adapter over provider invocation, durable attempt
// recording, error classification, and the application-owned approval gate.
type ArticleApprover struct {
	analyzer Analyzer
	recorder application.AIAttemptRecorder
}

func NewArticleApprover(analyzer Analyzer, recorder application.AIAttemptRecorder) (*ArticleApprover, error) {
	if analyzer == nil || recorder == nil {
		return nil, &Error{Code: CodeInvalidConfig, Message: "AI analyzer and attempt recorder are required"}
	}
	return &ArticleApprover{analyzer: analyzer, recorder: recorder}, nil
}

// Approve invokes mandatory AI analysis, records every provider call before
// returning, and crosses the application approval seam only after a successful
// record. The callback is normally ProcessorContext.ApproveArticle.
func (a *ArticleApprover) Approve(ctx context.Context, jobID domain.JobID, input ApprovalInput, approve func(application.ArticleDraft) (application.ApprovedArticle, error)) (application.ApprovedArticle, error) {
	if a == nil || a.analyzer == nil || a.recorder == nil || approve == nil {
		return application.ApprovedArticle{}, application.NewProcessingError(string(domain.FailureAIInvalidResponse), "The AI pipeline is not configured", false)
	}
	request := AnalyzeRequest{
		TargetLanguage: input.TargetLanguage,
		SourceURL:      input.SourceURL, RetrievedURL: input.RetrievedURL, CandidateText: input.CandidateText, CandidateHTML: input.CandidateHTML,
		Title: input.Title, Author: input.Author, SiteName: input.SiteName,
		PublicationDate: input.PublicationDate, Description: input.Description, Language: input.Language,
		ScreenshotDataURL: input.ScreenshotDataURL,
	}
	result, providerAttempts, analyzeErr := a.analyzer.Analyze(ctx, request)
	attempts := make([]application.AIAttempt, len(providerAttempts))
	for i, attempt := range providerAttempts {
		purpose := "analysis"
		if i > 0 {
			purpose = "repair"
		}
		attempts[i] = application.AIAttempt{
			Purpose: purpose, Model: attempt.Model, PromptVersion: attempt.PromptVersion,
			Latency: attempt.Latency, ProviderRequestID: attempt.ProviderRequestID,
			InputTokens: attempt.InputTokens, OutputTokens: attempt.OutputTokens, UsageReported: attempt.UsageReported,
			Status: string(attempt.Status), ErrorCategory: string(attempt.ErrorCode), CreatedAt: attempt.CreatedAt,
		}
	}
	var attemptIDs []string
	if len(attempts) > 0 {
		recordCtx := ctx
		var cancel context.CancelFunc
		if ctx != nil && ctx.Err() != nil {
			// The provider call happened and must remain auditable even when
			// shutdown cancelled the processing context. Detach only this small,
			// bounded metadata write; no page or provider body is persisted.
			recordCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
		}
		var err error
		attemptIDs, err = a.recorder.RecordAIAttempts(recordCtx, jobID, attempts)
		if err != nil || len(attemptIDs) != len(attempts) {
			return application.ApprovedArticle{}, application.NewProcessingError(string(domain.FailureStorageFailed), "AI attempt metadata could not be committed", true)
		}
	}
	if analyzeErr != nil {
		if ctx != nil && ctx.Err() != nil {
			return application.ApprovedArticle{}, ctx.Err()
		}
		return application.ApprovedArticle{}, mapProcessingError(analyzeErr)
	}
	if len(attemptIDs) == 0 {
		return application.ApprovedArticle{}, application.NewProcessingError(string(domain.FailureAIInvalidResponse), "The AI attempt is not recorded", false)
	}
	if input.TargetLanguage != "" && (!ValidTargetLanguage(input.TargetLanguage) || result.Decision != "replace_candidate" || result.Language != input.TargetLanguage) {
		return application.ApprovedArticle{}, mapProcessingError(invalidResponseError())
	}
	plainText := input.CandidateText
	extractionMethod := strings.TrimSpace(input.ExtractionMethod)
	if result.Decision == "replace_candidate" {
		cleanedHTML, err := sanitize.ArticleHTML(result.ContentHTML, result.Title)
		if err != nil {
			category := domain.FailureAIInvalidResponse
			if errors.Is(err, sanitize.ErrNoSemanticContent) {
				category = domain.FailureInsufficientContent
			}
			return application.ApprovedArticle{}, application.NewProcessingError(string(category), "The AI replacement did not contain safe article content", false)
		}
		result.ContentHTML = cleanedHTML
		plainText = semanticText(cleanedHTML)
		extractionMethod = "ai"
	}
	return approve(application.ArticleDraft{
		Classification: result.Classification, Decision: result.Decision, Title: result.Title,
		Author: result.Author, SiteName: result.SiteName, PublicationDate: result.PublicationDate,
		Description: result.Description, Language: result.Language, SemanticHTML: result.ContentHTML,
		PlainText: plainText, ExtractionMethod: extractionMethod, AIConfidence: result.Confidence,
		AICompleteness: result.Completeness, AIAttemptID: attemptIDs[len(attemptIDs)-1],
	})
}

func mapProcessingError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	category, retryable := domain.FailureAIInvalidResponse, false
	switch CodeOf(err) {
	case CodeAITimeout, CodeAIUnavailable, CodeAIRateLimited:
		category, retryable = domain.FailureAIUnavailable, true
	case CodeAIAuthFailed:
		category = domain.FailureAIAuthFailed
	case CodeAIModelUnsupported:
		category = domain.FailureAIModelUnsupported
	case CodePaywallDetected:
		category = domain.FailurePaywallDetected
	case CodeAccessDenied:
		category = domain.FailureAccessDenied
	case CodeFetchFailed:
		category = domain.FailureFetchFailed
	case CodeUnsupportedContent:
		category = domain.FailureUnsupportedContent
	case CodeInsufficientContent:
		category = domain.FailureInsufficientContent
	}
	return application.NewProcessingError(string(category), "AI analysis did not approve the article", retryable)
}

func semanticText(markup string) string {
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return ""
	}
	var values []string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			if value := strings.TrimSpace(node.Data); value != "" {
				values = append(values, value)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return strings.Join(values, " ")
}

var _ Analyzer = (*Client)(nil)
