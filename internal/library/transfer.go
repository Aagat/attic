package library

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path"
	"strings"
	"time"

	"attic/internal/ai"
	"attic/internal/domain"
)

const maxTransferBytes int64 = 16 << 30
const maxTransferFile int64 = 128 << 20
const maxTransferEntries = 100000

type archiveManifest struct {
	Format     string             `json:"format"`
	CreatedAt  time.Time          `json:"created_at"`
	Items      []archiveItem      `json:"items"`
	Sources    []archiveSource    `json:"browser_sources"`
	Tombstones []archiveTombstone `json:"tombstones"`
}
type archiveItem struct {
	Editions    []archiveEdition `json:"reading_editions,omitempty"`
	Content     json.RawMessage  `json:"approved_content,omitempty"`
	Item        Item             `json:"item"`
	Text        string           `json:"text"`
	SourceTitle string           `json:"source_title"`
	TitleEdited bool             `json:"title_edited"`
	Captures    []archiveCapture `json:"captures"`
	PDF         *domain.Artifact `json:"pdf,omitempty"`
}
type archiveEdition struct {
	JobID    string          `json:"job_id"`
	Language string          `json:"language"`
	Content  json.RawMessage `json:"approved_content,omitempty"`
	PDF      domain.Artifact `json:"pdf"`
}

type archiveCapture struct {
	Capture  Capture         `json:"capture"`
	Artifact domain.Artifact `json:"artifact"`
	Text     string          `json:"text"`
}
type archiveSource struct {
	ClientID string     `json:"client_id"`
	NodeID   string     `json:"node_id"`
	ItemID   *string    `json:"item_id"`
	URL      string     `json:"url"`
	Title    string     `json:"title"`
	Folder   string     `json:"folder"`
	SavedAt  *time.Time `json:"saved_at"`
}
type archiveTombstone struct {
	URLHash   string    `json:"url_hash"`
	DeletedAt time.Time `json:"deleted_at"`
}

// Export writes a portable manifest plus checksum-named files. Credentials, SMTP
// requests and processing leases are deliberately absent from a content backup.
func (l *Library) Export(ctx context.Context, out io.Writer) error {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return safe(err)
	}
	defer tx.Rollback()
	manifest := archiveManifest{Format: "attic-archive-v1", CreatedAt: time.Now().UTC(), Items: []archiveItem{}, Sources: []archiveSource{}, Tombstones: []archiveTombstone{}}
	rows, err := tx.QueryContext(ctx, itemSelect+`ORDER BY i.id`)
	if err != nil {
		return safe(err)
	}
	for rows.Next() {
		item, e := scanItem(rows)
		if e != nil {
			rows.Close()
			return e
		}
		manifest.Items = append(manifest.Items, archiveItem{Item: item, Text: item.Text, Captures: []archiveCapture{}})
		if len(manifest.Items) > maxTransferEntries {
			rows.Close()
			return ErrInvalid
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return safe(err)
	}
	artifacts := map[string]domain.Artifact{}
	for idx := range manifest.Items {
		item := &manifest.Items[idx]
		if err := tx.QueryRowContext(ctx, `SELECT source_title,title_edited FROM saved_items WHERE id=$1`, item.Item.ID).Scan(&item.SourceTitle, &item.TitleEdited); err != nil {
			return safe(err)
		}
		rows, err := tx.QueryContext(ctx, `SELECT id,artifact,final_url,title,text_content,status,missing,created_at,source FROM saved_captures WHERE item_id=$1 ORDER BY created_at,id`, item.Item.ID)
		if err != nil {
			return safe(err)
		}
		for rows.Next() {
			var c archiveCapture
			var raw, missing []byte
			if err := rows.Scan(&c.Capture.ID, &raw, &c.Capture.FinalURL, &c.Capture.Title, &c.Text, &c.Capture.Status, &missing, &c.Capture.CreatedAt, &c.Capture.Source); err != nil {
				rows.Close()
				return safe(err)
			}
			if json.Unmarshal(raw, &c.Artifact) != nil || json.Unmarshal(missing, &c.Capture.Missing) != nil {
				rows.Close()
				return ErrStorage
			}
			item.Captures = append(item.Captures, c)
			artifacts[c.Artifact.Checksum] = c.Artifact
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return safe(err)
		}
		rows, err = tx.QueryContext(ctx, `SELECT j.id,COALESCE(e.language,''),to_jsonb(c),a.storage_relative_path,a.safe_filename,a.media_type,a.byte_size,a.checksum_sha256,a.created_at
 FROM jobs j LEFT JOIN reading_editions e ON e.job_id=j.id LEFT JOIN content_documents c ON c.job_id=j.id
 JOIN LATERAL (SELECT * FROM artifacts WHERE job_id=j.id AND availability='available' ORDER BY created_at DESC LIMIT 1) a ON true
 WHERE (e.item_id=$1 OR j.id=$2) ORDER BY COALESCE(e.language,''),j.created_at,j.id`, item.Item.ID, item.Item.JobID)
		if err != nil {
			return safe(err)
		}
		for rows.Next() {
			var edition archiveEdition
			var content []byte
			a := &edition.PDF
			if err := rows.Scan(&edition.JobID, &edition.Language, &content, &a.Key, &a.Filename, &a.MediaType, &a.ByteSize, &a.Checksum, &a.CreatedAt); err != nil {
				rows.Close()
				return safe(err)
			}
			edition.Content = json.RawMessage(content)
			a.Available = true
			item.Editions = append(item.Editions, edition)
			artifacts[a.Checksum] = *a
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return safe(err)
		}
	}
	rows, err = tx.QueryContext(ctx, `SELECT client_id,node_id,item_id,url,title,folder,saved_at FROM bookmark_sources ORDER BY client_id,node_id`)
	if err != nil {
		return safe(err)
	}
	for rows.Next() {
		var s archiveSource
		if err := rows.Scan(&s.ClientID, &s.NodeID, &s.ItemID, &s.URL, &s.Title, &s.Folder, &s.SavedAt); err != nil {
			rows.Close()
			return safe(err)
		}
		manifest.Sources = append(manifest.Sources, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return safe(err)
	}
	rows, err = tx.QueryContext(ctx, `SELECT url_hash,deleted_at FROM saved_tombstones ORDER BY url_hash`)
	if err != nil {
		return safe(err)
	}
	for rows.Next() {
		var s archiveTombstone
		if err := rows.Scan(&s.URLHash, &s.DeletedAt); err != nil {
			rows.Close()
			return safe(err)
		}
		manifest.Tombstones = append(manifest.Tombstones, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return safe(err)
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if int64(len(manifestBytes)) > maxTransferFile || len(artifacts)+1 > maxTransferEntries {
		return ErrInvalid
	}
	writer := zip.NewWriter(out)
	entry, err := writer.Create("manifest.json")
	if err != nil {
		return err
	}
	if _, err = entry.Write(manifestBytes); err != nil {
		return err
	}
	total := int64(len(manifestBytes))
	for sum, a := range artifacts {
		if a.ByteSize < 0 || a.ByteSize > maxTransferFile {
			return ErrInvalid
		}
		total += a.ByteSize
		if total > maxTransferBytes {
			return ErrInvalid
		}
		file, err := l.files.Open(ctx, a)
		if err != nil {
			return err
		}
		entry, err := writer.Create("files/" + sum)
		if err != nil {
			file.Close()
			return err
		}
		_, err = io.Copy(entry, file)
		file.Close()
		if err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return safe(tx.Commit())
}

// Restore commits one item at a time. Repeating an interrupted restore merges
// existing items/capture IDs and never requests a fetch or an email.
func (l *Library) Restore(ctx context.Context, input io.ReaderAt, size int64) (int, error) {
	manifest, files, err := readArchive(input, size)
	if err != nil {
		return 0, err
	}
	ids := map[string]string{}
	count := 0
	for _, item := range manifest.Items {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		id, err := l.restoreItem(ctx, item, files)
		if err != nil {
			return count, err
		}
		ids[item.Item.ID] = id
		count++
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return count, safe(err)
	}
	defer tx.Rollback()
	for _, s := range manifest.Sources {
		var id *string
		if s.ItemID != nil {
			if mapped, ok := ids[*s.ItemID]; ok {
				id = &mapped
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO bookmark_sources(client_id,node_id,item_id,url,title,folder,saved_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, s.ClientID, s.NodeID, id, s.URL, s.Title, s.Folder, s.SavedAt)
		if err != nil {
			return count, safe(err)
		}
	}
	for _, s := range manifest.Tombstones {
		_, err = tx.ExecContext(ctx, `INSERT INTO saved_tombstones(url_hash,deleted_at) VALUES($1,$2) ON CONFLICT DO NOTHING`, s.URLHash, s.DeletedAt)
		if err != nil {
			return count, safe(err)
		}
	}
	return count, safe(tx.Commit())
}
func readArchive(input io.ReaderAt, size int64) (archiveManifest, map[string]*zip.File, error) {
	var manifest archiveManifest
	if size <= 0 || size > maxTransferBytes {
		return manifest, nil, ErrInvalid
	}
	reader, err := zip.NewReader(input, size)
	if err != nil {
		return manifest, nil, ErrInvalid
	}
	if len(reader.File) > maxTransferEntries {
		return manifest, nil, ErrInvalid
	}
	files := map[string]*zip.File{}
	var total uint64
	for _, f := range reader.File {
		if f.Name != path.Clean(f.Name) || strings.Contains(f.Name, "\\") || !f.Mode().IsRegular() {
			return manifest, nil, ErrInvalid
		}
		if f.Name != "manifest.json" {
			sum, e := hex.DecodeString(strings.TrimPrefix(f.Name, "files/"))
			if !strings.HasPrefix(f.Name, "files/") || e != nil || len(sum) != sha256.Size {
				return manifest, nil, ErrInvalid
			}
		}
		if f.UncompressedSize64 > uint64(maxTransferFile) || files[f.Name] != nil {
			return manifest, nil, ErrInvalid
		}
		total += f.UncompressedSize64
		if total > uint64(maxTransferBytes) {
			return manifest, nil, ErrInvalid
		}
		files[f.Name] = f
	}
	raw, err := readEntry(files["manifest.json"])
	if err != nil {
		return manifest, nil, err
	}
	if json.Unmarshal(raw, &manifest) != nil || manifest.Format != "attic-archive-v1" || len(manifest.Items) > maxTransferEntries {
		return manifest, nil, ErrInvalid
	}
	seen := map[string]bool{}
	for _, item := range manifest.Items {
		if item.Item.ID == "" || seen[item.Item.ID] || len(item.Item.ID) > 200 || item.Item.Kind != "bookmark" && item.Item.Kind != "pdf" {
			return manifest, nil, ErrInvalid
		}
		seen[item.Item.ID] = true
		if item.Item.Kind == "bookmark" {
			if _, err := normalize(item.Item.URL); err != nil {
				return manifest, nil, ErrInvalid
			}
		}
		for _, c := range item.Captures {
			if c.Capture.ID == "" || len(c.Capture.ID) > 200 {
				return manifest, nil, ErrInvalid
			}
			if err := validateTransferArtifact(c.Artifact, files); err != nil {
				return manifest, nil, err
			}
		}
		editionIDs := map[string]bool{}
		for _, edition := range item.Editions {
			if edition.JobID == "" || len(edition.JobID) > 200 || editionIDs[edition.JobID] || !ai.ValidTargetLanguage(edition.Language) || edition.PDF.MediaType != "application/pdf" {
				return manifest, nil, ErrInvalid
			}
			editionIDs[edition.JobID] = true
			if err := validateTransferArtifact(edition.PDF, files); err != nil {
				return manifest, nil, err
			}
			if edition.Language != "" {
				var content struct {
					Language string `json:"detected_language"`
					HTML     string `json:"semantic_html"`
				}
				if json.Unmarshal(edition.Content, &content) != nil || content.Language != edition.Language || strings.TrimSpace(content.HTML) == "" {
					return manifest, nil, ErrInvalid
				}
			}
		}
		if item.PDF != nil {
			if err := validateTransferArtifact(*item.PDF, files); err != nil {
				return manifest, nil, err
			}
		}
	}
	return manifest, files, nil
}
func validateTransferArtifact(a domain.Artifact, files map[string]*zip.File) error {
	sum, err := hex.DecodeString(a.Checksum)
	if err != nil || len(sum) != sha256.Size || a.ByteSize <= 0 || a.ByteSize > maxTransferFile {
		return ErrInvalid
	}
	f := files["files/"+a.Checksum]
	if f == nil || f.UncompressedSize64 != uint64(a.ByteSize) {
		return ErrInvalid
	}
	return nil
}
func readEntry(f *zip.File) ([]byte, error) {
	if f == nil {
		return nil, ErrInvalid
	}
	reader, err := f.Open()
	if err != nil {
		return nil, ErrInvalid
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxTransferFile+1))
	if err != nil || int64(len(data)) > maxTransferFile {
		return nil, ErrInvalid
	}
	return data, nil
}

func (l *Library) restoreItem(ctx context.Context, entry archiveItem, files map[string]*zip.File) (id string, err error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return "", safe(err)
	}
	defer tx.Rollback()
	var written []domain.Artifact
	committed := false
	defer func() {
		if !committed {
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			for _, a := range written {
				_ = l.files.Delete(cleanup, a)
			}
		}
	}()
	item := entry.Item
	lock := item.ID
	if item.URL != "" {
		lock = item.URL
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lock); err != nil {
		return "", safe(err)
	}
	var existingKind, existingURL, jobID string
	err = tx.QueryRowContext(ctx, `SELECT id,kind,COALESCE(url,''),COALESCE(job_id,'') FROM saved_items WHERE id=$1 OR ($2<>'' AND url=$2) ORDER BY (id=$1) DESC LIMIT 1 FOR UPDATE`, item.ID, item.URL).Scan(&id, &existingKind, &existingURL, &jobID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", safe(err)
	}
	if err == nil && (existingKind != item.Kind || existingURL != item.URL) {
		return "", ErrInvalid
	}
	if errors.Is(err, sql.ErrNoRows) {
		id = item.ID
		captureStatus := item.CaptureStatus
		if captureStatus == "queued" || captureStatus == "capturing" {
			captureStatus = "not_captured"
		}
		var rawURL *string
		if item.URL != "" {
			rawURL = &item.URL
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO saved_items(id,kind,url,title,notes,tags,folders,source_title,title_edited,saved_at,updated_at,text_content,classification,suggested_tags,enrichment_status,capture_status)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, id, item.Kind, rawURL, item.Title, item.Notes, jsonValue(item.Tags), jsonValue(item.Folders), entry.SourceTitle, entry.TitleEdited, item.SavedAt, item.UpdatedAt, entry.Text, item.Classification, jsonValue(item.SuggestedTags), item.EnrichmentStatus, captureStatus)
		if err != nil {
			return "", safe(err)
		}
	}
	put := func(a domain.Artifact, filename, media string) (domain.Artifact, error) {
		data, err := readEntry(files["files/"+a.Checksum])
		if err != nil {
			return domain.Artifact{}, err
		}
		sum := sha256.Sum256(data)
		if int64(len(data)) != a.ByteSize || hex.EncodeToString(sum[:]) != a.Checksum {
			return domain.Artifact{}, ErrInvalid
		}
		stored, err := l.files.Put(ctx, domain.JobID("restore-"+opaque()), filename, data, a.CreatedAt)
		if err != nil {
			return stored, err
		}
		stored.MediaType = media
		written = append(written, stored)
		return stored, nil
	}
	for _, c := range entry.Captures {
		var owner string
		err := tx.QueryRowContext(ctx, `SELECT item_id FROM saved_captures WHERE id=$1`, c.Capture.ID).Scan(&owner)
		if err == nil {
			if owner != id {
				return "", ErrInvalid
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", safe(err)
		}
		artifact, err := put(c.Artifact, "snapshot.html", "text/html")
		if err != nil {
			return "", err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO saved_captures(id,item_id,artifact,final_url,title,text_content,status,missing,created_at,source) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, c.Capture.ID, id, jsonValue(artifact), c.Capture.FinalURL, c.Capture.Title, c.Text, c.Capture.Status, jsonValue(c.Capture.Missing), c.Capture.CreatedAt, captureSource(c.Capture.Source))
		if err != nil {
			return "", safe(err)
		}
	}
	editions := entry.Editions
	if len(editions) == 0 && entry.PDF != nil {
		editions = []archiveEdition{{JobID: entry.Item.JobID, Content: entry.Content, PDF: *entry.PDF}}
	}
	selectedJob := ""
	for _, edition := range editions {
		// Stable restore identities make interrupted or repeated imports idempotent.
		identity := sha256.Sum256([]byte(id + "/" + edition.JobID))
		restoredID := "restore-" + hex.EncodeToString(identity[:16])
		var editionJob string
		err := tx.QueryRowContext(ctx, `SELECT e.job_id FROM reading_editions e WHERE e.item_id=$1 AND e.job_id IN ($2,$3) ORDER BY e.job_id LIMIT 1`, id, edition.JobID, restoredID).Scan(&editionJob)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", safe(err)
		}
		if errors.Is(err, sql.ErrNoRows) {
			filename := edition.PDF.Filename
			if filename == "" {
				filename = "document.pdf"
			}
			a, err := put(edition.PDF, filename, "application/pdf")
			if err != nil {
				return "", err
			}
			editionJob = restoredID
			var submittedURL *string
			source := "upload"
			if item.URL != "" {
				submittedURL = &item.URL
				source = "url"
			}
			profile := l.profile
			if profile == "" {
				profile = "kindle-scribe"
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO jobs(id,source_kind,submitted_url,title_hint,display_title,output_profile,status,correlation_id,created_at,updated_at,completed_at,delivery_pending) VALUES($1,$2,$3,$4,$4,$5,'ready',$1,$6,$6,$6,false)`, editionJob, source, submittedURL, item.Title, profile, a.CreatedAt)
			if err != nil {
				return "", safe(err)
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO artifacts(id,job_id,profile,storage_relative_path,safe_filename,media_type,byte_size,checksum_sha256,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, opaque(), editionJob, profile, a.Key, a.Filename, a.MediaType, a.ByteSize, a.Checksum, a.CreatedAt)
			if err != nil {
				return "", safe(err)
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO reading_editions(job_id,item_id,language) VALUES($1,$2,$3)`, editionJob, id, edition.Language)
			if err != nil {
				return "", safe(err)
			}
			if len(edition.Content) > 0 {
				_, err = tx.ExecContext(ctx, `INSERT INTO content_documents(id,job_id,title,author,site_name,publication_date,description,source_url,semantic_html,plain_text,extraction_method,ai_confidence,ai_completeness,detected_language,created_at,updated_at)
 SELECT $1,$2,c.title,c.author,c.site_name,c.publication_date,c.description,c.source_url,c.semantic_html,c.plain_text,c.extraction_method,c.ai_confidence,c.ai_completeness,c.detected_language,c.created_at,c.updated_at
 FROM jsonb_populate_record(NULL::content_documents,$3::jsonb) c`, opaque(), editionJob, string(edition.Content))
				if err != nil {
					return "", safe(err)
				}
			}
		}
		if selectedJob == "" || edition.JobID == item.JobID {
			selectedJob = editionJob
		}
	}
	// A restore may add editions, but cannot change a current local selection.
	if jobID == "" && selectedJob != "" {
		_, err = tx.ExecContext(ctx, `UPDATE saved_items SET job_id=$2 WHERE id=$1`, id, selectedJob)
		if err != nil {
			return "", safe(err)
		}
	}

	_, err = tx.ExecContext(ctx, `UPDATE saved_items SET indexed_version=0,index_error=false,version=version+1 WHERE id=$1`, id)
	if err != nil {
		return "", safe(err)
	}
	if err = tx.Commit(); err != nil {
		return "", safe(err)
	}
	committed = true
	return id, nil
}

func captureSource(source string) string {
	if source == "browser" {
		return "browser"
	}
	return "server"
}
