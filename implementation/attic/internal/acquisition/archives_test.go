package acquisition

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
)

func TestWaybackSnapshotValidatesSourceAndProvider(t *testing.T) {
	for _, tt := range []struct {
		snapshot string
		ok       bool
	}{
		{"http://web.archive.org/web/20260907120000/https://example.com/article?q=1", true},
		{"https://evil.example/web/20260907120000/https://example.com/article?q=1", false},
		{"https://web.archive.org/web/20260907120000/https://example.com/other?q=1", false},
		{"https://web.archive.org/web/20260907120000/https://example.com/article?q=2", false},
	} {
		body := []byte(fmt.Sprintf(`{"archived_snapshots":{"closest":{"available":true,"status":"200","url":%q}}}`, tt.snapshot))
		got := waybackSnapshot(body, "https://example.com/article?q=1")
		if (got != "") != tt.ok {
			t.Fatalf("snapshot %s: %q", tt.snapshot, got)
		}
	}
	if waybackSnapshot([]byte(`{"archived_snapshots":{}}`), "https://example.com") != "" {
		t.Fatal("invented snapshot")
	}
}

func TestArchivesNeverRecursivelyArchivesAnArchive(t *testing.T) {
	for _, source := range []string{"https://archive.ph/abcde", "https://web.archive.org/web/2026/https://example.com", "http://127.0.0.1/"} {
		if got := (Archives{}).Candidates(context.Background(), source); len(got) != 0 {
			t.Fatalf("unexpected archive targets: %v", got)
		}
	}
}

func TestJSONDiscoveryRetainsFetchLimits(t *testing.T) {
	f := testFetcher(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Error("missing JSON accept header")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"archived_snapshots":{}}`)
	}), FetchConfig{MaxBytes: 8})
	if _, err := f.FetchJSON(context.Background(), "http://archive.example/available"); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("limit error: %v", err)
	}
	if _, err := f.FetchJSON(context.Background(), "http://127.0.0.1/"); !errors.Is(err, ErrBlockedTarget) {
		t.Fatalf("address policy: %v", err)
	}
}
