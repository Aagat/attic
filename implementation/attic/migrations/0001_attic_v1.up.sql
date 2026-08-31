-- Attic V1 durable model.
--
-- This migration is intentionally extension-free. The migration runner must
-- acquire the Attic migration advisory lock before executing this file.
BEGIN;

SET LOCAL TIME ZONE 'UTC';

CREATE TABLE jobs (
    id                  text PRIMARY KEY,
    retry_of_job_id     text REFERENCES jobs (id) ON DELETE SET NULL,
    source_kind         text NOT NULL DEFAULT 'url',
    source_payload      jsonb NOT NULL DEFAULT '{}'::jsonb,
    submitted_url       text,
    canonical_url       text,
    title_hint          text,
    display_title       text,
    output_kind         text NOT NULL DEFAULT 'pdf',
    output_profile      text NOT NULL,
    status              text NOT NULL DEFAULT 'queued',
    stage               text,
    attempt_count       integer NOT NULL DEFAULT 0,
    version             bigint NOT NULL DEFAULT 1,
    next_attempt_at     timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    failure_category    text,
    failure_message     text,
    correlation_id      text NOT NULL,
    lease_token         text,
    lease_expires_at    timestamptz,
    created_at          timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at          timestamptz,
    completed_at        timestamptz,

    CONSTRAINT jobs_id_not_blank
        CHECK (char_length(btrim(id)) BETWEEN 1 AND 200),
    CONSTRAINT jobs_retry_id_not_self
        CHECK (retry_of_job_id IS NULL OR retry_of_job_id <> id),
    CONSTRAINT jobs_source_kind_not_blank
        CHECK (char_length(btrim(source_kind)) BETWEEN 1 AND 100),
    CONSTRAINT jobs_source_payload_object
        CHECK (jsonb_typeof(source_payload) = 'object'),
    CONSTRAINT jobs_url_has_submitted_url
        CHECK (source_kind <> 'url' OR submitted_url IS NOT NULL),
    CONSTRAINT jobs_output_kind_not_blank
        CHECK (char_length(btrim(output_kind)) BETWEEN 1 AND 100),
    CONSTRAINT jobs_output_profile_not_blank
        CHECK (char_length(btrim(output_profile)) BETWEEN 1 AND 100),
    CONSTRAINT jobs_status_valid
        CHECK (status IN (
            'queued', 'processing', 'delivering', 'ready', 'delivered',
            'delivery_failed', 'failed', 'cancelled'
        )),
    CONSTRAINT jobs_stage_valid
        CHECK (stage IS NULL OR stage IN (
            'fetching', 'extracting', 'ai_analyzing', 'formatting', 'persisting'
        )),
    CONSTRAINT jobs_processing_has_stage
        CHECK (
            (status = 'processing' AND stage IS NOT NULL)
            OR (status <> 'processing' AND stage IS NULL)
        ),
    CONSTRAINT jobs_attempt_count_nonnegative
        CHECK (attempt_count >= 0),
    CONSTRAINT jobs_version_positive
        CHECK (version > 0),
    CONSTRAINT jobs_failure_category_valid
        CHECK (failure_category IS NULL OR failure_category IN (
            'invalid_input', 'blocked_target', 'fetch_failed', 'render_timeout',
            'access_denied', 'paywall_detected', 'unsupported_content',
            'insufficient_content', 'ai_unavailable', 'ai_auth_failed',
            'ai_model_unsupported', 'ai_invalid_response', 'format_failed',
            'storage_failed', 'delivery_rejected', 'delivery_timeout',
            'internal_error'
        )),
    CONSTRAINT jobs_failed_has_category
        CHECK (
            status NOT IN ('failed', 'delivery_failed')
            OR failure_category IS NOT NULL
        ),
    CONSTRAINT jobs_correlation_id_not_blank
        CHECK (char_length(btrim(correlation_id)) BETWEEN 1 AND 200),
    CONSTRAINT jobs_lease_fields_match
        CHECK ((lease_token IS NULL) = (lease_expires_at IS NULL)),
    CONSTRAINT jobs_active_status_has_lease
        CHECK (
            (status IN ('processing', 'delivering'))
            = (lease_token IS NOT NULL AND lease_expires_at IS NOT NULL)
        ),
    CONSTRAINT jobs_failure_message_bounded
        CHECK (failure_message IS NULL OR char_length(failure_message) <= 2000),
    CONSTRAINT jobs_completed_at_terminal
        CHECK (
            completed_at IS NULL
            OR status IN ('ready', 'delivered', 'delivery_failed', 'failed', 'cancelled')
        )
);

CREATE UNIQUE INDEX jobs_correlation_id_uq
    ON jobs (correlation_id);

-- Reverse creation order is the public list order. The ID makes ties stable.
CREATE INDEX jobs_created_keyset_idx
    ON jobs (created_at DESC, id DESC);

CREATE INDEX jobs_status_created_keyset_idx
    ON jobs (status, created_at DESC, id DESC);

-- Claimers use this index for queued work; the lease-expiry index below covers
-- reclaiming active leases.
CREATE INDEX jobs_claim_idx
    ON jobs (status, next_attempt_at, created_at, id)
    WHERE status = 'queued';

CREATE INDEX jobs_lease_expiry_idx
    ON jobs (lease_expires_at, id)
    WHERE lease_expires_at IS NOT NULL;

CREATE INDEX jobs_retry_of_idx
    ON jobs (retry_of_job_id, created_at DESC, id DESC)
    WHERE retry_of_job_id IS NOT NULL;

CREATE TABLE idempotency_records (
    scope               text NOT NULL DEFAULT 'owner',
    idempotency_key     text NOT NULL,
    request_hash        text NOT NULL,
    job_id              text NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    created_at          timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at          timestamptz NOT NULL,

    PRIMARY KEY (scope, idempotency_key),
    CONSTRAINT idempotency_scope_not_blank
        CHECK (char_length(btrim(scope)) BETWEEN 1 AND 100),
    CONSTRAINT idempotency_key_not_blank
        CHECK (char_length(btrim(idempotency_key)) BETWEEN 1 AND 512),
    CONSTRAINT idempotency_hash_not_blank
        CHECK (char_length(btrim(request_hash)) BETWEEN 1 AND 512),
    CONSTRAINT idempotency_expiry_after_creation
        CHECK (expires_at > created_at)
);

CREATE INDEX idempotency_expiry_idx
    ON idempotency_records (expires_at, scope, idempotency_key);

CREATE TABLE content_documents (
    id                  text PRIMARY KEY,
    job_id              text NOT NULL UNIQUE REFERENCES jobs (id) ON DELETE CASCADE,
    title               text NOT NULL,
    author              text,
    site_name           text,
    publication_date    timestamptz,
    description         text,
    source_url          text NOT NULL,
    semantic_html       text NOT NULL,
    plain_text          text NOT NULL,
    extraction_method   text NOT NULL,
    ai_confidence       numeric(4, 3) NOT NULL,
    ai_completeness     numeric(4, 3),
    detected_language   text,
    created_at          timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT content_documents_id_not_blank
        CHECK (char_length(btrim(id)) BETWEEN 1 AND 200),
    CONSTRAINT content_documents_title_not_blank
        CHECK (char_length(btrim(title)) > 0),
    CONSTRAINT content_documents_source_url_not_blank
        CHECK (char_length(btrim(source_url)) > 0),
    CONSTRAINT content_documents_html_not_blank
        CHECK (char_length(btrim(semantic_html)) > 0),
    CONSTRAINT content_documents_text_not_blank
        CHECK (char_length(btrim(plain_text)) > 0),
    CONSTRAINT content_documents_extraction_method_not_blank
        CHECK (char_length(btrim(extraction_method)) BETWEEN 1 AND 100),
    CONSTRAINT content_documents_confidence_range
        CHECK (ai_confidence >= 0 AND ai_confidence <= 1),
    CONSTRAINT content_documents_completeness_range
        CHECK (ai_completeness IS NULL OR (ai_completeness >= 0 AND ai_completeness <= 1)),
    CONSTRAINT content_documents_language_bounded
        CHECK (detected_language IS NULL OR char_length(detected_language) <= 32)
);

-- Content documents are immutable V1 records. Job deletion still removes them
-- through the foreign key above; normal UPDATE is rejected at the database seam.
CREATE OR REPLACE FUNCTION attic_reject_content_document_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'content_documents are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER content_documents_immutable_update
    BEFORE UPDATE ON content_documents
    FOR EACH ROW
    EXECUTE FUNCTION attic_reject_content_document_update();

-- This is also the rebuild order for a future search adapter.
CREATE INDEX content_documents_created_keyset_idx
    ON content_documents (created_at DESC, id DESC);

CREATE TABLE artifacts (
    id                      text PRIMARY KEY,
    job_id                  text NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    content_id              text REFERENCES content_documents (id) ON DELETE CASCADE,
    output_kind             text NOT NULL DEFAULT 'pdf',
    profile                 text NOT NULL,
    storage_relative_path   text NOT NULL,
    safe_filename           text NOT NULL,
    media_type              text NOT NULL,
    byte_size               bigint NOT NULL,
    checksum_sha256         text NOT NULL,
    availability            text NOT NULL DEFAULT 'available',
    created_at              timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT artifacts_id_not_blank
        CHECK (char_length(btrim(id)) BETWEEN 1 AND 200),
    CONSTRAINT artifacts_output_kind_not_blank
        CHECK (char_length(btrim(output_kind)) BETWEEN 1 AND 100),
    CONSTRAINT artifacts_profile_not_blank
        CHECK (char_length(btrim(profile)) BETWEEN 1 AND 100),
    CONSTRAINT artifacts_storage_path_relative
        CHECK (
            char_length(btrim(storage_relative_path)) > 0
            AND left(storage_relative_path, 1) <> '/'
            AND position('..' in storage_relative_path) = 0
            AND position(chr(92) in storage_relative_path) = 0
        ),
    CONSTRAINT artifacts_filename_safe
        CHECK (
            char_length(btrim(safe_filename)) BETWEEN 1 AND 255
            AND position('/' in safe_filename) = 0
            AND position(chr(92) in safe_filename) = 0
            AND position('..' in safe_filename) = 0
        ),
    CONSTRAINT artifacts_media_type_not_blank
        CHECK (char_length(btrim(media_type)) BETWEEN 1 AND 255),
    CONSTRAINT artifacts_byte_size_nonnegative
        CHECK (byte_size >= 0),
    CONSTRAINT artifacts_checksum_sha256
        CHECK (checksum_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT artifacts_availability_valid
        CHECK (availability IN ('pending', 'available', 'unavailable', 'delete_pending', 'delete_failed')),
    CONSTRAINT artifacts_available_has_size
        CHECK (availability <> 'available' OR byte_size >= 0)
);

-- V1 has one configured profile, while this key permits future output kinds or
-- profiles without making the job-to-artifact relation one-to-one forever.
CREATE UNIQUE INDEX artifacts_job_output_profile_uq
    ON artifacts (job_id, output_kind, profile);

CREATE INDEX artifacts_job_idx
    ON artifacts (job_id, created_at DESC, id DESC);

CREATE TABLE ai_attempts (
    id                      text PRIMARY KEY,
    job_id                  text NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    purpose                 text NOT NULL DEFAULT 'analysis',
    attempt_number          integer NOT NULL,
    model_identifier        text NOT NULL,
    prompt_version          text NOT NULL,
    latency_ms              bigint NOT NULL,
    provider_request_id     text,
    input_tokens            bigint,
    output_tokens           bigint,
    result_status           text NOT NULL,
    error_category          text,
    created_at              timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT ai_attempts_id_not_blank
        CHECK (char_length(btrim(id)) BETWEEN 1 AND 200),
    CONSTRAINT ai_attempts_purpose_valid
        CHECK (purpose IN ('analysis', 'repair')),
    CONSTRAINT ai_attempts_number_positive
        CHECK (attempt_number > 0),
    CONSTRAINT ai_attempts_model_not_blank
        CHECK (char_length(btrim(model_identifier)) BETWEEN 1 AND 200),
    CONSTRAINT ai_attempts_prompt_version_not_blank
        CHECK (char_length(btrim(prompt_version)) BETWEEN 1 AND 200),
    CONSTRAINT ai_attempts_latency_nonnegative
        CHECK (latency_ms >= 0),
    CONSTRAINT ai_attempts_input_tokens_nonnegative
        CHECK (input_tokens IS NULL OR input_tokens >= 0),
    CONSTRAINT ai_attempts_output_tokens_nonnegative
        CHECK (output_tokens IS NULL OR output_tokens >= 0),
    CONSTRAINT ai_attempts_result_status_valid
        CHECK (result_status IN (
            'succeeded', 'malformed_response', 'rejected', 'failed'
        )),
    CONSTRAINT ai_attempts_error_category_valid
        CHECK (error_category IS NULL OR error_category IN (
            'ai_timeout', 'ai_canceled', 'ai_unavailable', 'ai_auth_failed',
            'ai_model_unsupported', 'ai_rate_limited',
            'ai_provider_rejected', 'ai_response_too_large',
            'ai_invalid_response',
            'paywall', 'paywall_detected', 'access_denied', 'error_page',
            'interactive', 'non_article', 'unsupported_content',
            'insufficient_content'
        )),
    CONSTRAINT ai_attempts_success_error_match
        CHECK (
            (result_status = 'succeeded' AND error_category IS NULL)
            OR (result_status <> 'succeeded' AND error_category IS NOT NULL)
        )
);

CREATE UNIQUE INDEX ai_attempts_job_purpose_number_uq
    ON ai_attempts (job_id, purpose, attempt_number);

CREATE INDEX ai_attempts_job_created_idx
    ON ai_attempts (job_id, created_at DESC, id DESC);

CREATE TABLE delivery_attempts (
    id                      text PRIMARY KEY,
    job_id                  text NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    artifact_id             text REFERENCES artifacts (id) ON DELETE SET NULL,
    delivery_kind           text NOT NULL DEFAULT 'smtp',
    attempt_number          integer NOT NULL,
    stable_message_id       text NOT NULL,
    started_at              timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at            timestamptz,
    outcome                 text NOT NULL,
    provider_response_code  text,
    error_category          text,
    created_at              timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT delivery_attempts_id_not_blank
        CHECK (char_length(btrim(id)) BETWEEN 1 AND 200),
    CONSTRAINT delivery_attempts_kind_not_blank
        CHECK (char_length(btrim(delivery_kind)) BETWEEN 1 AND 100),
    CONSTRAINT delivery_attempts_number_positive
        CHECK (attempt_number > 0),
    CONSTRAINT delivery_attempts_message_id_not_blank
        CHECK (char_length(btrim(stable_message_id)) BETWEEN 1 AND 512),
    CONSTRAINT delivery_attempts_completed_after_started
        CHECK (completed_at IS NULL OR completed_at >= started_at),
    CONSTRAINT delivery_attempts_outcome_valid
        CHECK (outcome IN ('accepted', 'rejected', 'transient_failure', 'uncertain', 'disabled')),
    CONSTRAINT delivery_attempts_response_code_bounded
        CHECK (provider_response_code IS NULL OR char_length(provider_response_code) <= 128),
    CONSTRAINT delivery_attempts_error_category_valid
        CHECK (error_category IS NULL OR error_category IN (
            'delivery_rejected', 'delivery_timeout', 'internal_error'
        )),
    CONSTRAINT delivery_attempts_accepted_has_no_error
        CHECK (outcome <> 'accepted' OR error_category IS NULL)
);

CREATE UNIQUE INDEX delivery_attempts_job_kind_number_uq
    ON delivery_attempts (job_id, delivery_kind, attempt_number);

CREATE UNIQUE INDEX delivery_attempts_stable_message_id_uq
    ON delivery_attempts (stable_message_id);

CREATE INDEX delivery_attempts_job_created_idx
    ON delivery_attempts (job_id, created_at DESC, id DESC);

CREATE TABLE deletion_tasks (
    id                      text PRIMARY KEY,
    job_id                  text REFERENCES jobs (id) ON DELETE SET NULL,
    artifact_id             text REFERENCES artifacts (id) ON DELETE SET NULL,
    storage_relative_path   text NOT NULL,
    status                  text NOT NULL DEFAULT 'queued',
    attempt_count           integer NOT NULL DEFAULT 0,
    next_attempt_at         timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    lease_token             text,
    lease_expires_at        timestamptz,
    last_error_category     text,
    last_error_message      text,
    created_at              timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at              timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at            timestamptz,

    CONSTRAINT deletion_tasks_id_not_blank
        CHECK (char_length(btrim(id)) BETWEEN 1 AND 200),
    CONSTRAINT deletion_tasks_storage_path_relative
        CHECK (
            char_length(btrim(storage_relative_path)) > 0
            AND left(storage_relative_path, 1) <> '/'
            AND position('..' in storage_relative_path) = 0
            AND position(chr(92) in storage_relative_path) = 0
        ),
    CONSTRAINT deletion_tasks_status_valid
        CHECK (status IN ('queued', 'processing', 'failed', 'completed')),
    CONSTRAINT deletion_tasks_attempt_count_nonnegative
        CHECK (attempt_count >= 0),
    CONSTRAINT deletion_tasks_lease_fields_match
        CHECK ((lease_token IS NULL) = (lease_expires_at IS NULL)),
    CONSTRAINT deletion_tasks_processing_has_lease
        CHECK ((status = 'processing') = (lease_token IS NOT NULL AND lease_expires_at IS NOT NULL)),
    CONSTRAINT deletion_tasks_completed_at_match
        CHECK ((status = 'completed') = (completed_at IS NOT NULL)),
    CONSTRAINT deletion_tasks_error_category_valid
        CHECK (last_error_category IS NULL OR last_error_category IN (
            'storage_failed', 'internal_error'
        )),
    CONSTRAINT deletion_tasks_error_message_bounded
        CHECK (last_error_message IS NULL OR char_length(last_error_message) <= 2000)
);

-- At most one unfinished deletion may target an artifact. The task retains the
-- relative path even after job/artifact rows are removed by their foreign keys.
CREATE UNIQUE INDEX deletion_tasks_one_active_artifact_uq
    ON deletion_tasks (artifact_id)
    WHERE artifact_id IS NOT NULL AND status <> 'completed';

CREATE INDEX deletion_tasks_claim_idx
    ON deletion_tasks (status, next_attempt_at, created_at, id)
    WHERE status IN ('queued', 'failed');

CREATE INDEX deletion_tasks_lease_expiry_idx
    ON deletion_tasks (lease_expires_at, id)
    WHERE lease_expires_at IS NOT NULL;

COMMIT;
