package library

import (
	"context"
	"strings"
	"testing"

	"attic/internal/domain"
)

func TestLibraryIntegrationTranslationEditionsPreserveOriginalAndIntent(t *testing.T) {
	ctx := context.Background()
	l := integrationLibrary(t, "reader@example.test")
	item, _, err := l.Save(ctx, SaveRequest{URL: "https://example.com/story", Title: "Original title"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = l.ImportBrowserCapture(ctx, item.ID, item.URL, "", importedMHTML("Original title", strings.Repeat("Este es el artículo original. ", 20))); err != nil {
		t.Fatal(err)
	}
	if err = l.GeneratePDF(ctx, item.ID, "original"); err != nil {
		t.Fatal(err)
	}
	original, err := l.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"translate", "translate", "translate-again"} {
		if err = l.Translate(ctx, item.ID, "en", key); err != nil {
			t.Fatal(err)
		}
	}
	translated, err := l.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if translated.JobID == original.JobID || translated.OutputLanguage != "en" || len(translated.Captures) != 1 {
		t.Fatalf("bad translated edition: %+v", translated)
	}
	if integrationCount(t, l, `SELECT count(*) FROM jobs`) != 2 || integrationCount(t, l, `SELECT count(*) FROM jobs WHERE delivery_destination IS NOT NULL OR delivery_pending`) != 0 {
		t.Fatal("duplicate job or unsolicited email")
	}
	if err = l.Translate(ctx, item.ID, "", "back"); err != nil {
		t.Fatal(err)
	}
	restored, err := l.Get(ctx, item.ID)
	if err != nil || restored.JobID != original.JobID || restored.OutputLanguage != "" {
		t.Fatalf("original lost: %+v %v", restored, err)
	}
	// A queued translated edition can still use its capture after selecting Original.
	if page, found, err := l.SavedPage(ctx, domain.JobID(translated.JobID)); err != nil || !found || !strings.Contains(string(page.DOM), "artículo original") {
		t.Fatalf("unselected job lost capture: %v %v", found, err)
	}
	if err = l.Translate(ctx, item.ID, "en", "again"); err != nil {
		t.Fatal(err)
	}
	if err = l.Send(ctx, item.ID, "explicit-send"); err != nil {
		t.Fatal(err)
	}
	if integrationCount(t, l, `SELECT count(*) FROM jobs WHERE delivery_destination IS NOT NULL`) != 1 {
		t.Fatal("delivery intent lost")
	}
	var destination string
	if err = l.db.QueryRow(`SELECT COALESCE(delivery_destination,'') FROM jobs WHERE id=$1`, original.JobID).Scan(&destination); err != nil || destination != "" {
		t.Fatal("sent original edition")
	}
	if _, err = l.db.Exec(`UPDATE jobs SET status='failed',failure_category='format_failed' WHERE id=$1`, translated.JobID); err != nil {
		t.Fatal(err)
	}
	if err = l.GeneratePDF(ctx, item.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	retried, err := l.Get(ctx, item.ID)
	if err != nil || retried.JobID == translated.JobID {
		t.Fatal("retry failed", err)
	}
	if language, err := l.OutputLanguage(ctx, domain.JobID(retried.JobID)); err != nil || language != "en" {
		t.Fatal("retry lost target language", err)
	}
	if err = l.Edit(ctx, item.ID, SaveRequest{Title: "My title"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = l.Save(ctx, SaveRequest{URL: item.URL, Title: "Browser title", Source: &Source{ClientID: "test", NodeID: "1"}}, ""); err != nil {
		t.Fatal(err)
	}
	edited, err := l.Get(ctx, item.ID)
	if err != nil || edited.Title != "My title" {
		t.Fatal("bookmark sync overwrote edited title", err)
	}
}

func TestLibraryIntegrationTranslatedReadingRequiresApprovedContent(t *testing.T) {
	ctx := context.Background()
	l := integrationLibrary(t, "")
	item, _, err := l.Save(ctx, SaveRequest{URL: "https://example.com/story"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = l.Translate(ctx, item.ID, "en", "translate"); err != nil {
		t.Fatal(err)
	}
	d, err := l.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.ReadingAvailable {
		t.Fatal("queued content reported ready")
	}
	if _, err = l.Reading(ctx, item.ID); err == nil {
		t.Fatal("unapproved content readable")
	}
	text := strings.Repeat("This is the complete translated article about preserving books and reading. ", 20)
	_, err = l.db.Exec(`INSERT INTO content_documents(id,job_id,title,source_url,semantic_html,plain_text,extraction_method,ai_confidence,detected_language) VALUES($1,$2,'Translated title',$3,$4,$5,'ai',1,'en')`, opaque(), d.JobID, item.URL, "<p>"+text+"</p>", text)
	if err != nil {
		t.Fatal(err)
	}
	content, err := l.Reading(ctx, item.ID)
	if err != nil || !strings.Contains(string(content), "Translated title") || !strings.Contains(string(content), text) {
		t.Fatalf("translated reading unavailable: %v", err)
	}
	if err = l.Translate(ctx, item.ID, "", "original"); err != nil {
		t.Fatal(err)
	}
	d, err = l.Get(ctx, item.ID)
	if err != nil || d.JobID != "" || d.OutputLanguage != "" {
		t.Fatalf("original without PDF not restored: %+v %v", d, err)
	}
	if err = l.Translate(ctx, item.ID, "javascript:bad", "invalid"); err != ErrInvalid {
		t.Fatal("invalid language accepted")
	}
}
