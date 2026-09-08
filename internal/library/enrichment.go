package library

import (
	"context"
	"database/sql"
	"errors"

	"attic/internal/ai"
)

type Enricher interface {
	Enrich(context.Context, string) (ai.Enrichment, error)
}

func (l *Library) EnrichOnce(ctx context.Context, e Enricher) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return safe(err)
	}
	defer tx.Rollback()
	var locked bool
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(649817302)`).Scan(&locked); err != nil {
		return safe(err)
	}
	if !locked {
		return nil
	}
	var id, text string
	var version int64
	err = tx.QueryRowContext(ctx, `SELECT id,title||E'\n'||text_content,version FROM saved_items WHERE enrichment_status='pending' AND (text_content<>'' OR capture_status IN ('failed','blocked','not_applicable')) ORDER BY updated_at LIMIT 1`).Scan(&id, &text, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return safe(err)
	}
	enrichment, enrichErr := e.Enrich(ctx, text)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	status := "complete"
	if enrichErr != nil {
		status = "failed"
	}
	_, err = tx.ExecContext(ctx, `UPDATE saved_items SET classification=CASE WHEN $4='failed' THEN classification ELSE $2 END,suggested_tags=CASE WHEN $4='failed' THEN suggested_tags ELSE $3::jsonb END,enrichment_status=$4,version=version+1 WHERE id=$1 AND version=$5`, id, enrichment.Classification, jsonValue(enrichment.Tags), status, version)
	if err != nil {
		return safe(err)
	}
	return safe(tx.Commit())
}
func (l *Library) RunEnrichment(ctx context.Context, e Enricher) error {
	return loop(ctx, func() error { return l.EnrichOnce(ctx, e) })
}
func (l *Library) RetryEnrichment(ctx context.Context, id string) error {
	_, err := l.db.ExecContext(ctx, `UPDATE saved_items SET enrichment_status='pending' WHERE id=$1`, id)
	return safe(err)
}
