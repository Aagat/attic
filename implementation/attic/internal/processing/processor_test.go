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

type rendererFunc func(context.Context, string) (acquisition.RenderedPage, error)

func (f rendererFunc) Render(ctx context.Context, raw string) (acquisition.RenderedPage, error) {
	return f(ctx, raw)
}

type approverFunc func(context.Context, domain.JobID, ai.ApprovalInput, func(application.ArticleDraft) (application.ApprovedArticle, error)) (application.ApprovedArticle, error)

func (f approverFunc) Approve(ctx context.Context, id domain.JobID, in ai.ApprovalInput, approve func(application.ArticleDraft) (application.ApprovedArticle, error)) (application.ApprovedArticle, error) {
	return f(ctx, id, in, approve)
}

type formatterFunc func(context.Context, formatter.Article) (formatter.Result, error)

func (f formatterFunc) Format(ctx context.Context, article formatter.Article) (formatter.Result, error) {
	return f(ctx, article)
}

func TestFakeEndToEndPersistsOnlyAfterMandatoryDurableAIApproval(t *testing.T) {
	body := strings.Repeat("A complete readable article sentence. ", 25)
	durableApproval := false
	renderer := rendererFunc(func(context.Context, string) (acquisition.RenderedPage, error) {
		return acquisition.RenderedPage{
			FinalURL: "https://publisher.example/final", Title: "Rendered title", Screenshot: []byte("png"),
			DOM: []byte("<html><head><title>Candidate</title></head><body><article><p>" + body + "</p></article></body></html>"),
		}, nil
	})
	approver := approverFunc(func(_ context.Context, _ domain.JobID, input ai.ApprovalInput, approve func(application.ArticleDraft) (application.ApprovedArticle, error)) (application.ApprovedArticle, error) {
		if !strings.HasPrefix(input.ScreenshotDataURL, "data:image/png;base64,") || input.CandidateText == "" {
			t.Fatalf("unbounded pipeline handoff = %#v", input)
		}
		// This models ArticleApprover's record-before-callback contract.
		durableApproval = true
		return approve(application.ArticleDraft{
			Classification: "article", Decision: "accept_candidate", Title: "AI approved",
			SemanticHTML: input.CandidateHTML, PlainText: input.CandidateText, ExtractionMethod: "deterministic",
			AIConfidence: .9, AICompleteness: .95, AIAttemptID: "durable-attempt",
		})
	})
	pdf := formatterFunc(func(_ context.Context, article formatter.Article) (formatter.Result, error) {
		if !durableApproval {
			t.Fatal("formatter ran before durable AI approval")
		}
		if article.Title != "AI approved" {
			t.Fatalf("formatted title = %q", article.Title)
		}
		return formatter.Result{PDF: []byte("%PDF-fake\nstartxref\n%%EOF"), Profile: article.Profile}, nil
	})
	processor, err := processing.New(renderer, approver, pdf)
	if err != nil {
		t.Fatal(err)
	}
	store := memory.NewStore()
	artifacts := memory.NewArtifactStore()
	archive, err := application.NewArchive(store, artifacts, application.ArchiveOptions{
		Processor: processor, MinContentChars: 100,
		NewJobID: func() domain.JobID { return "job-e2e" }, NewContentID: func() domain.ContentID { return "content-e2e" },
	})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := archive.SubmitURL(context.Background(), application.SubmitURLRequest{URL: "https://publisher.example/article", Profile: "a5"}, "")
	if err != nil {
		t.Fatal(err)
	}
	worker, err := application.NewWorker(archive, application.WorkerOptions{Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := worker.RunOnce(context.Background())
	if err != nil || !claimed {
		t.Fatalf("RunOnce() = %v, %v", claimed, err)
	}
	detail, err := archive.GetJob(context.Background(), accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != domain.StatusReady || detail.Title != "AI approved" || !detail.HasArtifact {
		t.Fatalf("completed detail = %#v", detail)
	}
}

func TestExtractionFailureStillUsesScreenshotAndAI(t *testing.T) {
	called := false
	renderer := rendererFunc(func(context.Context, string) (acquisition.RenderedPage, error) {
		return acquisition.RenderedPage{FinalURL: "https://example.com", Title: "Visual", DOM: []byte("<html><p>short</p></html>"), Screenshot: []byte{1, 2, 3}}, nil
	})
	approver := approverFunc(func(_ context.Context, _ domain.JobID, input ai.ApprovalInput, _ func(application.ArticleDraft) (application.ApprovedArticle, error)) (application.ApprovedArticle, error) {
		called = true
		if input.CandidateText != "" || input.Title != "Visual" || input.ScreenshotDataURL != "data:image/png;base64,AQID" {
			t.Fatalf("visual fallback input = %#v", input)
		}
		return application.ApprovedArticle{}, application.NewProcessingError(string(domain.FailureUnsupportedContent), "rejected", false)
	})
	processor, _ := processing.New(renderer, approver, formatterFunc(func(context.Context, formatter.Article) (formatter.Result, error) {
		t.Fatal("formatting must not run after AI rejection")
		return formatter.Result{}, nil
	}))
	_, err := processor.Process(context.Background(), domain.Job{ID: "job", SubmittedURL: "https://example.com"}, processorContext())
	if !called || err == nil {
		t.Fatalf("called=%v err=%v", called, err)
	}
}

func TestProcessorMapsTypedRenderErrors(t *testing.T) {
	tests := []struct {
		name     string
		input    error
		category domain.FailureCategory
		retry    bool
	}{
		{"blocked", acquisition.ErrBlockedTarget, domain.FailureBlockedTarget, false},
		{"timeout", acquisition.ErrRenderTimeout, domain.FailureRenderTimeout, true},
		{"browser", errors.New("browser crashed"), domain.FailureFetchFailed, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processor, _ := processing.New(rendererFunc(func(context.Context, string) (acquisition.RenderedPage, error) {
				return acquisition.RenderedPage{}, test.input
			}),
				approverFunc(func(context.Context, domain.JobID, ai.ApprovalInput, func(application.ArticleDraft) (application.ApprovedArticle, error)) (application.ApprovedArticle, error) {
					t.Fatal("AI called")
					return application.ApprovedArticle{}, nil
				}),
				formatterFunc(func(context.Context, formatter.Article) (formatter.Result, error) {
					t.Fatal("formatter called")
					return formatter.Result{}, nil
				}))
			_, err := processor.Process(context.Background(), domain.Job{SubmittedURL: "https://example.com"}, processorContext())
			var typed *application.ProcessingError
			if !errors.As(err, &typed) || typed.Category != string(test.category) || typed.Retryable != test.retry {
				t.Fatalf("error = %#v", err)
			}
		})
	}
}

func processorContext() application.ProcessorContext {
	return application.ProcessorContext{
		SetStage: func(domain.Stage) error { return nil },
		ApproveArticle: func(application.ArticleDraft) (application.ApprovedArticle, error) {
			return application.ApprovedArticle{}, errors.New("not used")
		},
	}
}
