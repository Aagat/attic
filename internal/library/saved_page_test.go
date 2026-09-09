package library

import (
	"context"
	"strings"
	"testing"

	"attic/internal/domain"
)

func TestLibraryIntegrationSavedPageForDocumentJob(t *testing.T) {
	l := integrationLibrary(t, "")
	ctx := context.Background()
	item, _, err := l.Save(ctx, SaveRequest{URL: "https://example.com/story"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.ImportBrowserCapture(ctx, item.ID, item.URL, "", importedMHTML("Saved version", "Only the browser has this content")); err != nil {
		t.Fatal(err)
	}
	if err := l.GeneratePDF(ctx, item.ID, "generate-saved"); err != nil {
		t.Fatal(err)
	}
	detail, err := l.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	page, found, err := l.SavedPage(ctx, domain.JobID(detail.JobID))
	if err != nil || !found || !strings.Contains(string(page.DOM), "Only the browser") || page.FinalURL != item.URL {
		t.Fatalf("saved page unavailable: found=%v err=%v", found, err)
	}
	// A newer blocked attempt must not hide the usable capture for PDF preparation.
	if _, err := l.db.Exec(`INSERT INTO saved_captures(id,item_id,artifact,final_url,title,text_content,status,created_at) SELECT 'newer-blocked',item_id,artifact,final_url,'CAPTCHA','verify you are human','blocked',created_at+interval '1 minute' FROM saved_captures WHERE item_id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	page, found, err = l.SavedPage(ctx, domain.JobID(detail.JobID))
	if err != nil || !found || page.Title != "Saved version" {
		t.Fatal("blocked capture hid usable earlier version", err)
	}
	_, found, err = l.SavedPage(ctx, "unknown-job")
	if err != nil || found {
		t.Fatal("unknown job returned capture", err)
	}
}
