package memory

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"attic/internal/application"
	"attic/internal/domain"
)

func TestStoreIdempotencyAndCursor(t *testing.T) {
	store := NewStore()
	base := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	for i, id := range []domain.JobID{"a", "b", "c"} {
		_, err := store.CreateOrReuse(context.Background(), application.CreateJob{
			ID:             id,
			Request:        application.SubmitURLRequest{URL: "https://example.test/" + string(id), Profile: "a5"},
			CreatedAt:      base.Add(time.Duration(i) * time.Second),
			IdempotencyKey: domain.IdempotencyKey("key-" + string(id)),
			RequestDigest:  string(id),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.List(context.Background(), application.StoreListRequest{Limit: 1})
	if err != nil || len(page.Jobs) != 1 || !page.HasMore || page.Jobs[0].ID != "c" {
		t.Fatalf("first page = %#v, err %v", page, err)
	}
	page, err = store.List(context.Background(), application.StoreListRequest{Limit: 10, After: &domain.CursorPosition{CreatedAt: page.Jobs[0].CreatedAt, ID: page.Jobs[0].ID}})
	if err != nil || len(page.Jobs) != 2 || page.Jobs[0].ID != "b" {
		t.Fatalf("after cursor = %#v, err %v", page, err)
	}
	outcome, err := store.CreateOrReuse(context.Background(), application.CreateJob{
		ID:             "ignored",
		Request:        application.SubmitURLRequest{URL: "https://example.test/a", Profile: "a5"},
		CreatedAt:      base,
		IdempotencyKey: "key-a",
		RequestDigest:  "a",
	})
	if err != nil || !outcome.Replayed || outcome.Job.ID != "a" {
		t.Fatalf("replay = %#v, err %v", outcome, err)
	}
}

func TestStoreLeaseCompletionAndExpiry(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	store := NewStoreWithClock(func() time.Time { return now })
	_, err := store.CreateOrReuse(context.Background(), application.CreateJob{ID: "job", Request: application.SubmitURLRequest{URL: "https://example.test", Profile: "a5"}, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.ClaimNext(context.Background(), now, time.Second)
	if err != nil || lease == nil || lease.Job.Status != domain.StatusProcessing || lease.Job.AttemptCount != 1 {
		t.Fatalf("lease = %#v, err %v", lease, err)
	}
	if lease.Job.Version != 2 {
		t.Fatalf("claimed version = %d, want 2", lease.Job.Version)
	}
	staleLease := application.Lease{Job: lease.Job, Token: lease.Token, ExpiresAt: lease.ExpiresAt}
	if err := store.RenewLease(context.Background(), lease, 2*time.Second); err != nil {
		t.Fatalf("RenewLease() error = %v", err)
	}
	if lease.Job.Version != 3 || !lease.ExpiresAt.Equal(now.Add(3*time.Second)) {
		t.Fatalf("renewed lease = %#v, want version 3 and extended expiry", lease)
	}
	if err := store.SetStage(context.Background(), &staleLease, domain.StageExtracting); !errors.Is(err, application.ErrLeaseLost) {
		t.Fatalf("stale stage transition = %v, want ErrLeaseLost", err)
	}
	if err := store.SetStage(context.Background(), lease, domain.StageExtracting); err != nil {
		t.Fatal(err)
	}
	if err := store.SetStage(context.Background(), lease, domain.StageFormatting); !errors.Is(err, application.ErrInvalidStage) {
		t.Fatalf("skipped stage error = %v, want ErrInvalidStage", err)
	}
	if err := store.SetStage(context.Background(), lease, domain.StageAIAnalyzing); err != nil {
		t.Fatal(err)
	}
	if err := store.Complete(context.Background(), lease, application.Completion{
		Content:  domain.ContentDocument{ID: "content-before-persisting"},
		Artifact: domain.Artifact{Available: true, ByteSize: 1},
	}, now); !errors.Is(err, application.ErrInvalidStage) {
		t.Fatalf("completion before persisting = %v, want ErrInvalidStage", err)
	}
	reclaimed, err := store.ClaimNext(context.Background(), now.Add(4*time.Second), time.Second)
	if err != nil || reclaimed == nil || reclaimed.Job.AttemptCount != 2 {
		t.Fatalf("reclaimed lease = %#v, err %v", reclaimed, err)
	}
	if err := store.Complete(context.Background(), lease, application.Completion{
		Content:  domain.ContentDocument{ID: "content", PlainText: "content"},
		Artifact: domain.Artifact{Available: true, ByteSize: 1, Checksum: "checksum"},
	}, now); !errors.Is(err, application.ErrLeaseLost) {
		t.Fatalf("stale completion = %v, want lease lost", err)
	}
}

func TestRenewLeaseNeverShortensCallerFutureLease(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	store := NewStoreWithClock(func() time.Time { return now })
	if _, err := store.CreateOrReuse(context.Background(), application.CreateJob{
		ID:        "future-lease",
		Request:   application.SubmitURLRequest{URL: "https://example.test/future", Profile: "a5"},
		CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	lease, err := store.ClaimNext(context.Background(), now.Add(time.Second), time.Second)
	if err != nil || lease == nil {
		t.Fatalf("ClaimNext() = %#v, err %v", lease, err)
	}
	claimedExpiry := lease.ExpiresAt
	if err := store.RenewLease(context.Background(), lease, time.Second); err != nil {
		t.Fatalf("RenewLease() error = %v", err)
	}
	if !lease.ExpiresAt.After(claimedExpiry) || !lease.ExpiresAt.Equal(now.Add(3*time.Second)) {
		t.Fatalf("renewed expiry = %s, want extension beyond %s", lease.ExpiresAt, claimedExpiry)
	}
}

func TestStoreExpiredDeliveryBecomesTerminalFailure(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	store := NewStoreWithClock(func() time.Time { return now })
	if _, err := store.CreateOrReuse(context.Background(), application.CreateJob{
		ID:        "delivery-job",
		Request:   application.SubmitURLRequest{URL: "https://example.test/delivery", Profile: "a5"},
		CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	job := store.jobs["delivery-job"]
	job.Status = domain.StatusDelivering
	job.LeaseToken = "delivery-lease"
	job.LeaseUntil = now.Add(-time.Second)
	job.Version = 7
	store.jobs[job.ID] = job
	store.mu.Unlock()

	lease, err := store.ClaimNext(context.Background(), now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if lease != nil {
		t.Fatalf("expired delivery was claimed again: %#v", lease)
	}
	failed, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != domain.StatusDeliveryFailed || failed.Failure == nil || failed.Failure.Category != domain.FailureDeliveryTimeout {
		t.Fatalf("expired delivery = %#v, want terminal delivery timeout", failed)
	}
	if failed.Failure.Message != "Email delivery could not be confirmed" || failed.AttemptCount != job.AttemptCount {
		t.Fatalf("expired delivery failure = %#v", failed.Failure)
	}
	if lease, err := store.ClaimNext(context.Background(), now.Add(time.Minute), time.Minute); err != nil || lease != nil {
		t.Fatalf("terminal delivery was claimable after expiry: lease=%#v err=%v", lease, err)
	}
}

func TestStoreRequeueHonorsNextAttemptSchedule(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	store := NewStoreWithClock(func() time.Time { return now })
	if _, err := store.CreateOrReuse(context.Background(), application.CreateJob{
		ID:        "scheduled-job",
		Request:   application.SubmitURLRequest{URL: "https://example.test/scheduled", Profile: "a5"},
		CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	lease, err := store.ClaimNext(context.Background(), now, time.Minute)
	if err != nil || lease == nil {
		t.Fatalf("ClaimNext() = %#v, err %v", lease, err)
	}
	next := now.Add(time.Hour)
	if err := store.Requeue(context.Background(), lease, domain.Failure{Category: domain.FailureAIUnavailable}, next); err != nil {
		t.Fatalf("Requeue() error = %v", err)
	}
	if scheduled, err := store.Get(context.Background(), "scheduled-job"); err != nil || !scheduled.NextAttemptAt.Equal(next) {
		t.Fatalf("scheduled job = %#v, err %v", scheduled, err)
	}
	if queued, err := store.ClaimNext(context.Background(), now.Add(time.Minute), time.Minute); err != nil || queued != nil {
		t.Fatalf("job claimed before next_attempt_at: lease=%#v err=%v", queued, err)
	}
	if queued, err := store.ClaimNext(context.Background(), next, time.Minute); err != nil || queued == nil {
		t.Fatalf("job not claimed at next_attempt_at: lease=%#v err=%v", queued, err)
	}
}

func TestArtifactStoreCopiesAndVerifiesBytes(t *testing.T) {
	artifacts := NewArtifactStore()
	created, err := artifacts.Put(context.Background(), "job", "article.pdf", []byte("pdf"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	reader, err := artifacts.Open(context.Background(), created)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(data) != "pdf" {
		t.Fatalf("read = %q, err %v", data, err)
	}
	created.Checksum = "bad"
	if _, err := artifacts.Open(context.Background(), created); err == nil {
		t.Fatal("checksum mismatch was accepted")
	}
	if err := artifacts.Delete(context.Background(), created); err != nil {
		t.Fatal(err)
	}
}
