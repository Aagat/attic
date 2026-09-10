package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"attic/internal/application"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrInvalidStage    = application.ErrInvalidStage
	ErrMigrationFailed = errors.New("migration failed")
)

// databaseFailure deliberately does not retain the database error.  The
// application can log the operation at an internal boundary, while callers
// receive no SQL, table names, parameters, or provider details.
type databaseFailure struct {
	operation string
}

func (e *databaseFailure) Error() string {
	if e == nil || e.operation == "" {
		return "postgres operation failed"
	}
	return "postgres " + e.operation + " failed"
}

func mapDBError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr != nil {
		switch pgErr.Code {
		case "23505": // unique_violation
			if pgErr.ConstraintName == "idempotency_records_pkey" {
				return application.ErrIdempotencyConflict
			}
		case "23503": // foreign_key_violation
			return application.ErrNotFound
		}
	}
	return &databaseFailure{operation: operation}
}

func migrationFailure(version int64, name string, err error) error {
	return &MigrationError{Version: version, Name: name, cause: err}
}

type MigrationError struct {
	Version int64
	Name    string
	cause   error
}

func (e *MigrationError) Error() string {
	if e == nil {
		return "migration failed"
	}
	if e.Name == "" {
		return fmt.Sprintf("migration %d failed", e.Version)
	}
	return fmt.Sprintf("migration %d (%s) failed", e.Version, e.Name)
}

func (e *MigrationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *MigrationError) Is(target error) bool {
	if e != nil && target == ErrMigrationFailed {
		return true
	}
	if e == nil {
		return false
	}
	return errors.Is(e.cause, target)
}
