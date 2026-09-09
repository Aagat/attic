package library

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"attic/internal/ai"
	"attic/internal/capture"
	"attic/internal/domain"
	"attic/internal/search"
)

// Every test gets a private schema. The configured database must be disposable;
// migrations and cleanup affect only that generated schema.
func integrationLibrary(t *testing.T, destination string) *Library {
	t.Helper()
	raw := os.Getenv("ATTIC_LIBRARY_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set ATTIC_LIBRARY_TEST_DATABASE_URL for PostgreSQL integration tests")
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	schema := "library_test_" + opaque()
	if _, err = admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	library, err := Open(context.Background(), Options{DatabaseURL: u.String(), ArtifactRoot: t.TempDir(), Profile: "kindle-scribe", Destination: destination})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { library.Close() })
	paths, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = library.db.Exec(string(data)); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	return library
}
func integrationCount(t *testing.T, l *Library, query string) int {
	t.Helper()
	var n int
	if err := l.db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLibraryIntegrationBookmarkAndKindleActions(t *testing.T) {
	ctx := context.Background()
	l := integrationLibrary(t, "")
	item, created, err := l.Save(ctx, SaveRequest{URL: "https://example.com/read?edition=one#heading", Title: "First"}, "")
	if err != nil || !created || item.JobID != "" || item.CaptureStatus != "queued" {
		t.Fatalf("bookmark: %+v %v", item, err)
	}
	if n := integrationCount(t, l, `SELECT count(*) FROM jobs`); n != 0 {
		t.Fatalf("bookmark queued %d reading jobs", n)
	}
	if err := l.Send(ctx, item.ID, "same-action"); !errors.Is(err, ErrSMTPDisabled) {
		t.Fatal(err)
	}
	if _, err := l.Get(ctx, item.ID); err != nil {
		t.Fatal("disabled SMTP erased bookmark", err)
	}
	duplicate, created, err := l.Save(ctx, SaveRequest{URL: "https://example.com/read?edition=one#other"}, "")
	if err != nil || created || duplicate.ID != item.ID {
		t.Fatal("fragment did not deduplicate", err)
	}
	distinct, created, err := l.Save(ctx, SaveRequest{URL: "https://example.com/read?edition=two"}, "")
	if err != nil || !created || distinct.ID == item.ID {
		t.Fatal("meaningful query parameter lost", err)
	}
	l.destination = "reader@example.com"
	for i := 0; i < 2; i++ {
		if err := l.Send(ctx, item.ID, "same-action"); err != nil {
			t.Fatal(err)
		}
	}
	if n := integrationCount(t, l, `SELECT count(*) FROM jobs`); n != 1 {
		t.Fatalf("retry created %d reading jobs", n)
	}
	if err := l.Send(ctx, distinct.ID, "same-action"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cross-item idempotency key silently accepted: %v", err)
	}
	if n := integrationCount(t, l, `SELECT count(*) FROM item_actions`); n != 1 {
		t.Fatalf("retry created %d actions", n)
	}
	var destination string
	if err := l.db.QueryRow(`SELECT delivery_destination FROM jobs`).Scan(&destination); err != nil || destination != "reader@example.com" {
		t.Fatalf("delivery intent lost: %q %v", destination, err)
	}
}

func TestLibraryIntegrationBrowserReconciliationAndTombstone(t *testing.T) {
	ctx := context.Background()
	l := integrationLibrary(t, "reader@example.com")
	date := time.Date(2020, 2, 3, 4, 5, 6, 0, time.UTC)
	request := SaveRequest{URL: "https://example.com/bookmark", Title: "Browser title", Action: "kindle", Source: &Source{ClientID: "browser-one", NodeID: "42", Folder: "Research/Systems", SavedAt: date}}
	item, _, err := l.Save(ctx, request, "browser-action")
	if err != nil {
		t.Fatal(err)
	}
	if item.JobID != "" || !item.SavedAt.Equal(date) || len(item.Folders) != 1 {
		t.Fatalf("browser source changed action/date/folder: %+v", item)
	}
	if err := l.Edit(ctx, item.ID, SaveRequest{Title: "My title", Notes: "My note", Tags: []string{"manual"}}); err != nil {
		t.Fatal(err)
	}
	request.Title = "Renamed in browser"
	request.Source.Folder = "Later"
	merged, created, err := l.Save(ctx, request, "")
	if err != nil || created || merged.Title != "My title" || merged.Notes != "My note" || len(merged.Tags) != 1 || len(merged.Folders) != 1 || merged.Folders[0] != "Later" {
		t.Fatalf("annotation/folder merge: %+v %v", merged, err)
	}
	if err := l.Delete(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.Save(ctx, request, ""); !errors.Is(err, ErrDeleted) {
		t.Fatalf("browser resurrected deleted item: %v", err)
	}
	if n := integrationCount(t, l, `SELECT count(*) FROM jobs`); n != 0 {
		t.Fatal("browser saves caused email job")
	}
	if n := integrationCount(t, l, `SELECT count(*) FROM saved_items`); n != 0 {
		t.Fatal("deleted item remains")
	}
	if n := integrationCount(t, l, `SELECT count(*) FROM search_deletions`); n != 1 {
		t.Fatal("missing durable index deletion")
	}
	request.Source = nil
	request.Action = "bookmark"
	if _, created, err := l.Save(ctx, request, ""); err != nil || !created {
		t.Fatal("explicit resave failed", err)
	}
}

type integrationCapturer struct {
	result capture.Result
	err    error
}

func (c integrationCapturer) Capture(context.Context, string) (capture.Result, error) {
	return c.result, c.err
}

func TestLibraryIntegrationCaptureVersionsAndUnavailableAI(t *testing.T) {
	ctx := context.Background()
	l := integrationLibrary(t, "")
	item, _, err := l.Save(ctx, SaveRequest{URL: "https://example.com/captured", Title: "Bookmark title"}, "")
	if err != nil {
		t.Fatal(err)
	}
	first := capture.Result{HTML: []byte(`<html><body>bodyonlyphrase<img src="data:image/png;base64,AA=="></body></html>`), PlainText: "bodyonlyphrase preserved content", Title: "Captured title", FinalURL: item.URL, Status: "complete", MissingResources: []string{}}
	if worked, err := l.CaptureOnce(ctx, integrationCapturer{result: first}); !worked || err != nil {
		t.Fatalf("capture: %v %v", worked, err)
	}
	detail, err := l.Get(ctx, item.ID)
	if err != nil || len(detail.Captures) != 1 || detail.EnrichmentStatus != "pending" || detail.Text != first.PlainText {
		t.Fatalf("capture needs AI unexpectedly: %+v %v", detail, err)
	}
	original := detail.Captures[0].ID
	reader, err := l.OpenCapture(ctx, item.ID, original)
	if err != nil {
		t.Fatal(err)
	}
	html, _ := io.ReadAll(reader)
	reader.Close()
	if !bytes.Equal(html, first.HTML) {
		t.Fatal("saved snapshot changed")
	}
	if err := l.Recapture(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	second := first
	second.PlainText = "Second capture body"
	second.HTML = []byte("<html>Second capture body</html>")
	if _, err := l.CaptureOnce(ctx, integrationCapturer{result: second}); err != nil {
		t.Fatal(err)
	}
	detail, err = l.Get(ctx, item.ID)
	if err != nil || len(detail.Captures) != 2 {
		t.Fatalf("version missing: %+v %v", detail, err)
	}
	reader, err = l.OpenCapture(ctx, item.ID, original)
	if err != nil {
		t.Fatal("old version unavailable", err)
	}
	html, _ = io.ReadAll(reader)
	reader.Close()
	if !bytes.Equal(html, first.HTML) {
		t.Fatal("recapture overwrote old version")
	}
	if err := l.Recapture(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := l.CaptureOnce(ctx, integrationCapturer{err: errors.New("origin unavailable")}); err != nil {
		t.Fatal(err)
	}
	detail, err = l.Get(ctx, item.ID)
	if err != nil || len(detail.Captures) != 2 || detail.Text != second.PlainText {
		t.Fatalf("failed recapture lost preserved text: %+v %v", detail, err)
	}
	size, err := l.StorageBytes(ctx)
	if err != nil || size != int64(len(first.HTML)+len(second.HTML)) {
		t.Fatalf("storage size=%d error=%v", size, err)
	}
}

func integrationSearch(t *testing.T) *search.Meilisearch {
	t.Helper()
	raw := os.Getenv("ATTIC_LIBRARY_TEST_SEARCH_URL")
	if raw == "" {
		t.Skip("set ATTIC_LIBRARY_TEST_SEARCH_URL for Meilisearch integration")
	}
	name := "library_" + opaque()
	key := os.Getenv("ATTIC_LIBRARY_TEST_SEARCH_KEY")
	index, err := search.NewMeilisearch(search.Config{URL: raw, APIKey: key, Index: name})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		request, _ := http.NewRequest(http.MethodDelete, raw+"/indexes/"+name, nil)
		request.Header.Set("Authorization", "Bearer "+key)
		response, err := http.DefaultClient.Do(request)
		if err == nil {
			response.Body.Close()
		}
	})
	return index
}

type integrationUnavailableIndex struct{}

func (integrationUnavailableIndex) Upsert(context.Context, search.Document) error {
	return search.ErrUnavailable
}
func (integrationUnavailableIndex) Delete(context.Context, string) error {
	return search.ErrUnavailable
}
func (integrationUnavailableIndex) Search(context.Context, search.Query) (search.Result, error) {
	return search.Result{}, search.ErrUnavailable
}

type integrationOfflineEnricher struct{}

func (integrationOfflineEnricher) Enrich(context.Context, string) (ai.Enrichment, error) {
	return ai.Enrichment{}, ai.ErrAIUnavailable
}

func TestLibraryIntegrationIndexRetriesAndCapturedTextSearch(t *testing.T) {
	ctx := context.Background()
	l := integrationLibrary(t, "")
	index := integrationSearch(t)
	item, _, err := l.Save(ctx, SaveRequest{URL: "https://example.com/searchable", Title: "Ordinary title"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.IndexOnce(ctx, integrationUnavailableIndex{}); !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
	detail, err := l.Get(ctx, item.ID)
	if err != nil || detail.IndexStatus != "retrying" {
		t.Fatalf("lost dirty state: %+v %v", detail, err)
	}
	if _, err := l.CaptureOnce(ctx, integrationCapturer{result: capture.Result{HTML: []byte("<p>uniquebodyphrase</p>"), PlainText: "uniquebodyphrase found only in saved text", Title: item.Title, FinalURL: item.URL, Status: "complete"}}); err != nil {
		t.Fatal(err)
	}
	if err := l.EnrichOnce(ctx, integrationOfflineEnricher{}); err != nil {
		t.Fatal(err)
	}
	detail, err = l.Get(ctx, item.ID)
	if err != nil || detail.EnrichmentStatus != "failed" || detail.CaptureStatus != "complete" || detail.Text == "" {
		t.Fatalf("AI outage damaged capture: %+v %v", detail, err)
	}
	if err := l.IndexOnce(ctx, index); err != nil {
		t.Fatal(err)
	}
	detail, err = l.Get(ctx, item.ID)
	if err != nil || detail.IndexStatus != "indexed" {
		t.Fatalf("index not acknowledged: %+v %v", detail, err)
	}
	result, err := index.Search(ctx, search.Query{Text: "uniquebodyphrase"})
	if err != nil || len(result.Hits) != 1 || result.Hits[0].Document.ID != item.ID || !strings.Contains(result.Hits[0].Snippet, "uniquebodyphrase") {
		t.Fatalf("captured text missing: %+v %v", result, err)
	}
	if err := l.Delete(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if err := l.IndexOnce(ctx, integrationUnavailableIndex{}); !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
	if n := integrationCount(t, l, `SELECT count(*) FROM search_deletions`); n != 1 {
		t.Fatal("failed deletion was acknowledged")
	}
	if err := l.IndexOnce(ctx, index); err != nil {
		t.Fatal(err)
	}
	result, err = index.Search(ctx, search.Query{Text: "uniquebodyphrase"})
	if err != nil || len(result.Hits) != 0 {
		t.Fatalf("deleted hit remains: %+v %v", result, err)
	}
}

func integrationPDF() []byte {
	var output bytes.Buffer
	output.WriteString("%PDF-1.4\n")
	objects := []string{`<< /Type /Catalog /Pages 2 0 R >>`, `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`, `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 400] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>`, `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`}
	stream := "BT /F1 12 Tf 20 350 Td (uploadeduniquetext preserved exactly) Tj ET\n"
	objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream))
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, output.Len())
		fmt.Fprintf(&output, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&output, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return output.Bytes()
}
func TestLibraryIntegrationUploadedPDFPreserved(t *testing.T) {
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		t.Skip("pdfinfo required")
	}
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext required")
	}
	ctx := context.Background()
	l := integrationLibrary(t, "")
	data := integrationPDF()
	item, err := l.Upload(ctx, "paper.pdf", data, "bookmark", "")
	if err != nil {
		t.Fatal(err)
	}
	if !item.HasPDF || item.CaptureStatus != "not_applicable" || !strings.Contains(item.Text, "uploadeduniquetext") || item.DeliveryStatus != "not_requested" {
		t.Fatalf("upload: %+v", item)
	}
	same, err := l.Upload(ctx, "renamed.pdf", data, "bookmark", "")
	if err != nil || same.ID != item.ID {
		t.Fatal("duplicate upload not merged", err)
	}
	artifact := domain.Artifact{Available: true}
	if err := l.db.QueryRow(`SELECT storage_relative_path,checksum_sha256,byte_size FROM artifacts WHERE job_id=$1`, item.JobID).Scan(&artifact.Key, &artifact.Checksum, &artifact.ByteSize); err != nil {
		t.Fatal(err)
	}
	reader, err := l.files.Open(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	file, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(file, data) {
		t.Fatal("original PDF changed", err)
	}
	if os.Getenv("ATTIC_LIBRARY_TEST_SEARCH_URL") != "" {
		index := integrationSearch(t)
		if err := l.IndexOnce(ctx, index); err != nil {
			t.Fatal(err)
		}
		result, err := index.Search(ctx, search.Query{Text: "uploadeduniquetext"})
		if err != nil || len(result.Hits) != 1 || result.Hits[0].Document.ID != item.ID {
			t.Fatalf("uploaded text not searchable: %+v %v", result, err)
		}
	}
}

// This opt-in check measures the first query after bulk indexing 10,000 varied,
// synthetic article-sized records. It is a reproducible baseline, not a claim
// that the PRD's representative personal collection has been benchmarked.
// Enable with ATTIC_LIBRARY_TEST_BENCHMARK=1 and the search URL/key variables.
func TestLibraryIntegrationSearchTenThousand(t *testing.T) {
	if os.Getenv("ATTIC_LIBRARY_TEST_BENCHMARK") != "1" {
		t.Skip("set ATTIC_LIBRARY_TEST_BENCHMARK=1 for 10k search latency check")
	}
	raw := os.Getenv("ATTIC_LIBRARY_TEST_SEARCH_URL")
	if raw == "" {
		t.Skip("Meilisearch URL required")
	}
	key := os.Getenv("ATTIC_LIBRARY_TEST_SEARCH_KEY")
	name := "benchmark_" + opaque()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	call := func(method, path string, body any, out any) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(data)
		}
		request, _ := http.NewRequestWithContext(ctx, method, raw+path, reader)
		request.Header.Set("Authorization", "Bearer "+key)
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			t.Fatalf("benchmark backend HTTP %d", response.StatusCode)
		}
		if out != nil {
			if err := json.NewDecoder(response.Body).Decode(out); err != nil {
				t.Fatal(err)
			}
		}
	}
	defer func() {
		request, _ := http.NewRequest(http.MethodDelete, raw+"/indexes/"+name, nil)
		request.Header.Set("Authorization", "Bearer "+key)
		response, err := http.DefaultClient.Do(request)
		if err == nil {
			response.Body.Close()
		}
	}()
	index, err := search.NewMeilisearch(search.Config{URL: raw, APIKey: key, Index: name})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.Search(ctx, search.Query{}); err != nil {
		t.Fatal(err)
	}
	topics := []string{"distributed systems", "speculative decoding", "compiler design", "eink typography", "database replication", "scientific publishing", "energy markets", "network protocols", "software testing", "personal archives"}
	documents := make([]search.Document, 10000)
	for i := range documents {
		topic := topics[i%len(topics)]
		documents[i] = search.Document{ID: fmt.Sprintf("doc_%05d", i), Title: fmt.Sprintf("%s study %d", topic, i), URL: fmt.Sprintf("https://source%d.example/articles/%d", i%37, i), Domain: fmt.Sprintf("source%d.example", i%37), Tags: []string{topic, "research"}, Notes: fmt.Sprintf("Read this for project %d", i%113), Text: strings.Repeat(fmt.Sprintf("This %s article discusses evidence, engineering methods, results and tradeoffs across practical implementations. Section %d. ", topic, i%23), 20), Kind: "bookmark", CaptureStatus: "complete", SavedAt: time.Date(2020+i%6, time.Month(1+i%12), 1+i%28, 12, 0, 0, 0, time.UTC)}
	}
	var task struct {
		UID int `json:"taskUid"`
	}
	call(http.MethodPost, "/indexes/"+name+"/documents", documents, &task)
	for {
		var state struct{ Status string }
		call(http.MethodGet, fmt.Sprintf("/tasks/%d", task.UID), nil, &state)
		if state.Status == "succeeded" {
			break
		}
		if state.Status == "failed" || state.Status == "canceled" {
			t.Fatal("bulk indexing failed")
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	start := time.Now()
	result, err := index.Search(ctx, search.Query{Text: "speculative decoding", Limit: 20})
	elapsed := time.Since(start)
	if err != nil || len(result.Hits) != 20 || result.Total != 1000 {
		t.Fatalf("10k retrieval: hits=%d total=%d err=%v", len(result.Hits), result.Total, err)
	}
	t.Logf("10,000 synthetic ~2.5KB article records; first post-index query=%s; hits=%d total=%d", elapsed, len(result.Hits), result.Total)
	if elapsed >= time.Second {
		t.Fatalf("first query exceeded one-second target: %s", elapsed)
	}
}

func TestLibraryIntegrationSourceURLMoveReconcilesBothItems(t *testing.T) {
	ctx := context.Background()
	l := integrationLibrary(t, "")
	request := SaveRequest{URL: "https://example.com/old", Title: "Original", Source: &Source{ClientID: "browser", NodeID: "one", Folder: "Research"}}
	old, _, err := l.Save(ctx, request, "")
	if err != nil {
		t.Fatal(err)
	}
	request.URL = "https://example.com/new"
	moved, _, err := l.Save(ctx, request, "")
	if err != nil || moved.ID == old.ID {
		t.Fatalf("URL move: %+v %v", moved, err)
	}
	prior, err := l.Get(ctx, old.ID)
	if err != nil || len(prior.Folders) != 0 {
		t.Fatalf("old item keeps stale folder: %+v %v", prior, err)
	}
	if len(moved.Folders) != 1 || moved.Folders[0] != "Research" {
		t.Fatalf("new item missing folder: %+v", moved)
	}
}

func TestLibraryIntegrationNetscapeImportKeepsNestedFolders(t *testing.T) {
	ctx := context.Background()
	l := integrationLibrary(t, "reader@example.com")
	exported := `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">
<TITLE>Bookmarks</TITLE><H1>Bookmarks</H1>
<DL><p>
 <DT><H3 ADD_DATE="1580702706">Research</H3>
 <DL><p>
  <DT><H3>Systems</H3>
  <DL><p>
   <DT><A HREF="https://example.com/paper" ADD_DATE="1580702706" TAGS="systems,reading">Paper &amp; notes</A>
  </DL><p>
 </DL><p>
 <DT><A HREF="https://example.com/root">Root bookmark</A>
</DL><p>`
	result, err := l.ImportHTML(ctx, strings.NewReader(exported))
	if err != nil || result.Imported != 2 {
		t.Fatalf("import: %+v %v", result, err)
	}
	items, total, err := l.List(ctx, 50, 0)
	if err != nil || total != 2 {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.JobID != "" {
			t.Fatal("HTML import caused delivery")
		}
		if item.URL == "https://example.com/paper" {
			if item.Title != "Paper & notes" || len(item.Folders) != 1 || item.Folders[0] != "Research/Systems" || len(item.Tags) != 2 || item.SavedAt.Unix() != 1580702706 {
				t.Fatalf("nested import metadata lost: %+v", item)
			}
		} else if len(item.Folders) != 0 {
			t.Fatalf("root bookmark inherited sibling folder: %+v", item)
		}
	}
	repeated, err := l.ImportHTML(ctx, strings.NewReader(exported))
	if err != nil || repeated.Merged != 2 || repeated.Imported != 0 {
		t.Fatalf("resumed import duplicates: %+v %v", repeated, err)
	}
}

func TestLibraryIntegrationApprovedTextSurvivesCaptureFailure(t *testing.T) {
	ctx := context.Background()
	l := integrationLibrary(t, "reader@example.com")
	index := integrationSearch(t)
	item, _, err := l.Save(ctx, SaveRequest{URL: "https://example.com/approved", Title: "Bookmark", Action: "kindle"}, "send-approved")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Edit(ctx, item.ID, SaveRequest{Title: "My edited title"}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.Exec(`UPDATE saved_items SET capture_status='failed' WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	_, err = l.db.Exec(`INSERT INTO content_documents(id,job_id,title,source_url,semantic_html,plain_text,extraction_method,ai_confidence) VALUES($1,$2,'Approved title',$3,'<p>approvedbodyphrase</p>','approvedbodyphrase found in approved PDF content','test',1)`, opaque(), item.JobID, item.URL)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := l.Get(ctx, item.ID)
	if err != nil || detail.Title != "My edited title" || !strings.Contains(detail.Text, "approvedbodyphrase") {
		t.Fatalf("approved content adoption: %+v %v", detail, err)
	}
	if err := l.IndexOnce(ctx, index); err != nil {
		t.Fatal(err)
	}
	result, err := index.Search(ctx, search.Query{Text: "approvedbodyphrase"})
	if err != nil || len(result.Hits) != 1 {
		t.Fatalf("approved body not searchable: %+v %v", result, err)
	}
}

type integrationEnricherFunc func(context.Context, string) (ai.Enrichment, error)

func (f integrationEnricherFunc) Enrich(ctx context.Context, text string) (ai.Enrichment, error) {
	return f(ctx, text)
}

func TestLibraryIntegrationFailedEnrichmentPreservesSuggestions(t *testing.T) {
	ctx := context.Background()
	l := integrationLibrary(t, "")
	item, _, err := l.Save(ctx, SaveRequest{URL: "https://example.com/enrichment", Title: "Saved page", Notes: "My note", Tags: []string{"manual"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.CaptureOnce(ctx, integrationCapturer{result: capture.Result{HTML: []byte("<p>Captured source</p>"), PlainText: "Captured source", Title: item.Title, FinalURL: item.URL, Status: "complete"}}); err != nil {
		t.Fatal(err)
	}
	suggested := ai.Enrichment{Classification: "research paper", Tags: []string{"systems", "performance"}}
	if err := l.EnrichOnce(ctx, integrationEnricherFunc(func(context.Context, string) (ai.Enrichment, error) { return suggested, nil })); err != nil {
		t.Fatal(err)
	}
	if err := l.RetryEnrichment(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if err := l.EnrichOnce(ctx, integrationOfflineEnricher{}); err != nil {
		t.Fatal(err)
	}
	detail, err := l.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.EnrichmentStatus != "failed" || detail.Classification != suggested.Classification || strings.Join(detail.SuggestedTags, ",") != "systems,performance" {
		t.Fatalf("failed enrichment erased previous suggestions: %+v", detail.Item)
	}
	if detail.Notes != "My note" || strings.Join(detail.Tags, ",") != "manual,systems,performance" || detail.Text != "Captured source" {
		t.Fatalf("failed enrichment changed canonical content or annotations: %+v", detail.Item)
	}
}

func TestLibraryIntegrationStaleEnrichmentLeavesNewVersionPending(t *testing.T) {
	ctx := context.Background()
	l := integrationLibrary(t, "")
	item, _, err := l.Save(ctx, SaveRequest{URL: "https://example.com/stale", Title: "Old title"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.CaptureOnce(ctx, integrationCapturer{result: capture.Result{HTML: []byte("<p>Old source</p>"), PlainText: "Old source", Title: item.Title, FinalURL: item.URL, Status: "complete"}}); err != nil {
		t.Fatal(err)
	}
	called := false
	var editedVersion int64
	err = l.EnrichOnce(ctx, integrationEnricherFunc(func(ctx context.Context, text string) (ai.Enrichment, error) {
		called = true
		if !strings.Contains(text, "Old title") {
			t.Errorf("enrichment did not receive original version: %q", text)
		}
		// An edit commits while the model request is in flight. It must invalidate
		// the eventual result even though the enrichment transaction remains open.
		if err := l.Edit(ctx, item.ID, SaveRequest{Title: "New title", Notes: "New note", Tags: []string{"chosen"}}); err != nil {
			return ai.Enrichment{}, err
		}
		edited, err := l.Get(ctx, item.ID)
		if err != nil {
			return ai.Enrichment{}, err
		}
		editedVersion = edited.Version
		return ai.Enrichment{Classification: "stale classification", Tags: []string{"stale"}}, nil
	}))
	if err != nil || !called {
		t.Fatalf("enrichment: called=%v err=%v", called, err)
	}
	detail, err := l.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.EnrichmentStatus != "pending" || detail.Classification != "" || len(detail.SuggestedTags) != 0 || detail.Version != editedVersion {
		t.Fatalf("stale result acknowledged newer version: %+v", detail.Item)
	}
	if detail.Title != "New title" || detail.Notes != "New note" || strings.Join(detail.Tags, ",") != "chosen" {
		t.Fatalf("stale result changed annotations: %+v", detail.Item)
	}
	err = l.EnrichOnce(ctx, integrationEnricherFunc(func(_ context.Context, text string) (ai.Enrichment, error) {
		if !strings.Contains(text, "New title") {
			t.Errorf("retry did not receive current version: %q", text)
		}
		return ai.Enrichment{Classification: "reference", Tags: []string{"current"}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	detail, err = l.Get(ctx, item.ID)
	if err != nil || detail.EnrichmentStatus != "complete" || detail.Classification != "reference" || strings.Join(detail.SuggestedTags, ",") != "current" || strings.Join(detail.Tags, ",") != "chosen,current" {
		t.Fatalf("current version did not complete on retry: %+v %v", detail.Item, err)
	}
}

func TestLibraryIntegrationGeneratePDFWithoutEmail(t *testing.T) {
	ctx := context.Background()
	l := integrationLibrary(t, "")
	item, _, err := l.Save(ctx, SaveRequest{URL: "https://example.com/generate"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"generate-once", "generate-once", "generate-again"} {
		if err := l.GeneratePDF(ctx, item.ID, key); err != nil {
			t.Fatal(err)
		}
	}
	if n := integrationCount(t, l, `SELECT count(*) FROM jobs`); n != 1 {
		t.Fatalf("generated %d jobs", n)
	}
	if n := integrationCount(t, l, `SELECT count(*) FROM jobs WHERE delivery_destination IS NOT NULL OR delivery_pending`); n != 0 {
		t.Fatal("generation requested email")
	}
	generated, err := l.Get(ctx, item.ID)
	if err != nil || generated.PDFStatus != "queued" || generated.DeliveryStatus != "not_requested" {
		t.Fatalf("generation state: %+v %v", generated, err)
	}
	// Explicitly sending while the same PDF is being prepared joins that job.
	l.destination = "reader@example.test"
	if err := l.Send(ctx, item.ID, "send-once"); err != nil {
		t.Fatal(err)
	}
	if err := l.GeneratePDF(ctx, item.ID, "generate-after-send"); err != nil {
		t.Fatal(err)
	}
	if n := integrationCount(t, l, `SELECT count(*) FROM jobs`); n != 1 {
		t.Fatal("send duplicated active preparation")
	}
	if n := integrationCount(t, l, `SELECT count(*) FROM jobs WHERE delivery_destination='reader@example.test'`); n != 1 {
		t.Fatal("explicit delivery request lost")
	}
	sent, err := l.Get(ctx, item.ID)
	if err != nil || sent.DeliveryStatus != "pending" {
		t.Fatalf("delivery state: %+v %v", sent, err)
	}
}

func TestLibraryIntegrationGenerateReusesPDF(t *testing.T) {
	ctx := context.Background()
	l := integrationLibrary(t, "reader@example.test")
	item, err := l.Upload(ctx, "existing.pdf", integrationPDF(), "bookmark", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.GeneratePDF(ctx, item.ID, "existing-pdf"); err != nil {
		t.Fatal(err)
	}
	if n := integrationCount(t, l, `SELECT count(*) FROM jobs`); n != 1 {
		t.Fatal("regenerated existing PDF")
	}
	if n := integrationCount(t, l, `SELECT count(*) FROM jobs WHERE delivery_destination IS NOT NULL OR delivery_pending`); n != 0 {
		t.Fatal("generation sent existing PDF")
	}
}

func TestLookupUsesSavedURLIdentity(t *testing.T) {
	l := integrationLibrary(t, "")
	ctx := context.Background()
	item, _, err := l.Save(ctx, SaveRequest{URL: "https://example.com/read?edition=one#intro", Title: "Saved"}, "")
	if err != nil {
		t.Fatal(err)
	}
	found, err := l.Lookup(ctx, "https://EXAMPLE.com/read?edition=one#different")
	if err != nil || len(found) != 1 || found[0].ID != item.ID {
		t.Fatalf("lookup: %+v %v", found, err)
	}
	found, err = l.Lookup(ctx, "https://example.com/read?edition=two")
	if err != nil || len(found) != 0 {
		t.Fatalf("different query matched: %+v %v", found, err)
	}
	if _, err = l.Lookup(ctx, "javascript:alert(1)"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid URL: %v", err)
	}
}

func TestFilterSuggestionsUseCanonicalItems(t *testing.T) {
	l := integrationLibrary(t, "")
	ctx := context.Background()
	for _, raw := range []string{"https://example.com:8443/one", "https://example.com/two", "https://other.example/read"} {
		if _, _, err := l.Save(ctx, SaveRequest{URL: raw, Tags: []string{"Design", "100%", "reading"}}, ""); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := l.Suggestions(ctx, "source", "EXAMPLE.COM")
	if err != nil || len(sources) != 1 || sources[0] != "example.com" {
		t.Fatalf("sources: %v %v", sources, err)
	}
	tags, err := l.Suggestions(ctx, "tag", "des")
	if err != nil || len(tags) != 1 || tags[0] != "Design" {
		t.Fatalf("tags: %v %v", tags, err)
	}
	tags, err = l.Suggestions(ctx, "tag", "%")
	if err != nil || len(tags) != 1 || tags[0] != "100%" {
		t.Fatalf("literal matching: %v %v", tags, err)
	}
	if _, err = l.Suggestions(ctx, "invalid", ""); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestApplyExistingSuggestedTags(t *testing.T) {
	l := integrationLibrary(t, "")
	ctx := context.Background()
	item, _, err := l.Save(ctx, SaveRequest{URL: "https://example.com/tag-migration", Tags: []string{"manual", "shared"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE saved_items SET suggested_tags='["shared","systems","systems"]'::jsonb WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/0009_apply_suggested_tags.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := l.db.ExecContext(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	detail, err := l.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(detail.Tags, ",") != "manual,shared,systems" {
		t.Fatalf("merged tags: %v", detail.Tags)
	}
}
