package postgres

import (
	"attic/internal/application"
	"attic/internal/delivery"
	"attic/internal/domain"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDeliveryQueueIntegration(t *testing.T) {
	url := os.Getenv("ATTIC_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ATTIC_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	now := time.Now().UTC()
	store, err := Open(ctx, url, Options{Now: func() time.Time { return now }, DeliveryDestination: "reader@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	migrations, err := NewMigrationRunnerForStore(store, "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Run(ctx); err != nil {
		t.Fatal(err)
	}
	create := func() domain.JobID {
		id := domain.JobID(fmt.Sprintf("mail-%d", time.Now().UnixNano()))
		_, err := store.CreateOrReuse(ctx, application.CreateJob{ID: id, Request: application.SubmitURLRequest{URL: "https://example.test/mail", Profile: "a5"}, CreatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { store.RequestDelete(ctx, id, now) })
		lease, err := store.ClaimNext(ctx, now, time.Minute)
		if err != nil || lease == nil {
			t.Fatalf("claim %v %v", lease, err)
		}
		for _, stage := range []domain.Stage{domain.StageExtracting, domain.StageAIAnalyzing, domain.StageFormatting, domain.StagePersisting} {
			if err := store.SetStage(ctx, lease, stage); err != nil {
				t.Fatal(err)
			}
		}
		err = store.Complete(ctx, lease, application.Completion{Content: domain.ContentDocument{ID: domain.ContentID(string(id) + "-content"), Title: "Email article", SourceURL: "https://example.test/mail", SemanticHTML: "<p>Article</p>", PlainText: "Article", ExtractionMethod: "test", AIConfidence: 1, AICompleteness: 1}, Artifact: domain.Artifact{Key: string(id) + "/article.pdf", Filename: "article.pdf", MediaType: "application/pdf", ByteSize: 4, Checksum: strings.Repeat("a", 64), Available: true}}, now)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := create()
	claim, err := store.ClaimDelivery(ctx, time.Minute)
	if err != nil || claim == nil || claim.JobID != id || claim.Title != "Email article" {
		t.Fatalf("delivery claim %+v %v", claim, err)
	}
	if other, err := store.ClaimDelivery(ctx, time.Minute); err != nil || other != nil {
		t.Fatalf("duplicate claim %v %v", other, err)
	}
	if err := store.FinishDelivery(ctx, claim, delivery.Result{Outcome: "transient_failure", Code: "451"}); err != nil {
		t.Fatal(err)
	}
	if other, err := store.ClaimDelivery(ctx, time.Minute); err != nil || other != nil {
		t.Fatalf("ignored backoff %v %v", other, err)
	}
	now = now.Add(time.Minute)
	second, err := store.ClaimDelivery(ctx, time.Minute)
	if err != nil || second == nil || second.MessageID != claim.MessageID {
		t.Fatalf("retry %v %v", second, err)
	}
	if err := store.FinishDelivery(ctx, second, delivery.Result{Outcome: "accepted", Code: "250"}); err != nil {
		t.Fatal(err)
	}
	job, err := store.Get(ctx, id)
	if err != nil || job.Status != domain.StatusDelivered || job.Delivery.AttemptCount != 2 || job.Artifact == nil {
		t.Fatalf("result %+v %v", job, err)
	}
	if other, err := store.ClaimDelivery(ctx, time.Minute); err != nil || other != nil {
		t.Fatalf("resent accepted %v %v", other, err)
	}
	id = create()
	claim, err = store.ClaimDelivery(ctx, time.Minute)
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if other, err := store.ClaimDelivery(ctx, time.Minute); err != nil || other != nil {
		t.Fatalf("resent expired %v %v", other, err)
	}
	job, err = store.Get(ctx, id)
	if err != nil || job.Status != domain.StatusDeliveryFailed || job.Artifact == nil {
		t.Fatalf("expired %+v %v", job, err)
	}
	retry, err := store.CreateRetry(ctx, id, "must-not-create-job", now)
	if err != nil || retry.ID != id || retry.Artifact == nil {
		t.Fatalf("manual retry %+v %v", retry, err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		claim, err = store.ClaimDelivery(ctx, time.Minute)
		if err != nil || claim == nil {
			t.Fatalf("retry %v %v", claim, err)
		}
		if err := store.FinishDelivery(ctx, claim, delivery.Result{Outcome: "transient_failure"}); err != nil {
			t.Fatal(err)
		}
		now = now.Add(2 * time.Minute)
	}
	job, err = store.Get(ctx, id)
	if err != nil || job.Status != domain.StatusDeliveryFailed {
		t.Fatalf("retry budget %+v %v", job, err)
	}
	// Enabling SMTP never sweeps previously completed library articles.
	store.deliveryDestination = ""
	create()
	store.deliveryDestination = "reader@example.test"
	if other, err := store.ClaimDelivery(ctx, time.Minute); err != nil || other != nil {
		t.Fatalf("retroactive delivery %v %v", other, err)
	}
}
