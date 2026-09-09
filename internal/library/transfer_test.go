package library

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"attic/internal/domain"
	"attic/internal/postgres"
)

func archiveFixture(t *testing.T) ([]byte, archiveManifest) {
	t.Helper()
	now := time.Date(2025, 4, 2, 3, 4, 5, 0, time.UTC)
	payloads := map[string][]byte{}
	artifact := func(body, filename, kind string) domain.Artifact {
		sum := sha256.Sum256([]byte(body))
		checksum := hex.EncodeToString(sum[:])
		payloads[checksum] = []byte(body)
		return domain.Artifact{Key: "original/" + filename, Filename: filename, MediaType: kind, ByteSize: int64(len(body)), Checksum: checksum, Available: true, CreatedAt: now}
	}
	pdf := artifact("%PDF-1.4\nexact original document bytes\n%%EOF", "reading.pdf", "application/pdf")
	id := "saved-bookmark"
	manifest := archiveManifest{Format: "attic-archive-v1", CreatedAt: now, Items: []archiveItem{
		{Item: Item{ID: id, Kind: "bookmark", URL: "https://example.com/story?edition=2", Title: "My edited title", Notes: "remember this", Tags: []string{"research"}, Folders: []string{"Reading/Research"}, SavedAt: now, UpdatedAt: now, CaptureStatus: "complete", EnrichmentStatus: "complete", Classification: "Technical", SuggestedTags: []string{"systems"}}, TitleEdited: true, SourceTitle: "Browser title", Text: "A phrase preserved only in the saved content", PDF: &pdf, Captures: []archiveCapture{
			{Capture: Capture{ID: "capture-v1", FinalURL: "https://example.com/story?edition=2", Title: "Version one", Status: "partial", Missing: []string{"missing.png"}, CreatedAt: now}, Text: "First version", Artifact: artifact("<html><body>First version</body></html>", "v1.html", "text/html")},
			{Capture: Capture{ID: "capture-v2", FinalURL: "https://archive.example/snapshot", Title: "Version two", Status: "complete", Missing: []string{}, CreatedAt: now.Add(time.Hour)}, Text: "Second version", Artifact: artifact("<html><body>Second version</body></html>", "v2.html", "text/html")},
		}},
		{Item: Item{ID: "saved-upload", Kind: "pdf", Title: "Uploaded PDF", SavedAt: now, UpdatedAt: now, CaptureStatus: "not_applicable", EnrichmentStatus: "complete"}, Text: "original PDF searchable phrase", PDF: &pdf},
	}, Sources: []archiveSource{{ClientID: "browser", NodeID: "42", ItemID: &id, URL: "https://example.com/story?edition=2", Title: "Browser title", Folder: "Reading/Research", SavedAt: &now}}, Tombstones: []archiveTombstone{{URLHash: urlHash("https://example.com/deleted"), DeletedAt: now}}}
	manifest.Items[0].Content, _ = json.Marshal(map[string]any{"title": "Approved title", "author": "Original author", "site_name": "Original publisher", "publication_date": now, "description": "Source description", "source_url": "https://example.com/story?edition=2", "semantic_html": "<article><h1>Approved title</h1><p>Approved prose.</p></article>", "plain_text": "Approved prose.", "extraction_method": "ai-approved", "ai_confidence": 0.9, "ai_completeness": 0.95, "detected_language": "en", "created_at": now, "updated_at": now})
	translated := artifact("%PDF-1.4\ntranslated reading edition\n%%EOF", "translated.pdf", "application/pdf")
	var translatedContent map[string]any
	if err := json.Unmarshal(manifest.Items[0].Content, &translatedContent); err != nil {
		t.Fatal(err)
	}
	translatedContent["title"] = "Título traducido"
	translatedContent["semantic_html"] = "<article><p>Prosa traducida.</p></article>"
	translatedContent["plain_text"] = "Prosa traducida."
	translatedContent["detected_language"] = "es"
	translatedJSON, _ := json.Marshal(translatedContent)
	manifest.Items[0].Item.JobID = "translated-edition"
	manifest.Items[0].Editions = []archiveEdition{
		{JobID: "original-edition", Content: manifest.Items[0].Content, PDF: pdf},
		{JobID: "translated-edition", Language: "es", Content: translatedJSON, PDF: translated},
	}
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	f, _ := writer.Create("manifest.json")
	if err := json.NewEncoder(f).Encode(manifest); err != nil {
		t.Fatal(err)
	}
	for sum, body := range payloads {
		f, _ := writer.Create("files/" + sum)
		f.Write(body)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes(), manifest
}
func TestRestoreRejectsUnsafeAndInvalidArchives(t *testing.T) {
	valid, _ := archiveFixture(t)
	if _, _, err := readArchive(bytes.NewReader(valid), int64(len(valid))); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../escape", "/absolute", "files/../../escape", "files\\escape", "manifest.json/../manifest.json"} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			writer := zip.NewWriter(&out)
			f, _ := writer.Create(name)
			f.Write([]byte("bad"))
			writer.Close()
			if _, _, err := readArchive(bytes.NewReader(out.Bytes()), int64(out.Len())); !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v", err)
			}
		})
	}
	if _, _, err := readArchive(bytes.NewReader(nil), maxTransferBytes+1); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for i := 0; i < 2; i++ {
		f, _ := writer.Create("manifest.json")
		f.Write([]byte(`{"format":"attic-archive-v1"}`))
	}
	writer.Close()
	if _, _, err := readArchive(bytes.NewReader(out.Bytes()), int64(out.Len())); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
func TestExportRestoreIntegration(t *testing.T) {
	rawURL := os.Getenv("ATTIC_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("set ATTIC_TEST_DATABASE_URL for isolated schema round-trip")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", rawURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	roots := map[*Library]string{}
	makeLibrary := func() *Library {
		schema := "transfer_" + opaque()
		if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { admin.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`) })
		u, err := url.Parse(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		root := t.TempDir()
		l, err := Open(ctx, Options{DatabaseURL: u.String(), ArtifactRoot: root, Profile: "kindle-scribe", Destination: "should-never-send@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		roots[l] = root
		t.Cleanup(func() { l.Close() })
		runner, err := postgres.NewMigrationRunner(l.db, "../../migrations")
		if err != nil {
			t.Fatal(err)
		}
		if err := runner.Run(ctx); err != nil {
			t.Fatal(err)
		}
		return l
	}
	source := makeLibrary()
	data, manifest := archiveFixture(t)
	// A valid ZIP with altered PDF bytes must roll back the item and its already
	// written capture files, rather than publishing a partially restored record.
	badLibrary := makeLibrary()
	zipReader, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	var corrupted bytes.Buffer
	zipWriter := zip.NewWriter(&corrupted)
	for _, f := range zipReader.File {
		body, _ := readEntry(f)
		if f.Name == "files/"+manifest.Items[0].Editions[1].PDF.Checksum {
			body[0] ^= 1
		}
		dest, _ := zipWriter.Create(f.Name)
		dest.Write(body)
	}
	zipWriter.Close()
	if count, err := badLibrary.Restore(ctx, bytes.NewReader(corrupted.Bytes()), int64(corrupted.Len())); !errors.Is(err, ErrInvalid) || count != 0 {
		t.Fatalf("corrupt restore count=%d err=%v", count, err)
	}
	var records int
	badLibrary.db.QueryRow(`SELECT count(*) FROM saved_items`).Scan(&records)
	if records != 0 {
		t.Fatal("failed restore published metadata")
	}
	filepath.WalkDir(roots[badLibrary], func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		if !entry.IsDir() {
			t.Errorf("failed restore leaked file %s", path)
		}
		return nil
	})
	if count, err := source.Restore(ctx, bytes.NewReader(data), int64(len(data))); err != nil || count != 2 {
		t.Fatalf("initial restore count=%d err=%v", count, err)
	}
	// Email delivery must not hide an already durable reading PDF from backups.
	if _, err := source.db.ExecContext(ctx, `UPDATE jobs SET status='delivering',delivery_pending=true,completed_at=NULL,lease_token='transfer-test',lease_expires_at=now()+interval '1 minute' WHERE id=(SELECT job_id FROM saved_items WHERE id='saved-bookmark')`); err != nil {
		t.Fatal(err)
	}
	var backup bytes.Buffer
	if err := source.Export(ctx, &backup); err != nil {
		t.Fatal(err)
	}
	destination := makeLibrary()
	for attempt := 0; attempt < 2; attempt++ {
		if count, err := destination.Restore(ctx, bytes.NewReader(backup.Bytes()), int64(backup.Len())); err != nil || count != 2 {
			t.Fatalf("restore %d: count=%d err=%v", attempt, count, err)
		}
	}
	detail, err := destination.Get(ctx, "saved-bookmark")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Title != "My edited title" || detail.Notes != "remember this" || len(detail.Tags) != 1 || len(detail.Captures) != 2 || detail.Captures[0].FinalURL != "https://archive.example/snapshot" || !detail.SavedAt.Equal(manifest.Items[0].Item.SavedAt) || !detail.HasPDF || detail.IndexStatus != "pending" {
		t.Fatalf("round-trip: %+v", detail)
	}
	reader, err := destination.files.Open(ctx, detail.Captures[0].Artifact)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || string(body) != "<html><body>Second version</body></html>" {
		t.Fatalf("capture bytes %s, err=%v", body, err)
	}
	var title, author, semantic, extraction string
	if err := destination.db.QueryRow(`SELECT title,author,semantic_html,extraction_method FROM content_documents WHERE job_id=$1`, detail.JobID).Scan(&title, &author, &semantic, &extraction); err != nil {
		t.Fatal(err)
	}
	if title != "Título traducido" || author != "Original author" || extraction != "ai-approved" || !strings.Contains(semantic, "Prosa traducida.") {
		t.Fatalf("approved content not preserved: %s %s %s %s", title, author, semantic, extraction)
	}
	var pending, jobs, captures, sources int
	if err := destination.db.QueryRow(`SELECT count(*) FILTER(WHERE delivery_pending OR status='queued'),count(*) FROM jobs`).Scan(&pending, &jobs); err != nil {
		t.Fatal(err)
	}
	destination.db.QueryRow(`SELECT count(*) FROM saved_captures`).Scan(&captures)
	destination.db.QueryRow(`SELECT count(*) FROM bookmark_sources`).Scan(&sources)
	if pending != 0 || jobs != 3 || captures != 2 || sources != 1 {
		t.Fatalf("side effects pending=%d jobs=%d captures=%d sources=%d", pending, jobs, captures, sources)
	}
	_, _, err = destination.Save(ctx, SaveRequest{URL: "https://example.com/deleted", Source: &Source{ClientID: "browser", NodeID: "deleted"}}, "")
	if !errors.Is(err, ErrDeleted) {
		t.Fatalf("tombstone not restored: %v", err)
	}
	var originalJob, translatedJob string
	if err := destination.db.QueryRow(`SELECT job_id FROM reading_editions WHERE item_id='saved-bookmark' AND language=''`).Scan(&originalJob); err != nil {
		t.Fatal(err)
	}
	if err := destination.db.QueryRow(`SELECT job_id FROM reading_editions WHERE item_id='saved-bookmark' AND language='es'`).Scan(&translatedJob); err != nil {
		t.Fatal(err)
	}
	var originalText string
	if err := destination.db.QueryRow(`SELECT plain_text FROM content_documents WHERE job_id=$1`, originalJob).Scan(&originalText); err != nil || originalText != "Approved prose." {
		t.Fatalf("original edition lost: %q %v", originalText, err)
	}
	if translatedJob != detail.JobID {
		t.Fatal("selected translated edition was not restored")
	}
	if _, err := destination.db.Exec(`UPDATE saved_items SET job_id=$1 WHERE id='saved-bookmark'`, originalJob); err != nil {
		t.Fatal(err)
	}
	if err := destination.Edit(ctx, "saved-bookmark", SaveRequest{Title: "Newer local edit", Notes: "new notes"}); err != nil {
		t.Fatal(err)
	}
	if _, err := destination.Restore(ctx, bytes.NewReader(backup.Bytes()), int64(backup.Len())); err != nil {
		t.Fatal(err)
	}
	detail, err = destination.Get(ctx, "saved-bookmark")
	if err != nil || detail.Title != "Newer local edit" || detail.Notes != "new notes" || detail.JobID != originalJob {
		t.Fatalf("restore overwrote edits: %+v err=%v", detail, err)
	}
}

func TestArchiveValidatesReadingEditions(t *testing.T) {
	data, _ := archiveFixture(t)
	for _, change := range []struct {
		name   string
		mutate func(*archiveEdition)
	}{
		{"unsupported language", func(e *archiveEdition) { e.Language = "xx" }},
		{"wrong content language", func(e *archiveEdition) { e.Language = "fr" }},
		{"missing content", func(e *archiveEdition) { e.Content = nil }},
		{"not PDF", func(e *archiveEdition) { e.PDF.MediaType = "text/html" }},
		{"missing artifact", func(e *archiveEdition) { e.PDF.Checksum = strings.Repeat("0", 64) }},
	} {
		t.Run(change.name, func(t *testing.T) {
			manifest, files, err := readArchive(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			change.mutate(&manifest.Items[0].Editions[1])
			var out bytes.Buffer
			writer := zip.NewWriter(&out)
			entry, _ := writer.Create("manifest.json")
			json.NewEncoder(entry).Encode(manifest)
			for name, file := range files {
				if name != "manifest.json" {
					body, _ := readEntry(file)
					entry, _ := writer.Create(name)
					entry.Write(body)
				}
			}
			writer.Close()
			if _, _, err := readArchive(bytes.NewReader(out.Bytes()), int64(out.Len())); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid edition accepted: %v", err)
			}
		})
	}
}
