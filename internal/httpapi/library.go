package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"attic/internal/application"
	"attic/internal/capture"
	"attic/internal/library"
	"attic/internal/search"
)

func (s *Server) serveLibrary(w http.ResponseWriter, r *http.Request, correlation string) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/v1/items") {
		return false
	}
	if s.library == nil {
		writeError(w, application.NewSafeError("unavailable", 503, "Saved library unavailable"), correlation)
		return true
	}
	w.Header().Set("Cache-Control", "no-store")
	fail := func(err error) {
		code, status, message := "library_unavailable", 503, "Saved library is unavailable"
		switch {
		case errors.Is(err, library.ErrInvalid), errors.Is(err, search.ErrInvalid):
			code, status, message = "invalid_input", 400, "Check the submitted URL, file or fields"
		case errors.Is(err, library.ErrNotFound):
			code, status, message = "not_found", 404, "Saved item was not found"
		case errors.Is(err, library.ErrDeleted):
			code, status, message = "removed", 409, err.Error()
		case errors.Is(err, library.ErrSMTPDisabled):
			code, status, message = "smtp_disabled", 409, err.Error()
		case errors.Is(err, search.ErrUnavailable):
			code, status, message = "search_unavailable", 503, "Search is unavailable; your saved items are safe"
		}
		writeError(w, application.NewSafeError(code, status, message), correlation)
	}
	respond := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(v)
	}
	decode := func(v any) bool {
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
		if json.NewDecoder(r.Body).Decode(v) != nil {
			fail(library.ErrInvalid)
			return false
		}
		return true
	}
	tail := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/items"), "/")
	if tail == "status" {
		if r.Method != "GET" {
			methodNotAllowed(w, "GET", correlation)
			return true
		}
		_, total, err := s.library.List(r.Context(), 1, 0)
		if err != nil {
			fail(err)
			return true
		}
		bytes, err := s.library.StorageBytes(r.Context())
		if err != nil {
			fail(err)
			return true
		}
		respond(200, map[string]any{"total": total, "storage_bytes": bytes, "kindle_configured": s.library.KindleConfigured(), "search_configured": s.search != nil})
		return true
	}
	if tail == "" {
		switch r.Method {
		case "GET":
			q := r.URL.Query()
			limit, _ := strconv.Atoi(q.Get("limit"))
			offset, _ := strconv.Atoi(q.Get("offset"))
			if limit <= 0 || limit > 100 {
				limit = 50
			}
			if offset < 0 {
				fail(library.ErrInvalid)
				return true
			}
			var items []library.Item
			total := 0
			totalEstimated := false
			snippets := map[string]string{}
			if q.Get("kind") != "" || q.Get("q") != "" || q.Get("tag") != "" || q.Get("domain") != "" || q.Get("from") != "" || q.Get("to") != "" || q.Get("capture_status") != "" {
				if s.search == nil {
					fail(search.ErrUnavailable)
					return true
				}
				query := search.Query{Kind: q.Get("kind"), Text: q.Get("q"), Tags: q["tag"], Domain: q.Get("domain"), CaptureStatus: q.Get("capture_status"), Limit: limit, Offset: offset}
				for name, target := range map[string]**time.Time{"from": &query.From, "to": &query.To} {
					if raw := q.Get(name); raw != "" {
						t, err := time.Parse(time.RFC3339, raw)
						if err != nil {
							t, err = time.Parse("2006-01-02", raw)
							if err == nil && name == "to" {
								t = t.Add(24*time.Hour - time.Nanosecond)
							}
						}
						if err != nil {
							fail(search.ErrInvalid)
							return true
						}
						*target = &t
					}
				}
				result, err := s.search.Search(r.Context(), query)
				if err != nil {
					fail(err)
					return true
				}
				total = result.Total
				totalEstimated = result.TotalEstimated
				items = []library.Item{}
				for _, hit := range result.Hits {
					d, err := s.library.Get(r.Context(), hit.Document.ID)
					if errors.Is(err, library.ErrNotFound) {
						continue
					}
					if err != nil {
						fail(err)
						return true
					}
					items = append(items, d.Item)
					snippets[d.ID] = hit.Snippet
				}
			} else {
				var err error
				items, total, err = s.library.List(r.Context(), limit, offset)
				if err != nil {
					fail(err)
					return true
				}
			}
			bytes, err := s.library.StorageBytes(r.Context())
			if err != nil {
				fail(err)
				return true
			}
			type hit struct {
				library.Item
				Snippet string `json:"snippet,omitempty"`
			}
			hits := []hit{}
			for _, i := range items {
				hits = append(hits, hit{i, snippets[i.ID]})
			}
			respond(200, map[string]any{"items": hits, "total": total, "storage_bytes": bytes, "total_estimated": totalEstimated})
			return true
		case "POST":
			var request library.SaveRequest
			if !decode(&request) {
				return true
			}
			item, _, err := s.library.Save(r.Context(), request, r.Header.Get("Idempotency-Key"))
			if err != nil {
				fail(err)
			} else {
				respond(201, item)
			}
			return true
		default:
			methodNotAllowed(w, "GET, POST", correlation)
			return true
		}
	}
	if tail == "import" && r.Method == "POST" {
		var result library.ImportResult
		var err error
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			r.Body = http.MaxBytesReader(w, r.Body, 11<<20)
			if err = r.ParseMultipartForm(1 << 20); err != nil {
				fail(library.ErrInvalid)
				return true
			}
			defer r.MultipartForm.RemoveAll()
			file, _, e := r.FormFile("file")
			if e != nil {
				fail(library.ErrInvalid)
				return true
			}
			defer file.Close()
			result, err = s.library.ImportHTML(r.Context(), file)
		} else {
			var request struct {
				Bookmarks []library.SaveRequest `json:"bookmarks"`
			}
			if !decode(&request) {
				return true
			}
			result, err = s.library.Import(r.Context(), request.Bookmarks)
		}
		if err != nil {
			fail(err)
		} else {
			respond(200, result)
		}
		return true
	}
	if tail == "upload" && r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, 26_000_000)
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			fail(library.ErrInvalid)
			return true
		}
		defer r.MultipartForm.RemoveAll()
		file, header, err := r.FormFile("file")
		if err != nil {
			fail(library.ErrInvalid)
			return true
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, 25_000_001))
		if err != nil {
			fail(err)
			return true
		}
		item, err := s.library.Upload(r.Context(), header.Filename, data, r.FormValue("action"), r.Header.Get("Idempotency-Key"))
		if err != nil {
			fail(err)
		} else {
			respond(201, item)
		}
		return true
	}
	if tail == "export" && (r.Method == "GET" || r.Method == "HEAD") {
		http.NewResponseController(w).SetWriteDeadline(time.Now().Add(30 * time.Minute))
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="attic.zip"`)
		if r.Method == "HEAD" {
			w.WriteHeader(200)
			return true
		}
		if err := s.library.Export(r.Context(), w); err != nil {
			panic(http.ErrAbortHandler)
		}
		return true
	}
	if tail == "restore" && r.Method == "POST" {
		http.NewResponseController(w).SetReadDeadline(time.Now().Add(30 * time.Minute))
		http.NewResponseController(w).SetWriteDeadline(time.Now().Add(30 * time.Minute))
		r.Body = http.MaxBytesReader(w, r.Body, (16<<30)+(1<<20))
		parts, err := r.MultipartReader()
		if err != nil {
			fail(library.ErrInvalid)
			return true
		}
		file, err := parts.NextPart()
		if err != nil || file.FormName() != "file" {
			fail(library.ErrInvalid)
			return true
		}
		defer file.Close()
		n, err := s.library.RestoreUpload(r.Context(), file)
		if err != nil {
			fail(err)
		} else {
			respond(200, map[string]int{"restored": n})
		}
		return true
	}
	if tail == "reindex" && r.Method == "POST" {
		if err := s.library.Reindex(r.Context()); err != nil {
			fail(err)
		} else {
			respond(202, map[string]string{"status": "pending"})
		}
		return true
	}
	parts := strings.Split(tail, "/")
	id := parts[0]
	if len(parts) == 3 && parts[1] == "captures" && (r.Method == "GET" || r.Method == "HEAD") {
		body, err := s.library.OpenCapture(r.Context(), id, parts[2])
		if err != nil {
			fail(err)
			return true
		}
		defer body.Close()
		if r.URL.Query().Get("view") == "reader" {
			detail, err := s.library.Get(r.Context(), id)
			if err != nil {
				fail(err)
				return true
			}
			raw, err := io.ReadAll(io.LimitReader(body, 64<<20))
			if err != nil {
				fail(err)
				return true
			}
			html, err := capture.ReadingView(raw, detail.URL)
			if err != nil {
				writeError(w, application.NewSafeError("reading_view_unavailable", 422, "A cleaned reading view is unavailable for this capture. Open its original layout."), correlation)
				return true
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", capture.ReplayCSP)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "no-referrer")
			if r.Method == "GET" {
				w.Write(html)
			}
			return true
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", capture.ReplayCSP)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method == "GET" {
			io.Copy(w, body)
		}
		return true
	}
	if len(parts) == 2 && r.Method == "POST" {
		var err error
		switch parts[1] {
		case "send":
			err = s.library.Send(r.Context(), id, r.Header.Get("Idempotency-Key"))
		case "enrich":
			err = s.library.RetryEnrichment(r.Context(), id)
		case "recapture":
			err = s.library.Recapture(r.Context(), id)
		default:
			fail(library.ErrNotFound)
			return true
		}
		if err != nil {
			fail(err)
		} else {
			d, e := s.library.Get(r.Context(), id)
			if e != nil {
				fail(e)
			} else {
				respond(202, d)
			}
		}
		return true
	}
	if len(parts) == 1 {
		switch r.Method {
		case "GET":
			d, err := s.library.Get(r.Context(), id)
			if err != nil {
				fail(err)
			} else {
				respond(200, d)
			}
		case "PUT":
			var request library.SaveRequest
			if !decode(&request) {
				return true
			}
			if err := s.library.Edit(r.Context(), id, request); err != nil {
				fail(err)
			} else {
				respond(200, map[string]string{"status": "saved"})
			}
		case "DELETE":
			if err := s.library.Delete(r.Context(), id); err != nil {
				fail(err)
			} else {
				w.WriteHeader(204)
			}
		default:
			methodNotAllowed(w, "GET, PUT, DELETE", correlation)
		}
		return true
	}
	fail(library.ErrNotFound)
	return true
}
