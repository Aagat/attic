package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"attic/internal/capture"
)

func importedMHTML(title, body string) []byte {
	return []byte(fmt.Sprintf("MIME-Version: 1.0\r\nContent-Type: multipart/related; boundary=attic\r\n\r\n--attic\r\nContent-Type: text/html\r\nContent-Location: https://example.com/story\r\n\r\n<html><head><title>%s</title></head><body>%s</body></html>\r\n--attic--\r\n", title, body))
}
func TestLibraryIntegrationBrowserCapture(t *testing.T) {
	l := integrationLibrary(t, "")
	ctx := context.Background()
	item, _, err := l.Save(ctx, SaveRequest{URL: "https://example.com/story", Title: "Saved title"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.db.Exec(`UPDATE saved_items SET capture_status='capturing',capture_token='stale',capture_until=now()+interval '4 minutes' WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	data := importedMHTML("Loaded article", "Browser-only article text<script>bad()</script>")
	for range 2 {
		if err = l.ImportBrowserCapture(ctx, item.ID, item.URL+"#section", "Tab", data); err != nil {
			t.Fatal(err)
		}
	}
	if err = l.persistCapture(ctx, item.ID, "stale", opaque(), "server", capture.Result{HTML: []byte("stale"), PlainText: "stale", Status: "complete"}); err != nil {
		t.Fatal(err)
	}
	d, err := l.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Captures) != 1 || d.Captures[0].Source != "browser" || d.CaptureStatus != "complete" || !strings.Contains(d.Text, "Browser-only") {
		t.Fatalf("unexpected import %+v", d)
	}
	for _, tc := range []struct {
		url  string
		data []byte
		want error
	}{{item.URL, importedMHTML("CAPTCHA", "Please verify you are human"), ErrBrowserCaptureBlocked}, {"https://example.com/other", data, ErrInvalid}, {item.URL, []byte("invalid"), ErrInvalid}} {
		if err = l.ImportBrowserCapture(ctx, item.ID, tc.url, "", tc.data); !errors.Is(err, tc.want) {
			t.Fatalf("rejection=%v want=%v", err, tc.want)
		}
	}
	if err = l.ImportBrowserCapture(ctx, item.ID, item.URL, "", importedMHTML("New version", "Updated content")); err != nil {
		t.Fatal(err)
	}
	d, err = l.Get(ctx, item.ID)
	if err != nil || len(d.Captures) != 2 || !strings.Contains(d.Text, "Updated content") {
		t.Fatalf("history %+v %v", d, err)
	}
	if integrationCount(t, l, `SELECT count(*) FROM jobs`) != 0 {
		t.Fatal("capture import generated PDF or email")
	}
}
