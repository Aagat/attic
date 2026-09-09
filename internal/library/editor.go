package library

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"

	"attic/internal/acquisition"
	"attic/internal/application"
	"attic/internal/capture"
	"attic/internal/domain"
)

var ErrEditConflict = errors.New("the reading version changed; reopen the editor")

type ReadingEditor struct {
	HTML     string `json:"html"`
	Revision string `json:"revision"`
}

func (l *Library) ReadingEdit(ctx context.Context, jobID domain.JobID) (application.ArticleDraft, bool, error) {
	var draft application.ArticleDraft
	var raw []byte
	err := l.db.QueryRowContext(ctx, `SELECT draft FROM reading_edits WHERE job_id=$1`, string(jobID)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return draft, false, nil
	}
	if err != nil {
		return draft, false, safe(err)
	}
	if json.Unmarshal(raw, &draft) != nil {
		return draft, false, ErrStorage
	}
	return draft, true, nil
}

func (l *Library) editorDraft(ctx context.Context, id string) (application.ArticleDraft, Detail, error) {
	detail, err := l.Get(ctx, id)
	if err != nil {
		return application.ArticleDraft{}, detail, err
	}
	if detail.Kind != "bookmark" {
		return application.ArticleDraft{}, detail, ErrInvalid
	}
	draft, found, err := l.ReadingEdit(ctx, domain.JobID(detail.JobID))
	if err != nil {
		return draft, detail, err
	}
	if !found && detail.ReadingAvailable {
		err = l.db.QueryRowContext(ctx, `SELECT title,COALESCE(author,''),COALESCE(site_name,''),COALESCE(publication_date::text,''),COALESCE(description,''),COALESCE(detected_language,''),semantic_html FROM content_documents WHERE job_id=$1`, detail.JobID).Scan(&draft.Title, &draft.Author, &draft.SiteName, &draft.PublicationDate, &draft.Description, &draft.Language, &draft.SemanticHTML)
		if err != nil {
			return draft, detail, safe(err)
		}
		found = true
	}
	if !found {
		var selected *Capture
		for index := range detail.Captures {
			c := &detail.Captures[index]
			if c.Status == "complete" || c.Status == "partial" {
				selected = c
				break
			}
		}
		if selected == nil {
			return draft, detail, ErrNotFound
		}
		reader, err := l.files.Open(ctx, selected.Artifact)
		if err != nil {
			return draft, detail, err
		}
		defer reader.Close()
		raw, err := io.ReadAll(io.LimitReader(reader, 64<<20))
		if err != nil {
			return draft, detail, safe(err)
		}
		view, err := capture.ReadingView(raw, detail.URL)
		if err != nil {
			return draft, detail, ErrInvalid
		}
		candidate, err := acquisition.Extract(acquisition.Page{HTML: view, FinalURL: detail.URL, Status: 200})
		if err != nil {
			return draft, detail, ErrInvalid
		}
		draft = application.ArticleDraft{Title: detail.Title, SemanticHTML: candidate.SemanticHTML, Author: candidate.Author, SiteName: candidate.SiteName, PublicationDate: candidate.PublicationDate, Language: candidate.Language}
	}
	draft.Title = detail.Title
	approved, err := application.ApproveReadingEdit(draft)
	if err != nil {
		return draft, detail, ErrInvalid
	}
	draft, _ = approved.Snapshot()
	return draft, detail, nil
}

func editorRevision(draft application.ArticleDraft, detail Detail) string {
	hash := sha256.Sum256([]byte(detail.JobID + ":" + strconv.FormatInt(detail.Version, 10) + ":" + draft.SemanticHTML))
	return hex.EncodeToString(hash[:])
}
func (l *Library) Editor(ctx context.Context, id string) (ReadingEditor, error) {
	draft, detail, err := l.editorDraft(ctx, id)
	return ReadingEditor{HTML: draft.SemanticHTML, Revision: editorRevision(draft, detail)}, err
}

// SaveReading records a new immutable owner-reviewed source and queues its PDF.
// It neither overwrites captures/previous PDFs nor requests delivery.
func (l *Library) SaveReading(ctx context.Context, id, body, revision string) error {
	if len(body) > 16<<20 || revision == "" {
		return ErrInvalid
	}
	draft, detail, err := l.editorDraft(ctx, id)
	if err != nil {
		return err
	}
	if revision != editorRevision(draft, detail) {
		return ErrEditConflict
	}
	draft.SemanticHTML = body
	reviewed, err := application.ApproveReadingEdit(draft)
	if err != nil {
		return ErrInvalid
	}
	draft, _ = reviewed.Snapshot()
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return safe(err)
	}
	defer tx.Rollback()
	var version int64
	if err := tx.QueryRowContext(ctx, `SELECT version FROM saved_items WHERE id=$1 FOR UPDATE`, id).Scan(&version); err != nil {
		return safe(err)
	}
	if version != detail.Version {
		return ErrEditConflict
	}
	jobID := opaque()
	_, err = tx.ExecContext(ctx, `INSERT INTO jobs(id,submitted_url,title_hint,output_profile,correlation_id) VALUES($1,$2,$3,$4,$1)`, jobID, detail.URL, detail.Title, l.profile)
	if err != nil {
		return safe(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO reading_editions(job_id,item_id,language) VALUES($1,$2,$3)`, jobID, id, detail.OutputLanguage); err != nil {
		return safe(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO reading_edits(job_id,draft) VALUES($1,$2)`, jobID, jsonValue(draft)); err != nil {
		return safe(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE saved_items SET job_id=$2,version=version+1,updated_at=now() WHERE id=$1`, id, jobID); err != nil {
		return safe(err)
	}
	return safe(tx.Commit())
}
