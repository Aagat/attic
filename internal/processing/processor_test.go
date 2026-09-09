package processing_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

func TestQualityFailureNeverPublishesAnArtifact(t *testing.T) {
	body := strings.Repeat("A readable article sentence. ", 30)
	renderer := rendererFunc(func(context.Context, string) (acquisition.RenderedPage, error) {
		return acquisition.RenderedPage{FinalURL: "https://example.test/article", Title: "Article", Screenshot: []byte("png"), DOM: []byte("<article><p>" + body + "</p></article>")}, nil
	})
	approver := approverFunc(func(_ context.Context, _ domain.JobID, in ai.ApprovalInput, approve func(application.ArticleDraft) (application.ApprovedArticle, error)) (application.ApprovedArticle, error) {
		return approve(application.ArticleDraft{Classification: "article", Decision: "accept_candidate", Title: "Article", SemanticHTML: in.CandidateHTML, PlainText: in.CandidateText, ExtractionMethod: "deterministic", AIConfidence: 1, AICompleteness: 1, AIAttemptID: "attempt"})
	})
	pdf := formatterFunc(func(context.Context, formatter.Article) (formatter.Result, error) {
		return formatter.Result{}, &formatter.QualityError{Reason: "missing_images"}
	})
	processor, err := processing.New(renderer, approver, pdf)
	if err != nil {
		t.Fatal(err)
	}
	store := memory.NewStore()
	archive, err := application.NewArchive(store, memory.NewArtifactStore(), application.ArchiveOptions{Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := archive.SubmitURL(context.Background(), application.SubmitURLRequest{URL: "https://example.test/article"}, "")
	if err != nil {
		t.Fatal(err)
	}
	worker, err := application.NewWorker(archive, application.WorkerOptions{Processor: processor, MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	detail, err := archive.GetJob(context.Background(), accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != domain.StatusFailed || detail.HasArtifact || detail.Failure == nil || detail.Failure.Category != "pdf_quality_failed" {
		t.Fatalf("unsafe quality outcome: %+v", detail)
	}
}

type editedSource struct{ draft application.ArticleDraft }

func (s editedSource) ReadingEdit(context.Context, domain.JobID) (application.ArticleDraft, bool, error) {
	return s.draft, true, nil
}

func TestOwnerEditsGoDirectlyToPDFWithoutReextraction(t *testing.T) {
	draft := application.ArticleDraft{Title: "Edited", Author: "Original author", Language: "es", SemanticHTML: `<p>Owner text</p><pre><code>if (x &lt; y) return;</code></pre><table><tr><td>Kept cell</td></tr></table>`}
	renderer := rendererFunc(func(context.Context, string) (acquisition.RenderedPage, error) {
		t.Fatal("edited content fetched again")
		return acquisition.RenderedPage{}, nil
	})
	approver := approverFunc(func(context.Context, domain.JobID, ai.ApprovalInput, func(application.ArticleDraft) (application.ApprovedArticle, error)) (application.ApprovedArticle, error) {
		t.Fatal("owner edits sent to AI")
		return application.ApprovedArticle{}, nil
	})
	formatted := false
	pdf := formatterFunc(func(_ context.Context, article formatter.Article) (formatter.Result, error) {
		formatted = true
		if article.Author != draft.Author || !strings.Contains(article.SemanticHTML, "Owner text") || !strings.Contains(article.SemanticHTML, "<table>") || !strings.Contains(article.SemanticHTML, "<pre>") {
			t.Fatalf("format input: %+v", article)
		}
		return formatter.Result{PDF: []byte("%PDF-edited")}, nil
	})
	p, err := processing.New(renderer, approver, pdf, processing.WithReadingEdits(editedSource{draft}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Process(context.Background(), domain.Job{ID: "edited-job", SubmittedURL: "https://example.com", Profile: "kindle-scribe"}, application.ProcessorContext{
		ApproveArticle: func(application.ArticleDraft) (application.ApprovedArticle, error) {
			t.Fatal("AI callback called")
			return application.ApprovedArticle{}, nil
		},
		SetStage: func(domain.Stage) error { return nil },
	})
	if err != nil || !formatted || len(result.PDF) == 0 {
		t.Fatalf("process: %+v %v", result, err)
	}
	saved, ok := result.Article.Snapshot()
	if !ok || saved.ExtractionMethod != "owner_edit" || saved.AIAttemptID != "" {
		t.Fatalf("provenance: %+v", saved)
	}
}

func TestOwnerEditsRealPDF(t *testing.T) {
	if os.Getenv("ATTIC_TEST_REAL_PDF") != "1" {
		t.Skip("requires Pandoc, XeLaTeX and Poppler")
	}
	draft := application.ArticleDraft{Title: "A cleaner reading copy", Author: "Attic test", SemanticHTML: `<article><p>Edited introduction. This copy keeps the useful article and removes the newsletter signup.</p><h2>Implementation</h2><pre><code>if (saved) {
  render(document);
}</code></pre><table><tr><th>Feature</th><th>Result</th></tr><tr><td>Reading edits</td><td>Preserved</td></tr></table><p>The final paragraph remains present in the generated document.</p></article>`}
	forbidden := errors.New("edited sources must not be fetched or rewritten")
	p, err := processing.New(rendererFunc(func(context.Context, string) (acquisition.RenderedPage, error) {
		return acquisition.RenderedPage{}, forbidden
	}), approverFunc(func(context.Context, domain.JobID, ai.ApprovalInput, func(application.ArticleDraft) (application.ApprovedArticle, error)) (application.ApprovedArticle, error) {
		return application.ApprovedArticle{}, forbidden
	}), formatter.Checked{Renderer: formatter.PDF{}}, processing.WithReadingEdits(editedSource{draft}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Process(context.Background(), domain.Job{ID: "real-edit", SubmittedURL: "https://example.com/article", Profile: "kindle-scribe"}, application.ProcessorContext{ApproveArticle: application.ApproveReadingEdit, SetStage: func(domain.Stage) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("ATTIC_TEST_PDF_OUTPUT")
	if dir == "" {
		dir = t.TempDir()
	}
	path := filepath.Join(dir, "edited.pdf")
	if err := os.WriteFile(path, result.PDF, 0644); err != nil {
		t.Fatal(err)
	}
	text, err := exec.Command("pdftotext", path, "-").Output()
	if err != nil || !strings.Contains(string(text), "Edited introduction") || !strings.Contains(string(text), "Preserved") || !strings.Contains(string(text), "render(document)") {
		t.Fatalf("PDF text: %s %v", text, err)
	}
}
