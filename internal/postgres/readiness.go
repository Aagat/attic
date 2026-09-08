package postgres

import (
	"context"
	"errors"
	"os"
	"strings"

	"attic/internal/application"
)

var ErrSchemaNotReady = errors.New("database schema is not ready")

// Readiness composes the database, migration, and artifact-volume checks used
// by the HTTP health endpoint. Liveness deliberately remains independent of
// these external resources.
type Readiness struct {
	store        *Store
	migrations   *MigrationRunner
	artifactRoot string
}

func NewReadiness(store *Store, migrations *MigrationRunner, artifactRoot string) (*Readiness, error) {
	if store == nil || store.db == nil {
		return nil, errors.New("postgres store is required")
	}
	if migrations == nil || migrations.db == nil {
		return nil, errors.New("migration runner is required")
	}
	artifactRoot = strings.TrimSpace(artifactRoot)
	if artifactRoot == "" {
		return nil, errors.New("artifact root is required")
	}
	return &Readiness{store: store, migrations: migrations, artifactRoot: artifactRoot}, nil
}

var _ application.Readiness = (*Readiness)(nil)

func (r *Readiness) Live(ctx context.Context) error {
	return contextErr(ctx)
}

func (r *Readiness) Ready(ctx context.Context) error {
	if r == nil || r.store == nil || r.migrations == nil {
		return errors.New("readiness is not configured")
	}
	ctx = nonNilContext(ctx)
	if err := r.store.Ping(ctx); err != nil {
		return err
	}
	if err := r.migrations.Ready(ctx); err != nil {
		return err
	}
	return checkArtifactRootWritable(ctx, r.artifactRoot)
}

func (r *MigrationRunner) Ready(ctx context.Context) error {
	if r == nil || r.db == nil {
		return ErrSchemaNotReady
	}
	ctx = nonNilContext(ctx)
	migrations, err := r.Migrations()
	if err != nil {
		return ErrSchemaNotReady
	}
	if len(migrations) == 0 {
		return ErrSchemaNotReady
	}
	rows, err := r.db.QueryContext(ctx, `SELECT version, name FROM schema_migrations`)
	if err != nil {
		return mapDBError("check migration state", err)
	}
	defer rows.Close()
	applied := make(map[int64]string, len(migrations))
	for rows.Next() {
		var version int64
		var name string
		if err := rows.Scan(&version, &name); err != nil {
			return mapDBError("scan migration state", err)
		}
		applied[version] = name
	}
	if err := rows.Err(); err != nil {
		return mapDBError("iterate migration state", err)
	}
	for _, migration := range migrations {
		if name, ok := applied[migration.Version]; !ok || name != migration.Name {
			return ErrSchemaNotReady
		}
	}
	return nil
}

func checkArtifactRootWritable(ctx context.Context, root string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return readinessFailure("artifact storage")
	}
	temporary, err := os.CreateTemp(root, ".attic-readiness-*")
	if err != nil {
		return readinessFailure("artifact storage")
	}
	name := temporary.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(name)
		}
	}()
	if err := temporary.Close(); err != nil {
		return readinessFailure("artifact storage")
	}
	if err := os.Remove(name); err != nil {
		return readinessFailure("artifact storage")
	}
	remove = false
	return nil
}

type readinessFailure string

func (e readinessFailure) Error() string {
	if e == "" {
		return "readiness check failed"
	}
	return string(e) + " readiness check failed"
}
