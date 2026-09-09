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
	"attic/internal/capture"
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

type OutputLanguages interface {
	OutputLanguage(context.Context, domain.JobID) (string, error)
}

func WithOutputLanguages(source OutputLanguages) func(*Processor) {
	return func(p *Processor) { p.outputLanguages = source }
}

type ReadingEdits interface {
	ReadingEdit(context.Context, domain.JobID) (application.ArticleDraft, bool, error)
}

func WithReadingEdits(source ReadingEdits) func(*Processor) {
	return func(p *Processor) { p.readingEdits = source }
}

type Processor struct {
	readingEdits    ReadingEdits
	outputLanguages OutputLanguages
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
	p.savedRenderer, _ = renderer.(SavedRenderer)
	for _, option := range options {
		option(p)
	}
	return p, nil
}

func (p *Processor) Process(ctx context.Context, job domain.Job, pc application.ProcessorContext) (application.ProcessResult, error) {
	if p == nil || p.renderer == nil || p.approver == nil || p.formatter == nil || pc.SetStage == nil || pc.ApproveArticle == nil {
		return application.ProcessResult{}, processingError(domain.FailureInternalError, false)
	}
	if p.readingEdits != nil {
		draft, found, err := p.readingEdits.ReadingEdit(ctx, job.ID)
		if err != nil {
			return application.ProcessResult{}, processingError(domain.FailureStorageFailed, true)
		}
		if found {
			approved, err := application.ApproveReadingEdit(draft)
			if err != nil {
				return application.ProcessResult{}, err
			}
			return p.formatArticle(ctx, job, pc, approved, job.SubmittedURL)
		}
	}
	targetLanguage := ""
	if p.outputLanguages != nil {
		var err error
		targetLanguage, err = p.outputLanguages.OutputLanguage(ctx, job.ID)
		if err != nil {
			return application.ProcessResult{}, processingError(domain.FailureStorageFailed, true)
		}
		if !ai.ValidTargetLanguage(targetLanguage) {
			return application.ProcessResult{}, processingError(domain.FailureAIInvalidResponse, false)
		}
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
			result, savedErr := p.processPage(ctx, job, pc, page, saved.FinalURL, true, targetLanguage)
			if savedErr == nil || !recoverable(savedErr) || ctx.Err() != nil {
				return result, savedErr
			}
			// Only an unusable preserved article falls back to a fresh source.
			if err := pc.SetStage(domain.StageFetching); err != nil {
				return application.ProcessResult{}, err
			}
		}
	}
	// The same source policy serves preservation and PDF preparation; only
	// acceptance differs. Every usable source still crosses mandatory approval.
	recoveryCtx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	var originalErr error
	transientArchiveFailure := false
	var fallback *capture.Source
	attempt := 0
	for source := range capture.New(sourceRenderer{p.renderer}, p.archives).Sources(recoveryCtx, job.SubmittedURL) {
		if source.Fallback && source.Err == nil {
			copy := source
			fallback = &copy
			continue
		}
		if attempt > 0 {
			if err := pc.SetStage(domain.StageFetching); err != nil {
				return application.ProcessResult{}, err
			}
		}
		attempt++
		result, err := p.processAttempt(recoveryCtx, job, pc, source, targetLanguage)
		if err == nil {
			return result, nil
		}
		if source.URL == job.SubmittedURL || originalErr == nil {
			originalErr = err
		}
		var failure *application.ProcessingError
		blockedArchive := errors.As(err, &failure) && failure.Category == string(domain.FailureBlockedTarget) && source.URL != job.SubmittedURL
		if failure != nil && source.URL != job.SubmittedURL {
			transientArchiveFailure = transientArchiveFailure || failure.Retryable
			u, _ := url.Parse(source.URL)
			host := ""
			if u != nil {
				host = u.Hostname()
			}
			slog.Info("source attempt failed", "job_id", job.ID, "provider", host, "category", failure.Category)
		}
		if !recoverable(err) && !blockedArchive && recoveryCtx.Err() == nil {
			return application.ProcessResult{}, err
		}
	}
	if fallback != nil && recoveryCtx.Err() == nil {
		if err := pc.SetStage(domain.StageFetching); err != nil {
			return application.ProcessResult{}, err
		}
		result, err := p.processAttempt(recoveryCtx, job, pc, *fallback, targetLanguage)
		if err == nil || !recoverable(err) {
			return result, err
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
			return p.processPage(recoveryCtx, job, pc, page, job.SubmittedURL, true, targetLanguage)
		}
	}
	// Let the existing durable retry budget retry temporary archive outages too.
	var originalFailure *application.ProcessingError
	if transientArchiveFailure && errors.As(originalErr, &originalFailure) {
		return application.ProcessResult{}, application.NewProcessingError(originalFailure.Category, originalFailure.Error(), true)
	}
	if originalErr == nil {
		originalErr = mapRenderError(recoveryCtx, recoveryCtx.Err())
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

// sourceRenderer keeps browser output intact for visual article approval.
type sourceRenderer struct{ acquisition.Renderer }

func (r sourceRenderer) Snapshot(ctx context.Context, raw string) (acquisition.RenderedPage, error) {
	return r.Render(ctx, raw)
}

func (p *Processor) processAttempt(ctx context.Context, job domain.Job, pc application.ProcessorContext, source capture.Source, targetLanguage string) (application.ProcessResult, error) {
	if source.Err != nil {
		return application.ProcessResult{}, mapRenderError(ctx, source.Err)
	}
	page := source.Page
	if source.Static != nil {
		if p.savedRenderer == nil {
			return application.ProcessResult{}, processingError(domain.FailureUnsupportedContent, false)
		}
		var err error
		page, err = p.savedRenderer.RenderSaved(ctx, source.Static.FinalURL, source.Static.HTML)
		if err != nil {
			return application.ProcessResult{}, mapRenderError(ctx, err)
		}
		if page.Title == "" {
			page.Title = source.Static.Title
		}
	}
	return p.processPage(ctx, job, pc, page, source.URL, source.URL != job.SubmittedURL, targetLanguage)
}

func (p *Processor) processPage(ctx context.Context, job domain.Job, pc application.ProcessorContext, page acquisition.RenderedPage, requestedURL string, archived bool, targetLanguage string) (application.ProcessResult, error) {
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
		TargetLanguage: targetLanguage,
		SourceURL:      job.SubmittedURL, RetrievedURL: sourceURL, CandidateText: candidate.PlainText, CandidateHTML: candidate.SemanticHTML,
		Title: candidate.Title, Author: candidate.Author, SiteName: candidate.SiteName,
		PublicationDate: candidate.PublicationDate, Description: candidate.Description, Language: candidate.Language,
		ExtractionMethod:  candidate.ExtractionMethod,
		ScreenshotDataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(page.Screenshot),
	}, pc.ApproveArticle)
	if err != nil {
		return application.ProcessResult{}, err
	}
	return p.formatArticle(ctx, job, pc, approved, canonicalURL)
}

func (p *Processor) formatArticle(ctx context.Context, job domain.Job, pc application.ProcessorContext, approved application.ApprovedArticle, canonicalURL string) (application.ProcessResult, error) {
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
