package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// MigrationAdvisoryLock is shared by every Attic process that applies the
// schema.  It is an application-level lock, so it does not depend on an
// extension or on a database object that the migrations themselves create.
const MigrationAdvisoryLock int64 = 874611204

var (
	ErrMigrationVersionMismatch = errors.New("migration version mismatch")
	ErrMigrationDirectory       = errors.New("migration directory is invalid")
)

// Migration describes one ordered up migration. SQL is loaded during
// discovery so a later file change cannot alter a run that has already
// started.
type Migration struct {
	Version  int64
	Name     string
	Filename string
	UpSQL    string
}

// DiscoverMigrations reads numeric *_name.up.sql files in directory and
// returns them in execution order. Files that are not up migrations are
// ignored, which permits README files and matching down migrations to live
// beside the schema files.
func DiscoverMigrations(directory string) ([]Migration, error) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return nil, ErrMigrationDirectory
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, &migrationDiscoveryError{cause: err}
	}
	upPattern := regexp.MustCompile(`^([0-9]+)_(.+)\.up\.sql$`)
	migrations := make([]Migration, 0)
	seenVersions := make(map[int64]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := upPattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}
		version, parseErr := strconv.ParseInt(matches[1], 10, 64)
		if parseErr != nil || version <= 0 {
			return nil, &migrationDiscoveryError{cause: fmt.Errorf("invalid migration version")}
		}
		if previous, exists := seenVersions[version]; exists {
			return nil, &migrationDiscoveryError{cause: fmt.Errorf("duplicate migration version %d in %s and %s", version, previous, entry.Name())}
		}
		seenVersions[version] = entry.Name()
		upPath := filepath.Join(directory, entry.Name())
		upSQL, readErr := os.ReadFile(upPath)
		if readErr != nil {
			return nil, &migrationDiscoveryError{cause: readErr}
		}
		migration := Migration{
			Version:  version,
			Name:     matches[2],
			Filename: entry.Name(),
			UpSQL:    string(upSQL),
		}
		migrations = append(migrations, migration)
	}
	sort.Slice(migrations, func(i, j int) bool {
		if migrations[i].Version != migrations[j].Version {
			return migrations[i].Version < migrations[j].Version
		}
		return migrations[i].Filename < migrations[j].Filename
	})
	return migrations, nil
}

type migrationDiscoveryError struct {
	cause error
}

func (e *migrationDiscoveryError) Error() string {
	return ErrMigrationDirectory.Error()
}

func (e *migrationDiscoveryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *migrationDiscoveryError) Is(target error) bool {
	return target == ErrMigrationDirectory
}

// MigrationRunner applies discovered migrations one transaction at a time.
// The advisory lock is acquired in each transaction, including the
// schema_migrations creation transaction, so concurrent startup is safe.
type MigrationRunner struct {
	db        *sql.DB
	directory string
	lockKey   int64
}

func NewMigrationRunner(db *sql.DB, directory string) (*MigrationRunner, error) {
	if db == nil {
		return nil, errors.New("postgres database is required")
	}
	if strings.TrimSpace(directory) == "" {
		return nil, ErrMigrationDirectory
	}
	return &MigrationRunner{db: db, directory: directory, lockKey: MigrationAdvisoryLock}, nil
}

// NewMigrationRunnerForStore keeps the raw database handle inside the
// PostgreSQL adapter seam while allowing composition code to wire the store
// and runner together.
func NewMigrationRunnerForStore(store *Store, directory string) (*MigrationRunner, error) {
	if store == nil || store.db == nil {
		return nil, errors.New("postgres store is required")
	}
	return NewMigrationRunner(store.db, directory)
}

// Migrations returns the immutable migration set currently present in the
// configured directory. It is useful to report the planned schema during
// startup and makes discovery independently testable.
func (r *MigrationRunner) Migrations() ([]Migration, error) {
	if r == nil {
		return nil, ErrMigrationDirectory
	}
	return DiscoverMigrations(r.directory)
}

func (r *MigrationRunner) Run(ctx context.Context) error {
	if r == nil || r.db == nil {
		return &databaseFailure{operation: "migration"}
	}
	ctx = nonNilContext(ctx)
	migrations, err := r.Migrations()
	if err != nil {
		return err
	}
	if err := r.ensureSchemaMigrations(ctx); err != nil {
		return err
	}
	if len(migrations) == 0 {
		// There is no applyOne transaction in which to inspect the history.
		// Still reject a database that has recorded migrations which this
		// binary cannot account for.
		return r.validateAppliedMigrations(ctx, migrations)
	}
	for _, migration := range migrations {
		if err := r.applyOne(ctx, migration, migrations); err != nil {
			return err
		}
	}
	return nil
}

func (r *MigrationRunner) ensureSchemaMigrations(ctx context.Context) error {
	tx, err := r.beginLocked(ctx)
	if err != nil {
		return mapDBError("begin migration", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version bigint PRIMARY KEY,
			name text NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return mapDBError("create migration table", err)
	}
	if err := tx.Commit(); err != nil {
		return mapDBError("commit migration table", err)
	}
	return nil
}

type recordedMigration struct {
	Version int64
	Name    string
}

// validateMigrationHistory rejects state that this binary cannot safely
// reason about. In particular, an old binary must not report success after a
// newer binary has recorded a migration it does not contain, and a deployment
// with a missing local migration must not silently skip the recorded version.
func validateMigrationHistory(local []Migration, applied []recordedMigration) error {
	localByVersion := make(map[int64]Migration, len(local))
	highestLocalVersion := int64(0)
	for _, migration := range local {
		localByVersion[migration.Version] = migration
		if migration.Version > highestLocalVersion {
			highestLocalVersion = migration.Version
		}
	}

	for _, recorded := range applied {
		if recorded.Version > highestLocalVersion {
			return migrationFailure(recorded.Version, "", ErrMigrationVersionMismatch)
		}
		migration, ok := localByVersion[recorded.Version]
		if !ok {
			return migrationFailure(recorded.Version, "", ErrMigrationVersionMismatch)
		}
		if migration.Name != recorded.Name {
			return migrationFailure(migration.Version, migration.Name, ErrMigrationVersionMismatch)
		}
	}
	return nil
}

func readMigrationHistory(ctx context.Context, tx *sql.Tx) ([]recordedMigration, error) {
	rows, err := tx.QueryContext(ctx, `SELECT version, name FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, mapDBError("read migration state", err)
	}
	defer func() { _ = rows.Close() }()

	applied := make([]recordedMigration, 0)
	for rows.Next() {
		var migration recordedMigration
		if err := rows.Scan(&migration.Version, &migration.Name); err != nil {
			return nil, mapDBError("scan migration state", err)
		}
		applied = append(applied, migration)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBError("iterate migration state", err)
	}
	return applied, nil
}

func (r *MigrationRunner) validateAppliedMigrations(ctx context.Context, migrations []Migration) error {
	tx, err := r.beginLocked(ctx)
	if err != nil {
		return mapDBError("begin migration validation", err)
	}
	defer func() { _ = tx.Rollback() }()

	applied, err := readMigrationHistory(ctx, tx)
	if err != nil {
		return err
	}
	if err := validateMigrationHistory(migrations, applied); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return mapDBError("commit migration validation", err)
	}
	return nil
}

func (r *MigrationRunner) applyOne(ctx context.Context, migration Migration, allMigrations []Migration) error {
	tx, err := r.beginLocked(ctx)
	if err != nil {
		return migrationFailure(migration.Version, migration.Name, mapDBError("begin migration", err))
	}
	defer func() { _ = tx.Rollback() }()

	applied, err := readMigrationHistory(ctx, tx)
	if err != nil {
		return migrationFailure(migration.Version, migration.Name, err)
	}
	if err := validateMigrationHistory(allMigrations, applied); err != nil {
		return err
	}
	appliedByVersion := make(map[int64]struct{}, len(applied))
	for _, recorded := range applied {
		appliedByVersion[recorded.Version] = struct{}{}
	}
	if _, alreadyApplied := appliedByVersion[migration.Version]; alreadyApplied {
		if err := tx.Commit(); err != nil {
			return migrationFailure(migration.Version, migration.Name, mapDBError("commit migration check", err))
		}
		return nil
	}
	sqlText := stripMigrationTransactionWrapper(migration.UpSQL)
	if strings.TrimSpace(sqlText) == "" {
		return migrationFailure(migration.Version, migration.Name, errors.New("empty migration"))
	}
	if _, err := tx.ExecContext(ctx, sqlText); err != nil {
		return migrationFailure(migration.Version, migration.Name, mapDBError("execute migration", err))
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, migration.Version, migration.Name); err != nil {
		return migrationFailure(migration.Version, migration.Name, mapDBError("record migration", err))
	}
	if err := tx.Commit(); err != nil {
		return migrationFailure(migration.Version, migration.Name, mapDBError("commit migration", err))
	}
	return nil
}

func (r *MigrationRunner) beginLocked(ctx context.Context) (*sql.Tx, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, r.lockKey); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

var (
	leadingBegin   = regexp.MustCompile(`(?is)^(?:\s|--[^\n]*(?:\n|$)|/\*.*?\*/)*BEGIN\s*;\s*`)
	trailingCommit = regexp.MustCompile(`(?is);\s*(?:--[^\n]*(?:\n|$)|/\*.*?\*/\s*)*COMMIT\s*;\s*$`)
)

func stripMigrationTransactionWrapper(sqlText string) string {
	sqlText = leadingBegin.ReplaceAllString(sqlText, "")
	sqlText = trailingCommit.ReplaceAllString(sqlText, "")
	return strings.TrimSpace(sqlText)
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
