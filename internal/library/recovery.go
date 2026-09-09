package library

import (
	"attic/internal/capture"
	"context"
)

// SaveRecoveredPage appends a server-browser version to an existing bookmark.
// It cannot create, delete or replace an item or any earlier capture. A completed
// recovery supersedes the outstanding capture lease, just like a browser upload.
func (l *Library) SaveRecoveredPage(ctx context.Context, raw string, result capture.Result) error {
	normalized, err := normalize(raw)
	if err != nil {
		return ErrInvalid
	}
	var id string
	if err = l.db.QueryRowContext(ctx, `SELECT id FROM saved_items WHERE url=$1 AND kind='bookmark'`, normalized).Scan(&id); err != nil {
		return safe(err)
	}
	if result.Status == "blocked" || len(result.HTML) == 0 || result.PlainText == "" {
		return ErrBrowserCaptureBlocked
	}
	captureID := opaque()
	if err = l.persistCapture(ctx, id, "", captureID, "server", result); err != nil {
		return err
	}
	return l.ResumeRecovered(ctx)
}

// ResumeRecovered closes the race between capture completion and a concurrent
// PDF failure. A capture can resume a failed job once: the new job is newer
// than that capture, and the old job remains stored unchanged.
func (l *Library) ResumeRecovered(ctx context.Context) error {
	rows, err := l.db.QueryContext(ctx, `SELECT i.id,j.id,COALESCE(j.delivery_destination,'') FROM saved_items i JOIN jobs j ON j.id=i.job_id
 WHERE j.status='failed' AND NOT EXISTS(SELECT 1 FROM item_actions a WHERE a.key='recovery:'||j.id) AND EXISTS(SELECT 1 FROM saved_captures c WHERE c.item_id=i.id AND c.status IN ('complete','partial') AND c.text_content<>'' AND c.created_at>j.created_at) LIMIT 10`)
	if err != nil {
		return safe(err)
	}
	type candidate struct{ id, job, destination string }
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.job, &c.destination); err != nil {
			rows.Close()
			return safe(err)
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return safe(err)
	}
	for _, c := range candidates {
		if err = l.prepare(ctx, c.id, "recovery:"+c.job, c.destination, c.job); err != nil {
			return err
		}
	}
	return nil
}
