package processing_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"attic/internal/acquisition"
	"attic/internal/ai"
	"attic/internal/application"
	"attic/internal/domain"
	"attic/internal/formatter"
	"attic/internal/processing"
)

type archiveCandidates []string

func (a archiveCandidates) Candidates(context.Context, string) []string { return a }

func TestSourcePolicyContinuesAfterArchiveApprovalRejectsContent(t *testing.T) {
	original := "https://publisher.example/article"
	var requests, approvals []string
	rejected := application.NewProcessingError(string(domain.FailureInsufficientContent), "incomplete", false)
	p, err := processing.New(rendererFunc(func(_ context.Context, raw string) (acquisition.RenderedPage, error) {
		requests = append(requests, raw)
		return acquisition.RenderedPage{FinalURL: raw, Status: 200, DOM: []byte("<p>Readable but incomplete content</p>"), Screenshot: []byte("screenshot")}, nil
	}), approverFunc(func(_ context.Context, _ domain.JobID, input ai.ApprovalInput, _ func(application.ArticleDraft) (application.ApprovedArticle, error)) (application.ApprovedArticle, error) {
		approvals = append(approvals, input.RetrievedURL)
		return application.ApprovedArticle{}, rejected
	}), formatterFunc(func(context.Context, formatter.Article) (formatter.Result, error) {
		t.Fatal("unapproved source reached formatter")
		return formatter.Result{}, nil
	}), processing.WithArchives(archiveCandidates{original, "https://archive.example/1", "https://archive.example/1", "https://archive.example/2", "https://archive.example/3", "https://archive.example/4"}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Process(context.Background(), domain.Job{SubmittedURL: original}, application.ProcessorContext{
		SetStage: func(domain.Stage) error { return nil },
		ApproveArticle: func(application.ArticleDraft) (application.ApprovedArticle, error) {
			t.Fatal("unexpected approval")
			return application.ApprovedArticle{}, nil
		},
	})
	want := []string{original, "https://archive.example/1", "https://archive.example/2", "https://archive.example/3"}
	if !errors.Is(err, rejected) || !slices.Equal(requests, want) || !slices.Equal(approvals, want) {
		t.Fatalf("requests=%v approvals=%v err=%v", requests, approvals, err)
	}
}
