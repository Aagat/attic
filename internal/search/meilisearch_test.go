package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMeilisearchIntegration(t *testing.T) {
	endpoint := os.Getenv("ATTIC_TEST_MEILI_URL")
	if endpoint == "" {
		t.Skip("set ATTIC_TEST_MEILI_URL to run against Meilisearch")
	}
	index, err := NewMeilisearch(Config{URL: endpoint, APIKey: os.Getenv("ATTIC_TEST_MEILI_KEY"), Index: fmt.Sprintf("test_%d", time.Now().UnixNano())})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.mutate(context.Background(), http.MethodDelete, index.path(""), nil) })
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	d := Document{ID: "one", Title: "A saved paper", URL: "https://example.com/paper", Notes: "Personal annotation", Tags: []string{"research", "quote\" OR kind = web"}, Text: "The otherwise invisible quasarphrase lives only in this PDF text. <script>alert(1)</script>", Domain: "example.com", Kind: "pdf", CaptureStatus: "complete", SavedAt: now}
	if err := index.Upsert(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	other := d
	other.ID = "two"
	other.Title = "Another saved paper"
	other.Text = "A different body"
	other.Tags = []string{"other"}
	if err := index.Upsert(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	q := Query{Text: "quasarphrase", Tags: d.Tags, Domain: d.Domain, From: &now, To: &now, CaptureStatus: "complete"}
	result, err := index.Search(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 1 || result.Hits[0].Document.ID != "one" {
		t.Fatalf("wrong filtered results: %+v", result)
	}
	if !strings.Contains(result.Hits[0].Snippet, "quasarphrase") {
		t.Fatalf("missing matching body snippet: %q", result.Hits[0].Snippet)
	}
	if result.Hits[0].Document.Text != "" {
		t.Fatal("search returns unbounded full body")
	}
	result, err = index.Search(context.Background(), Query{Text: "annotation"})
	if err != nil || len(result.Hits) != 2 || !strings.Contains(result.Hits[0].Snippet, "annotation") {
		t.Fatalf("missing notes snippet: %+v %v", result, err)
	}
	d.Text = "Replacement body with replacementphrase"
	if err := index.Upsert(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	result, err = index.Search(context.Background(), Query{Text: "quasarphrase"})
	if err != nil || len(result.Hits) != 0 {
		t.Fatalf("stale text after acknowledged update: %+v %v", result, err)
	}
	result, err = index.Search(context.Background(), Query{Limit: 1, Offset: 1})
	if err != nil || len(result.Hits) != 1 || result.Total != 2 {
		t.Fatalf("pagination: %+v %v", result, err)
	}
	if err := index.Delete(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	if err := index.Delete(context.Background(), d.ID); err != nil {
		t.Fatal("delete must be idempotent:", err)
	}
	result, err = index.Search(context.Background(), Query{Text: "replacementphrase"})
	if err != nil || len(result.Hits) != 0 {
		t.Fatalf("deleted document remains: %+v %v", result, err)
	}
}

func TestMutationsWaitForTasksAndDoNotLeakErrors(t *testing.T) {
	for _, state := range []string{"succeeded", "failed", "processing"} {
		t.Run(state, func(t *testing.T) {
			var polls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer private-secret" {
					t.Error("missing authorization")
				}
				w.Header().Set("Content-Type", "application/json")
				if strings.HasPrefix(r.URL.Path, "/tasks/") {
					polls.Add(1)
					fmt.Fprintf(w, `{"status":%q,"error":{"message":"private-secret"}}`, state)
					return
				}
				if r.Method == http.MethodGet {
					fmt.Fprint(w, `{}`)
					return
				}
				w.WriteHeader(http.StatusAccepted)
				fmt.Fprint(w, `{"taskUid":1}`)
			}))
			defer server.Close()
			index, _ := NewMeilisearch(Config{URL: server.URL, APIKey: "private-secret", Timeout: 150 * time.Millisecond})
			err := index.Upsert(context.Background(), Document{ID: "one", SavedAt: time.Now()})
			if state == "succeeded" {
				if err != nil || polls.Load() < 2 {
					t.Fatalf("not completed: %v", err)
				}
			} else {
				if err == nil || strings.Contains(err.Error(), "private-secret") {
					t.Fatalf("unsafe failure: %v", err)
				}
			}
			if state == "processing" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("unbounded task: %v", err)
			}
		})
	}
}

func TestFiltersRemainValuesAndSnippetsRemainPlainText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Filter                []string
			AttributesToHighlight []string
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.Filter) != 1 || request.Filter[0] != `tags = "quoted\" OR kind = web"` {
			t.Errorf("unsafe filter: %#v", request.Filter)
		}
		if len(request.AttributesToHighlight) != 0 {
			t.Error("HTML highlighting requested")
		}
		fmt.Fprint(w, `{"hits":[{"id":"one","_formatted":{"text":"<script>literal source</script>"}}],"estimatedTotalHits":1}`)
	}))
	defer server.Close()
	index, _ := NewMeilisearch(Config{URL: server.URL})
	index.ready.Store(true)
	result, err := index.Search(context.Background(), Query{Tags: []string{`quoted" OR kind = web`}})
	if err != nil || result.Hits[0].Snippet != "<script>literal source</script>" {
		t.Fatalf("unexpected snippet: %+v %v", result, err)
	}
}

func TestUnavailableAndInvalidInputs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		fmt.Fprint(w, "private backend detail")
	}))
	defer server.Close()
	index, _ := NewMeilisearch(Config{URL: server.URL})
	_, err := index.Search(context.Background(), Query{})
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "private") {
		t.Fatalf("unsafe error: %v", err)
	}
	for _, q := range []Query{{Limit: 101}, {Offset: -1}} {
		if _, err := index.Search(context.Background(), q); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	if err := index.Delete(context.Background(), "../tasks"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestMeilisearchIntegrationRebuildAfterExternalDeletion(t *testing.T) {
	endpoint := os.Getenv("ATTIC_TEST_MEILI_URL")
	if endpoint == "" {
		t.Skip("set ATTIC_TEST_MEILI_URL to run against Meilisearch")
	}
	index, err := NewMeilisearch(Config{URL: endpoint, APIKey: os.Getenv("ATTIC_TEST_MEILI_KEY"), Index: fmt.Sprintf("rebuild_%d", time.Now().UnixNano())})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = index.mutate(cleanup, http.MethodDelete, index.path(""), nil)
	})
	document := Document{ID: "preserved", Title: "Rebuilt record", Text: "rebuildphrase lives in canonical text", Tags: []string{"saved"}, SavedAt: time.Now()}
	query := Query{Text: "rebuildphrase", Tags: []string{"saved"}}
	if err := index.Upsert(ctx, document); err != nil {
		t.Fatal(err)
	}
	// Deleting via the engine endpoint models an external index reset. Successful
	// deletion deliberately leaves the adapter's initialization cache untouched.
	if err := index.mutate(ctx, http.MethodDelete, index.path(""), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Search(ctx, query); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing index error: %v", err)
	}
	if index.ready.Load() {
		t.Fatal("404 retained stale initialization state")
	}
	if err := index.Upsert(ctx, document); err != nil {
		t.Fatal("reindex after 404:", err)
	}
	result, err := index.Search(ctx, query)
	if err != nil || len(result.Hits) != 1 {
		t.Fatalf("404 recovery failed: %+v %v", result, err)
	}
	// If upsert is the first operation after deletion, Meilisearch may create a
	// default index implicitly. Its missing filter settings must also be repaired.
	if err := index.mutate(ctx, http.MethodDelete, index.path(""), nil); err != nil {
		t.Fatal(err)
	}
	if err := index.Upsert(ctx, document); err != nil {
		if err := index.Upsert(ctx, document); err != nil {
			t.Fatal("upsert retry failed:", err)
		}
	}
	result, err = index.Search(ctx, query)
	if err != nil {
		result, err = index.Search(ctx, query)
	}
	if err != nil || len(result.Hits) != 1 || result.Hits[0].Document.ID != document.ID {
		t.Fatalf("implicit recreation settings not repaired: %+v %v", result, err)
	}
}

func TestPreservedFilterIncludesExtractedPartialCopies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Filter []string `json:"filter"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Filter) != 1 || body.Filter[0] != `capture_status IN ["complete", "partial"]` {
			t.Errorf("preserved filter excludes usable copies: %v", body.Filter)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"hits":[],"estimatedTotalHits":0}`)
	}))
	defer server.Close()
	index, err := NewMeilisearch(Config{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	index.ready.Store(true) // This test exercises query construction, not index setup.
	if _, err := index.Search(context.Background(), Query{CaptureStatus: "preserved"}); err != nil {
		t.Fatal(err)
	}
}
