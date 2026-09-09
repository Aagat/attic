// Package processing composes the bounded render, extraction, mandatory AI
// approval, and PDF formatting stages into the application processor seam.
package processing

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
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
type ArchiveResolver interface {
	Candidates(context.Context, string) []string
}

// WithArchives enables bounded recovery using existing public snapshots.
func WithArchives(resolver ArchiveResolver) func(*Processor) {
	return func(p *Processor) { p.archives = resolver }
}

type SavedPages interface {
	SavedPage(context.Context, domain.JobID) (acquisition.RenderedPage, bool, error)
}
type SavedRenderer interface {
	RenderSaved(context.Context, string, []byte) (acquisition.RenderedPage, error)
}

// WithSavedPages prioritizes preserved content without another visit to the source.
func WithSavedPages(source SavedPages, renderer SavedRenderer) func(*Processor) {
	return func(p *Processor) { p.saved = source; p.savedRenderer = renderer }
}

type BrowserRecovery interface {
	Recover(context.Context, string) (acquisition.RenderedPage, error)
}

func WithBrowserRecovery(browser BrowserRecovery) func(*Processor) {
	return func(p *Processor) { p.browserRecovery = browser }
}

type Processor struct {
	browserRecovery BrowserRecovery
	saved           SavedPages
	savedRenderer   SavedRenderer
	archives        ArchiveResolver
	renderer        acquisition.Renderer
	approver        Approver
	formatter       formatter.Formatter
}

func New(renderer acquisition.Renderer, approver Approver, pdf formatter.Formatter, options ...func(*Processor)) (*Processor, error) {
	if renderer == nil || approver == nil || pdf == nil {
		return nil, errors.New("renderer, AI approver, and formatter are required")
	}
	p := &Processor{renderer: renderer, approver: approver, formatter: pdf}
	for _, option := range options {
		option(p)
	}
	return p, nil
}

func (p *Processor) Process(ctx context.Context, job domain.Job, pc application.ProcessorContext) (application.ProcessResult, error) {
	if p == nil || p.renderer == nil || p.approver == nil || p.formatter == nil || pc.SetStage == nil || pc.ApproveArticle == nil {
		return application.ProcessResult{}, processingError(domain.FailureInternalError, false)
	}
	if p.saved != nil && p.savedRenderer != nil {
		saved, found, err := p.saved.SavedPage(ctx, job.ID)
		if err != nil {
			return application.ProcessResult{}, processingError(domain.FailureFetchFailed, true)
		}
		if found {
			page, renderErr := p.savedRenderer.RenderSaved(ctx, saved.FinalURL, saved.DOM)
			if renderErr != nil {
				return application.ProcessResult{}, mapRenderError(ctx, renderErr)
			}
			if page.Title == "" {
				page.Title = saved.Title
			}
			result, savedErr := p.processPage(ctx, job, pc, page, saved.FinalURL, true)
			if savedErr == nil || !recoverable(savedErr) || ctx.Err() != nil {
				return result, savedErr
			}
			// Only an unusable preserved article falls back to a fresh source.
			if err := pc.SetStage(domain.StageFetching); err != nil {
				return application.ProcessResult{}, err
			}
		}
	}
	result, originalErr := p.processSource(ctx, job, pc, job.SubmittedURL, false)
	if originalErr == nil || (p.archives == nil && p.browserRecovery == nil) || !recoverable(originalErr) || ctx.Err() != nil {
		return result, originalErr
	}
	// Discovery and all source attempts share a total deadline; individual browser,
	// AI and formatter limits remain in force as well.
	recoveryCtx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	slog.Info("article recovery started", "job_id", job.ID)
	var sources []string
	if p.archives != nil {
		sources = p.archives.Candidates(recoveryCtx, job.SubmittedURL)
	}
	seen := map[string]bool{job.SubmittedURL: true}
	transientArchiveFailure := false
	for i, source := range sources {
		if i >= 3 || recoveryCtx.Err() != nil {
			break
		}
		if seen[source] {
			continue
		}
		seen[source] = true
		if err := pc.SetStage(domain.StageFetching); err != nil {
			return application.ProcessResult{}, err
		}
		u, _ := url.Parse(source)
		host := ""
		if u != nil {
			host = u.Hostname()
		}
		slog.Info("trying article archive", "job_id", job.ID, "provider", host, "source_attempt", i+1)
		result, err := p.processSource(recoveryCtx, job, pc, source, true)
		if err == nil {
			return result, nil
		}
		var failure *application.ProcessingError
		blockedArchive := errors.As(err, &failure) && failure.Category == string(domain.FailureBlockedTarget)
		if failure != nil {
			transientArchiveFailure = transientArchiveFailure || failure.Retryable
			slog.Info("archive attempt failed", "job_id", job.ID, "provider", host, "category", failure.Category)
		}
		if !recoverable(err) && !blockedArchive && recoveryCtx.Err() == nil {
			return application.ProcessResult{}, err
		}
	}
	if ctx.Err() != nil {
		return application.ProcessResult{}, ctx.Err()
	}
	if p.browserRecovery != nil && recoveryCtx.Err() == nil {
		if err := pc.SetStage(domain.StageFetching); err != nil {
			return application.ProcessResult{}, err
		}
		if page, err := p.browserRecovery.Recover(recoveryCtx, job.SubmittedURL); err == nil {
			return p.processPage(recoveryCtx, job, pc, page, job.SubmittedURL, true)
		}
	}
	// Let the existing durable retry budget retry temporary archive outages too.
	var originalFailure *application.ProcessingError
	if transientArchiveFailure && errors.As(originalErr, &originalFailure) {
		return application.ProcessResult{}, application.NewProcessingError(originalFailure.Category, originalFailure.Error(), true)
	}
	return application.ProcessResult{}, originalErr
}

func recoverable(err error) bool {
	var failure *application.ProcessingError
	if !errors.As(err, &failure) {
		return false
	}
	switch domain.FailureCategory(failure.Category) {
	case domain.FailurePaywallDetected, domain.FailureAccessDenied, domain.FailureFetchFailed,
		domain.FailureRenderTimeout, domain.FailureInsufficientContent, domain.FailureUnsupportedContent,
		domain.FailurePDFQualityFailed, domain.FailureFormatFailed:
		return true
	}
	return false
}

func (p *Processor) processSource(ctx context.Context, job domain.Job, pc application.ProcessorContext, requestedURL string, archived bool) (application.ProcessResult, error) {
	// ClaimNext durably enters fetching before invoking the processor.
	page, err := p.renderer.Render(ctx, requestedURL)
	if err != nil {
		return application.ProcessResult{}, mapRenderError(ctx, err)
	}
	return p.processPage(ctx, job, pc, page, requestedURL, archived)
}

func (p *Processor) processPage(ctx context.Context, job domain.Job, pc application.ProcessorContext, page acquisition.RenderedPage, requestedURL string, archived bool) (application.ProcessResult, error) {
	if len(page.Screenshot) == 0 {
		return application.ProcessResult{}, processingError(domain.FailureFetchFailed, true)
	}
	sourceURL := strings.TrimSpace(page.FinalURL)
	if sourceURL == "" {
		sourceURL = requestedURL
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

	if archived {
		canonicalURL = sourceURL
	}
	if err := pc.SetStage(domain.StageAIAnalyzing); err != nil {
		return application.ProcessResult{}, err
	}
	approved, err := p.approver.Approve(ctx, job.ID, ai.ApprovalInput{
		SourceURL: job.SubmittedURL, RetrievedURL: sourceURL, CandidateText: candidate.PlainText, CandidateHTML: candidate.SemanticHTML,
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
		var quality *formatter.QualityError
		if errors.As(err, &quality) {
			return application.ProcessResult{}, processingError(domain.FailurePDFQualityFailed, false)
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
