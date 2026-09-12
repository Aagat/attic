package library

import (
	"context"
	"testing"
)

func TestLibraryIntegrationSetupPausesOldDeliveryAndKeepsFailureStages(t *testing.T) {
	l := integrationLibrary(t, "reader@kindle.test")
	ctx := context.Background()
	item, _, err := l.Save(ctx, SaveRequest{URL: "https://example.com/article", Title: "Saved", Action: "kindle"}, "original-send")
	if err != nil {
		t.Fatal(err)
	}
	if err = l.PausePendingDelivery(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := l.Get(ctx, item.ID)
	if err != nil || before.DeliveryStatus != "paused" {
		t.Fatalf("old delivery was not paused: %+v %v", before, err)
	}
	if err = l.Send(ctx, item.ID, "explicit-resume"); err != nil {
		t.Fatal(err)
	}
	var paused bool
	if err = l.db.QueryRow(`SELECT delivery_paused FROM jobs WHERE id=$1`, before.JobID).Scan(&paused); err != nil || paused {
		t.Fatal("explicit send did not resume pending job")
	}
	_, err = l.db.Exec(`UPDATE jobs SET status='failed',failure_category='unsupported_content',failure_message='unsafe upstream detail' WHERE id=$1`, before.JobID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := l.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Diagnostics) == 0 || after.Diagnostics[0].Stage != "AI approval" || after.Diagnostics[0].Reason != "The page is not a supported article" {
		t.Fatalf("incorrect diagnostic: %+v", after.Diagnostics)
	}
}
