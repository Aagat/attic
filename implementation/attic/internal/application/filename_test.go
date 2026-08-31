package application

import (
	"strings"
	"testing"

	"attic/internal/domain"
)

func TestSafeFilenameNeverContainsParentTraversal(t *testing.T) {
	for _, title := range []string{"Part..One", "...", "Article....Draft"} {
		filename := safeFilename(title, domain.JobID("job-1"))
		if strings.Contains(filename, "..") {
			t.Fatalf("safeFilename(%q) = %q", title, filename)
		}
	}
	if filename := safeFilename("", domain.JobID("job..1")); strings.Contains(filename, "..") {
		t.Fatalf("fallback safeFilename contains parent traversal: %q", filename)
	}
}
