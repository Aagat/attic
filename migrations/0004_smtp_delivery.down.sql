-- Refuse rollback if retry history would violate the former uniqueness rule.
CREATE UNIQUE INDEX delivery_attempts_stable_message_id_uq ON delivery_attempts (stable_message_id);
DROP INDEX delivery_attempts_message_id_idx;
DROP INDEX jobs_delivery_queue_idx;
ALTER TABLE jobs DROP COLUMN delivery_retry_count, DROP COLUMN delivery_pending, DROP COLUMN delivery_destination;
