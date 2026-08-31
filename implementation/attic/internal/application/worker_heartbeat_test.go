package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"attic/internal/application"
	"attic/internal/domain"
	"attic/internal/memory"
)

type renewFailureStore struct{ application.JobStore }

func (s renewFailureStore) RenewLease(context.Context, *application.Lease, time.Duration) error {
	return errors.New("temporary database failure")
}

func TestWorkerRequeuesTransientHeartbeatStorageFailure(t *testing.T) {
	base := memory.NewStore()
	store := renewFailureStore{JobStore: base}
	processor := application.ProcessorFunc(func(ctx context.Context, _ domain.Job, _ application.ProcessorContext) (application.ProcessResult, error) {
		<-ctx.Done()
		return application.ProcessResult{}, ctx.Err()
	})
	archive, err := application.NewArchive(store, memory.NewArtifactStore(), application.ArchiveOptions{Processor: processor, NewJobID: func() domain.JobID { return "heartbeat-job" }})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := archive.SubmitURL(context.Background(), application.SubmitURLRequest{URL: "https://example.com/article"}, "")
	if err != nil {
		t.Fatal(err)
	}
	worker, err := application.NewWorker(archive, application.WorkerOptions{Processor: processor, LeaseDuration: time.Second, HeartbeatInterval: time.Millisecond, MaxAttempts: 3})
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
	if detail.Status != domain.StatusQueued {
		t.Fatalf("status = %s, want queued for retry", detail.Status)
	}
}
