package processing_test

import (
	"attic/internal/acquisition"
	"attic/internal/ai"
	"attic/internal/application"
	"attic/internal/domain"
	"attic/internal/formatter"
	"attic/internal/memory"
	"attic/internal/processing"
	"context"
	"strings"
	"testing"
)

type recoveryFunc func(context.Context, string) (acquisition.RenderedPage, error)

func (f recoveryFunc) Recover(ctx context.Context, raw string) (acquisition.RenderedPage, error) {
	return f(ctx, raw)
}
func TestBrowserRecoveryStillRequiresArticleApproval(t *testing.T) {
	calls := 0
	approved := false
	renderer := rendererFunc(func(context.Context, string) (acquisition.RenderedPage, error) {
		return acquisition.RenderedPage{}, acquisition.ErrRenderTimeout
	})
	recovery := recoveryFunc(func(_ context.Context, raw string) (acquisition.RenderedPage, error) {
		calls++
		return acquisition.RenderedPage{FinalURL: raw, Status: 200, Screenshot: []byte("viewport"), DOM: []byte("<article><h1>Recovered article</h1><p>" + strings.Repeat("A recovered article about preserving useful public documents. ", 20) + "</p></article>")}, nil
	})
	approver := approverFunc(func(_ context.Context, _ domain.JobID, in ai.ApprovalInput, approve func(application.ArticleDraft) (application.ApprovedArticle, error)) (application.ApprovedArticle, error) {
		approved = true
		return approve(application.ArticleDraft{Classification: "article", Decision: "accept_candidate", Title: "Recovered article", SemanticHTML: in.CandidateHTML, PlainText: in.CandidateText, ExtractionMethod: "deterministic", AIConfidence: 1, AICompleteness: 1, AIAttemptID: "recovered-approval"})
	})
	processor, err := processing.New(renderer, approver, formatterFunc(func(context.Context, formatter.Article) (formatter.Result, error) {
		if !approved {
			t.Fatal("recovery bypassed approval")
		}
		return formatter.Result{PDF: []byte("%PDF-recovery\nstartxref\n%%EOF")}, nil
	}), processing.WithBrowserRecovery(recovery))
	if err != nil {
		t.Fatal(err)
	}
	store := memory.NewStore()
	archive, err := application.NewArchive(store, memory.NewArtifactStore(), application.ArchiveOptions{Processor: processor, MinContentChars: 100})
	if err != nil {
		t.Fatal(err)
	}
	item, err := archive.SubmitURL(context.Background(), application.SubmitURLRequest{URL: "https://example.com/recovered", Profile: "a5"}, "")
	if err != nil {
		t.Fatal(err)
	}
	worker, err := application.NewWorker(archive, application.WorkerOptions{Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	if ran, err := worker.RunOnce(context.Background()); err != nil || !ran {
		t.Fatal("worker did not finish", err)
	}
	detail, err := archive.GetJob(context.Background(), item.ID)
	if err != nil || detail.Status != domain.StatusReady || !detail.HasArtifact || calls != 1 || !approved {
		t.Fatal("recovered PDF failed", err)
	}
}
