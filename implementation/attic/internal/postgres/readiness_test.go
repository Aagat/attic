package postgres

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactRootReadinessRequiresWritableDirectory(t *testing.T) {
	root := t.TempDir()
	if err := checkArtifactRootWritable(context.Background(), root); err != nil {
		t.Fatalf("writable root check = %v", err)
	}
	missing := filepath.Join(root, "missing")
	if err := checkArtifactRootWritable(context.Background(), missing); err == nil {
		t.Fatal("missing root unexpectedly passed readiness")
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkArtifactRootWritable(context.Background(), file); err == nil {
		t.Fatal("file root unexpectedly passed readiness")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := checkArtifactRootWritable(canceled, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled root check = %v, want context.Canceled", err)
	}
}
