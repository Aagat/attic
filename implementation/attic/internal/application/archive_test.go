package application_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"attic/internal/application"
	"attic/internal/domain"
	"attic/internal/memory"
)

func testArchive(t *testing.T, processor application.Processor) (*application.Archive, *memory.Store, *memory.ArtifactStore) {
	t.Helper()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	store := memory.NewStoreWithClock(func() time.Time { return now })
	artifacts := memory.NewArtifactStore()
	ids := 0
	archive, err := application.NewArchive(store, artifacts, application.ArchiveOptions{
		Profiles: map[string]struct{}{"a5": {}},
		Now:      func() time.Time { return now },
		NewJobID: func() domain.JobID {
			ids++
			return domain.JobID("job-" + string(rune('0'+ids)))
		},
		NewContentID: func() domain.ContentID { return domain.ContentID("content-1") },
	})
	if err != nil {
		t.Fatal(err)
	}
	archive.SetProcessor(processor)
	return archive, store, artifacts
}

func successfulProcessor(ctx context.Context, job domain.Job, pc application.ProcessorContext) (application.ProcessResult, error) {
	if err := pc.SetStage(domain.StageExtracting); err != nil {
		return application.ProcessResult{}, err
	}
	if err := pc.SetStage(domain.StageAIAnalyzing); err != nil {
		return application.ProcessResult{}, err
	}
	article, err := pc.ApproveArticle(application.ArticleDraft{
		Classification: "article",
		Decision:       "accept_candidate",
		Title:          "A safe article",
		Author:         "Author",
		SiteName:       "Example",
		PlainText:      strings.Repeat("Readable article text. ", 40),
		SemanticHTML:   "<article><p>Readable article text.</p></article>",
		AIConfidence:   0.95,
		AIAttemptID:    "ai-attempt-1",
	})
	if err != nil {
		return application.ProcessResult{}, err
	}
	if err := pc.SetStage(domain.StageFormatting); err != nil {
		return application.ProcessResult{}, err
	}
	return application.ProcessResult{Article: article, PDF: []byte("%PDF-1.7 test artifact"), Filename: "A safe article.pdf"}, nil
}

func newWorker(t *testing.T, archive *application.Archive) *application.Worker {
	t.Helper()
	worker, err := application.NewWorker(archive, application.WorkerOptions{PollInterval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

func TestSubmitIdempotencyAndValidation(t *testing.T) {
	archive, _, _ := testArchive(t, application.ProcessorFunc(successfulProcessor))
	ctx := context.Background()
	first, err := archive.SubmitURL(ctx, application.SubmitURLRequest{URL: "https://example.test/read?token=secret", Title: "Hint"}, "key-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := archive.SubmitURL(ctx, application.SubmitURLRequest{URL: "https://example.test/read?token=secret", Title: "Hint"}, "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Status != domain.StatusQueued {
		t.Fatalf("idempotent replay = %#v, want original queued job %#v", second, first)
	}
	_, err = archive.SubmitURL(ctx, application.SubmitURLRequest{URL: "https://example.test/other"}, "key-1")
	var safe *application.SafeError
	if !errors.As(err, &safe) || safe.Code != "idempotency_conflict" || safe.HTTPStatus != 409 {
		t.Fatalf("conflicting idempotency error = %#v, want safe 409", err)
	}
	_, err = archive.SubmitURL(ctx, application.SubmitURLRequest{URL: "file:///secret"}, "")
	if !errors.As(err, &safe) || safe.Code != "invalid_input" {
		t.Fatalf("invalid URL error = %#v", err)
	}
}

func TestWorkerMandatoryApprovalAndArtifact(t *testing.T) {
	archive, _, _ := testArchive(t, application.ProcessorFunc(successfulProcessor))
	ctx := context.Background()
	accepted, err := archive.SubmitURL(ctx, application.SubmitURLRequest{URL: "https://example.test/article"}, "")
	if err != nil {
		t.Fatal(err)
	}
	worker := newWorker(t, archive)
	claimed, err := worker.RunOnce(ctx)
	if err != nil || !claimed {
		t.Fatalf("RunOnce = claimed %v, err %v", claimed, err)
	}
	detail, err := archive.GetJob(ctx, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != domain.StatusReady || !detail.HasArtifact || detail.Metadata == nil || detail.Metadata.Title != "A safe article" {
		t.Fatalf("completed detail = %#v", detail)
	}
	opened, err := archive.OpenArtifact(ctx, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(opened.Body)
	_ = opened.Body.Close()
	if err != nil || string(data) != "%PDF-1.7 test artifact" {
		t.Fatalf("artifact = %q, err %v", data, err)
	}
}

func TestWorkerRenewsLeaseDuringLongProcessor(t *testing.T) {
	store := memory.NewStore()
	artifacts := memory.NewArtifactStore()
	archive, err := application.NewArchive(store, artifacts, application.ArchiveOptions{
		Profiles: map[string]struct{}{"a5": {}},
		Now:      func() time.Time { return time.Now().UTC() },
		NewJobID: func() domain.JobID { return "job-heartbeat" },
	})
	if err != nil {
		t.Fatal(err)
	}
	archive.SetProcessor(application.ProcessorFunc(func(ctx context.Context, job domain.Job, pc application.ProcessorContext) (application.ProcessResult, error) {
		if err := pc.SetStage(domain.StageExtracting); err != nil {
			return application.ProcessResult{}, err
		}
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return application.ProcessResult{}, ctx.Err()
		}
		if err := pc.SetStage(domain.StageAIAnalyzing); err != nil {
			return application.ProcessResult{}, err
		}
		article, err := pc.ApproveArticle(application.ArticleDraft{
			Classification: "article",
			Decision:       "accept_candidate",
			Title:          "Heartbeat article",
			PlainText:      strings.Repeat("Readable article text. ", 40),
			SemanticHTML:   "<article><p>Readable article text.</p></article>",
			AIConfidence:   0.9,
			AIAttemptID:    "heartbeat-attempt",
		})
		if err != nil {
			return application.ProcessResult{}, err
		}
		if err := pc.SetStage(domain.StageFormatting); err != nil {
			return application.ProcessResult{}, err
		}
		return application.ProcessResult{Article: article, PDF: []byte("heartbeat pdf"), Filename: "heartbeat.pdf"}, nil
	}))
	accepted, err := archive.SubmitURL(context.Background(), application.SubmitURLRequest{URL: "https://example.test/heartbeat"}, "")
	if err != nil {
		t.Fatal(err)
	}
	worker, err := application.NewWorker(archive, application.WorkerOptions{
		LeaseDuration:     60 * time.Millisecond,
		HeartbeatInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := worker.RunOnce(context.Background()); err != nil || !claimed {
		t.Fatalf("RunOnce() = claimed %v, err %v", claimed, err)
	}
	detail, err := archive.GetJob(context.Background(), accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != domain.StatusReady {
		t.Fatalf("long processor job = %#v, want ready after lease renewal", detail)
	}
}

func TestWorkerCancelsProcessorWhenLeaseIsLost(t *testing.T) {
	baseStore := memory.NewStore()
	store := &leaseLossRenewStore{JobStore: baseStore}
	archive, err := application.NewArchive(store, memory.NewArtifactStore(), application.ArchiveOptions{
		Profiles: map[string]struct{}{"a5": {}},
		Now:      func() time.Time { return time.Now().UTC() },
		NewJobID: func() domain.JobID { return "job-lease-lost" },
	})
	if err != nil {
		t.Fatal(err)
	}
	processorCanceled := make(chan struct{})
	archive.SetProcessor(application.ProcessorFunc(func(ctx context.Context, job domain.Job, pc application.ProcessorContext) (application.ProcessResult, error) {
		<-ctx.Done()
		close(processorCanceled)
		return application.ProcessResult{}, ctx.Err()
	}))
	accepted, err := archive.SubmitURL(context.Background(), application.SubmitURLRequest{URL: "https://example.test/lease-lost"}, "")
	if err != nil {
		t.Fatal(err)
	}
	worker, err := application.NewWorker(archive, application.WorkerOptions{
		LeaseDuration:     60 * time.Millisecond,
		HeartbeatInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := worker.RunOnce(context.Background()); err != nil || !claimed {
		t.Fatalf("RunOnce() = claimed %v, err %v", claimed, err)
	}
	select {
	case <-processorCanceled:
	case <-time.After(time.Second):
		t.Fatal("processor was not canceled after lease loss")
	}
	detail, err := archive.GetJob(context.Background(), accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != domain.StatusProcessing {
		t.Fatalf("lease-lost job = %#v, want ownership left for durable recovery", detail)
	}
}

func TestWorkerRejectsProcessorWithoutOpaqueApproval(t *testing.T) {
	archive, _, _ := testArchive(t, application.ProcessorFunc(func(context.Context, domain.Job, application.ProcessorContext) (application.ProcessResult, error) {
		return application.ProcessResult{PDF: []byte("not enough")}, nil
	}))
	ctx := context.Background()
	accepted, err := archive.SubmitURL(ctx, application.SubmitURLRequest{URL: "https://example.test/article"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newWorker(t, archive).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	detail, err := archive.GetJob(ctx, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != domain.StatusFailed || detail.Failure == nil || detail.Failure.Category != string(domain.FailureAIInvalidResponse) {
		t.Fatalf("unapproved result = %#v", detail)
	}
}

func TestApprovalRequiresAIAttemptID(t *testing.T) {
	archive, _, _ := testArchive(t, application.ProcessorFunc(func(ctx context.Context, job domain.Job, pc application.ProcessorContext) (application.ProcessResult, error) {
		_, err := pc.ApproveArticle(application.ArticleDraft{
			Classification: "article",
			Decision:       "accept_candidate",
			Title:          "Article without an attempt",
			PlainText:      strings.Repeat("Readable article text. ", 40),
			SemanticHTML:   "<article><p>Readable article text.</p></article>",
			AIConfidence:   0.9,
		})
		return application.ProcessResult{}, err
	}))
	accepted, err := archive.SubmitURL(context.Background(), application.SubmitURLRequest{URL: "https://example.test/missing-attempt"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newWorker(t, archive).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	detail, err := archive.GetJob(context.Background(), accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Failure == nil || detail.Failure.Category != string(domain.FailureAIInvalidResponse) {
		t.Fatalf("missing AI attempt result = %#v", detail)
	}
}

func TestPaginationRemainsStableWhenNewJobsArrive(t *testing.T) {
	archive, _, _ := testArchive(t, application.ProcessorFunc(successfulProcessor))
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := archive.SubmitURL(ctx, application.SubmitURLRequest{URL: "https://example.test/" + string(rune('a'+i))}, ""); err != nil {
			t.Fatal(err)
		}
	}
	first, err := archive.ListJobs(ctx, application.ListJobsRequest{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("first page = %#v, err %v", first, err)
	}
	if _, err := archive.SubmitURL(ctx, application.SubmitURLRequest{URL: "https://example.test/new"}, ""); err != nil {
		t.Fatal(err)
	}
	second, err := archive.ListJobs(ctx, application.ListJobsRequest{Limit: 10, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 2 || second.Items[0].ID == first.Items[0].ID {
		t.Fatalf("cursor page = %#v", second)
	}
}

func TestRetryAndDelete(t *testing.T) {
	archive, _, _ := testArchive(t, application.ProcessorFunc(func(context.Context, domain.Job, application.ProcessorContext) (application.ProcessResult, error) {
		return application.ProcessResult{}, application.NewProcessingError(string(domain.FailureUnsupportedContent), "not an article", false)
	}))
	ctx := context.Background()
	accepted, err := archive.SubmitURL(ctx, application.SubmitURLRequest{URL: "https://example.test/nope"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newWorker(t, archive).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	retried, err := archive.RetryJob(ctx, accepted.ID)
	if err != nil || retried.ID == accepted.ID || retried.Status != domain.StatusQueued {
		t.Fatalf("retry = %#v, err %v", retried, err)
	}
	if err := archive.DeleteJob(ctx, retried.ID); err != nil {
		t.Fatal(err)
	}
	if err := archive.DeleteJob(ctx, retried.ID); err != nil {
		t.Fatal("delete should be idempotent:", err)
	}
	_, err = archive.GetJob(ctx, retried.ID)
	var safe *application.SafeError
	if !errors.As(err, &safe) || safe.Code != "not_found" {
		t.Fatalf("deleted get = %#v", err)
	}
}

func TestWorkerCleansArtifactAndRecordsStorageFailureAfterCommitError(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC) }
	baseStore := memory.NewStoreWithClock(now)
	store := &failingCompletionStore{JobStore: baseStore, err: errors.New("database write failed")}
	baseArtifacts := memory.NewArtifactStore()
	artifacts := &trackingArtifactStore{ArtifactStore: baseArtifacts}
	archive, err := application.NewArchive(store, artifacts, application.ArchiveOptions{
		Profiles: map[string]struct{}{"a5": {}},
		Now:      now,
		NewJobID: func() domain.JobID { return "job-storage-failure" },
	})
	if err != nil {
		t.Fatal(err)
	}
	archive.SetProcessor(application.ProcessorFunc(successfulProcessor))
	accepted, err := archive.SubmitURL(context.Background(), application.SubmitURLRequest{URL: "https://example.test/storage-failure"}, "")
	if err != nil {
		t.Fatal(err)
	}
	worker, err := application.NewWorker(archive, application.WorkerOptions{MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := worker.RunOnce(context.Background())
	if err != nil || !claimed {
		t.Fatalf("RunOnce = claimed %v, err %v", claimed, err)
	}
	detail, err := archive.GetJob(context.Background(), accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != domain.StatusFailed || detail.Failure == nil || detail.Failure.Category != string(domain.FailureStorageFailed) {
		t.Fatalf("post-artifact persistence failure = %#v", detail)
	}
	if artifacts.deleteCalls != 1 {
		t.Fatalf("artifact delete calls = %d, want 1", artifacts.deleteCalls)
	}
	if artifacts.last.Key == "" {
		t.Fatal("worker did not create an artifact before the persistence failure")
	}
	if _, err := baseArtifacts.Open(context.Background(), artifacts.last); err == nil {
		t.Fatal("artifact remained after failed durable commit")
	}
}

func TestDeleteSchedulesBeforePhysicalRemoval(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC) }
	baseStore := memory.NewStoreWithClock(now)
	store := &failingDeleteStore{JobStore: baseStore, err: errors.New("database unavailable")}
	baseArtifacts := memory.NewArtifactStore()
	artifacts := &trackingArtifactStore{ArtifactStore: baseArtifacts}
	archive, err := application.NewArchive(store, artifacts, application.ArchiveOptions{
		Profiles: map[string]struct{}{"a5": {}},
		Now:      now,
		NewJobID: func() domain.JobID { return "job-delete-order" },
	})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := archive.SubmitURL(context.Background(), application.SubmitURLRequest{URL: "https://example.test/delete-order"}, "")
	if err != nil {
		t.Fatal(err)
	}
	// Attach a synthetic artifact through the in-memory seam so the archive has
	// bytes to delete if ordering regresses.
	artifact, err := baseArtifacts.Put(context.Background(), accepted.ID, "article.pdf", []byte("pdf"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	store.artifact = artifact
	if err := archive.DeleteJob(context.Background(), accepted.ID); err == nil {
		t.Fatal("delete unexpectedly succeeded")
	}
	if artifacts.deleteCalls != 0 {
		t.Fatalf("artifact delete calls = %d, want 0 when durable scheduling fails", artifacts.deleteCalls)
	}
	if _, err := baseArtifacts.Open(context.Background(), artifact); err != nil {
		t.Fatalf("artifact was removed before durable deletion: %v", err)
	}
}

type failingCompletionStore struct {
	application.JobStore
	err error
}

type leaseLossRenewStore struct {
	application.JobStore
}

func (s *leaseLossRenewStore) RenewLease(context.Context, *application.Lease, time.Duration) error {
	return application.ErrLeaseLost
}

func (s *failingCompletionStore) Complete(context.Context, *application.Lease, application.Completion, time.Time) error {
	return s.err
}

type failingDeleteStore struct {
	application.JobStore
	err      error
	artifact domain.Artifact
}

func (s *failingDeleteStore) RequestDelete(context.Context, domain.JobID, time.Time) error {
	return s.err
}

func (s *failingDeleteStore) Get(ctx context.Context, id domain.JobID) (domain.Job, error) {
	job, err := s.JobStore.Get(ctx, id)
	if err == nil {
		job.Artifact = &s.artifact
	}
	return job, err
}

type trackingArtifactStore struct {
	application.ArtifactStore
	last        domain.Artifact
	deleteCalls int
}

func (s *trackingArtifactStore) Put(ctx context.Context, jobID domain.JobID, filename string, data []byte, now time.Time) (domain.Artifact, error) {
	artifact, err := s.ArtifactStore.Put(ctx, jobID, filename, data, now)
	if err == nil {
		s.last = artifact
	}
	return artifact, err
}

func (s *trackingArtifactStore) Delete(ctx context.Context, artifact domain.Artifact) error {
	s.deleteCalls++
	return s.ArtifactStore.Delete(ctx, artifact)
}
