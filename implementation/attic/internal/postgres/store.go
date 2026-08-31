// Package postgres contains the PostgreSQL adapters for Attic's durable
// application seams.  SQL and transaction details remain private to this
// package; callers receive application sentinels or safe adapter errors.
package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"attic/internal/application"
	"attic/internal/domain"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	defaultScope        = "owner"
	defaultLease        = 5 * time.Minute
	idempotencyLifetime = 24 * time.Hour
)

type Options struct {
	Now          func() time.Time
	Scope        string
	MaxOpenConns int
	MaxIdleConns int
}

type Store struct {
	db    *sql.DB
	now   func() time.Time
	scope string
}

func Open(ctx context.Context, databaseURL string, options Options) (*Store, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, &databaseFailure{operation: "open"}
	}
	store, err := NewStore(db, options)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if ctx != nil {
		if err := store.Ping(ctx); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return store, nil
}

func NewStore(db *sql.DB, options Options) (*Store, error) {
	if db == nil {
		return nil, errors.New("postgres database is required")
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	scope := strings.TrimSpace(options.Scope)
	if scope == "" {
		scope = defaultScope
	}
	if options.MaxOpenConns > 0 {
		db.SetMaxOpenConns(options.MaxOpenConns)
	}
	if options.MaxIdleConns >= 0 {
		db.SetMaxIdleConns(options.MaxIdleConns)
	}
	return &Store{db: db, now: now, scope: scope}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return mapDBError("close", s.db.Close())
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return &databaseFailure{operation: "ping"}
	}
	ctx = nonNilContext(ctx)
	if err := s.db.PingContext(ctx); err != nil {
		return mapDBError("ping", err)
	}
	return nil
}

var _ application.JobStore = (*Store)(nil)
var _ application.AIAttemptRecorder = (*Store)(nil)

// RecordAIAttempts stores only the bounded operational metadata allowed by
// application.AIAttempt. Locking the owning job makes per-purpose numbering
// deterministic across durable job retries and concurrent workers.
func (s *Store) RecordAIAttempts(ctx context.Context, jobID domain.JobID, attempts []application.AIAttempt) ([]string, error) {
	ctx = nonNilContext(ctx)
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(jobID)) == "" || len(attempts) == 0 {
		return nil, &databaseFailure{operation: "record AI attempts"}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, mapDBError("begin AI attempts", err)
	}
	defer func() { _ = tx.Rollback() }()
	var exists string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM jobs WHERE id = $1 FOR UPDATE`, string(jobID)).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, application.ErrLeaseLost
		}
		return nil, mapDBError("lock AI attempt job", err)
	}
	next := make(map[string]int)
	ids := make([]string, 0, len(attempts))
	for _, attempt := range attempts {
		purpose := strings.TrimSpace(attempt.Purpose)
		if purpose != "analysis" && purpose != "repair" {
			return nil, &databaseFailure{operation: "record AI attempts"}
		}
		if next[purpose] == 0 {
			var number int
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt_number), 0) + 1 FROM ai_attempts WHERE job_id = $1 AND purpose = $2`, string(jobID), purpose).Scan(&number); err != nil {
				return nil, mapDBError("number AI attempt", err)
			}
			next[purpose] = number
		}
		id := randomToken()
		createdAt := attempt.CreatedAt.UTC()
		if createdAt.IsZero() {
			createdAt = s.now().UTC()
		}
		latencyMS := attempt.Latency.Milliseconds()
		if latencyMS < 0 {
			latencyMS = 0
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO ai_attempts (
				id, job_id, purpose, attempt_number, model_identifier,
				prompt_version, latency_ms, provider_request_id, input_tokens,
				output_tokens, result_status, error_category, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			id, string(jobID), purpose, next[purpose], attempt.Model, attempt.PromptVersion,
			latencyMS, nullableString(attempt.ProviderRequestID), nullableTokenCount(attempt.InputTokens, attempt.UsageReported),
			nullableTokenCount(attempt.OutputTokens, attempt.UsageReported), attempt.Status, nullableString(attempt.ErrorCategory), createdAt)
		if err != nil {
			return nil, mapDBError("record AI attempt", err)
		}
		next[purpose]++
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		return nil, mapDBError("commit AI attempts", err)
	}
	return ids, nil
}

func nullableTokenCount(value int, reported bool) any {
	if !reported || value < 0 {
		return nil
	}
	return value
}

func (s *Store) CreateOrReuse(ctx context.Context, command application.CreateJob) (application.CreateOutcome, error) {
	ctx = nonNilContext(ctx)
	if err := contextErr(ctx); err != nil {
		return application.CreateOutcome{}, err
	}
	if strings.TrimSpace(string(command.ID)) == "" {
		return application.CreateOutcome{}, &databaseFailure{operation: "create"}
	}
	createdAt := command.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = s.now().UTC()
	}
	currentTime := s.now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return application.CreateOutcome{}, mapDBError("begin create", err)
	}
	defer func() { _ = tx.Rollback() }()

	if command.IdempotencyKey != "" {
		var existingJobID, existingHash string
		var expiresAt time.Time
		err = tx.QueryRowContext(ctx, `
			SELECT job_id, request_hash, expires_at
			FROM idempotency_records
			WHERE scope = $1 AND idempotency_key = $2
			FOR UPDATE`, s.scope, string(command.IdempotencyKey)).Scan(&existingJobID, &existingHash, &expiresAt)
		switch {
		case err == nil && expiresAt.After(currentTime):
			if existingHash != command.RequestDigest {
				return application.CreateOutcome{Conflict: true}, application.ErrIdempotencyConflict
			}
			job, loadErr := loadJob(ctx, tx, existingJobID)
			if loadErr != nil {
				return application.CreateOutcome{}, loadErr
			}
			if err := tx.Commit(); err != nil {
				return application.CreateOutcome{}, mapDBError("commit idempotent replay", err)
			}
			return application.CreateOutcome{Job: job, Replayed: true}, nil
		case err == nil:
			if _, deleteErr := tx.ExecContext(ctx, `DELETE FROM idempotency_records WHERE scope = $1 AND idempotency_key = $2`, s.scope, string(command.IdempotencyKey)); deleteErr != nil {
				return application.CreateOutcome{}, mapDBError("expire idempotency record", deleteErr)
			}
		case errors.Is(err, sql.ErrNoRows):
		default:
			return application.CreateOutcome{}, mapDBError("read idempotency record", err)
		}
	}

	correlationID := randomToken()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO jobs (
			id, source_kind, source_payload, submitted_url, title_hint,
			output_kind, output_profile, status, stage, correlation_id,
			version, next_attempt_at, created_at, updated_at
		) VALUES ($1, 'url', '{}'::jsonb, $2, $3, 'pdf', $4, 'queued', NULL, $5, 1, $6, $6, $6)`,
		string(command.ID), command.Request.URL, nullableString(command.Request.Title), command.Request.Profile, correlationID, createdAt)
	if err != nil {
		return application.CreateOutcome{}, mapDBError("create job", err)
	}

	if command.IdempotencyKey != "" {
		result, insertErr := tx.ExecContext(ctx, `
			INSERT INTO idempotency_records (
				scope, idempotency_key, request_hash, job_id, created_at, expires_at
			) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (scope, idempotency_key) DO NOTHING`,
			s.scope, string(command.IdempotencyKey), command.RequestDigest, string(command.ID), createdAt, createdAt.Add(idempotencyLifetime))
		if insertErr != nil {
			return application.CreateOutcome{}, mapDBError("create idempotency record", insertErr)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return application.CreateOutcome{}, mapDBError("inspect idempotency record", rowsErr)
		}
		if rows == 0 {
			if _, deleteErr := tx.ExecContext(ctx, `DELETE FROM jobs WHERE id = $1`, string(command.ID)); deleteErr != nil {
				return application.CreateOutcome{}, mapDBError("discard duplicate job", deleteErr)
			}
			var existingJobID, existingHash string
			var expiresAt time.Time
			if readErr := tx.QueryRowContext(ctx, `
				SELECT job_id, request_hash, expires_at
				FROM idempotency_records
				WHERE scope = $1 AND idempotency_key = $2`, s.scope, string(command.IdempotencyKey)).Scan(&existingJobID, &existingHash, &expiresAt); readErr != nil {
				return application.CreateOutcome{}, mapDBError("read concurrent idempotency record", readErr)
			}
			if !expiresAt.After(currentTime) {
				return application.CreateOutcome{}, &databaseFailure{operation: "create idempotency record"}
			}
			if existingHash != command.RequestDigest {
				return application.CreateOutcome{Conflict: true}, application.ErrIdempotencyConflict
			}
			job, loadErr := loadJob(ctx, tx, existingJobID)
			if loadErr != nil {
				return application.CreateOutcome{}, loadErr
			}
			if err := tx.Commit(); err != nil {
				return application.CreateOutcome{}, mapDBError("commit concurrent replay", err)
			}
			return application.CreateOutcome{Job: job, Replayed: true}, nil
		}
	}

	job, err := loadJob(ctx, tx, string(command.ID))
	if err != nil {
		return application.CreateOutcome{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.CreateOutcome{}, mapDBError("commit create", err)
	}
	return application.CreateOutcome{Job: job}, nil
}

func (s *Store) List(ctx context.Context, request application.StoreListRequest) (application.StorePage, error) {
	ctx = nonNilContext(ctx)
	if err := contextErr(ctx); err != nil {
		return application.StorePage{}, err
	}
	limit := request.Limit
	if limit <= 0 {
		limit = 25
	}
	conditions := []string{"1 = 1"}
	args := make([]any, 0, 4)
	arg := 1
	if request.Status != nil {
		conditions = append(conditions, fmt.Sprintf("j.status = $%d", arg))
		args = append(args, string(*request.Status))
		arg++
	}
	if request.After != nil {
		conditions = append(conditions, fmt.Sprintf("(j.created_at, j.id) < ($%d, $%d)", arg, arg+1))
		args = append(args, request.After.CreatedAt.UTC(), string(request.After.ID))
		arg += 2
	}
	args = append(args, limit+1)
	query := fmt.Sprintf(`
		SELECT
			j.id, j.retry_of_job_id, j.submitted_url, j.canonical_url,
			j.title_hint, j.output_profile, j.status, j.stage, j.attempt_count,
			j.version, j.next_attempt_at, j.failure_category, j.correlation_id,
			j.created_at, j.completed_at,
			c.id, c.title, c.author, c.site_name, c.publication_date,
			c.description, c.detected_language, c.ai_confidence, c.ai_completeness,
			a.id, a.safe_filename, a.media_type, a.byte_size,
			a.checksum_sha256, a.availability, a.created_at
		FROM jobs j
		LEFT JOIN content_documents c ON c.job_id = j.id
		LEFT JOIN artifacts a ON a.job_id = j.id AND a.profile = j.output_profile AND a.output_kind = 'pdf'
		WHERE %s
		ORDER BY j.created_at DESC, j.id DESC
		LIMIT $%d`, strings.Join(conditions, " AND "), arg)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return application.StorePage{}, mapDBError("list jobs", err)
	}
	defer rows.Close()
	jobs := make([]domain.Job, 0, limit+1)
	for rows.Next() {
		job, scanErr := scanListJob(rows)
		if scanErr != nil {
			return application.StorePage{}, mapDBError("scan jobs", scanErr)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return application.StorePage{}, mapDBError("iterate jobs", err)
	}
	hasMore := len(jobs) > limit
	if hasMore {
		jobs = jobs[:limit]
	}
	return application.StorePage{Jobs: jobs, HasMore: hasMore}, nil
}

func (s *Store) Get(ctx context.Context, id domain.JobID) (domain.Job, error) {
	ctx = nonNilContext(ctx)
	if err := contextErr(ctx); err != nil {
		return domain.Job{}, err
	}
	job, err := loadJob(ctx, s.db, string(id))
	if err != nil {
		return domain.Job{}, err
	}
	return job, nil
}

func (s *Store) CreateRetry(ctx context.Context, sourceID, newID domain.JobID, now time.Time) (domain.Job, error) {
	ctx = nonNilContext(ctx)
	if err := contextErr(ctx); err != nil {
		return domain.Job{}, err
	}
	if now.IsZero() {
		now = s.now()
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Job{}, mapDBError("begin retry", err)
	}
	defer func() { _ = tx.Rollback() }()
	var submittedURL, titleHint, profile, status sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT submitted_url, title_hint, output_profile, status
		FROM jobs WHERE id = $1 FOR UPDATE`, string(sourceID)).Scan(&submittedURL, &titleHint, &profile, &status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Job{}, application.ErrNotFound
		}
		return domain.Job{}, mapDBError("read retry source", err)
	}
	if !domain.Status(status.String).Terminal() {
		return domain.Job{}, application.ErrNotTerminal
	}
	correlationID := randomToken()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO jobs (
			id, retry_of_job_id, source_kind, source_payload, submitted_url,
			title_hint, output_kind, output_profile, status, stage,
			correlation_id, version, next_attempt_at, created_at, updated_at
		) VALUES ($1, $2, 'url', '{}'::jsonb, $3, $4, 'pdf', $5, 'queued', NULL, $6, 1, $7, $7, $7)`,
		string(newID), string(sourceID), submittedURL.String, nullableString(titleHint.String), profile.String, correlationID, now); err != nil {
		return domain.Job{}, mapDBError("create retry", err)
	}
	job, err := loadJob(ctx, tx, string(newID))
	if err != nil {
		return domain.Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Job{}, mapDBError("commit retry", err)
	}
	return job, nil
}

func (s *Store) RequestDelete(ctx context.Context, id domain.JobID, now time.Time) error {
	ctx = nonNilContext(ctx)
	if err := contextErr(ctx); err != nil {
		return err
	}
	if now.IsZero() {
		now = s.now()
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return mapDBError("begin deletion", err)
	}
	defer func() { _ = tx.Rollback() }()
	var targetID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM jobs WHERE id = $1 FOR UPDATE`, string(id)).Scan(&targetID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrNotFound
		}
		return mapDBError("read deletion target", err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, storage_relative_path
		FROM artifacts WHERE job_id = $1
		ORDER BY created_at DESC, id DESC`, string(id))
	if err != nil {
		return mapDBError("read deletion artifacts", err)
	}
	type artifactTarget struct{ id, path string }
	targets := make([]artifactTarget, 0)
	for rows.Next() {
		var target artifactTarget
		if scanErr := rows.Scan(&target.id, &target.path); scanErr != nil {
			_ = rows.Close()
			return mapDBError("scan deletion artifact", scanErr)
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return mapDBError("iterate deletion artifacts", err)
	}
	_ = rows.Close()
	for _, target := range targets {
		var exists bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM deletion_tasks
				WHERE artifact_id = $1 AND status <> 'completed'
			)`, target.id).Scan(&exists); err != nil {
			return mapDBError("check deletion task", err)
		}
		if exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO deletion_tasks (
				id, job_id, artifact_id, storage_relative_path, status,
				next_attempt_at, created_at, updated_at
			) VALUES ($1, $2, $3, $4, 'queued', $5, $5, $5)`, randomToken(), string(id), target.id, target.path, now); err != nil {
			return mapDBError("create deletion task", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'cancelled', stage = NULL, lease_token = NULL,
			lease_expires_at = NULL, completed_at = $2, updated_at = $2
		WHERE id = $1`, string(id), now); err != nil {
		return mapDBError("cancel deletion target", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM jobs WHERE id = $1`, string(id)); err != nil {
		return mapDBError("delete job", err)
	}
	if err := tx.Commit(); err != nil {
		return mapDBError("commit deletion", err)
	}
	return nil
}

func (s *Store) ClaimNext(ctx context.Context, now time.Time, leaseDuration time.Duration) (*application.Lease, error) {
	ctx = nonNilContext(ctx)
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = s.now()
	}
	if leaseDuration <= 0 {
		leaseDuration = defaultLease
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, mapDBError("begin claim", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'delivery_failed', stage = NULL,
			failure_category = 'delivery_timeout',
			failure_message = 'Email delivery could not be confirmed',
			lease_token = NULL, lease_expires_at = NULL,
			completed_at = $1, next_attempt_at = $1, updated_at = $1,
			version = version + 1
		WHERE status = 'delivering'
		  AND lease_expires_at IS NOT NULL
		  AND lease_expires_at <= $1`, now); err != nil {
		return nil, mapDBError("expire delivery leases", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'queued', stage = NULL, lease_token = NULL,
			lease_expires_at = NULL, failure_category = NULL,
			failure_message = NULL, completed_at = NULL,
			next_attempt_at = $1, updated_at = $1,
			version = version + 1
		WHERE status = 'processing'
		  AND lease_expires_at IS NOT NULL
		  AND lease_expires_at <= $1`, now); err != nil {
		return nil, mapDBError("reclaim processing leases", err)
	}
	row := tx.QueryRowContext(ctx, `
		SELECT id, retry_of_job_id, submitted_url, canonical_url, title_hint,
			output_profile, status, stage, attempt_count, version,
			next_attempt_at, failure_category, correlation_id,
			created_at, completed_at
		FROM jobs
		WHERE status = 'queued' AND next_attempt_at <= $1
		ORDER BY created_at ASC, id ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED`, now)
	job, err := scanClaimJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if commitErr := tx.Commit(); commitErr != nil {
				return nil, mapDBError("commit empty claim", commitErr)
			}
			return nil, nil
		}
		return nil, mapDBError("select claim", err)
	}
	token := randomToken()
	expires := now.Add(leaseDuration)
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'processing', stage = 'fetching', attempt_count = attempt_count + 1,
			lease_token = $2, lease_expires_at = $3, started_at = COALESCE(started_at, $4),
			updated_at = $4, version = version + 1
		WHERE id = $1 AND status = 'queued' AND version = $5`, string(job.ID), token, expires, now, job.Version)
	if err != nil {
		return nil, mapDBError("claim job", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, mapDBError("inspect claimed job", err)
	} else if affected != 1 {
		return nil, application.ErrLeaseLost
	}
	job.Status = domain.StatusProcessing
	job.Stage = domain.StageFetching
	job.AttemptCount++
	job.LeaseToken = token
	job.LeaseUntil = expires
	job.Version++
	if err := tx.Commit(); err != nil {
		return nil, mapDBError("commit claim", err)
	}
	return application.NewLease(job, token, expires), nil
}

func (s *Store) RenewLease(ctx context.Context, lease *application.Lease, leaseDuration time.Duration) error {
	ctx = nonNilContext(ctx)
	if err := contextErr(ctx); err != nil {
		return err
	}
	if lease == nil {
		return application.ErrLeaseLost
	}
	if leaseDuration <= 0 {
		leaseDuration = defaultLease
	}
	now := s.now().UTC()
	durationMicros := leaseDuration.Microseconds()
	if durationMicros <= 0 {
		durationMicros = 1
	}
	version := lease.CurrentVersion()
	var renewedExpiry time.Time
	var renewedVersion int64
	err := s.db.QueryRowContext(ctx, `
		UPDATE jobs
		SET lease_expires_at = GREATEST(lease_expires_at, $4) + ($3::double precision * interval '1 microsecond'),
			updated_at = $4, version = version + 1
		WHERE id = $1
		  AND status IN ('processing', 'delivering')
		  AND lease_token = $2
		  AND version = $5
		  AND lease_expires_at > $4
		RETURNING lease_expires_at, version`, string(lease.Job.ID), lease.Token, durationMicros, now, version).Scan(&renewedExpiry, &renewedVersion)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrLeaseLost
		}
		return mapDBError("renew lease", err)
	}
	if renewedVersion <= 0 {
		return application.ErrLeaseLost
	}
	lease.Observe(uint64(renewedVersion), renewedExpiry, lease.Job.Stage)
	return nil
}

func (s *Store) SetStage(ctx context.Context, lease *application.Lease, stage domain.Stage) error {
	ctx = nonNilContext(ctx)
	if err := contextErr(ctx); err != nil {
		return err
	}
	if !stage.Valid() {
		return ErrInvalidStage
	}
	if lease == nil {
		return application.ErrLeaseLost
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return mapDBError("begin stage", err)
	}
	defer func() { _ = tx.Rollback() }()
	var currentStage sql.NullString
	var status, token string
	var leaseExpiry time.Time
	var version int64
	if err := tx.QueryRowContext(ctx, `SELECT status, stage, lease_token, lease_expires_at, version FROM jobs WHERE id = $1 FOR UPDATE`, string(lease.Job.ID)).Scan(&status, &currentStage, &token, &leaseExpiry, &version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrLeaseLost
		}
		return mapDBError("read stage", err)
	}
	now := s.now().UTC()
	if version <= 0 || status != string(domain.StatusProcessing) || token != lease.Token || uint64(version) != lease.CurrentVersion() || !leaseExpiry.After(now) {
		return application.ErrLeaseLost
	}
	current := domain.Stage(currentStage.String)
	if stage != current && nextStage(current) != stage {
		return ErrInvalidStage
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET stage = $2, updated_at = $3, version = version + 1
		WHERE id = $1 AND status = 'processing' AND lease_token = $4
		  AND version = $5 AND lease_expires_at > $3`, string(lease.Job.ID), string(stage), now, lease.Token, version)
	if err != nil {
		return mapDBError("set stage", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return mapDBError("inspect stage", err)
	}
	if affected != 1 {
		return application.ErrLeaseLost
	}
	if err := tx.Commit(); err != nil {
		return mapDBError("commit stage", err)
	}
	lease.Observe(uint64(version)+1, leaseExpiry, stage)
	return nil
}

func (s *Store) Complete(ctx context.Context, lease *application.Lease, completion application.Completion, now time.Time) error {
	ctx = nonNilContext(ctx)
	if err := contextErr(ctx); err != nil {
		return err
	}
	if completion.Content.ID == "" || !completion.Artifact.Available || completion.Artifact.ByteSize <= 0 {
		return &databaseFailure{operation: "complete"}
	}
	if lease == nil {
		return application.ErrLeaseLost
	}
	if now.IsZero() {
		now = s.now()
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return mapDBError("begin complete", err)
	}
	defer func() { _ = tx.Rollback() }()
	var status, currentStage, token, profile string
	var leaseExpiry time.Time
	var version int64
	if err := tx.QueryRowContext(ctx, `SELECT status, stage, lease_token, output_profile, lease_expires_at, version FROM jobs WHERE id = $1 FOR UPDATE`, string(lease.Job.ID)).Scan(&status, &currentStage, &token, &profile, &leaseExpiry, &version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrLeaseLost
		}
		return mapDBError("read completion job", err)
	}
	leaseNow := s.now().UTC()
	if version <= 0 || status != string(domain.StatusProcessing) || token != lease.Token || uint64(version) != lease.CurrentVersion() || !leaseExpiry.After(leaseNow) {
		return application.ErrLeaseLost
	}
	if domain.Stage(currentStage) != domain.StagePersisting {
		return ErrInvalidStage
	}
	publicationDate := nullablePublicationDate(completion.Content.PublicationDate)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO content_documents (
			id, job_id, title, author, site_name, publication_date, description,
			source_url, semantic_html, plain_text, extraction_method,
			ai_confidence, ai_completeness, detected_language, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $15)`,
		string(completion.Content.ID), string(lease.Job.ID), completion.Content.Title,
		nullableString(completion.Content.Author), nullableString(completion.Content.SiteName), publicationDate,
		nullableString(completion.Content.Description), completion.Content.SourceURL,
		completion.Content.SemanticHTML, completion.Content.PlainText,
		completion.Content.ExtractionMethod, completion.Content.AIConfidence, completion.Content.AICompleteness,
		nullableString(completion.Content.Language), now); err != nil {
		return mapDBError("persist content", err)
	}
	artifactID := randomToken()
	mediaType := completion.Artifact.MediaType
	if strings.TrimSpace(mediaType) == "" {
		mediaType = "application/pdf"
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artifacts (
			id, job_id, content_id, output_kind, profile, storage_relative_path,
			safe_filename, media_type, byte_size, checksum_sha256, availability, created_at
		) VALUES ($1, $2, $3, 'pdf', $4, $5, $6, $7, $8, $9, 'available', $10)`,
		artifactID, string(lease.Job.ID), string(completion.Content.ID), profile,
		completion.Artifact.Key, completion.Artifact.Filename, mediaType,
		completion.Artifact.ByteSize, completion.Artifact.Checksum, now); err != nil {
		return mapDBError("persist artifact", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs SET display_title = $2, canonical_url = NULLIF($7, ''), status = 'ready', stage = NULL,
			lease_token = NULL, lease_expires_at = NULL, completed_at = $3,
			updated_at = $3, version = version + 1
		WHERE id = $1 AND status = 'processing' AND lease_token = $4
		  AND version = $5 AND lease_expires_at > $6`, string(lease.Job.ID), completion.Content.Title, now, lease.Token, version, leaseNow, completion.CanonicalURL)
	if err != nil {
		return mapDBError("complete job", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return mapDBError("inspect completed job", err)
		}
		return application.ErrLeaseLost
	}
	if err := tx.Commit(); err != nil {
		return mapDBError("commit complete", err)
	}
	lease.Observe(uint64(version)+1, time.Time{}, "")
	return nil
}

func (s *Store) Requeue(ctx context.Context, lease *application.Lease, failure domain.Failure, next time.Time) error {
	ctx = nonNilContext(ctx)
	if err := contextErr(ctx); err != nil {
		return err
	}
	if lease == nil {
		return application.ErrLeaseLost
	}
	if next.IsZero() {
		next = s.now()
	}
	next = next.UTC()
	currentTime := s.now().UTC()
	result, err := s.db.ExecContext(ctx, `
		UPDATE jobs SET status = 'queued', stage = NULL, lease_token = NULL,
			lease_expires_at = NULL, failure_category = NULL,
			failure_message = NULL, next_attempt_at = $3, updated_at = $3,
			version = version + 1
		WHERE id = $1 AND status IN ('processing', 'delivering') AND lease_token = $2
		  AND version = $4 AND lease_expires_at > $5`, string(lease.Job.ID), lease.Token, next, lease.CurrentVersion(), currentTime)
	if err != nil {
		return mapDBError("requeue job", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return mapDBError("inspect requeue", err)
	}
	if affected != 1 {
		return application.ErrLeaseLost
	}
	lease.Observe(lease.CurrentVersion()+1, time.Time{}, "")
	return nil
}

func (s *Store) Fail(ctx context.Context, lease *application.Lease, failure domain.Failure, now time.Time) error {
	ctx = nonNilContext(ctx)
	if err := contextErr(ctx); err != nil {
		return err
	}
	if lease == nil {
		return application.ErrLeaseLost
	}
	if now.IsZero() {
		now = s.now()
	}
	now = now.UTC()
	currentTime := s.now().UTC()
	category := failure.Category
	if !validFailureCategory(category) {
		category = domain.FailureInternalError
	}
	message := safeFailureMessage(category)
	result, err := s.db.ExecContext(ctx, `
		UPDATE jobs SET status = 'failed', stage = NULL, failure_category = $3,
			failure_message = $4, lease_token = NULL, lease_expires_at = NULL,
			completed_at = $5, next_attempt_at = $5, updated_at = $5,
			version = version + 1
		WHERE id = $1 AND status IN ('processing', 'delivering') AND lease_token = $2
		  AND version = $6 AND lease_expires_at > $7`, string(lease.Job.ID), lease.Token, string(category), message, now, lease.CurrentVersion(), currentTime)
	if err != nil {
		return mapDBError("fail job", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return mapDBError("inspect failed job", err)
	}
	if affected != 1 {
		return application.ErrLeaseLost
	}
	lease.Observe(lease.CurrentVersion()+1, time.Time{}, "")
	return nil
}

const jobSelect = `
	SELECT
		j.id, j.retry_of_job_id, j.submitted_url, j.canonical_url, j.title_hint,
		j.output_profile, j.status, j.stage, j.attempt_count, j.version,
		j.next_attempt_at, j.failure_category, j.failure_message,
		j.correlation_id, j.created_at,
		j.completed_at, j.lease_token, j.lease_expires_at,
		c.id, c.job_id, c.title, c.author, c.site_name, c.publication_date,
		c.description, c.source_url, c.semantic_html, c.plain_text,
		c.extraction_method, c.ai_confidence, c.ai_completeness, c.detected_language, c.created_at,
		c.updated_at,
		a.id, a.storage_relative_path, a.safe_filename, a.media_type,
		a.byte_size, a.checksum_sha256, a.availability, a.created_at,
		d.outcome, d.attempt_number, d.stable_message_id
	FROM jobs j
	LEFT JOIN content_documents c ON c.job_id = j.id
	LEFT JOIN artifacts a ON a.job_id = j.id AND a.profile = j.output_profile AND a.output_kind = 'pdf'
	LEFT JOIN LATERAL (
		SELECT outcome, attempt_number, stable_message_id
		FROM delivery_attempts
		WHERE job_id = j.id
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	) d ON TRUE
	WHERE j.id = $1`

func loadJob(ctx context.Context, source interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (domain.Job, error) {
	return scanJob(source.QueryRowContext(ctx, jobSelect, id))
}

func scanJob(row interface{ Scan(...any) error }) (domain.Job, error) {
	var (
		id, profile, status, correlation                   string
		retryID, submittedURL, canonicalURL, titleHint     sql.NullString
		stage, failureCategory, failureMessage             sql.NullString
		createdAt, completedAt, leaseExpiry, nextAttemptAt sql.NullTime
		leaseToken                                         sql.NullString
		attemptCount                                       int
		version                                            int64

		contentID, contentJobID, contentTitle, contentAuthor, contentSite sql.NullString
		publicationDate                                                   sql.NullTime
		contentDescription, contentSourceURL, semanticHTML, plainText     sql.NullString
		extractionMethod, detectedLanguage                                sql.NullString
		confidence, completeness                                          sql.NullString
		contentCreatedAt, contentUpdatedAt                                sql.NullTime

		artifactID, artifactKey, artifactFilename, artifactMediaType sql.NullString
		artifactSize                                                 sql.NullInt64
		artifactChecksum, artifactAvailability                       sql.NullString
		artifactCreatedAt                                            sql.NullTime
		deliveryOutcome, deliveryMessageID                           sql.NullString
		deliveryAttempt                                              sql.NullInt64
	)
	err := row.Scan(
		&id, &retryID, &submittedURL, &canonicalURL, &titleHint,
		&profile, &status, &stage, &attemptCount, &version, &nextAttemptAt,
		&failureCategory, &failureMessage, &correlation, &createdAt, &completedAt,
		&leaseToken, &leaseExpiry,
		&contentID, &contentJobID, &contentTitle, &contentAuthor, &contentSite,
		&publicationDate, &contentDescription, &contentSourceURL, &semanticHTML,
		&plainText, &extractionMethod, &confidence, &completeness, &detectedLanguage,
		&contentCreatedAt, &contentUpdatedAt,
		&artifactID, &artifactKey, &artifactFilename, &artifactMediaType,
		&artifactSize, &artifactChecksum, &artifactAvailability, &artifactCreatedAt,
		&deliveryOutcome, &deliveryAttempt, &deliveryMessageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Job{}, application.ErrNotFound
		}
		return domain.Job{}, &databaseFailure{operation: "scan job"}
	}
	if version <= 0 || !nextAttemptAt.Valid {
		return domain.Job{}, &databaseFailure{operation: "scan job"}
	}
	job := domain.Job{
		ID:            domain.JobID(id),
		RetryOfJobID:  domain.JobID(retryID.String),
		SubmittedURL:  submittedURL.String,
		CanonicalURL:  canonicalURL.String,
		TitleHint:     titleHint.String,
		Profile:       profile,
		Status:        domain.Status(status),
		Stage:         domain.Stage(stage.String),
		AttemptCount:  attemptCount,
		CreatedAt:     createdAt.Time.UTC(),
		LeaseToken:    leaseToken.String,
		LeaseUntil:    leaseExpiry.Time.UTC(),
		Version:       uint64(version),
		NextAttemptAt: nextAttemptAt.Time.UTC(),
	}
	if completedAt.Valid {
		value := completedAt.Time.UTC()
		job.CompletedAt = &value
	}
	if failureCategory.Valid {
		message := safeFailureMessage(domain.FailureCategory(failureCategory.String))
		// The database value is intentionally not returned.  Failure details can
		// contain provider/page data; the adapter exposes the stable, bounded
		// application message instead.
		job.Failure = &domain.Failure{Category: domain.FailureCategory(failureCategory.String), Message: message, CorrelationID: correlation, OccurredAt: completedOrCreated(job)}
	}
	if contentID.Valid {
		content := domain.ContentDocument{
			ID:               domain.ContentID(contentID.String),
			JobID:            domain.JobID(contentJobID.String),
			Title:            contentTitle.String,
			Author:           contentAuthor.String,
			SiteName:         contentSite.String,
			Description:      contentDescription.String,
			SourceURL:        contentSourceURL.String,
			SemanticHTML:     semanticHTML.String,
			PlainText:        plainText.String,
			ExtractionMethod: extractionMethod.String,
			AIConfidence:     parseFloat(confidence.String),
			AICompleteness:   parseFloat(completeness.String),
			Language:         detectedLanguage.String,
			CreatedAt:        contentCreatedAt.Time.UTC(),
			UpdatedAt:        contentUpdatedAt.Time.UTC(),
		}
		if publicationDate.Valid {
			content.PublicationDate = publicationDate.Time.UTC().Format(time.RFC3339)
		}
		job.Content = &content
		job.AIConfidence = content.AIConfidence
	}
	if artifactID.Valid {
		job.Artifact = &domain.Artifact{
			Key:       artifactKey.String,
			Filename:  artifactFilename.String,
			MediaType: artifactMediaType.String,
			ByteSize:  artifactSize.Int64,
			Checksum:  artifactChecksum.String,
			Available: artifactAvailability.String == "available",
			CreatedAt: artifactCreatedAt.Time.UTC(),
		}
	}
	if deliveryOutcome.Valid {
		job.Delivery = &domain.Delivery{Status: deliveryOutcome.String, AttemptCount: int(deliveryAttempt.Int64), MessageID: deliveryMessageID.String}
	}
	return job, nil
}

func scanListJob(row interface{ Scan(...any) error }) (domain.Job, error) {
	var (
		id, status, profile, correlation                                                        string
		retryID, submittedURL, canonicalURL, titleHint                                          sql.NullString
		stage, failureCategory                                                                  sql.NullString
		attemptCount                                                                            int
		version                                                                                 int64
		createdAt, completedAt, nextAttemptAt                                                   sql.NullTime
		contentID, contentTitle, contentAuthor, contentSite, description, language              sql.NullString
		publicationDate                                                                         sql.NullTime
		confidence, completeness                                                                sql.NullString
		artifactID, artifactFilename, artifactMediaType, artifactChecksum, artifactAvailability sql.NullString
		artifactSize                                                                            sql.NullInt64
		artifactCreatedAt                                                                       sql.NullTime
	)
	err := row.Scan(
		&id, &retryID, &submittedURL, &canonicalURL, &titleHint, &profile, &status,
		&stage, &attemptCount, &version, &nextAttemptAt, &failureCategory, &correlation,
		&createdAt, &completedAt,
		&contentID, &contentTitle, &contentAuthor, &contentSite, &publicationDate,
		&description, &language, &confidence, &completeness, &artifactID, &artifactFilename,
		&artifactMediaType, &artifactSize, &artifactChecksum, &artifactAvailability,
		&artifactCreatedAt)
	if err != nil {
		return domain.Job{}, err
	}
	if version <= 0 || !nextAttemptAt.Valid {
		return domain.Job{}, &databaseFailure{operation: "scan list jobs"}
	}
	job := domain.Job{
		ID:            domain.JobID(id),
		RetryOfJobID:  domain.JobID(retryID.String),
		SubmittedURL:  submittedURL.String,
		CanonicalURL:  canonicalURL.String,
		TitleHint:     titleHint.String,
		Profile:       profile,
		Status:        domain.Status(status),
		Stage:         domain.Stage(stage.String),
		AttemptCount:  attemptCount,
		CreatedAt:     createdAt.Time.UTC(),
		Version:       uint64(version),
		NextAttemptAt: nextAttemptAt.Time.UTC(),
	}
	if completedAt.Valid {
		value := completedAt.Time.UTC()
		job.CompletedAt = &value
	}
	if failureCategory.Valid {
		job.Failure = &domain.Failure{Category: domain.FailureCategory(failureCategory.String), Message: safeFailureMessage(domain.FailureCategory(failureCategory.String)), CorrelationID: correlation}
	}
	if contentID.Valid {
		content := domain.ContentDocument{ID: domain.ContentID(contentID.String), JobID: job.ID, Title: contentTitle.String, Author: contentAuthor.String, SiteName: contentSite.String, Description: description.String, Language: language.String, AIConfidence: parseFloat(confidence.String), AICompleteness: parseFloat(completeness.String)}
		if publicationDate.Valid {
			content.PublicationDate = publicationDate.Time.UTC().Format(time.RFC3339)
		}
		job.Content = &content
		job.AIConfidence = content.AIConfidence
	}
	if artifactID.Valid {
		job.Artifact = &domain.Artifact{Key: "", Filename: artifactFilename.String, MediaType: artifactMediaType.String, ByteSize: artifactSize.Int64, Checksum: artifactChecksum.String, Available: artifactAvailability.String == "available", CreatedAt: artifactCreatedAt.Time.UTC()}
	}
	return job, nil
}

func scanClaimJob(row interface{ Scan(...any) error }) (domain.Job, error) {
	var (
		id, status, profile, correlation                                       string
		retryID, submittedURL, canonicalURL, titleHint, stage, failureCategory sql.NullString
		attemptCount                                                           int
		version                                                                int64
		nextAttemptAt, createdAt, completedAt                                  sql.NullTime
	)
	if err := row.Scan(&id, &retryID, &submittedURL, &canonicalURL, &titleHint, &profile, &status, &stage, &attemptCount, &version, &nextAttemptAt, &failureCategory, &correlation, &createdAt, &completedAt); err != nil {
		return domain.Job{}, err
	}
	if version <= 0 || !nextAttemptAt.Valid {
		return domain.Job{}, &databaseFailure{operation: "scan claim job"}
	}
	job := domain.Job{ID: domain.JobID(id), RetryOfJobID: domain.JobID(retryID.String), SubmittedURL: submittedURL.String, CanonicalURL: canonicalURL.String, TitleHint: titleHint.String, Profile: profile, Status: domain.Status(status), Stage: domain.Stage(stage.String), AttemptCount: attemptCount, CreatedAt: createdAt.Time.UTC(), Version: uint64(version), NextAttemptAt: nextAttemptAt.Time.UTC()}
	if completedAt.Valid {
		value := completedAt.Time.UTC()
		job.CompletedAt = &value
	}
	if failureCategory.Valid {
		job.Failure = &domain.Failure{Category: domain.FailureCategory(failureCategory.String), Message: safeFailureMessage(domain.FailureCategory(failureCategory.String)), CorrelationID: correlation}
	}
	return job, nil
}

func completedOrCreated(job domain.Job) time.Time {
	if job.CompletedAt != nil {
		return job.CompletedAt.UTC()
	}
	return job.CreatedAt.UTC()
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullablePublicationDate(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return nil
}

func parseFloat(value string) float64 {
	if strings.TrimSpace(value) == "" {
		return 0
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0
	}
	return parsed
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func nextStage(stage domain.Stage) domain.Stage {
	switch stage {
	case domain.StageFetching:
		return domain.StageExtracting
	case domain.StageExtracting:
		return domain.StageAIAnalyzing
	case domain.StageAIAnalyzing:
		return domain.StageFormatting
	case domain.StageFormatting:
		return domain.StagePersisting
	default:
		return ""
	}
}

func validFailureCategory(category domain.FailureCategory) bool {
	switch category {
	case domain.FailureInvalidInput, domain.FailureBlockedTarget, domain.FailureFetchFailed,
		domain.FailureRenderTimeout, domain.FailureAccessDenied, domain.FailurePaywallDetected,
		domain.FailureUnsupportedContent, domain.FailureInsufficientContent,
		domain.FailureAIUnavailable, domain.FailureAIAuthFailed, domain.FailureAIModelUnsupported,
		domain.FailureAIInvalidResponse, domain.FailureFormatFailed, domain.FailureStorageFailed,
		domain.FailureDeliveryRejected, domain.FailureDeliveryTimeout, domain.FailureInternalError:
		return true
	default:
		return false
	}
}

func safeFailureMessage(category domain.FailureCategory) string {
	switch category {
	case domain.FailureAIUnavailable:
		return "The AI provider is temporarily unavailable"
	case domain.FailureAIAuthFailed:
		return "The AI provider rejected the configured credentials"
	case domain.FailureAIModelUnsupported:
		return "The configured AI model is unsupported"
	case domain.FailureAIInvalidResponse:
		return "The AI provider returned an invalid response"
	case domain.FailureUnsupportedContent:
		return "The page is not a supported article"
	case domain.FailureInsufficientContent:
		return "The page did not contain enough readable content"
	case domain.FailureFormatFailed:
		return "The PDF could not be generated"
	case domain.FailureStorageFailed:
		return "Artifact storage is temporarily unavailable"
	case domain.FailureFetchFailed:
		return "The page could not be retrieved"
	case domain.FailureBlockedTarget:
		return "The destination was blocked by network policy"
	case domain.FailureRenderTimeout:
		return "Page rendering exceeded its deadline"
	case domain.FailureAccessDenied:
		return "The page denied access"
	case domain.FailurePaywallDetected:
		return "The page appears to be behind a paywall"
	case domain.FailureDeliveryRejected:
		return "Email delivery was rejected"
	case domain.FailureDeliveryTimeout:
		return "Email delivery could not be confirmed"
	case domain.FailureInvalidInput:
		return "The submitted input is invalid"
	default:
		return "The job could not be completed"
	}
}

func randomToken() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err == nil {
		return hex.EncodeToString(data[:])
	}
	return hex.EncodeToString([]byte(strconv.FormatInt(time.Now().UTC().UnixNano(), 10)))
}
