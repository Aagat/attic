package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"

	"attic/internal/capture"
	"attic/internal/domain"
	"attic/internal/search"
)

type Capturer interface {
	Capture(context.Context, string) (capture.Result, error)
}

func (l *Library) CaptureOnce(ctx context.Context, c Capturer) (bool, error) {
	var id, raw, token string
	var attempts int
	token = opaque()
	err := l.db.QueryRowContext(ctx, `UPDATE saved_items SET capture_status='capturing',capture_token=$1,capture_until=now()+interval '6 minutes',capture_attempts=capture_attempts+1
 WHERE id=(SELECT id FROM saved_items WHERE (capture_status='queued' AND capture_next<=now()) OR (capture_status='capturing' AND capture_until<now()) ORDER BY capture_next FOR UPDATE SKIP LOCKED LIMIT 1)
 RETURNING id,url,capture_attempts`, token).Scan(&id, &raw, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, safe(err)
	}
	result, captureErr := c.Capture(ctx, raw)
	if captureErr != nil || len(result.HTML) == 0 {
		status := "failed"
		if attempts < 3 {
			status = "queued"
		}
		_, err = l.db.ExecContext(ctx, `UPDATE saved_items SET capture_status=$3,capture_next=now()+interval '30 seconds',capture_token=NULL,capture_until=NULL,version=version+1 WHERE id=$1 AND capture_token=$2`, id, token, status)
		return true, safe(err)
	}
	return true, l.persistCapture(ctx, id, token, opaque(), "server", result)
}

// persistCapture serializes the item update with capture imports. Server results
// must still own their lease; a browser import clears it atomically.
func (l *Library) persistCapture(ctx context.Context, id, token, captureID, source string, result capture.Result) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return safe(err)
	}
	defer tx.Rollback()
	var current sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT capture_token FROM saved_items WHERE id=$1 FOR UPDATE`, id).Scan(&current); err != nil {
		return safe(err)
	}
	if source == "server" && token != "" && current.String != token {
		return nil
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM saved_captures WHERE id=$1)`, captureID).Scan(&exists); err != nil {
		return safe(err)
	}
	if exists {
		return nil
	}
	artifact, err := l.files.Put(ctx, domain.JobID("capture-"+captureID), "snapshot.html", result.HTML, time.Now())
	if err != nil {
		return err
	}
	artifact.MediaType = "text/html"
	committed := false
	defer func() {
		if !committed {
			l.files.Delete(context.WithoutCancel(ctx), artifact)
		}
	}()
	_, err = tx.ExecContext(ctx, `INSERT INTO saved_captures(id,item_id,artifact,final_url,title,text_content,status,missing,source) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, captureID, id, jsonValue(artifact), result.FinalURL, result.Title, result.PlainText, result.Status, jsonValue(result.MissingResources), source)
	if err != nil {
		return safe(err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE saved_items SET capture_status=$2,capture_token=NULL,capture_until=NULL,
 title=CASE WHEN title_edited OR $3='' OR $2='blocked' THEN title ELSE $3 END,
 text_content=CASE WHEN $2='blocked' AND text_content<>'' THEN text_content ELSE $4 END,
 enrichment_status='pending',version=version+1,updated_at=now() WHERE id=$1`, id, result.Status, result.Title, result.PlainText)
	if err != nil {
		return safe(err)
	}
	err = tx.Commit()
	committed = err == nil
	return safe(err)
}

func (l *Library) RunCaptures(ctx context.Context, c Capturer) error {
	return loop(ctx, func() error {
		if err := l.ResumeRecovered(ctx); err != nil {
			return err
		}
		_, err := l.CaptureOnce(ctx, c)
		return err
	})
}
func loop(ctx context.Context, f func() error) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := f(); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// IndexOnce holds a transaction-scoped advisory lock across mutations so multiple
// app instances cannot apply stale projections out of order. Saves remain independent.
func (l *Library) IndexOnce(ctx context.Context, index search.Index) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return safe(err)
	}
	defer tx.Rollback()
	var locked bool
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(649817301)`).Scan(&locked); err != nil {
		return safe(err)
	}
	if !locked {
		return nil
	}
	var removed string
	err = tx.QueryRowContext(ctx, `SELECT id FROM search_deletions LIMIT 1`).Scan(&removed)
	if err == nil {
		if err := index.Delete(ctx, removed); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM search_deletions WHERE id=$1`, removed); err != nil {
			return safe(err)
		}
		return safe(tx.Commit())
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return safe(err)
	}
	i, err := scanItem(tx.QueryRowContext(ctx, itemSelect+`WHERE i.indexed_version<i.version ORDER BY i.updated_at LIMIT 1`))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	host := ""
	if u, err := url.Parse(i.URL); err == nil {
		host = u.Hostname()
	}
	err = index.Upsert(ctx, search.Document{ID: i.ID, Title: i.Title, URL: i.URL, Notes: i.Notes, Tags: append(append([]string{}, i.Tags...), i.SuggestedTags...), Text: i.Text + "\n" + i.Classification + "\n" + strings.Join(i.Folders, " "), Domain: host, Kind: i.Kind, CaptureStatus: i.CaptureStatus, SavedAt: i.SavedAt})
	if err != nil {
		tx.ExecContext(ctx, `UPDATE saved_items SET index_error=true WHERE id=$1`, i.ID)
		tx.Commit()
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE saved_items SET indexed_version=$2,index_error=false WHERE id=$1`, i.ID, i.Version); err != nil {
		return safe(err)
	}
	return safe(tx.Commit())
}
func (l *Library) RunIndex(ctx context.Context, index search.Index) error {
	return loop(ctx, func() error { return l.IndexOnce(ctx, index) })
}
func (l *Library) Reindex(ctx context.Context) error {
	_, err := l.db.ExecContext(ctx, `UPDATE saved_items SET indexed_version=0,index_error=false`)
	return safe(err)
}
func (l *Library) OpenCapture(ctx context.Context, id, captureID string) (io.ReadCloser, error) {
	var data []byte
	if err := l.db.QueryRowContext(ctx, `SELECT artifact FROM saved_captures WHERE id=$1 AND item_id=$2`, captureID, id).Scan(&data); err != nil {
		return nil, safe(err)
	}
	var a domain.Artifact
	if json.Unmarshal(data, &a) != nil {
		return nil, ErrStorage
	}
	return l.files.Open(ctx, a)
}
func (l *Library) StorageBytes(ctx context.Context) (int64, error) {
	var total int64
	err := l.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT sum(byte_size) FROM artifacts),0)+COALESCE((SELECT sum((artifact->>'ByteSize')::bigint) FROM saved_captures),0)`).Scan(&total)
	return total, safe(err)
}
func (l *Library) Delete(ctx context.Context, id string) error {
	d, err := l.Get(ctx, id)
	if err != nil {
		return err
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return safe(err)
	}
	defer tx.Rollback()
	if d.URL != "" {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, d.URL); err != nil {
			return safe(err)
		}
	}
	if _, err := tx.ExecContext(ctx, `SELECT id FROM saved_items WHERE id=$1 FOR UPDATE`, id); err != nil {
		return safe(err)
	}
	// Record file cleanup before deleting metadata; cleanup is idempotent after restart.
	_, err = tx.ExecContext(ctx, `INSERT INTO deletion_tasks(id,storage_relative_path)
 SELECT 'capture-'||id,artifact->>'Key' FROM saved_captures WHERE item_id=$1 ON CONFLICT DO NOTHING`, id)
	if err != nil {
		return safe(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO deletion_tasks(id,storage_relative_path) SELECT 'item-'||a.id,a.storage_relative_path FROM artifacts a JOIN jobs j ON j.id=a.job_id WHERE j.id=$1 OR ($2<>'' AND j.submitted_url=$2) ON CONFLICT DO NOTHING`, d.JobID, d.URL)
	if err != nil {
		return safe(err)
	}
	if d.URL != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO saved_tombstones(url_hash) VALUES($1) ON CONFLICT DO NOTHING`, urlHash(d.URL)); err != nil {
			return safe(err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO search_deletions(id) VALUES($1) ON CONFLICT DO NOTHING`, id); err != nil {
		return safe(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM saved_items WHERE id=$1`, id); err != nil {
		return safe(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM jobs WHERE id=$1 OR ($2<>'' AND submitted_url=$2)`, d.JobID, d.URL); err != nil {
		return safe(err)
	}
	return safe(tx.Commit())
}
func (l *Library) CleanupOnce(ctx context.Context) error {
	rows, err := l.db.QueryContext(ctx, `SELECT id,storage_relative_path FROM deletion_tasks WHERE status='queued' LIMIT 50`)
	if err != nil {
		return safe(err)
	}
	type task struct{ id, key string }
	tasks := []task{}
	for rows.Next() {
		var t task
		if err := rows.Scan(&t.id, &t.key); err != nil {
			rows.Close()
			return safe(err)
		}
		tasks = append(tasks, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return safe(err)
	}
	for _, t := range tasks {
		if err := l.files.Delete(ctx, domain.Artifact{Key: t.key}); err != nil {
			continue
		}
		if _, err := l.db.ExecContext(ctx, `UPDATE deletion_tasks SET status='completed',completed_at=now() WHERE id=$1`, t.id); err != nil {
			return safe(err)
		}
	}
	return nil
}
func (l *Library) RunCleanup(ctx context.Context) error {
	return loop(ctx, func() error { return l.CleanupOnce(ctx) })
}
