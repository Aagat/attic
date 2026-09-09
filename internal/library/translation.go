package library

import (
	"context"
	"html"
	"strings"

	"attic/internal/ai"
	"attic/internal/capture"
	"attic/internal/domain"
)

// Translate selects a reading edition, preparing it only when necessary.
// Empty language restores the original edition. Captures and earlier PDFs stay stored.
func (l *Library) Translate(ctx context.Context, id, language, key string) error {
	if !ai.ValidTargetLanguage(language) {
		return ErrInvalid
	}
	return l.prepare(ctx, id, key, "", "", &language)
}

func (l *Library) OutputLanguage(ctx context.Context, jobID domain.JobID) (string, error) {
	var language string
	err := l.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT language FROM reading_editions WHERE job_id=$1),'')`, string(jobID)).Scan(&language)
	return language, safe(err)
}

// Reading returns only application-approved content for the selected edition.
func (l *Library) Reading(ctx context.Context, id string) ([]byte, error) {
	var title, body, raw, language string
	err := l.db.QueryRowContext(ctx, `SELECT c.title,c.semantic_html,COALESCE(NULLIF(c.source_url,''),i.url),COALESCE(c.detected_language,'') FROM saved_items i JOIN content_documents c ON c.job_id=i.job_id WHERE i.id=$1`, id).Scan(&title, &body, &raw, &language)
	if err != nil {
		return nil, safe(err)
	}
	document := "<!doctype html><html lang=\"" + html.EscapeString(language) + "\"><head><title>" + html.EscapeString(title) + "</title></head><body><article>" + body + "</article></body></html>"
	return capture.ReadingView([]byte(strings.TrimSpace(document)), raw)
}
