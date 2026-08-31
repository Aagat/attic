package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"attic/internal/ai"
	"attic/internal/application"
	"attic/internal/domain"
	"attic/internal/memory"
)

type attemptRecorder struct {
	attempts []application.AIAttempt
	err      error
}

func (r *attemptRecorder) RecordAIAttempts(_ context.Context, _ domain.JobID, attempts []application.AIAttempt) ([]string, error) {
	r.attempts = append(r.attempts, attempts...)
	if r.err != nil {
		return nil, r.err
	}
	ids := make([]string, len(attempts))
	for i := range ids {
		ids[i] = "durable-attempt-" + string(rune('1'+i))
	}
	return ids, nil
}

func TestArticleApproverFakeProviderProducesApplicationApprovedArticle(t *testing.T) {
	body := strings.Repeat("Readable primary article text. ", 30)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		messages := request["messages"].([]any)
		parts := messages[1].(map[string]any)["content"].([]any)
		if parts[1].(map[string]any)["type"] != "image_url" {
			t.Errorf("request has no image input: %#v", parts)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "safe-provider-id")
		_, _ = w.Write([]byte(`{"id":"completion-id","choices":[{"message":{"role":"assistant","content":"{\"classification\":\"article\",\"decision\":\"accept_candidate\",\"title\":\"Approved title\",\"completeness\":0.95,\"confidence\":0.9,\"decision_reason\":\"complete\"}"}}],"usage":{"prompt_tokens":11,"completion_tokens":7}}`))
	}))
	defer server.Close()

	client, err := ai.NewClient(ai.Config{BaseURL: server.URL + "/v1", APIKey: "test-key", AllowInsecureHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	recorder := &attemptRecorder{}
	approver, err := ai.NewArticleApprover(client, recorder)
	if err != nil {
		t.Fatal(err)
	}
	store := memory.NewStore()
	artifacts := memory.NewArtifactStore()
	var pipelineErr error
	processor := application.ProcessorFunc(func(ctx context.Context, job domain.Job, pc application.ProcessorContext) (application.ProcessResult, error) {
		if err := pc.SetStage(domain.StageExtracting); err != nil {
			return application.ProcessResult{}, err
		}
		if err := pc.SetStage(domain.StageAIAnalyzing); err != nil {
			return application.ProcessResult{}, err
		}
		article, err := approver.Approve(ctx, job.ID, ai.ApprovalInput{
			SourceURL: job.SubmittedURL, CandidateText: body,
			CandidateHTML: "<article><p>" + body + "</p></article>", Title: "Candidate title",
			ExtractionMethod: "deterministic", ScreenshotDataURL: "data:image/png;base64,aGVsbG8=",
		}, pc.ApproveArticle)
		if err != nil {
			pipelineErr = err
			return application.ProcessResult{}, err
		}
		if err := pc.SetStage(domain.StageFormatting); err != nil {
			return application.ProcessResult{}, err
		}
		return application.ProcessResult{Article: article, Filename: "approved.pdf", PDF: []byte("%PDF-test")}, nil
	})
	archive, err := application.NewArchive(store, artifacts, application.ArchiveOptions{
		MinContentChars: 100, Processor: processor, NewJobID: func() domain.JobID { return "job-ai" },
		NewContentID: func() domain.ContentID { return "content-ai" },
	})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := archive.SubmitURL(context.Background(), application.SubmitURLRequest{URL: "https://example.test/article", Profile: "a5"}, "")
	if err != nil {
		t.Fatal(err)
	}
	worker, err := application.NewWorker(archive, application.WorkerOptions{Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := worker.RunOnce(context.Background()); err != nil || !claimed {
		t.Fatalf("RunOnce() = %v, %v", claimed, err)
	}
	detail, err := archive.GetJob(context.Background(), accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != domain.StatusReady || detail.Metadata == nil || detail.Metadata.Title != "Approved title" || !detail.HasArtifact {
		t.Fatalf("completed job = %#v; pipeline error = %#v; attempts = %#v", detail, pipelineErr, recorder.attempts)
	}
	if len(recorder.attempts) != 1 || recorder.attempts[0].Status != "succeeded" || recorder.attempts[0].ProviderRequestID != "safe-provider-id" || recorder.attempts[0].InputTokens != 11 {
		t.Fatalf("durable attempts = %#v", recorder.attempts)
	}
}

type analyzerFunc func(context.Context, ai.AnalyzeRequest) (ai.Approved, []ai.Attempt, error)

func (f analyzerFunc) Analyze(ctx context.Context, request ai.AnalyzeRequest) (ai.Approved, []ai.Attempt, error) {
	return f(ctx, request)
}

func TestArticleApproverMapsProviderFailuresAndRecordsAttemptFirst(t *testing.T) {
	tests := []struct {
		name      string
		provider  error
		category  string
		retryable bool
	}{
		{"rate limit", &ai.Error{Code: ai.CodeAIRateLimited}, string(domain.FailureAIUnavailable), true},
		{"server unavailable", &ai.Error{Code: ai.CodeAIUnavailable}, string(domain.FailureAIUnavailable), true},
		{"authentication", &ai.Error{Code: ai.CodeAIAuthFailed}, string(domain.FailureAIAuthFailed), false},
		{"unsupported model", &ai.Error{Code: ai.CodeAIModelUnsupported}, string(domain.FailureAIModelUnsupported), false},
		{"malformed", &ai.Error{Code: ai.CodeAIInvalidResponse}, string(domain.FailureAIInvalidResponse), false},
		{"paywall", &ai.Error{Code: ai.CodePaywallDetected}, string(domain.FailurePaywallDetected), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := &attemptRecorder{}
			analyzer := analyzerFunc(func(context.Context, ai.AnalyzeRequest) (ai.Approved, []ai.Attempt, error) {
				return ai.Approved{}, []ai.Attempt{{Number: 1, Model: "fake", PromptVersion: "v1", Status: ai.AttemptFailed, ErrorCode: ai.CodeOf(test.provider), CreatedAt: time.Now()}}, test.provider
			})
			approver, err := ai.NewArticleApprover(analyzer, recorder)
			if err != nil {
				t.Fatal(err)
			}
			_, err = approver.Approve(context.Background(), "job", ai.ApprovalInput{}, func(application.ArticleDraft) (application.ApprovedArticle, error) {
				t.Fatal("approval callback called after provider failure")
				return application.ApprovedArticle{}, nil
			})
			var processing *application.ProcessingError
			if !errors.As(err, &processing) || processing.Category != test.category || processing.Retryable != test.retryable {
				t.Fatalf("error = %#v", err)
			}
			if len(recorder.attempts) != 1 || recorder.attempts[0].ErrorCategory == "" {
				t.Fatalf("attempt was not recorded first: %#v", recorder.attempts)
			}
		})
	}
}

func TestArticleApproverTreatsAttemptPersistenceFailureAsRetryableStorageFailure(t *testing.T) {
	recorder := &attemptRecorder{err: errors.New("database detail must not escape")}
	analyzer := analyzerFunc(func(context.Context, ai.AnalyzeRequest) (ai.Approved, []ai.Attempt, error) {
		return ai.Approved{Classification: "article", Decision: "accept_candidate"}, []ai.Attempt{{Status: ai.AttemptSucceeded}}, nil
	})
	approver, _ := ai.NewArticleApprover(analyzer, recorder)
	_, err := approver.Approve(context.Background(), "job", ai.ApprovalInput{}, func(application.ArticleDraft) (application.ApprovedArticle, error) {
		t.Fatal("approval callback called without durable attempt")
		return application.ApprovedArticle{}, nil
	})
	var processing *application.ProcessingError
	if !errors.As(err, &processing) || processing.Category != string(domain.FailureStorageFailed) || !processing.Retryable || strings.Contains(err.Error(), "database detail") {
		t.Fatalf("error = %#v", err)
	}
}

func TestArticleApproverRejectsReplacementWhoseOnlyTextIsUnsafe(t *testing.T) {
	recorder := &attemptRecorder{}
	analyzer := analyzerFunc(func(context.Context, ai.AnalyzeRequest) (ai.Approved, []ai.Attempt, error) {
		return ai.Approved{
			Classification: "article", Decision: "replace_candidate", Title: "Unsafe",
			ContentHTML: "<article><script>" + strings.Repeat("not article text ", 100) + "</script></article>",
			Confidence:  0.9, Completeness: 0.9,
		}, []ai.Attempt{{Status: ai.AttemptSucceeded}}, nil
	})
	approver, _ := ai.NewArticleApprover(analyzer, recorder)
	_, err := approver.Approve(context.Background(), "job", ai.ApprovalInput{}, func(application.ArticleDraft) (application.ApprovedArticle, error) {
		t.Fatal("unsafe replacement crossed the application approval seam")
		return application.ApprovedArticle{}, nil
	})
	var processing *application.ProcessingError
	if !errors.As(err, &processing) || processing.Category != string(domain.FailureInsufficientContent) {
		t.Fatalf("error = %#v", err)
	}
	if len(recorder.attempts) != 1 {
		t.Fatalf("successful provider attempt was not recorded: %#v", recorder.attempts)
	}
}
