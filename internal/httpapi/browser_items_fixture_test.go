package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"attic/internal/application"
)

// browserItemsFixture exposes a saved-item collection around the in-memory
// document fixture. It keeps reader/PWA checks independent of PostgreSQL and AI;
// production saved-item routes have their own integration coverage.
func browserItemsFixture(t *testing.T, server *Server, archive *application.Archive, next http.Handler) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/items" || !server.authorized(r) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var body struct {
				URL    string `json:"url"`
				Action string `json:"action"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				http.Error(w, "invalid body", 400)
				return
			}
			if body.Action != "bookmark" {
				t.Errorf("shared link defaulted to %q instead of bookmarking", body.Action)
			}
			accepted, err := archive.SubmitURL(r.Context(), application.SubmitURLRequest{URL: body.URL}, r.Header.Get("Idempotency-Key"))
			if err != nil {
				t.Error(err)
				http.Error(w, "save failed", 500)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"id": accepted.ID})
			return
		}
		page, err := archive.ListJobs(r.Context(), application.ListJobsRequest{Limit: 50})
		if err != nil {
			t.Error(err)
			http.Error(w, "list failed", 500)
			return
		}
		items := make([]map[string]any, 0, len(page.Items))
		for _, job := range page.Items {
			items = append(items, map[string]any{"id": job.ID, "job_id": job.ID, "kind": "web", "title": job.Title, "url": "https://" + job.SourceHost + "/article", "saved_at": job.CreatedAt, "has_pdf": job.HasArtifact, "pdf_status": job.Status, "capture_status": "pending", "index_status": "pending", "delivery_status": "not_requested"})
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items, "total": len(items), "index_status": "pending"})
	})
}
