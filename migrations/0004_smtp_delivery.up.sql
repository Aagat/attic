ALTER TABLE jobs ADD COLUMN delivery_destination text;
ALTER TABLE jobs ADD COLUMN delivery_pending boolean NOT NULL DEFAULT false;
ALTER TABLE jobs ADD COLUMN delivery_retry_count integer NOT NULL DEFAULT 0;
CREATE INDEX jobs_delivery_queue_idx ON jobs (next_attempt_at, created_at) WHERE delivery_pending AND status = 'ready';
-- Safe retries of one logical email use the same Message-ID.
DROP INDEX delivery_attempts_stable_message_id_uq;
CREATE INDEX delivery_attempts_message_id_idx ON delivery_attempts (stable_message_id);
