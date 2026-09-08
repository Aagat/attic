package filesystem

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"attic/internal/application"
	"attic/internal/domain"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return store
}

func testNow() time.Time {
	return time.Date(2026, time.August, 31, 12, 0, 0, 123, time.FixedZone("test", 2*60*60))
}

func artifactPath(store *Store, artifact domain.Artifact) string {
	return filepath.Join(store.root, filepath.FromSlash(artifact.Key))
}

func TestStoreImplementsArtifactStore(t *testing.T) {
	var _ application.ArtifactStore = (*Store)(nil)
}

func TestNewCreatesRestrictedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "artifacts")
	store, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	info, err := os.Stat(store.root)
	if err != nil {
		t.Fatalf("Stat(root) error = %v", err)
	}
	if !info.IsDir() {
		t.Fatal("root is not a directory")
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o750); got != want {
		t.Fatalf("root mode = %o, want %o", got, want)
	}
}

func TestNewRejectsRootSymlink(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := New(link); !errors.Is(err, ErrInvalidRoot) {
		t.Fatalf("New(symlink) error = %v, want ErrInvalidRoot", err)
	}
}

func TestPutOpenRoundTripAndMetadata(t *testing.T) {
	store := newTestStore(t)
	payload := []byte("a selectable article body\n")
	now := testNow()

	artifact, err := store.Put(context.Background(), domain.JobID("job-123"), "article.pdf", payload, now)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	hash := sha256.Sum256(payload)
	if artifact.Key != "job-123/article.pdf" {
		t.Fatalf("Key = %q", artifact.Key)
	}
	if artifact.Filename != "article.pdf" || artifact.MediaType != "application/pdf" {
		t.Fatalf("metadata = %#v", artifact)
	}
	if artifact.ByteSize != int64(len(payload)) {
		t.Fatalf("ByteSize = %d, want %d", artifact.ByteSize, len(payload))
	}
	if artifact.Checksum != hex.EncodeToString(hash[:]) {
		t.Fatalf("Checksum = %q", artifact.Checksum)
	}
	if !artifact.Available || !artifact.CreatedAt.Equal(now.UTC()) {
		t.Fatalf("availability/time = %#v", artifact)
	}

	fileInfo, err := os.Stat(artifactPath(store, artifact))
	if err != nil {
		t.Fatalf("Stat(file) error = %v", err)
	}
	if got, want := fileInfo.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("file mode = %o, want %o", got, want)
	}
	dirInfo, err := os.Stat(filepath.Dir(artifactPath(store, artifact)))
	if err != nil {
		t.Fatalf("Stat(job dir) error = %v", err)
	}
	if got, want := dirInfo.Mode().Perm(), os.FileMode(0o750); got != want {
		t.Fatalf("job dir mode = %o, want %o", got, want)
	}

	body, err := store.Open(context.Background(), artifact)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	got, err := io.ReadAll(body)
	closeErr := body.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("Read/Close error = %v/%v", err, closeErr)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("body = %q, want %q", got, payload)
	}
}

func TestPutRejectsEmptyAndUnsafeKeys(t *testing.T) {
	store := newTestStore(t)
	unsafe := []struct {
		name     string
		jobID    domain.JobID
		filename string
	}{
		{name: "job traversal", jobID: domain.JobID("../outside"), filename: "article.pdf"},
		{name: "filename traversal", jobID: domain.JobID("job"), filename: "../outside.pdf"},
		{name: "absolute filename", jobID: domain.JobID("job"), filename: "/tmp/outside.pdf"},
		{name: "nested filename", jobID: domain.JobID("job"), filename: "nested/article.pdf"},
		{name: "backslash", jobID: domain.JobID(`job\outside`), filename: "article.pdf"},
		{name: "dot filename", jobID: domain.JobID("job"), filename: "."},
	}
	for _, test := range unsafe {
		t.Run(test.name, func(t *testing.T) {
			_, err := store.Put(context.Background(), test.jobID, test.filename, []byte("pdf"), testNow())
			if !errors.Is(err, ErrUnsafeKey) {
				t.Fatalf("Put() error = %v, want ErrUnsafeKey", err)
			}
		})
	}
	if _, err := store.Put(context.Background(), domain.JobID("job"), "empty.pdf", nil, testNow()); !errors.Is(err, ErrEmptyArtifact) {
		t.Fatalf("Put(empty) error = %v, want ErrEmptyArtifact", err)
	}
}

func TestPutAndOpenRejectSymlinkEscape(t *testing.T) {
	store := newTestStore(t)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("must remain untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	jobLink := filepath.Join(store.root, "job-link")
	if err := os.Symlink(filepath.Dir(outside), jobLink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := store.Put(context.Background(), domain.JobID("job-link"), "artifact.pdf", []byte("new"), testNow()); !errors.Is(err, ErrUnsafeKey) {
		t.Fatalf("Put through parent symlink error = %v", err)
	}
	artifact := domain.Artifact{Key: "job-link/artifact.pdf", Filename: "artifact.pdf", ByteSize: 3, Checksum: strings.Repeat("0", 64), Available: true}
	if _, err := store.Open(context.Background(), artifact); !errors.Is(err, ErrUnsafeKey) {
		t.Fatalf("Open through parent symlink error = %v", err)
	}
	if err := store.Delete(context.Background(), artifact); !errors.Is(err, ErrUnsafeKey) {
		t.Fatalf("Delete through parent symlink error = %v", err)
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "must remain untouched" {
		t.Fatalf("outside file changed to %q", got)
	}
}

func TestFinalSymlinkIsNotFollowed(t *testing.T) {
	store := newTestStore(t)
	jobDir := filepath.Join(store.root, "job")
	if err := os.Mkdir(jobDir, 0o750); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.pdf")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(jobDir, "article.pdf")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	artifact := domain.Artifact{Key: "job/article.pdf", Filename: "article.pdf", ByteSize: 7, Checksum: strings.Repeat("0", 64), Available: true}
	if _, err := store.Open(context.Background(), artifact); !errors.Is(err, ErrUnsafeKey) {
		t.Fatalf("Open(final symlink) error = %v", err)
	}
	if _, err := store.Put(context.Background(), domain.JobID("job"), "article.pdf", []byte("replacement"), testNow()); !errors.Is(err, ErrUnsafeKey) {
		t.Fatalf("Put(final symlink) error = %v", err)
	}
	if err := store.Delete(context.Background(), artifact); err != nil {
		t.Fatalf("Delete(final symlink) error = %v", err)
	}
	if _, err := os.Stat(link); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("final symlink still exists, stat error = %v", err)
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "outside" {
		t.Fatalf("outside file changed to %q", got)
	}
}

func TestOpenVerifiesSizeAndChecksum(t *testing.T) {
	store := newTestStore(t)
	payload := []byte("valid-pdf-payload")
	artifact, err := store.Put(context.Background(), domain.JobID("job"), "article.pdf", payload, testNow())
	if err != nil {
		t.Fatal(err)
	}
	path := artifactPath(store, artifact)
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(context.Background(), artifact); !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("Open(size mismatch) error = %v", err)
	}

	sameSize := bytes.Repeat([]byte{'x'}, len(payload))
	if err := os.WriteFile(path, sameSize, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(context.Background(), artifact); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("Open(checksum mismatch) error = %v", err)
	}

	badMetadata := artifact
	badMetadata.Checksum = "not-a-sha256"
	if _, err := store.Open(context.Background(), badMetadata); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("Open(invalid checksum) error = %v", err)
	}
}

func TestOpenMissingUnavailableAndUnsafe(t *testing.T) {
	store := newTestStore(t)
	missing := domain.Artifact{
		Key:       "missing/article.pdf",
		Filename:  "article.pdf",
		ByteSize:  1,
		Checksum:  strings.Repeat("0", 64),
		Available: true,
	}
	if _, err := store.Open(context.Background(), missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open(missing) error = %v, want os.ErrNotExist", err)
	}
	unavailable := missing
	unavailable.Available = false
	if _, err := store.Open(context.Background(), unavailable); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open(unavailable) error = %v, want os.ErrNotExist", err)
	}
	unsafe := missing
	unsafe.Key = "../article.pdf"
	if _, err := store.Open(context.Background(), unsafe); !errors.Is(err, ErrUnsafeKey) {
		t.Fatalf("Open(unsafe) error = %v, want ErrUnsafeKey", err)
	}
	if err := store.Delete(context.Background(), unsafe); !errors.Is(err, ErrUnsafeKey) {
		t.Fatalf("Delete(unsafe) error = %v, want ErrUnsafeKey", err)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	store := newTestStore(t)
	artifact, err := store.Put(context.Background(), domain.JobID("job"), "article.pdf", []byte("pdf"), testNow())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), artifact); err != nil {
		t.Fatalf("first Delete() error = %v", err)
	}
	if err := store.Delete(context.Background(), artifact); err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if _, err := os.Stat(artifactPath(store, artifact)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact still exists, stat error = %v", err)
	}
}

func TestPutCancellationLeavesNoPartialArtifact(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Put(ctx, domain.JobID("cancelled"), "article.pdf", []byte("pdf"), testNow()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Put(cancelled) error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(filepath.Join(store.root, "cancelled")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled job directory exists, stat error = %v", err)
	}
}

func TestPutCancellationDuringWriteRemovesTemporaryFile(t *testing.T) {
	store := newTestStore(t)
	ctx := &cancelAfterChecksContext{Context: context.Background(), cancelAt: 3}
	data := bytes.Repeat([]byte("pdf\n"), 32*1024)
	if _, err := store.Put(ctx, domain.JobID("cancelled"), "article.pdf", data, testNow()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Put(mid-write cancellation) error = %v, want context.Canceled", err)
	}
	jobDir := filepath.Join(store.root, "cancelled")
	entries, err := os.ReadDir(jobDir)
	if err != nil {
		t.Fatalf("ReadDir(job dir) error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary entries remain after cancellation: %v", entries)
	}
}

func TestOpenReadStopsOnCancellation(t *testing.T) {
	store := newTestStore(t)
	artifact, err := store.Put(context.Background(), domain.JobID("job"), "article.pdf", bytes.Repeat([]byte("x"), 1024), testNow())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	body, err := store.Open(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	buffer := make([]byte, 1)
	if _, err := body.Read(buffer); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read(after cancel) error = %v, want context.Canceled", err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestPutIsAtomicOverExistingArtifact(t *testing.T) {
	store := newTestStore(t)
	oldPayload := []byte("old-pdf")
	artifact, err := store.Put(context.Background(), domain.JobID("job"), "article.pdf", oldPayload, testNow())
	if err != nil {
		t.Fatal(err)
	}
	newPayload := bytes.Repeat([]byte("new-pdf\n"), 1024*1024)
	done := make(chan error, 1)
	go func() {
		_, putErr := store.Put(context.Background(), domain.JobID("job"), "article.pdf", newPayload, testNow())
		done <- putErr
	}()

	path := artifactPath(store, artifact)
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("replacement Put() error = %v", err)
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(got, newPayload) {
				t.Fatalf("committed payload length = %d, want %d", len(got), len(newPayload))
			}
			return
		default:
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(got, oldPayload) && !bytes.Equal(got, newPayload) {
				t.Fatalf("observed partial payload length = %d", len(got))
			}
			if time.Now().After(deadline) {
				t.Fatal("replacement Put() did not finish")
			}
			runtime.Gosched()
		}
	}
}

func TestErrorsDoNotExposeRootPath(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Open(context.Background(), domain.Artifact{
		Key:       "../outside.pdf",
		Filename:  "outside.pdf",
		ByteSize:  1,
		Checksum:  strings.Repeat("0", 64),
		Available: true,
	})
	if err == nil || strings.Contains(err.Error(), store.root) {
		t.Fatalf("error = %v, root path leaked or error missing", err)
	}
}

type cancelAfterChecksContext struct {
	context.Context
	cancelAt int
	checks   int
}

func (c *cancelAfterChecksContext) Err() error {
	c.checks++
	if c.checks >= c.cancelAt {
		return context.Canceled
	}
	return nil
}
