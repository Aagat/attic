package library

import (
	"attic/internal/capture"
	"context"
	"testing"
)

func TestLibraryIntegrationRecoveryPreservesHistoryAndResumesRequestedPDF(t *testing.T) {
	l := integrationLibrary(t, "")
	ctx := context.Background()
	item, _, err := l.Save(ctx, SaveRequest{URL: "https://example.com/story", Title: "Original title", Notes: "Keep my notes"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = l.ImportBrowserCapture(ctx, item.ID, item.URL, "", importedMHTML("Original capture", "Original content")); err != nil {
		t.Fatal(err)
	}
	if err = l.GeneratePDF(ctx, item.ID, "requested-pdf"); err != nil {
		t.Fatal(err)
	}
	before, err := l.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.db.Exec(`UPDATE jobs SET status='failed',failure_category='access_denied',failure_message='Blocked page' WHERE id=$1`, before.JobID); err != nil {
		t.Fatal(err)
	}
	result := capture.Result{HTML: []byte("<article>Recovered content</article>"), PlainText: "Recovered content", Title: "Recovered title", OriginalURL: item.URL, FinalURL: item.URL, Status: "complete"}
	if err = l.SaveRecoveredPage(ctx, item.URL, result); err != nil {
		t.Fatal(err)
	}
	after, err := l.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Captures) != 2 || after.Captures[1].ID != before.Captures[0].ID || after.Notes != "Keep my notes" {
		t.Fatal("recovery lost existing history or annotations")
	}
	if after.JobID == before.JobID || after.PDFStatus != "queued" {
		t.Fatal("failed requested PDF did not resume")
	}
	if integrationCount(t, l, `SELECT count(*) FROM jobs`) != 2 || integrationCount(t, l, `SELECT count(*) FROM saved_items`) != 1 {
		t.Fatal("old job or item changed")
	}
	var destination string
	if err = l.db.QueryRow(`SELECT COALESCE(delivery_destination,'') FROM jobs WHERE id=$1`, after.JobID).Scan(&destination); err != nil || destination != "" {
		t.Fatal("PDF-only request acquired an email destination")
	}
	if err = l.SaveRecoveredPage(ctx, item.URL, capture.Result{Status: "blocked"}); err != ErrBrowserCaptureBlocked {
		t.Fatal("stored challenge")
	}
}

func TestLibraryIntegrationRecoveryBeforePDFFailureStillResumes(t *testing.T) {
	l := integrationLibrary(t, "")
	ctx := context.Background()
	item, _, err := l.Save(ctx, SaveRequest{URL: "https://example.com/story"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = l.GeneratePDF(ctx, item.ID, "pdf-before-capture"); err != nil {
		t.Fatal(err)
	}
	before, err := l.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := capture.Result{HTML: []byte("<article>Recovered content</article>"), PlainText: "Recovered content", FinalURL: item.URL, Status: "complete"}
	if err = l.SaveRecoveredPage(ctx, item.URL, result); err != nil {
		t.Fatal(err)
	}
	if integrationCount(t, l, `SELECT count(*) FROM jobs`) != 1 {
		t.Fatal("duplicated active job")
	}
	if _, err = l.db.Exec(`UPDATE jobs SET status='failed',failure_category='access_denied',failure_message='Blocked' WHERE id=$1`, before.JobID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = l.ResumeRecovered(ctx); err != nil {
			t.Fatal(err)
		}
	}
	after, err := l.Get(ctx, item.ID)
	if err != nil || after.JobID == before.JobID || integrationCount(t, l, `SELECT count(*) FROM jobs`) != 2 {
		t.Fatal("late failure was not resumed exactly once", err)
	}
	if _, err = l.db.Exec(`UPDATE jobs SET status='failed',failure_category='format_failed',failure_message='Formatter failed' WHERE id=$1`, after.JobID); err != nil {
		t.Fatal(err)
	}
	if err = l.ResumeRecovered(ctx); err != nil {
		t.Fatal(err)
	}
	if integrationCount(t, l, `SELECT count(*) FROM jobs`) != 2 {
		t.Fatal("same capture caused an unbounded retry loop")
	}
}
