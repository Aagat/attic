package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"attic/internal/application"
	"attic/internal/domain"
	"attic/internal/memory"
)

func testServer(t *testing.T) (*Server, *application.Archive, *application.Worker) {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC) }
	store := memory.NewStoreWithClock(now)
	artifacts := memory.NewArtifactStore()
	archive, err := application.NewArchive(store, artifacts, application.ArchiveOptions{
		MinContentChars: 10,
		Now:             now,
		NewJobID:        func() domain.JobID { return "job-http" },
		NewContentID:    func() domain.ContentID { return "content-http" },
	})
	if err != nil {
		t.Fatal(err)
	}
	archive.SetProcessor(application.ProcessorFunc(func(ctx context.Context, job domain.Job, pc application.ProcessorContext) (application.ProcessResult, error) {
		if err := pc.SetStage(domain.StageExtracting); err != nil {
			return application.ProcessResult{}, err
		}
		if err := pc.SetStage(domain.StageAIAnalyzing); err != nil {
			return application.ProcessResult{}, err
		}
		article, err := pc.ApproveArticle(application.ArticleDraft{
			Classification: "article",
			Decision:       "accept_candidate",
			Title:          "HTTP article",
			PlainText:      "Enough readable text",
			SemanticHTML:   "<article><p>Enough readable text</p></article>",
			AIConfidence:   0.8,
			AIAttemptID:    "ai-attempt-http",
		})
		if err != nil {
			return application.ProcessResult{}, err
		}
		if err := pc.SetStage(domain.StageFormatting); err != nil {
			return application.ProcessResult{}, err
		}
		return application.ProcessResult{Article: article, PDF: []byte("pdf")}, nil
	}))
	worker, err := application.NewWorker(archive, application.WorkerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(archive, memory.NewReadiness(true), "secret-token"), archive, worker
}

func request(server http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	return requestFrom(server, method, path, body, token, "192.0.2.1:1234", "")
}

func requestFrom(server http.Handler, method, path, body, token, remoteAddr, forwardedFor string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = remoteAddr
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
		req.Header.Set("Forwarded", "for="+forwardedFor)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	return recorder
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(delta time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(delta)
	c.mu.Unlock()
}

func TestRoutesRequireBearerAuthentication(t *testing.T) {
	server, _, _ := testServer(t)
	response := request(server, http.MethodPost, "/api/v1/jobs", `{"url":"https://example.test/a"}`, "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	if !strings.Contains(response.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("body = %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "secret-token") {
		t.Fatal("secret leaked in error")
	}
}

func TestPollAndDownloadRoutes(t *testing.T) {
	server, archive, worker := testServer(t)
	accepted, err := archive.SubmitURL(context.Background(), application.SubmitURLRequest{URL: "https://example.test/article?token=secret"}, "")
	if err != nil {
		t.Fatal(err)
	}
	response := request(server, http.MethodGet, "/api/v1/jobs/"+string(accepted.ID), "", "secret-token")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"queued"`) {
		t.Fatalf("inspect status = %d, body %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "secret") {
		t.Fatal("query value leaked from URL")
	}

	claimed, err := worker.RunOnce(context.Background())
	if err != nil || !claimed {
		t.Fatalf("worker claimed %v, err %v", claimed, err)
	}
	response = request(server, http.MethodGet, "/api/v1/jobs/"+string(accepted.ID)+"/artifact", "", "secret-token")
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/pdf" || response.Body.String() != "pdf" {
		t.Fatalf("artifact response = %d %q headers %#v", response.Code, response.Body.String(), response.Header())
	}
}

func TestListAndHealthRoutes(t *testing.T) {
	server, _, _ := testServer(t)
	response := request(server, http.MethodGet, "/api/v1/jobs?limit=25", "", "secret-token")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"items"`) {
		t.Fatalf("list response = %d %s", response.Code, response.Body.String())
	}
	response = request(server, http.MethodGet, "/health/live", "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("live status = %d", response.Code)
	}
	response = request(server, http.MethodGet, "/health/ready", "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("ready status = %d", response.Code)
	}
	response = request(server, http.MethodPost, "/health/live", "", "")
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET" {
		t.Fatalf("live method status = %d allow %q", response.Code, response.Header().Get("Allow"))
	}
}

func TestUnknownJobUsesSafeEnvelope(t *testing.T) {
	server, _, _ := testServer(t)
	response := request(server, http.MethodGet, "/api/v1/jobs/unknown", "", "secret-token")
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"not_found"`) {
		t.Fatalf("unknown job = %d %s", response.Code, response.Body.String())
	}
	if strings.ContainsAny(response.Body.String(), "\n\t") && strings.Contains(response.Body.String(), "stack") {
		t.Fatal("unexpected implementation detail in error")
	}
}

func TestDownloadDoesNotNeedJSONBodyAndRejectsMissingArtifact(t *testing.T) {
	server, _, _ := testServer(t)
	response := request(server, http.MethodGet, "/api/v1/jobs/missing/artifact", "", "secret-token")
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing artifact = %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Header().Get("Content-Type"), "application/pdf") {
		t.Fatal("missing artifact returned PDF content type")
	}
}

func TestFailedAuthenticationIsThrottledAfterThreshold(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)}
	base, _, _ := testServer(t)
	server := NewServerWithOptions(base.archive, base.readiness, base.bearerToken, Options{
		Now:                   clock.Now,
		AuthFailureLimit:      3,
		AuthFailureWindow:     time.Minute,
		AuthFailureMaxEntries: 8,
	})

	for i := 0; i < 3; i++ {
		response := requestFrom(server, http.MethodGet, "/api/v1/jobs", "", "wrong-token", "198.51.100.10:1234", "")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d status = %d, want 401", i+1, response.Code)
		}
	}
	response := requestFrom(server, http.MethodGet, "/api/v1/jobs", "", "wrong-token", "198.51.100.10:1234", "")
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("throttled status = %d, want 429", response.Code)
	}
	if response.Header().Get("Retry-After") != "60" {
		t.Fatalf("Retry-After = %q, want 60", response.Header().Get("Retry-After"))
	}
	if !strings.Contains(response.Body.String(), `"code":"rate_limited"`) || strings.Contains(response.Body.String(), "198.51.100.10") || strings.Contains(response.Body.String(), "wrong-token") {
		t.Fatalf("unsafe throttle response = %s", response.Body.String())
	}
}

func TestAuthenticationWindowResets(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)}
	base, _, _ := testServer(t)
	server := NewServerWithOptions(base.archive, base.readiness, base.bearerToken, Options{
		Now:               clock.Now,
		AuthFailureLimit:  2,
		AuthFailureWindow: time.Minute,
	})

	for i := 0; i < 2; i++ {
		if response := requestFrom(server, http.MethodGet, "/api/v1/jobs", "", "wrong-token", "198.51.100.11:1234", ""); response.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d status = %d, want 401", i+1, response.Code)
		}
	}
	if response := requestFrom(server, http.MethodGet, "/api/v1/jobs", "", "wrong-token", "198.51.100.11:1234", ""); response.Code != http.StatusTooManyRequests {
		t.Fatalf("pre-reset status = %d, want 429", response.Code)
	}
	clock.Advance(time.Minute)
	if response := requestFrom(server, http.MethodGet, "/api/v1/jobs", "", "wrong-token", "198.51.100.11:1234", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("post-reset status = %d, want 401", response.Code)
	}
}

func TestAuthenticationLimiterIsolatesRemoteIPsAndIgnoresForwardedHeaders(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)}
	base, _, _ := testServer(t)
	server := NewServerWithOptions(base.archive, base.readiness, base.bearerToken, Options{
		Now:               clock.Now,
		AuthFailureLimit:  2,
		AuthFailureWindow: time.Minute,
	})
	for i := 0; i < 2; i++ {
		if response := requestFrom(server, http.MethodGet, "/api/v1/jobs", "", "wrong-token", "198.51.100.20:1234", ""); response.Code != http.StatusUnauthorized {
			t.Fatalf("IP A failure %d status = %d, want 401", i+1, response.Code)
		}
	}
	if response := requestFrom(server, http.MethodGet, "/api/v1/jobs", "", "wrong-token", "198.51.100.20:1234", ""); response.Code != http.StatusTooManyRequests {
		t.Fatalf("IP A status = %d, want 429", response.Code)
	}
	if response := requestFrom(server, http.MethodGet, "/api/v1/jobs", "", "wrong-token", "198.51.100.21:1234", "198.51.100.20"); response.Code != http.StatusUnauthorized {
		t.Fatalf("IP B with forwarded IP status = %d, want 401", response.Code)
	}
}

func TestSuccessfulAuthenticationClearsRemoteFailures(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)}
	base, _, _ := testServer(t)
	server := NewServerWithOptions(base.archive, base.readiness, base.bearerToken, Options{
		Now:               clock.Now,
		AuthFailureLimit:  2,
		AuthFailureWindow: time.Minute,
	})
	for i := 0; i < 2; i++ {
		if response := requestFrom(server, http.MethodGet, "/api/v1/jobs", "", "wrong-token", "198.51.100.30:1234", ""); response.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d status = %d, want 401", i+1, response.Code)
		}
	}
	if response := requestFrom(server, http.MethodGet, "/api/v1/jobs", "", "secret-token", "198.51.100.30:1234", ""); response.Code != http.StatusOK {
		t.Fatalf("successful authentication status = %d, body = %s", response.Code, response.Body.String())
	}
	if response := requestFrom(server, http.MethodGet, "/api/v1/jobs", "", "wrong-token", "198.51.100.30:1234", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("post-success failure status = %d, want 401", response.Code)
	}
}

func TestAuthenticationLimiterBoundsEntriesAndCleansExpiredWindows(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)}
	base, _, _ := testServer(t)
	server := NewServerWithOptions(base.archive, base.readiness, base.bearerToken, Options{
		Now:                   clock.Now,
		AuthFailureLimit:      1,
		AuthFailureWindow:     time.Minute,
		AuthFailureMaxEntries: 2,
	})
	for _, remote := range []string{"198.51.100.40:1234", "198.51.100.41:1234", "198.51.100.42:1234"} {
		if response := requestFrom(server, http.MethodGet, "/api/v1/jobs", "", "wrong-token", remote, ""); response.Code != http.StatusUnauthorized {
			t.Fatalf("remote %s status = %d, want 401", remote, response.Code)
		}
	}
	if got := len(server.authFailures.entries); got > 2 {
		t.Fatalf("limiter entries = %d, want at most 2", got)
	}
	clock.Advance(time.Minute)
	if response := requestFrom(server, http.MethodGet, "/api/v1/jobs", "", "wrong-token", "198.51.100.43:1234", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("post-expiry status = %d, want 401", response.Code)
	}
	if got := len(server.authFailures.entries); got > 1 {
		t.Fatalf("expired limiter entries = %d, want at most 1", got)
	}
}

func TestOverlongRequestURIIsRejectedBeforeRouting(t *testing.T) {
	base, _, _ := testServer(t)
	server := NewServerWithOptions(base.archive, base.readiness, base.bearerToken, Options{MaxRequestURIBytes: 32})
	path := "/api/v1/jobs?cursor=" + strings.Repeat("x", 100)
	response := requestFrom(server, http.MethodGet, path, "", "", "198.51.100.50:1234", "")
	if response.Code != http.StatusRequestURITooLong {
		t.Fatalf("status = %d, want 414", response.Code)
	}
	if !strings.Contains(response.Body.String(), `"code":"request_uri_too_long"`) || strings.Contains(response.Body.String(), "secret-token") {
		t.Fatalf("unsafe URI error = %s", response.Body.String())
	}
	if got := len(server.authFailures.entries); got != 0 {
		t.Fatalf("auth limiter entries = %d, want 0", got)
	}
}

func TestObsoleteJobMutationsCannotBypassSavedItems(t *testing.T) {
	server, archive, _ := testServer(t)
	for _, route := range []struct{ method, path string }{{"POST", "/api/v1/jobs"}, {"POST", "/api/v1/jobs/job-http/retry"}, {"DELETE", "/api/v1/jobs/job-http"}} {
		response := request(server, route.method, route.path, `{"url":"https://example.test/article"}`, "secret-token")
		if response.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d: %s", route.method, route.path, response.Code, response.Body.String())
		}
	}
	page, err := archive.ListJobs(context.Background(), application.ListJobsRequest{})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("obsolete routes created work: %+v, %v", page, err)
	}
	accepted, err := archive.SubmitURL(context.Background(), application.SubmitURLRequest{URL: "https://example.test/article"}, "")
	if err != nil {
		t.Fatal(err)
	}
	response := request(server, "DELETE", "/api/v1/jobs/"+string(accepted.ID), "", "secret-token")
	if response.Code != 405 {
		t.Fatalf("delete existing job: %d", response.Code)
	}
	if _, err := archive.GetJob(context.Background(), accepted.ID); err != nil {
		t.Fatalf("read-only route removed job: %v", err)
	}
}
