BEGIN;

ALTER TABLE ai_attempts
    DROP CONSTRAINT ai_attempts_error_category_valid;

-- Categories introduced by the up migration have no exact representation in
-- V1. Preserve the attempt rows while conservatively degrading their category.
UPDATE ai_attempts
SET error_category = 'ai_invalid_response'
WHERE error_category IN ('invalid_input', 'input_too_large', 'fetch_failed');

ALTER TABLE ai_attempts
    ADD CONSTRAINT ai_attempts_error_category_valid
    CHECK (error_category IS NULL OR error_category IN (
        'ai_timeout', 'ai_canceled', 'ai_unavailable', 'ai_auth_failed',
        'ai_model_unsupported', 'ai_rate_limited',
        'ai_provider_rejected', 'ai_response_too_large',
        'ai_invalid_response', 'paywall', 'paywall_detected',
        'access_denied', 'error_page', 'interactive', 'non_article',
        'unsupported_content', 'insufficient_content'
    ));

COMMIT;
