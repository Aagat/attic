// Package processing composes the bounded render, extraction, mandatory AI
// approval, and PDF formatting stages into the application processor seam.
package processing

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"attic/internal/acquisition"
	"attic/internal/ai"
	"attic/internal/application"
	"attic/internal/domain"
	"attic/internal/formatter"
)

type Approver interface {
	Approve(context.Context, domain.JobID, ai.ApprovalInput, func(application.ArticleDraft) (application.ApprovedArticle, error)) (application.ApprovedArticle, error)
}

// Processor is a deep module: callers supply a job and stage callbacks while
// it owns ordering, intermediate-data lifetime, error mapping, and the durable
// approval invariant.
type Processor struct {
	renderer  acquisition.Renderer
	approver  Approver
	formatter formatter.Formatter
}

func New(renderer acquisition.Renderer, approver Approver, pdf formatter.Formatter) (*Processor, error) {
	if renderer == nil || approver == nil || pdf == nil {
		return nil, errors.New("renderer, AI approver, and formatter are required")
	}
	return &Processor{renderer: renderer, approver: approver, formatter: pdf}, nil
}

func (p *Processor) Process(ctx context.Context, job domain.Job, pc application.ProcessorContext) (application.ProcessResult, error) {
	if p == nil || p.renderer == nil || p.approver == nil || p.formatter == nil || pc.SetStage == nil || pc.ApproveArticle == nil {
		return application.ProcessResult{}, processingError(domain.FailureInternalError, false)
	}
	// ClaimNext durably enters fetching before invoking the processor.
	page, err := p.renderer.Render(ctx, job.SubmittedURL)
	if err != nil {
		return application.ProcessResult{}, mapRenderError(ctx, err)
	}
	if len(page.Screenshot) == 0 {
		return application.ProcessResult{}, processingError(domain.FailureFetchFailed, true)
	}
	sourceURL := strings.TrimSpace(page.FinalURL)
	if sourceURL == "" {
		sourceURL = job.SubmittedURL
	}

	if err := pc.SetStage(domain.StageExtracting); err != nil {
		return application.ProcessResult{}, err
	}
	candidate, extractErr := acquisition.Extract(acquisition.Page{FinalURL: sourceURL, Status: page.Status, HTML: page.DOM})
	if extractErr != nil {
		// Visual extraction is an explicit V1 fallback. Do not let a failed
		// deterministic candidate bypass the mandatory AI stage.
		candidate = acquisition.Candidate{CanonicalURL: sourceURL}
	}
	if candidate.Title == "" {
		candidate.Title = strings.TrimSpace(page.Title)
	}
	canonicalURL := strings.TrimSpace(candidate.CanonicalURL)
	if canonicalURL == "" {
		canonicalURL = sourceURL
	}

	if err := pc.SetStage(domain.StageAIAnalyzing); err != nil {
		return application.ProcessResult{}, err
	}
	approved, err := p.approver.Approve(ctx, job.ID, ai.ApprovalInput{
		SourceURL: canonicalURL, CandidateText: candidate.PlainText, CandidateHTML: candidate.SemanticHTML,
		Title: candidate.Title, Author: candidate.Author, SiteName: candidate.SiteName,
		PublicationDate: candidate.PublicationDate, Description: candidate.Description, Language: candidate.Language,
		ExtractionMethod:  candidate.ExtractionMethod,
		ScreenshotDataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(page.Screenshot),
	}, pc.ApproveArticle)
	if err != nil {
		return application.ProcessResult{}, err
	}
	draft, ok := approved.Snapshot()
	if !ok {
		return application.ProcessResult{}, processingError(domain.FailureAIInvalidResponse, false)
	}

	if err := pc.SetStage(domain.StageFormatting); err != nil {
		return application.ProcessResult{}, err
	}
	result, err := p.formatter.Format(ctx, formatter.Article{
		Profile: job.Profile, Title: draft.Title, Author: draft.Author, SiteName: draft.SiteName,
		PublicationDate: draft.PublicationDate, SourceURL: canonicalURL, SemanticHTML: draft.SemanticHTML,
		GeneratedAt: time.Now().UTC(),
	})
	if err != nil {
		if ctx.Err() != nil {
			return application.ProcessResult{}, ctx.Err()
		}
		return application.ProcessResult{}, processingError(domain.FailureFormatFailed, false)
	}
	return application.ProcessResult{Article: approved, CanonicalURL: canonicalURL, Filename: draft.Title + ".pdf", PDF: result.PDF}, nil
}

func mapRenderError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	switch {
	case errors.Is(err, acquisition.ErrBlockedTarget):
		return processingError(domain.FailureBlockedTarget, false)
	case errors.Is(err, acquisition.ErrRenderTimeout), errors.Is(err, context.DeadlineExceeded):
		return processingError(domain.FailureRenderTimeout, true)
	case errors.Is(err, acquisition.ErrDOMTooLarge), errors.Is(err, acquisition.ErrScreenshotTooLarge),
		errors.Is(err, acquisition.ErrResponseTooLarge), errors.Is(err, acquisition.ErrRequestLimit), errors.Is(err, acquisition.ErrTooManyRedirects):
		return processingError(domain.FailureFetchFailed, false)
	default:
		return processingError(domain.FailureFetchFailed, true)
	}
}

func processingError(category domain.FailureCategory, retryable bool) error {
	return application.NewProcessingError(string(category), fmt.Sprintf("processing failed: %s", category), retryable)
}

var _ application.Processor = (*Processor)(nil)
