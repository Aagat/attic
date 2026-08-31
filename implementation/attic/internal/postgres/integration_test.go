package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"attic/internal/application"
	"attic/internal/domain"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("ATTIC_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("ATTIC_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(8)
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("database ping error = %v", err)
	}

	directory := filepath.Join("..", "..", "migrations")
	runner, err := NewMigrationRunner(db, directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("migration run error = %v", err)
	}
	// A second startup must observe the recorded version and remain a no-op.
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("second migration run error = %v", err)
	}

	store, err := NewStore(db, Options{Now: func() time.Time { return time.Now().UTC() }})
	if err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("integration-%d", time.Now().UTC().UnixNano())
	jobID := domain.JobID(prefix + "-source")
	retryID := domain.JobID(prefix + "-retry")
	deliveryID := domain.JobID(prefix + "-delivery")
	cleanup := func(id domain.JobID) {
		_ = store.RequestDelete(context.Background(), id, time.Now().UTC())
	}
	t.Cleanup(func() {
		cleanup(deliveryID)
		cleanup(retryID)
		cleanup(jobID)
	})

	createdAt := time.Now().UTC()
	command := application.CreateJob{
		ID:             jobID,
		Request:        application.SubmitURLRequest{URL: "https://example.test/article/" + prefix, Title: "Integration article", Profile: "a5"},
		CreatedAt:      createdAt,
		IdempotencyKey: domain.IdempotencyKey(prefix + "-key"),
		RequestDigest:  strings.Repeat("b", 64),
	}
	created, err := store.CreateOrReuse(ctx, command)
	if err != nil {
		t.Fatalf("CreateOrReuse() error = %v", err)
	}
	if created.Replayed || created.Job.Status != domain.StatusQueued {
		t.Fatalf("created outcome = %#v", created)
	}
	scheduleDelta := created.Job.NextAttemptAt.Sub(createdAt)
	if scheduleDelta < -time.Microsecond || scheduleDelta > time.Microsecond || created.Job.Version != 1 {
		t.Fatalf("created durable revision/schedule = version %d, next %s", created.Job.Version, created.Job.NextAttemptAt)
	}
	replayed, err := store.CreateOrReuse(ctx, command)
	if err != nil {
		t.Fatalf("idempotent replay error = %v", err)
	}
	if !replayed.Replayed || replayed.Job.ID != jobID {
		t.Fatalf("replayed outcome = %#v", replayed)
	}
	conflict := command
	conflict.RequestDigest = strings.Repeat("c", 64)
	if _, err := store.CreateOrReuse(ctx, conflict); !errors.Is(err, application.ErrIdempotencyConflict) {
		t.Fatalf("conflict error = %v, want ErrIdempotencyConflict", err)
	}

	page, err := store.List(ctx, application.StoreListRequest{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if !containsJob(page.Jobs, jobID) {
		t.Fatalf("list did not contain %s: %#v", jobID, page.Jobs)
	}
	if _, err := store.Get(ctx, jobID); err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	lease, err := store.ClaimNext(ctx, time.Now().UTC().Add(time.Second), time.Minute)
	if err != nil {
		t.Fatalf("ClaimNext() error = %v", err)
	}
	if lease == nil || lease.Job.ID != jobID {
		t.Fatalf("claim = %#v; integration database must be isolated for this test", lease)
	}
	claimedVersion := lease.Job.Version
	claimedExpiry := lease.ExpiresAt
	if err := store.RenewLease(ctx, lease, time.Minute); err != nil {
		t.Fatalf("RenewLease() error = %v", err)
	}
	if lease.Job.Version != claimedVersion+1 || !lease.ExpiresAt.After(claimedExpiry) {
		t.Fatalf("renewed lease = %#v; want advanced version and expiry", lease)
	}
	wrongLease := *lease
	wrongLease.Token = "not-the-lease"
	if err := store.SetStage(ctx, &wrongLease, domain.StageExtracting); !errors.Is(err, application.ErrLeaseLost) {
		t.Fatalf("wrong SetStage() error = %v, want ErrLeaseLost", err)
	}
	if err := store.SetStage(ctx, lease, domain.StageAIAnalyzing); !errors.Is(err, application.ErrInvalidStage) {
		t.Fatalf("skipped SetStage() error = %v, want ErrInvalidStage", err)
	}
	for _, stage := range []domain.Stage{domain.StageExtracting, domain.StageAIAnalyzing, domain.StageFormatting, domain.StagePersisting} {
		if err := store.SetStage(ctx, lease, stage); err != nil {
			t.Fatalf("SetStage(%s) error = %v", stage, err)
		}
	}
	checksum := strings.Repeat("a", 64)
	if err := store.Complete(ctx, lease, application.Completion{
		Content: domain.ContentDocument{
			ID:               domain.ContentID(prefix + "-content"),
			Title:            "Approved integration article",
			SourceURL:        command.Request.URL,
			SemanticHTML:     "<article>integration content</article>",
			PlainText:        "integration content",
			ExtractionMethod: "integration",
			AIConfidence:     0.95,
		},
		Artifact: domain.Artifact{
			Key:       "integration/" + prefix + "/article.pdf",
			Filename:  "article.pdf",
			MediaType: "application/pdf",
			ByteSize:  4,
			Checksum:  checksum,
			Available: true,
		},
	}, time.Now().UTC()); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	detail, err := store.Get(ctx, jobID)
	if err != nil {
		t.Fatalf("Get() after completion error = %v", err)
	}
	if detail.Status != domain.StatusReady || detail.Content == nil || detail.Artifact == nil {
		t.Fatalf("completed job = %#v", detail)
	}

	if _, err := store.CreateOrReuse(ctx, application.CreateJob{
		ID:        deliveryID,
		Request:   application.SubmitURLRequest{URL: "https://example.test/delivery/" + prefix, Profile: "a5"},
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create delivery job error = %v", err)
	}
	deliveryLeaseUntil := time.Now().UTC().Add(-time.Second)
	if _, err := db.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'delivering', stage = NULL, lease_token = $2,
			lease_expires_at = $3, version = version + 1
		WHERE id = $1`, string(deliveryID), "delivery-expired-lease", deliveryLeaseUntil); err != nil {
		t.Fatalf("mark expired delivery error = %v", err)
	}
	if expiredClaim, err := store.ClaimNext(ctx, time.Now().UTC(), time.Minute); err != nil {
		t.Fatalf("ClaimNext() after expired delivery error = %v", err)
	} else if expiredClaim != nil {
		t.Fatalf("expired delivery was reclaimed: %#v", expiredClaim)
	}
	delivery, err := store.Get(ctx, deliveryID)
	if err != nil {
		t.Fatalf("Get() expired delivery error = %v", err)
	}
	if delivery.Status != domain.StatusDeliveryFailed || delivery.Failure == nil || delivery.Failure.Category != domain.FailureDeliveryTimeout || delivery.Failure.Message != "Email delivery could not be confirmed" {
		t.Fatalf("expired delivery = %#v, want terminal safe delivery timeout", delivery)
	}

	retry, err := store.CreateRetry(ctx, jobID, retryID, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateRetry() error = %v", err)
	}
	if retry.RetryOfJobID != jobID || retry.Status != domain.StatusQueued {
		t.Fatalf("retry job = %#v", retry)
	}
	if err := store.RequestDelete(ctx, retryID, time.Now().UTC()); err != nil {
		t.Fatalf("RequestDelete(retry) error = %v", err)
	}
	if _, err := store.Get(ctx, retryID); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("deleted retry Get() error = %v, want ErrNotFound", err)
	}
	if err := store.RequestDelete(ctx, jobID, time.Now().UTC()); err != nil {
		t.Fatalf("RequestDelete(source) error = %v", err)
	}
	if _, err := store.Get(ctx, jobID); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("deleted source Get() error = %v, want ErrNotFound", err)
	}
}

func containsJob(jobs []domain.Job, id domain.JobID) bool {
	for _, job := range jobs {
		if job.ID == id {
			return true
		}
	}
	return false
}
