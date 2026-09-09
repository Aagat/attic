package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"

	"attic/internal/acquisition"
	"attic/internal/domain"
)

// SavedPage locates the latest usable snapshot for a document job. The stored
// snapshot is self-contained; callers can render it without fetching its source.
func (l *Library) SavedPage(ctx context.Context, jobID domain.JobID) (acquisition.RenderedPage, bool, error) {
	var page acquisition.RenderedPage
	var encoded []byte
	err := l.db.QueryRowContext(ctx, `SELECT c.artifact,c.final_url,c.title FROM saved_captures c JOIN saved_items i ON i.id=c.item_id WHERE i.job_id=$1 AND c.status IN ('complete','partial') AND c.text_content<>'' ORDER BY c.created_at DESC LIMIT 1`, string(jobID)).Scan(&encoded, &page.FinalURL, &page.Title)
	if errors.Is(err, sql.ErrNoRows) {
		return page, false, nil
	}
	if err != nil {
		return page, false, safe(err)
	}
	var artifact domain.Artifact
	if json.Unmarshal(encoded, &artifact) != nil {
		return page, false, ErrStorage
	}
	reader, err := l.files.Open(ctx, artifact)
	if err != nil {
		return page, false, err
	}
	defer reader.Close()
	page.DOM, err = io.ReadAll(io.LimitReader(reader, 64<<20+1))
	if err != nil || len(page.DOM) > 64<<20 {
		return acquisition.RenderedPage{}, false, ErrStorage
	}
	page.Status = 200
	return page, true, nil
}
