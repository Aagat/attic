UPDATE jobs SET failure_category='format_failed' WHERE failure_category='pdf_quality_failed';
ALTER TABLE jobs DROP CONSTRAINT jobs_failure_category_valid;
ALTER TABLE jobs ADD CONSTRAINT jobs_failure_category_valid
        CHECK (failure_category IS NULL OR failure_category IN (
            'invalid_input', 'blocked_target', 'fetch_failed', 'render_timeout',
            'access_denied', 'paywall_detected', 'unsupported_content',
            'insufficient_content', 'ai_unavailable', 'ai_auth_failed',
            'ai_model_unsupported', 'ai_invalid_response', 'format_failed',
            'storage_failed', 'delivery_rejected', 'delivery_timeout',
            'internal_error'
        ));
