package library

import (
	"attic/internal/capture"
	"attic/internal/domain"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReadingEditorPreservesSourcesAndQueuesEditedPDF(t *testing.T) {
	l := integrationLibrary(t, "reader@example.com")
	ctx := context.Background()
	item, _, err := l.Save(ctx, SaveRequest{URL: "https://example.com/edit", Title: "Saved title"}, "")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("Original readable article with useful information. ", 20)
	original := "<article><p>" + text + "</p><p>Remove this advert</p></article>"
	if _, err := l.CaptureOnce(ctx, integrationCapturer{result: capture.Result{HTML: []byte(original), PlainText: text, Title: item.Title, FinalURL: item.URL, Status: "complete"}}); err != nil {
		t.Fatal(err)
	}
	editor, err := l.Editor(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(editor.HTML, "Original readable") {
		t.Fatal(editor.HTML)
	}
	edited := `<article><p>Edited text with <strong>formatting</strong>.</p><pre><code>a &lt; b\n  print(a)</code></pre><table><tr><td>Keep cell</td></tr></table><script>alert(1)</script></article>`
	if err := l.SaveReading(ctx, item.ID, edited, editor.Revision); err != nil {
		t.Fatal(err)
	}
	detail, err := l.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !detail.ReadingAvailable || detail.JobID == "" || detail.HasPDF || detail.DeliveryStatus != "not_requested" {
		t.Fatalf("edit state: %+v", detail.Item)
	}
	view, err := l.Reading(ctx, item.ID)
	if err != nil || !strings.Contains(string(view), "Edited text") || strings.Contains(string(view), "Remove this advert") || strings.Contains(string(view), "alert(1)") {
		t.Fatalf("edited view: %s %v", view, err)
	}
	if len(detail.Captures) != 1 || detail.Captures[0].Text != text {
		t.Fatal("capture changed")
	}
	if err := l.SaveReading(ctx, item.ID, edited, editor.Revision); !errors.Is(err, ErrEditConflict) {
		t.Fatalf("stale edit: %v", err)
	}
	jobID := detail.JobID
	if _, err := l.db.Exec(`UPDATE jobs SET status='failed',failure_category='format_failed' WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	if err := l.GeneratePDF(ctx, item.ID, "retry-edit"); err != nil {
		t.Fatal(err)
	}
	detail, err = l.Get(ctx, item.ID)
	if err != nil || detail.JobID == jobID {
		t.Fatalf("retry: %v", err)
	}
	draft, found, err := l.ReadingEdit(ctx, domain.JobID(detail.JobID))
	if err != nil || !found || !strings.Contains(draft.SemanticHTML, "Edited text") {
		t.Fatalf("retry lost edit: %+v %v", draft, err)
	}
}
