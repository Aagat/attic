// Package library owns lasting saved items and their independently retryable outputs.
package library

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"attic/internal/domain"
	"attic/internal/filesystem"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var ErrNotFound = errors.New("saved item not found")
var ErrInvalid = errors.New("invalid saved item")
var ErrDeleted = errors.New("bookmark was explicitly removed from Attic")
var ErrSMTPDisabled = errors.New("Kindle email delivery is not configured; the item is saved")
var ErrStorage = errors.New("saved item storage unavailable")

type Source struct {
	ClientID string    `json:"client_id"`
	NodeID   string    `json:"node_id"`
	Folder   string    `json:"folder"`
	SavedAt  time.Time `json:"saved_at"`
}
type SaveRequest struct {
	URL    string   `json:"url"`
	Title  string   `json:"title"`
	Notes  string   `json:"notes"`
	Tags   []string `json:"tags"`
	Action string   `json:"action"`
	Source *Source  `json:"source,omitempty"`
}
type Item struct {
	TextAvailable    bool      `json:"text_available"`
	ID               string    `json:"id"`
	Kind             string    `json:"kind"`
	URL              string    `json:"url"`
	Title            string    `json:"title"`
	Notes            string    `json:"notes"`
	Tags             []string  `json:"tags"`
	Folders          []string  `json:"folders"`
	SavedAt          time.Time `json:"saved_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Classification   string    `json:"classification"`
	SuggestedTags    []string  `json:"suggested_tags"`
	EnrichmentStatus string    `json:"enrichment_status"`
	CaptureStatus    string    `json:"capture_status"`
	IndexStatus      string    `json:"index_status"`
	JobID            string    `json:"job_id,omitempty"`
	PDFStatus        string    `json:"pdf_status"`
	DeliveryStatus   string    `json:"delivery_status"`
	HasPDF           bool      `json:"has_pdf"`
	Text             string    `json:"-"`
	Version          int64     `json:"-"`
}
type Capture struct {
	Source    string          `json:"source"`
	ID        string          `json:"id"`
	FinalURL  string          `json:"final_url"`
	Title     string          `json:"title"`
	Status    string          `json:"status"`
	Missing   []string        `json:"missing"`
	CreatedAt time.Time       `json:"created_at"`
	Artifact  domain.Artifact `json:"-"`
	Text      string          `json:"-"`
}
type Detail struct {
	OutputLanguage   string `json:"output_language"`
	ReadingAvailable bool   `json:"reading_available"`
	Item
	Captures []Capture `json:"captures"`
}
type Options struct{ DatabaseURL, ArtifactRoot, Profile, Destination string }
type Library struct {
	db                   *sql.DB
	files                *filesystem.Store
	profile, destination string
	root                 string
}

func Open(ctx context.Context, o Options) (*Library, error) {
	db, err := sql.Open("pgx", o.DatabaseURL)
	if err != nil {
		return nil, ErrStorage
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	files, err := filesystem.New(o.ArtifactRoot)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, ErrStorage
	}
	return &Library{db: db, files: files, profile: o.Profile, destination: o.Destination, root: o.ArtifactRoot}, nil
}
func (l *Library) Close() error { return l.db.Close() }
func opaque() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func urlHash(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func normalize(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", ErrInvalid
	}
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}
func jsonValue(v any) string { b, _ := json.Marshal(v); return string(b) }
func cleanTags(tags []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" && len(tag) <= 100 && !seen[tag] && len(out) < 50 {
			out = append(out, tag)
			seen[tag] = true
		}
	}
	return out
}
func safe(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return ErrStorage
}

// Save persists first. Browser ingestion is always bookmark-only and respects tombstones.
func (l *Library) Save(ctx context.Context, r SaveRequest, key string) (Item, bool, error) {
	normalized, err := normalize(r.URL)
	if err != nil {
		return Item{}, false, err
	}
	if len(r.Title) > 2000 || len(r.Notes) > 20000 || len(r.Tags) > 50 {
		return Item{}, false, ErrInvalid
	}
	if r.Action == "" {
		r.Action = "bookmark"
	}
	if r.Action != "bookmark" && r.Action != "kindle" {
		return Item{}, false, ErrInvalid
	}
	if r.Source != nil && (r.Source.ClientID == "" || r.Source.NodeID == "" || len(r.Source.ClientID) > 200 || len(r.Source.NodeID) > 200 || len(r.Source.Folder) > 4000) {
		return Item{}, false, ErrInvalid
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, false, ErrStorage
	}
	defer tx.Rollback()
	// Serializes save/delete of the same URL, including tombstone decisions.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, normalized); err != nil {
		return Item{}, false, safe(err)
	}
	if r.Source != nil {
		var exists bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM saved_tombstones WHERE url_hash=$1)`, urlHash(normalized)).Scan(&exists)
		if err != nil {
			return Item{}, false, safe(err)
		}
		if exists {
			return Item{}, false, ErrDeleted
		}
		r.Action = "bookmark"
	}
	id := opaque()
	created := true
	saved := time.Now().UTC()
	if r.Source != nil && !r.Source.SavedAt.IsZero() {
		saved = r.Source.SavedAt
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO saved_items(id,kind,url,title,source_title,notes,tags,saved_at) VALUES($1,'bookmark',$2,$3,$3,$4,$5,$6) ON CONFLICT(url) DO NOTHING RETURNING id`, id, normalized, r.Title, r.Notes, jsonValue(cleanTags(r.Tags)), saved).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		created = false
		err = tx.QueryRowContext(ctx, `SELECT id FROM saved_items WHERE url=$1 FOR UPDATE`, normalized).Scan(&id)
	}
	if err != nil {
		return Item{}, false, safe(err)
	}
	if r.Source == nil {
		_, err = tx.ExecContext(ctx, `DELETE FROM saved_tombstones WHERE url_hash=$1`, urlHash(normalized))
	} else {
		var priorItem string
		priorErr := tx.QueryRowContext(ctx, `SELECT COALESCE(item_id,'') FROM bookmark_sources WHERE client_id=$1 AND node_id=$2`, r.Source.ClientID, r.Source.NodeID).Scan(&priorItem)
		if priorErr != nil && !errors.Is(priorErr, sql.ErrNoRows) {
			return Item{}, false, safe(priorErr)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO bookmark_sources(client_id,node_id,item_id,url,title,folder,saved_at) VALUES($1,$2,$3,$4,$5,$6,$7)
 ON CONFLICT(client_id,node_id) DO UPDATE SET item_id=excluded.item_id,url=excluded.url,title=excluded.title,folder=excluded.folder,saved_at=excluded.saved_at`, r.Source.ClientID, r.Source.NodeID, id, normalized, r.Title, r.Source.Folder, saved)
		if err == nil && priorItem != "" && priorItem != id {
			_, err = tx.ExecContext(ctx, `UPDATE saved_items SET folders=COALESCE((SELECT jsonb_agg(DISTINCT folder) FROM bookmark_sources WHERE item_id=$1 AND folder<>''),'[]'),version=version+1,updated_at=now() WHERE id=$1`, priorItem)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE saved_items SET source_title=$2,title=CASE WHEN title_edited THEN title ELSE COALESCE(NULLIF($2,''),title) END,
 folders=COALESCE((SELECT jsonb_agg(DISTINCT folder) FROM bookmark_sources WHERE item_id=$1 AND folder<>''),'[]'),version=version+1,updated_at=now() WHERE id=$1`, id, r.Title)
		}
	}
	if err != nil {
		return Item{}, false, safe(err)
	}
	if err = tx.Commit(); err != nil {
		return Item{}, false, safe(err)
	}
	if r.Action == "kindle" {
		if err = l.Send(ctx, id, key); err != nil {
			item, _ := l.Get(ctx, id)
			return item.Item, created, err
		}
	}
	item, err := l.Get(ctx, id)
	return item.Item, created, err
}

const itemSelect = `SELECT i.id,i.kind,COALESCE(i.url,''),i.title,i.notes,i.tags,i.folders,i.saved_at,i.updated_at,i.classification,i.suggested_tags,i.enrichment_status,i.capture_status,
 CASE WHEN i.indexed_version=i.version THEN 'indexed' WHEN i.index_error THEN 'retrying' ELSE 'pending' END,COALESCE(i.job_id,''),COALESCE(j.status,'not_requested'),
 CASE WHEN j.status='delivered' THEN 'accepted' WHEN j.status='delivery_failed' THEN 'failed' WHEN j.delivery_pending OR (j.delivery_destination IS NOT NULL AND j.status IN ('queued','processing','failed')) THEN 'pending' ELSE 'not_requested' END,
 EXISTS(SELECT 1 FROM artifacts a WHERE a.job_id=i.job_id AND a.availability='available'),i.text_content,i.version FROM saved_items i LEFT JOIN jobs j ON j.id=i.job_id `

type scanner interface{ Scan(...any) error }

func scanItem(row scanner) (Item, error) {
	var i Item
	var tags, folders, suggested []byte
	err := row.Scan(&i.ID, &i.Kind, &i.URL, &i.Title, &i.Notes, &tags, &folders, &i.SavedAt, &i.UpdatedAt, &i.Classification, &suggested, &i.EnrichmentStatus, &i.CaptureStatus, &i.IndexStatus, &i.JobID, &i.PDFStatus, &i.DeliveryStatus, &i.HasPDF, &i.Text, &i.Version)
	json.Unmarshal(tags, &i.Tags)
	json.Unmarshal(folders, &i.Folders)
	json.Unmarshal(suggested, &i.SuggestedTags)
	i.TextAvailable = strings.TrimSpace(i.Text) != ""
	return i, safe(err)
}
func (l *Library) Get(ctx context.Context, id string) (Detail, error) {
	i, err := scanItem(l.db.QueryRowContext(ctx, itemSelect+`WHERE i.id=$1`, id))
	if err != nil {
		return Detail{}, err
	}
	d := Detail{Item: i, Captures: []Capture{}}
	if err := l.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT language FROM reading_editions WHERE job_id=$1),''), (EXISTS(SELECT 1 FROM content_documents WHERE job_id=$1) OR EXISTS(SELECT 1 FROM reading_edits WHERE job_id=$1))`, i.JobID).Scan(&d.OutputLanguage, &d.ReadingAvailable); err != nil {
		return d, safe(err)
	}
	rows, err := l.db.QueryContext(ctx, `SELECT id,artifact,final_url,title,text_content,status,missing,created_at,source FROM saved_captures WHERE item_id=$1 ORDER BY created_at DESC`, id)
	if err != nil {
		return d, safe(err)
	}
	defer rows.Close()
	for rows.Next() {
		var c Capture
		var artifact, missing []byte
		if err := rows.Scan(&c.ID, &artifact, &c.FinalURL, &c.Title, &c.Text, &c.Status, &missing, &c.CreatedAt, &c.Source); err != nil {
			return d, safe(err)
		}
		json.Unmarshal(artifact, &c.Artifact)
		json.Unmarshal(missing, &c.Missing)
		d.Captures = append(d.Captures, c)
	}
	return d, safe(rows.Err())
}

// Lookup uses the same normalized URL identity as saving, independent of indexing.
func (l *Library) Lookup(ctx context.Context, raw string) ([]Item, error) {
	url, err := normalize(raw)
	if err != nil {
		return nil, err
	}
	item, err := scanItem(l.db.QueryRowContext(ctx, itemSelect+`WHERE i.url=$1`, url))
	if errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return []Item{}, nil
	}
	if err != nil {
		return nil, err
	}
	return []Item{item}, nil
}

func (l *Library) List(ctx context.Context, limit, offset int) ([]Item, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := l.db.QueryRowContext(ctx, `SELECT count(*) FROM saved_items`).Scan(&total); err != nil {
		return nil, 0, safe(err)
	}
	rows, err := l.db.QueryContext(ctx, itemSelect+`ORDER BY i.saved_at DESC,i.id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, safe(err)
	}
	defer rows.Close()
	items := []Item{}
	for rows.Next() {
		i, err := scanItem(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, i)
	}
	return items, total, safe(rows.Err())
}
func (l *Library) Edit(ctx context.Context, id string, r SaveRequest) error {
	if len(r.Title) > 2000 || len(r.Notes) > 20000 {
		return ErrInvalid
	}
	result, err := l.db.ExecContext(ctx, `UPDATE saved_items SET title=$2,notes=$3,tags=$4,title_edited=true,updated_at=now(),version=version+1 WHERE id=$1`, id, r.Title, r.Notes, jsonValue(cleanTags(r.Tags)))
	if err != nil {
		return safe(err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (l *Library) Recapture(ctx context.Context, id string) error {
	result, err := l.db.ExecContext(ctx, `UPDATE saved_items SET capture_status='queued',capture_attempts=0,capture_next=now(),version=version+1 WHERE id=$1 AND kind='bookmark' AND capture_status<>'capturing'`, id)
	if err != nil {
		return safe(err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrInvalid
	}
	return nil
}

// Send deduplicates retries of the same user action, then reuses an existing PDF.
func (l *Library) Send(ctx context.Context, id, key string) error {
	if l.destination == "" {
		return ErrSMTPDisabled
	}
	return l.prepare(ctx, id, key, l.destination, "", nil)
}

// GeneratePDF prepares a reading document without requesting email delivery.
func (l *Library) GeneratePDF(ctx context.Context, id, key string) error {
	return l.prepare(ctx, id, key, "", "", nil)
}

func (l *Library) prepare(ctx context.Context, id, key, destination, expectedJob string, selection *string) error {
	if key == "" {
		key = opaque()
	}
	if len(key) > 200 {
		return ErrInvalid
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return safe(err)
	}
	defer tx.Rollback()
	var jobID, raw, title, kind string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(job_id,''),COALESCE(url,''),title,kind FROM saved_items WHERE id=$1 FOR UPDATE`, id).Scan(&jobID, &raw, &title, &kind)
	if err != nil {
		return safe(err)
	}
	if expectedJob != "" && jobID != expectedJob {
		return nil
	}
	var actionID string
	err = tx.QueryRowContext(ctx, `INSERT INTO item_actions(key,item_id) VALUES($1,$2) ON CONFLICT DO NOTHING RETURNING key`, key, id).Scan(&actionID)
	if errors.Is(err, sql.ErrNoRows) {
		var existingItem string
		if err := tx.QueryRowContext(ctx, `SELECT item_id FROM item_actions WHERE key=$1`, key).Scan(&existingItem); err != nil {
			return safe(err)
		}
		if existingItem != id {
			return ErrInvalid
		}
		return nil
	}
	if err != nil {
		return safe(err)
	}
	var language string
	if jobID != "" {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT language FROM reading_editions WHERE job_id=$1),'')`, jobID).Scan(&language); err != nil {
			return safe(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO reading_editions(job_id,item_id,language) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, jobID, id, language); err != nil {
			return safe(err)
		}
	}
	if selection != nil {
		if kind != "bookmark" {
			return ErrInvalid
		}
		language = *selection
		jobID = ""
		err := tx.QueryRowContext(ctx, `SELECT e.job_id FROM reading_editions e JOIN jobs j ON j.id=e.job_id WHERE e.item_id=$1 AND e.language=$2 ORDER BY j.created_at DESC LIMIT 1`, id, language).Scan(&jobID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return safe(err)
		}
		if language == "" {
			if _, err := tx.ExecContext(ctx, `UPDATE saved_items SET job_id=NULLIF($2,''),version=version+1,updated_at=now() WHERE id=$1`, id, jobID); err != nil {
				return safe(err)
			}
			return safe(tx.Commit())
		}
	}
	var hasPDF bool
	if jobID != "" {
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM artifacts WHERE job_id=$1 AND availability='available')`, jobID).Scan(&hasPDF)
		if err != nil {
			return safe(err)
		}
	}
	if hasPDF {
		if destination != "" {
			_, err = tx.ExecContext(ctx, `UPDATE jobs SET status='ready',delivery_pending=true,delivery_destination=$2,delivery_retry_count=0,next_attempt_at=now(),failure_category=NULL,failure_message=NULL,version=version+1 WHERE id=$1 AND status NOT IN ('processing','delivering')`, jobID, destination)
		}
	} else {
		var active bool
		if jobID != "" {
			err = tx.QueryRowContext(ctx, `SELECT status IN ('queued','processing') FROM jobs WHERE id=$1`, jobID).Scan(&active)
			if err != nil {
				return safe(err)
			}
		}
		if !active {
			if kind != "bookmark" {
				return ErrInvalid
			}
			previousJobID := jobID
			jobID = opaque()
			_, err = tx.ExecContext(ctx, `INSERT INTO jobs(id,submitted_url,title_hint,output_profile,correlation_id,delivery_destination) VALUES($1,$2,$3,$4,$1,NULLIF($5,''))`, jobID, raw, title, l.profile, destination)
			if err == nil {
				_, err = tx.ExecContext(ctx, `INSERT INTO reading_editions(job_id,item_id,language) VALUES($1,$2,$3)`, jobID, id, language)
			}
			if err == nil {
				_, err = tx.ExecContext(ctx, `INSERT INTO reading_edits(job_id,draft) SELECT $1,draft FROM reading_edits WHERE job_id=$2`, jobID, previousJobID)
			}
		} else if destination != "" {
			// A Kindle request may join PDF generation already in progress.
			_, err = tx.ExecContext(ctx, `UPDATE jobs SET delivery_destination=$2 WHERE id=$1`, jobID, destination)
		}
	}
	if err != nil {
		return safe(err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE saved_items SET job_id=$2,version=version+1,updated_at=now() WHERE id=$1`, id, jobID)
	if err != nil {
		return safe(err)
	}
	return safe(tx.Commit())
}

// KindleConfigured reports whether explicit deliveries have a destination.
func (l *Library) KindleConfigured() bool { return l.destination != "" }
