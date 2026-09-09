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

type savedSourceFunc func(context.Context, domain.JobID) (acquisition.RenderedPage, bool, error)

func (f savedSourceFunc) SavedPage(ctx context.Context, id domain.JobID) (acquisition.RenderedPage, bool, error) {
	return f(ctx, id)
}

type savedRendererFunc func(context.Context, string, []byte) (acquisition.RenderedPage, error)

func (f savedRendererFunc) RenderSaved(ctx context.Context, url string, body []byte) (acquisition.RenderedPage, error) {
	return f(ctx, url, body)
}

func TestSavedCapturePreparesPDFWithoutRevisitingOriginal(t *testing.T) {
	body := []byte("<article><h1>Preserved article</h1><p>" + strings.Repeat("This useful content survives a challenge on the original website. ", 20) + "</p></article>")
	renderer := rendererFunc(func(context.Context, string) (acquisition.RenderedPage, error) {
		t.Fatal("visited original website despite saved content")
		return acquisition.RenderedPage{}, errors.New("CAPTCHA")
	})
	source := savedSourceFunc(func(_ context.Context, id domain.JobID) (acquisition.RenderedPage, bool, error) {
		return acquisition.RenderedPage{DOM: body, FinalURL: "https://publisher.example/saved"}, true, nil
	})
	offline := savedRendererFunc(func(_ context.Context, url string, html []byte) (acquisition.RenderedPage, error) {
		return acquisition.RenderedPage{DOM: html, FinalURL: url, Screenshot: []byte("screenshot")}, nil
	})
	approver := approverFunc(func(_ context.Context, _ domain.JobID, in ai.ApprovalInput, approve func(application.ArticleDraft) (application.ApprovedArticle, error)) (application.ApprovedArticle, error) {
		if in.TargetLanguage != "es" {
			t.Fatalf("target language lost: %q", in.TargetLanguage)
		}
		if !strings.Contains(in.CandidateText, "useful content") || in.ScreenshotDataURL == "" {
			t.Fatal("saved text and screenshot must reach AI approval")
		}
		return approve(application.ArticleDraft{Classification: "article", Decision: "accept_candidate", Title: "Preserved article", SemanticHTML: in.CandidateHTML, PlainText: in.CandidateText, ExtractionMethod: "deterministic", AIConfidence: 1, AICompleteness: 1, AIAttemptID: "saved-approval"})
	})
	processor, err := processing.New(renderer, approver, formatterFunc(func(context.Context, formatter.Article) (formatter.Result, error) {
		return formatter.Result{PDF: []byte("%PDF-saved\nstartxref\n%%EOF")}, nil
	}), processing.WithSavedPages(source, offline), processing.WithOutputLanguages(outputLanguageFunc(func(context.Context, domain.JobID) (string, error) { return "es", nil })))
	if err != nil {
		t.Fatal(err)
	}
	store := memory.NewStore()
	archive, err := application.NewArchive(store, memory.NewArtifactStore(), application.ArchiveOptions{Processor: processor, MinContentChars: 100})
	if err != nil {
		t.Fatal(err)
	}
	item, err := archive.SubmitURL(context.Background(), application.SubmitURLRequest{URL: "https://publisher.example/saved", Profile: "a5"}, "")
	if err != nil {
		t.Fatal(err)
	}
	worker, err := application.NewWorker(archive, application.WorkerOptions{Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	if ran, err := worker.RunOnce(context.Background()); err != nil || !ran {
		t.Fatalf("worker: %v %v", ran, err)
	}
	detail, err := archive.GetJob(context.Background(), item.ID)
	if err != nil || detail.Status != domain.StatusReady || !detail.HasArtifact {
		t.Fatalf("saved PDF not ready: %+v %v", detail, err)
	}
}
