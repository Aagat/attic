package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"attic/internal/delivery"
)

func (s *Store) ClaimDelivery(ctx context.Context, duration time.Duration) (*delivery.Claim, error) {
	now := s.now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, mapDBError("begin delivery", err)
	}
	defer tx.Rollback()
	if err := expireDeliveries(ctx, tx, now); err != nil {
		return nil, err
	}

	var id, destination string
	err = tx.QueryRowContext(ctx, `SELECT id, delivery_destination FROM jobs WHERE status='ready' AND delivery_pending AND NOT delivery_paused
 AND next_attempt_at <= $1 ORDER BY next_attempt_at, created_at FOR UPDATE SKIP LOCKED LIMIT 1`, now).Scan(&id, &destination)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, mapDBError("claim email", err)
	}
	job, err := loadJob(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if job.Artifact == nil {
		return nil, &databaseFailure{operation: "delivery artifact"}
	}
	token := randomToken()
	messageID := delivery.MessageID(id)
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status='delivering', completed_at=NULL, lease_token=$2,
 lease_expires_at=$3, updated_at=$4, version=version+1, delivery_retry_count=delivery_retry_count+1 WHERE id=$1`, id, token, now.Add(duration), now); err != nil {
		return nil, mapDBError("lease email", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_attempts (id,job_id,artifact_id,attempt_number,stable_message_id,outcome,error_category,started_at)
 SELECT $1,$2,(SELECT id FROM artifacts WHERE job_id=$2 AND availability='available' ORDER BY created_at DESC LIMIT 1),
 COALESCE(MAX(attempt_number),0)+1,$3,'uncertain','delivery_timeout',$4 FROM delivery_attempts WHERE job_id=$2`, token, id, messageID, now); err != nil {
		return nil, mapDBError("record email attempt", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, mapDBError("commit email claim", err)
	}
	title := job.TitleHint
	if job.Content != nil {
		title = job.Content.Title
	}
	return &delivery.Claim{JobID: job.ID, Token: token, MessageID: messageID, Destination: destination, Title: title, Artifact: *job.Artifact}, nil
}

func (s *Store) FinishDelivery(ctx context.Context, claim *delivery.Claim, result delivery.Result) error {
	now := s.now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return mapDBError("begin email result", err)
	}
	defer tx.Rollback()
	var retries int
	err = tx.QueryRowContext(ctx, `SELECT delivery_retry_count FROM jobs WHERE id=$1 AND status='delivering' AND lease_token=$2 AND lease_expires_at>$3 FOR UPDATE`, string(claim.JobID), claim.Token, now).Scan(&retries)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return mapDBError("read email claim", err)
	}
	status, category, message := "delivery_failed", "delivery_timeout", "Email delivery could not be confirmed"
	pending := false
	switch result.Outcome {
	case "accepted":
		status, category, message = "delivered", "", ""
	case "rejected":
		category, message = "delivery_rejected", "Email delivery was rejected"
	case "transient_failure":
		if retries < 3 {
			status = "ready"
			pending = true
			category = ""
			message = ""
		}
	case "uncertain":
	default:
		return &databaseFailure{operation: "invalid email result"}
	}
	attemptCategory := "delivery_timeout"
	if result.Outcome == "accepted" {
		attemptCategory = ""
	}
	if result.Outcome == "rejected" {
		attemptCategory = "delivery_rejected"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE delivery_attempts SET outcome=$2,provider_response_code=NULLIF($3,''),error_category=NULLIF($4,''),completed_at=$5 WHERE id=$1`, claim.Token, result.Outcome, result.Code, attemptCategory, now); err != nil {
		return mapDBError("record email result", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status=$3,delivery_pending=$4,failure_category=NULLIF($5,''),failure_message=NULLIF($6,''),
 lease_token=NULL,lease_expires_at=NULL,completed_at=$7,updated_at=$7,next_attempt_at=$8,version=version+1 WHERE id=$1 AND lease_token=$2`, string(claim.JobID), claim.Token, status, pending, category, message, now, now.Add(time.Duration(retries)*30*time.Second)); err != nil {
		return mapDBError("finish email", err)
	}
	return mapDBError("commit email result", tx.Commit())
}

var _ delivery.Queue = (*Store)(nil)

// An expired SMTP claim is ambiguous: never resend it automatically.
func expireDeliveries(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE jobs SET status='delivery_failed', delivery_pending=false,
 failure_category='delivery_timeout', failure_message='Email delivery could not be confirmed',
 lease_token=NULL, lease_expires_at=NULL, completed_at=$1, updated_at=$1, version=version+1
 WHERE status='delivering' AND lease_expires_at <= $1`, now)
	return mapDBError("expire delivery leases", err)
}
