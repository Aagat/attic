package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"attic/internal/domain"
	"golang.org/x/net/html"
)

type ImportResult struct {
	Imported int           `json:"imported"`
	Merged   int           `json:"merged"`
	Skipped  int           `json:"skipped"`
	Errors   []ImportError `json:"errors"`
}
type ImportError struct {
	Index   int    `json:"index"`
	Message string `json:"message"`
}

func (l *Library) Import(ctx context.Context, bookmarks []SaveRequest) (ImportResult, error) {
	r := ImportResult{Errors: []ImportError{}}
	if len(bookmarks) > 10000 {
		return r, ErrInvalid
	}
	for i, b := range bookmarks {
		b.Action = "bookmark"
		_, created, err := l.Save(ctx, b, "")
		switch {
		case errors.Is(err, ErrDeleted):
			r.Skipped++
		case errors.Is(err, ErrInvalid):
			r.Skipped++
		case err != nil:
			r.Errors = append(r.Errors, ImportError{i, err.Error()})
		case created:
			r.Imported++
		default:
			r.Merged++
		}
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
	}
	return r, nil
}

// ImportHTML accepts Netscape bookmark exports, preserving their folder hierarchy.
func (l *Library) ImportHTML(ctx context.Context, reader io.Reader) (ImportResult, error) {
	doc, err := html.Parse(io.LimitReader(reader, 10<<20))
	if err != nil {
		return ImportResult{}, ErrInvalid
	}
	bookmarks := []SaveRequest{}
	var walk func(*html.Node, []string)
	walk = func(n *html.Node, folders []string) {
		current := append([]string{}, folders...)
		pending := ""
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && child.Data == "h3" {
				pending = nodeText(child)
				continue
			}
			if child.Type == html.ElementNode && child.Data == "a" {
				b := SaveRequest{Title: nodeText(child), Action: "bookmark", Source: &Source{ClientID: "html-import", Folder: strings.Join(current, "/")}}
				for _, a := range child.Attr {
					switch a.Key {
					case "href":
						b.URL = a.Val
					case "add_date":
						if sec, e := strconv.ParseInt(a.Val, 10, 64); e == nil {
							b.Source.SavedAt = time.Unix(sec, 0)
						}
					case "tags":
						b.Tags = strings.Split(a.Val, ",")
					}
				}
				b.Source.NodeID = urlHash(b.URL + "\n" + b.Source.Folder)
				bookmarks = append(bookmarks, b)
				continue
			}
			next := current
			if child.Type == html.ElementNode && child.Data == "dl" && pending != "" {
				next = append(append([]string{}, current...), pending)
				pending = ""
			}
			walk(child, next)
		}
	}
	walk(doc, nil)
	if len(bookmarks) == 0 {
		return ImportResult{}, ErrInvalid
	}
	return l.Import(ctx, bookmarks)
}
func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

func (l *Library) Upload(ctx context.Context, filename string, data []byte, action, key string) (Item, error) {
	if len(data) == 0 || len(data) > 25_000_000 || !bytes.HasPrefix(data, []byte("%PDF-")) {
		return Item{}, ErrInvalid
	}
	if action != "" && action != "bookmark" && action != "kindle" {
		return Item{}, ErrInvalid
	}
	sum := sha256.Sum256(data)
	checksum := hex.EncodeToString(sum[:])
	var existing string
	err := l.db.QueryRowContext(ctx, `SELECT i.id FROM saved_items i JOIN artifacts a ON a.job_id=i.job_id WHERE i.kind='pdf' AND a.checksum_sha256=$1 LIMIT 1`, checksum).Scan(&existing)
	if err == nil {
		if action == "kindle" {
			if err := l.Send(ctx, existing, key); err != nil {
				return Item{}, err
			}
		}
		d, err := l.Get(ctx, existing)
		return d.Item, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Item{}, safe(err)
	}
	work, err := os.MkdirTemp("", "attic-upload-")
	if err != nil {
		return Item{}, ErrStorage
	}
	defer os.RemoveAll(work)
	input := filepath.Join(work, "document.pdf")
	if err := os.WriteFile(input, data, 0600); err != nil {
		return Item{}, ErrStorage
	}
	commandCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := exec.CommandContext(commandCtx, "pdfinfo", input).Run(); err != nil {
		return Item{}, ErrInvalid
	}
	textFile := filepath.Join(work, "text.txt")
	text := ""
	if exec.CommandContext(commandCtx, "pdftotext", "-enc", "UTF-8", input, textFile).Run() == nil {
		if f, err := os.Open(textFile); err == nil {
			b, _ := io.ReadAll(io.LimitReader(f, 2<<20))
			f.Close()
			text = strings.TrimSpace(string(b))
		}
	}
	id, jobID := opaque(), opaque()
	filename = filepath.Base(strings.ReplaceAll(filename, "\\", "/"))
	if len(filename) > 180 || filename == "." || filename == "" || strings.ContainsAny(filename, "\x00\r\n") {
		filename = "document.pdf"
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".pdf") {
		filename += ".pdf"
	}
	artifact, err := l.files.Put(ctx, domain.JobID(jobID), filename, data, time.Now())
	if err != nil {
		return Item{}, err
	}
	committed := false
	defer func() {
		if !committed {
			l.files.Delete(context.WithoutCancel(ctx), artifact)
		}
	}()
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, safe(err)
	}
	defer tx.Rollback()
	// Serialize duplicate concurrent uploads by content checksum.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, checksum); err != nil {
		return Item{}, safe(err)
	}
	err = tx.QueryRowContext(ctx, `SELECT i.id FROM saved_items i JOIN artifacts a ON a.job_id=i.job_id WHERE i.kind='pdf' AND a.checksum_sha256=$1 LIMIT 1`, checksum).Scan(&existing)
	if err == nil {
		tx.Rollback()
		if action == "kindle" {
			if err := l.Send(ctx, existing, key); err != nil {
				return Item{}, err
			}
		}
		d, err := l.Get(ctx, existing)
		return d.Item, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Item{}, safe(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO jobs(id,source_kind,title_hint,display_title,output_profile,status,correlation_id,completed_at) VALUES($1,'upload',$2,$2,$3,'ready',$1,now())`, jobID, filename, l.profile)
	if err != nil {
		return Item{}, safe(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO artifacts(id,job_id,profile,storage_relative_path,safe_filename,media_type,byte_size,checksum_sha256) VALUES($1,$2,$3,$4,$5,'application/pdf',$6,$7)`, opaque(), jobID, l.profile, artifact.Key, artifact.Filename, artifact.ByteSize, artifact.Checksum)
	if err != nil {
		return Item{}, safe(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO saved_items(id,kind,title,job_id,text_content,capture_status) VALUES($1,'pdf',$2,$3,$4,'not_applicable')`, id, filename, jobID, text)
	if err != nil {
		return Item{}, safe(err)
	}
	if err := tx.Commit(); err != nil {
		return Item{}, safe(err)
	}
	committed = true
	if action == "kindle" {
		if err := l.Send(ctx, id, key); err != nil {
			return Item{}, err
		}
	}
	d, err := l.Get(ctx, id)
	return d.Item, err
}
