package postgres

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverMigrationsOrdersNumericVersions(t *testing.T) {
	directory := t.TempDir()
	files := map[string]string{
		"0002_second.up.sql":   "CREATE TABLE second (id integer);",
		"0002_second.down.sql": "DROP TABLE second;",
		"0001_first.up.sql":    "-- first\nBEGIN;\nCREATE TABLE first (id integer);\nCOMMIT;\n",
		"README.md":            "not a migration",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	migrations, err := DiscoverMigrations(directory)
	if err != nil {
		t.Fatalf("DiscoverMigrations() error = %v", err)
	}
	if len(migrations) != 2 {
		t.Fatalf("migration count = %d, want 2", len(migrations))
	}
	if migrations[0].Version != 1 || migrations[0].Name != "first" {
		t.Fatalf("first migration = %#v", migrations[0])
	}
	if migrations[1].Version != 2 || migrations[1].Name != "second" {
		t.Fatalf("second migration = %#v", migrations[1])
	}
	if migrations[0].UpSQL == "" || migrations[1].UpSQL == "" {
		t.Fatalf("up SQL was not loaded: %#v", migrations)
	}
}

func TestDiscoverMigrationsRejectsDuplicateVersion(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"0001_first.up.sql", "0001_other.up.sql"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("SELECT 1;"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, err := DiscoverMigrations(directory)
	if err == nil {
		t.Fatal("DiscoverMigrations() error = nil, want duplicate-version error")
	}
	if got := err.Error(); got != ErrMigrationDirectory.Error() {
		t.Fatalf("duplicate error = %q, want safe directory error", got)
	}
	if !errors.Is(err, ErrMigrationDirectory) {
		t.Fatalf("duplicate error does not match ErrMigrationDirectory")
	}
}

func TestStripMigrationTransactionWrapper(t *testing.T) {
	wrapped := "-- migration comment\nBEGIN;\n\nSET LOCAL TIME ZONE 'UTC';\nCREATE TABLE example (id integer);\nCOMMIT;\n"
	got := stripMigrationTransactionWrapper(wrapped)
	if strings.Contains(strings.ToUpper(got), "BEGIN") || strings.Contains(strings.ToUpper(got), "COMMIT") {
		t.Fatalf("wrapper was not removed: %q", got)
	}
	if !strings.Contains(got, "CREATE TABLE example") {
		t.Fatalf("migration body was removed: %q", got)
	}
	plain := "CREATE TABLE plain (id integer);"
	if got := stripMigrationTransactionWrapper(plain); got != plain {
		t.Fatalf("plain migration changed from %q to %q", plain, got)
	}
}

func TestAtticMigrationWrapperIsStripped(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "migrations", "0001_attic_v1.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	got := stripMigrationTransactionWrapper(string(contents))
	trimmed := strings.TrimSpace(got)
	if strings.HasPrefix(strings.ToUpper(trimmed), "BEGIN") || strings.HasSuffix(strings.ToUpper(trimmed), "COMMIT;") {
		t.Fatalf("Attic migration transaction wrapper remains: prefix/suffix of %q", trimmed[:min(len(trimmed), 80)])
	}
}

func TestAtticMigrationPersistsJobVersionAndSchedule(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "migrations", "0001_attic_v1.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(contents)
	if !strings.Contains(sqlText, "version             bigint NOT NULL DEFAULT 1") {
		t.Fatal("jobs.version is not durable in the initial migration")
	}
	if !strings.Contains(sqlText, "next_attempt_at     timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP") {
		t.Fatal("jobs.next_attempt_at is not durable in the initial migration")
	}
	if !strings.Contains(sqlText, "jobs_version_positive") {
		t.Fatal("jobs.version positive constraint is missing")
	}
}

func TestMigrationErrorDoesNotExposeDatabaseCause(t *testing.T) {
	err := migrationFailure(7, "unsafe_name", &databaseFailure{operation: "execute migration"})
	if !strings.Contains(err.Error(), "migration 7 (unsafe_name) failed") {
		t.Fatalf("migration error = %q", err)
	}
	if strings.Contains(err.Error(), "execute migration") {
		t.Fatalf("migration error leaked operation details: %q", err)
	}
	if !errors.Is(err, ErrMigrationFailed) {
		t.Fatalf("migration error does not match ErrMigrationFailed")
	}
}

func TestValidateMigrationHistoryRejectsVersionNewerThanLocalMaximum(t *testing.T) {
	local := []Migration{
		{Version: 1, Name: "first"},
		{Version: 2, Name: "second"},
	}

	err := validateMigrationHistory(local, []recordedMigration{{Version: 3, Name: "future"}})
	if err == nil {
		t.Fatal("validateMigrationHistory() error = nil, want newer-version error")
	}
	if !errors.Is(err, ErrMigrationVersionMismatch) {
		t.Fatalf("validateMigrationHistory() error = %v, want ErrMigrationVersionMismatch", err)
	}
	var migrationErr *MigrationError
	if !errors.As(err, &migrationErr) || migrationErr.Version != 3 {
		t.Fatalf("validateMigrationHistory() error = %#v, want version 3 migration error", err)
	}
	if strings.Contains(err.Error(), "future") {
		t.Fatalf("future database name leaked into migration error: %q", err)
	}
}

func TestValidateMigrationHistoryRejectsRecordedVersionMissingLocally(t *testing.T) {
	local := []Migration{
		{Version: 1, Name: "first"},
		{Version: 3, Name: "third"},
	}

	err := validateMigrationHistory(local, []recordedMigration{{Version: 2, Name: "removed"}})
	if err == nil {
		t.Fatal("validateMigrationHistory() error = nil, want missing-version error")
	}
	if !errors.Is(err, ErrMigrationVersionMismatch) {
		t.Fatalf("validateMigrationHistory() error = %v, want ErrMigrationVersionMismatch", err)
	}
	var migrationErr *MigrationError
	if !errors.As(err, &migrationErr) || migrationErr.Version != 2 {
		t.Fatalf("validateMigrationHistory() error = %#v, want version 2 migration error", err)
	}
}

func TestValidateMigrationHistoryAllowsPendingLocalMigrations(t *testing.T) {
	local := []Migration{
		{Version: 1, Name: "first"},
		{Version: 2, Name: "second"},
	}

	if err := validateMigrationHistory(local, []recordedMigration{{Version: 1, Name: "first"}}); err != nil {
		t.Fatalf("validateMigrationHistory() error = %v, want nil for pending local migration", err)
	}
}
