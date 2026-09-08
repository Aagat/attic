BEGIN;

ALTER TABLE ai_attempts
    DROP CONSTRAINT ai_attempts_error_category_valid;

ALTER TABLE ai_attempts
    ADD CONSTRAINT ai_attempts_error_category_valid
    CHECK (error_category IS NULL OR error_category IN (
        'invalid_input', 'input_too_large', 'ai_timeout', 'ai_canceled',
        'ai_unavailable', 'ai_auth_failed', 'ai_model_unsupported',
        'ai_rate_limited', 'ai_provider_rejected', 'ai_response_too_large',
        'ai_invalid_response', 'fetch_failed', 'paywall',
        'paywall_detected', 'access_denied', 'error_page', 'interactive',
        'non_article', 'unsupported_content', 'insufficient_content'
    ));

COMMIT;
