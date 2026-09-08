package library

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"attic/internal/delivery"
	"attic/internal/postgres"
)

func TestLibraryIntegrationAdoptsOlderAvailablePDF(t *testing.T) {
	l := integrationLibrary(t, "")
	ctx := context.Background()
	_, err := l.db.Exec(`INSERT INTO jobs(id,submitted_url,output_profile,status,correlation_id,failure_category) VALUES
 ('older','https://example.test/old','kindle-scribe','ready','older',NULL),
 ('newer','https://example.test/old','kindle-scribe','failed','newer','fetch_failed');
 INSERT INTO artifacts(id,job_id,profile,storage_relative_path,safe_filename,media_type,byte_size,checksum_sha256) VALUES('old-artifact','older','kindle-scribe','older/test.pdf','test.pdf','application/pdf',4,repeat('a',64));
 INSERT INTO saved_items(id,kind,url,job_id) VALUES('adopted','bookmark','https://example.test/old','newer')`)
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/0006_adopt_available_documents.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	item, err := l.Get(ctx, "adopted")
	if err != nil || item.JobID != "older" || !item.HasPDF {
		t.Fatalf("lost available document %+v %v", item, err)
	}
}

func TestLibraryIntegrationDeliveryClaimsUploadedDocument(t *testing.T) {
	l := integrationLibrary(t, "reader@example.test")
	ctx := context.Background()
	// Uploaded documents have no AI-approved content row. Delivery must still work.
	_, err := l.db.Exec(`INSERT INTO jobs(id,source_kind,title_hint,output_profile,status,correlation_id,delivery_pending,delivery_destination) VALUES('upload','upload','Original PDF','kindle-scribe','ready','upload',true,'reader@example.test');
 INSERT INTO artifacts(id,job_id,profile,storage_relative_path,safe_filename,media_type,byte_size,checksum_sha256) VALUES('upload-artifact','upload','kindle-scribe','upload/test.pdf','test.pdf','application/pdf',4,repeat('a',64))`)
	if err != nil {
		t.Fatal(err)
	}
	store, err := postgres.NewStore(l.db, postgres.Options{})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimDelivery(ctx, time.Minute)
	if err != nil || claim == nil || claim.Title != "Original PDF" {
		t.Fatalf("upload claim %+v %v", claim, err)
	}
	if err := store.FinishDelivery(ctx, claim, delivery.Result{Outcome: "accepted", Code: "250"}); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryIntegrationRestoreUploadCleansStaging(t *testing.T) {
	l := integrationLibrary(t, "")
	ctx := context.Background()
	var archive bytes.Buffer
	if err := l.Export(ctx, &archive); err != nil {
		t.Fatal(err)
	}
	if _, err := l.RestoreUpload(ctx, bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatal(err)
	}
	if _, err := l.RestoreUpload(ctx, strings.NewReader("not a zip")); err == nil {
		t.Fatal("accepted corrupt backup")
	}
	files, err := filepath.Glob(filepath.Join(l.root, ".restore-*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("staging files remain %v %v", files, err)
	}
}
