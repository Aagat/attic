-- DESTRUCTIVE DOWN MIGRATION.
--
-- This permanently removes all Attic V1 jobs, content, artifact metadata,
-- AI-attempt metadata, delivery-attempt metadata, and deletion tasks. It does
-- not remove files from the configured artifact volume; remove that volume
-- separately only when data loss is explicitly intended.
BEGIN;

SET LOCAL TIME ZONE 'UTC';

DROP TABLE IF EXISTS deletion_tasks;
DROP TABLE IF EXISTS delivery_attempts;
DROP TABLE IF EXISTS ai_attempts;
DROP TABLE IF EXISTS artifacts;
DROP TABLE IF EXISTS content_documents;
DROP TABLE IF EXISTS idempotency_records;
DROP TABLE IF EXISTS jobs;

DROP FUNCTION IF EXISTS attic_reject_content_document_update();

COMMIT;
