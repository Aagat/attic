package acquisition

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
	for _, source := range []string{"https://archive.ph/abcde", "https://web.archive.org/web/2026/https://archive.is/abcde", "http://127.0.0.1/"} {
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

func TestArchivesPreferAPIAndFallBackToCDX(t *testing.T) {
	for _, available := range []bool{true, false} {
		t.Run(fmt.Sprint(available), func(t *testing.T) {
			calls := 0
			resolver := Archives{fetchJSON: func(ctx context.Context, raw string) (Page, error) {
				calls++
				u, _ := url.Parse(raw)
				if u.Query().Get("url") != "https://example.com/article?q=1" {
					t.Fatalf("wrong source: %s", raw)
				}
				if calls == 1 {
					if u.Host != "archive.org" {
						t.Fatal(raw)
					}
					if !available {
						return Page{}, errors.New("service unavailable")
					}
					return Page{HTML: []byte(`{"archived_snapshots":{"closest":{"available":true,"status":"200","url":"http://web.archive.org/web/20260907120000/https://example.com/article?q=1"}}}`)}, nil
				}
				if calls > 2 || u.Host != "web.archive.org" || u.Query().Get("matchType") != "exact" || u.Query().Get("limit") != "-3" {
					t.Fatal(raw)
				}
				return Page{HTML: []byte(`[["timestamp","original","statuscode"],["20260907120000","https://example.com/article?q=1","200"],["20260908120000","https://other.example/article?q=1","200"]]`)}, nil
			}}
			got := resolver.Candidates(context.Background(), "https://example.com/article?q=1#section")
			if len(got) != 3 || got[0] != "https://web.archive.org/web/20260907120000/https://example.com/article?q=1" || !strings.HasPrefix(got[1], "https://archive.ph/newest/") {
				t.Fatalf("candidates: %v", got)
			}
			if available && calls != 1 || !available && calls != 2 {
				t.Fatal(calls)
			}
		})
	}
}

func TestArchivesRecoverEmbeddedOriginal(t *testing.T) {
	for _, source := range []string{
		"https://web.archive.org/web/20260907120000id_/https://example.com/a%2Fb?q=1",
		"https://archive.is/2026.09.07-120000/https://example.com/a%2Fb?q=1",
		"https://archive.ph/newest/https://example.com/a%2Fb?q=1",
	} {
		resolver := Archives{fetchJSON: func(_ context.Context, raw string) (Page, error) {
			u, _ := url.Parse(raw)
			if u.Query().Get("url") != "https://example.com/a%2Fb?q=1" {
				t.Fatalf("lost original escaping: %s", raw)
			}
			return Page{HTML: []byte(`{}`)}, nil
		}}
		got := resolver.Candidates(context.Background(), source)
		if len(got) != 3 || got[0] != "https://example.com/a%2Fb?q=1" {
			t.Fatalf("%s: %v", source, got)
		}
	}
}

func TestArchiveDiscoveryFailuresKeepHTMLFallbacks(t *testing.T) {
	calls := 0
	resolver := Archives{fetchJSON: func(ctx context.Context, raw string) (Page, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("missing discovery deadline")
		}
		return Page{}, errors.New("unavailable")
	}}
	if got := resolver.Candidates(context.Background(), "https://example.com/a"); len(got) != 2 || calls != 2 {
		t.Fatalf("%v (%d calls)", got, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := resolver.Candidates(ctx, "https://example.com/a"); len(got) != 0 || calls != 2 {
		t.Fatal("discovery after cancellation")
	}
}

func TestArchiveDiscoveryRejectsUnrelatedOrUnsafeSnapshots(t *testing.T) {
	for _, source := range []string{
		"https://web.archive.org/web/no-date/https://example.com/a%2Fb",
		"https://web.archive.org/web/20260907120000/https://example.com/a/b",
		"https://web.archive.org:8443/web/20260907120000/https://example.com/a%2Fb",
		"https://web.archive.org/web/20260907120000/file://example.com/a%2Fb",
	} {
		body := []byte(fmt.Sprintf(`{"archived_snapshots":{"closest":{"available":true,"status":"200","url":%q}}}`, source))
		if got := waybackSnapshot(body, "https://example.com/a%2Fb"); got != "" {
			t.Fatal(got)
		}
	}
	body := []byte(`[["timestamp","original","statuscode"],["20260908120000","https://example.com/a","200"],["20260907120000","https://example.com/a","200"],["20260909120000","https://example.com/a","403"],["20260910120000","https://other.example/a","200"]]`)
	if got := cdxSnapshot(body, "https://example.com/a", "https://web.archive.org/web/20260908120000/https://example.com/a"); got != "https://web.archive.org/web/20260907120000/https://example.com/a" {
		t.Fatal(got)
	}
}
