package processing_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"attic/internal/acquisition"
	"attic/internal/ai"
	"attic/internal/application"
	"attic/internal/domain"
	"attic/internal/formatter"
	"attic/internal/memory"
	"attic/internal/processing"
)

type archiveFunc func(context.Context, string) []string

func (f archiveFunc) Candidates(ctx context.Context, source string) []string { return f(ctx, source) }

func TestRecoveryUsesNextArchiveAndPublishesOnlyApprovedPDF(t *testing.T) {
	for _, category := range []domain.FailureCategory{domain.FailurePaywallDetected, domain.FailureAccessDenied, domain.FailureInsufficientContent} {
		t.Run(string(category), func(t *testing.T) {
			original := "https://publisher.example/article"
			first := "https://archive.ph/newest/" + original
			second := "https://web.archive.org/web/20260907/" + original
			var renders []string
			renderer := rendererFunc(func(_ context.Context, source string) (acquisition.RenderedPage, error) {
				renders = append(renders, source)
				if source == first {
					return acquisition.RenderedPage{}, errors.New("archive unavailable")
				}
				return acquisition.RenderedPage{FinalURL: source, Screenshot: []byte("png"), DOM: []byte("<article><h1>Complete article</h1><p>" + strings.Repeat("Preserved article text. ", 30) + "</p></article>")}, nil
			})
			approver := approverFunc(func(_ context.Context, _ domain.JobID, in ai.ApprovalInput, approve func(application.ArticleDraft) (application.ApprovedArticle, error)) (application.ApprovedArticle, error) {
				if in.SourceURL != original {
					t.Fatalf("lost original attribution: %s", in.SourceURL)
				}
				if in.RetrievedURL == original {
					return application.ApprovedArticle{}, application.NewProcessingError(string(category), "source rejected", false)
				}
				return approve(application.ArticleDraft{Classification: "article", Decision: "accept_candidate", Title: "Complete article", SemanticHTML: in.CandidateHTML, PlainText: in.CandidateText, ExtractionMethod: "deterministic", AIConfidence: 1, AICompleteness: 1, AIAttemptID: "archive-approval"})
			})
			pdf := formatterFunc(func(_ context.Context, a formatter.Article) (formatter.Result, error) {
				if a.SourceURL != second {
					t.Fatalf("lost snapshot provenance: %s", a.SourceURL)
				}
				return formatter.Result{PDF: []byte("%PDF-approved archive")}, nil
			})
			p, _ := processing.New(renderer, approver, pdf, processing.WithArchives(archiveFunc(func(context.Context, string) []string { return []string{first, second} })))
			store := memory.NewStore()
			app, _ := application.NewArchive(store, memory.NewArtifactStore(), application.ArchiveOptions{Processor: p})
			worker, _ := application.NewWorker(app, application.WorkerOptions{Processor: p, MaxAttempts: 1})
			// Two jobs exercise per-job recovery budgets and real leased stage transitions.
			for i := 0; i < 2; i++ {
				accepted, err := app.SubmitURL(context.Background(), application.SubmitURLRequest{URL: original}, "")
				if err != nil {
					t.Fatal(err)
				}
				if _, err = worker.RunOnce(context.Background()); err != nil {
					t.Fatal(err)
				}
				detail, err := app.GetJob(context.Background(), accepted.ID)
				if err != nil {
					t.Fatal(err)
				}
				if detail.Status != domain.StatusReady || !detail.HasArtifact || detail.CanonicalURL != second {
					t.Fatalf("recovery result: %+v", detail)
				}
			}
			if len(renders) != 6 {
				t.Fatalf("source attempts: %v", renders)
			}
		})
	}
}

func TestRecoveryExhaustionAndFatalFailures(t *testing.T) {
	for _, category := range []domain.FailureCategory{domain.FailurePaywallDetected, domain.FailureAIAuthFailed, domain.FailureStorageFailed, domain.FailureBlockedTarget} {
		t.Run(string(category), func(t *testing.T) {
			calls := 0
			discoveries := 0
			p, _ := processing.New(rendererFunc(func(context.Context, string) (acquisition.RenderedPage, error) {
				calls++
				return acquisition.RenderedPage{Screenshot: []byte("png")}, nil
			}), approverFunc(func(context.Context, domain.JobID, ai.ApprovalInput, func(application.ArticleDraft) (application.ApprovedArticle, error)) (application.ApprovedArticle, error) {
				return application.ApprovedArticle{}, application.NewProcessingError(string(category), "rejected", false)
			}), formatterFunc(func(context.Context, formatter.Article) (formatter.Result, error) {
				t.Fatal("published rejected source")
				return formatter.Result{}, nil
			}), processing.WithArchives(archiveFunc(func(context.Context, string) []string {
				discoveries++
				return []string{"https://archive.ph/a", "https://archive.is/b", "https://web.archive.org/c", "https://archive.ph/d"}
			})))
			_, err := p.Process(context.Background(), domain.Job{SubmittedURL: "https://example.com"}, processorContext())
			var failure *application.ProcessingError
			if !errors.As(err, &failure) || failure.Category != string(category) {
				t.Fatalf("failure=%v", err)
			}
			want := 1
			if category == domain.FailurePaywallDetected {
				want = 4
			}
			if calls != want || (want == 1 && discoveries != 0) {
				t.Fatalf("calls=%d discoveries=%d", calls, discoveries)
			}
		})
	}
}

func TestCancellationDoesNotStartArchiveDiscovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p, _ := processing.New(rendererFunc(func(context.Context, string) (acquisition.RenderedPage, error) {
		cancel()
		return acquisition.RenderedPage{}, context.Canceled
	}), approverFunc(func(context.Context, domain.JobID, ai.ApprovalInput, func(application.ArticleDraft) (application.ApprovedArticle, error)) (application.ApprovedArticle, error) {
		t.Fatal("AI after cancellation")
		return application.ApprovedArticle{}, nil
	}), formatterFunc(func(context.Context, formatter.Article) (formatter.Result, error) {
		t.Fatal("format after cancellation")
		return formatter.Result{}, nil
	}), processing.WithArchives(archiveFunc(func(context.Context, string) []string { t.Fatal("discovery after cancellation"); return nil })))
	if _, err := p.Process(ctx, domain.Job{SubmittedURL: "https://example.com"}, processorContext()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
