// Package httpapi is the HTTP transport adapter for the JobArchive module.
package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"attic/internal/application"
	"attic/internal/domain"
	"attic/internal/library"
	"attic/internal/search"
)

const maxBodyBytes = 1 << 20

const (
	defaultAuthFailureLimit      = 5
	defaultAuthFailureWindow     = time.Minute
	defaultAuthFailureMaxEntries = 1024
	defaultMaxRequestURIBytes    = 8 << 10
)

// Options controls transport limits and the clock used by the authentication
// limiter. Zero values select conservative production defaults.
type Options struct {
	Library *library.Library
	Search  search.Index
	Now     func() time.Time

	AuthFailureLimit      int
	AuthFailureWindow     time.Duration
	AuthFailureMaxEntries int
	MaxRequestURIBytes    int
}

type Server struct {
	library            *library.Library
	search             search.Index
	archive            application.JobArchive
	readiness          application.Readiness
	bearerToken        string
	authFailures       *authFailureLimiter
	maxRequestURIBytes int
	now                func() time.Time
	sessionsMu         sync.Mutex
	sessions           map[[32]byte]time.Time
}

func NewServer(archive application.JobArchive, readiness application.Readiness, bearerToken string) *Server {
	return NewServerWithOptions(archive, readiness, bearerToken, Options{})
}

// NewServerWithOptions constructs a server with injectable transport limits.
// The default constructor remains the preferred production entry point.
func NewServerWithOptions(archive application.JobArchive, readiness application.Readiness, bearerToken string, options Options) *Server {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	if options.AuthFailureLimit <= 0 {
		options.AuthFailureLimit = defaultAuthFailureLimit
	}
	if options.AuthFailureWindow <= 0 {
		options.AuthFailureWindow = defaultAuthFailureWindow
	}
	if options.AuthFailureMaxEntries <= 0 {
		options.AuthFailureMaxEntries = defaultAuthFailureMaxEntries
	}
	if options.MaxRequestURIBytes <= 0 {
		options.MaxRequestURIBytes = defaultMaxRequestURIBytes
	}
	return &Server{
		library: options.Library, search: options.Search,
		sessions:           make(map[[32]byte]time.Time),
		archive:            archive,
		readiness:          readiness,
		bearerToken:        bearerToken,
		authFailures:       newAuthFailureLimiter(now, options.AuthFailureLimit, options.AuthFailureWindow, options.AuthFailureMaxEntries),
		maxRequestURIBytes: options.MaxRequestURIBytes,
		now:                now,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	correlationID := requestCorrelationID(r)
	w.Header().Set("X-Correlation-ID", correlationID)
	if s.requestURITooLong(r) {
		writeError(w, application.NewSafeError("request_uri_too_long", http.StatusRequestURITooLong, "The request URI is too long"), correlationID)
		return
	}
	if r.URL.Path == "/health/live" {
		s.handleLive(w, r, correlationID)
		return
	}
	if r.URL.Path == "/health/ready" {
		s.handleReady(w, r, correlationID)
		return
	}
	if s.serveWeb(w, r) {
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/v1/") && r.URL.Path != "/api/v1/jobs" {
		writeError(w, application.NewSafeError("not_found", 404, "Route was not found"), correlationID)
		return
	}
	remoteIP := remoteIP(r)
	if !s.authorized(r) {
		now := s.now()
		if blocked, retryAfter := s.authFailures.blocked(remoteIP, now); blocked {
			w.Header().Set("Retry-After", retryAfterHeader(retryAfter))
			writeError(w, application.NewSafeError("rate_limited", http.StatusTooManyRequests, "Too many failed authentication attempts; try again later"), correlationID)
			return
		}
		s.authFailures.recordFailure(remoteIP, now)
		writeError(w, application.NewSafeError("unauthorized", 401, "Authentication is required"), correlationID)
		return
	}
	s.authFailures.clear(remoteIP)
	if r.URL.Path == "/api/v1/session" {
		s.handleSession(w, r, correlationID)
		return
	}
	if s.serveLibrary(w, r, correlationID) {
		return
	}
	s.handleAPI(w, r, correlationID)
}

func (s *Server) requestURITooLong(r *http.Request) bool {
	requestURI := r.RequestURI
	if requestURI == "" && r.URL != nil {
		requestURI = r.URL.RequestURI()
	}
	return len(requestURI) > s.maxRequestURIBytes
}

func (s *Server) authorized(r *http.Request) bool {
	if s.bearerToken == "" {
		return false
	}
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return s.authorizedSession(r)
	}
	presented := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if presented == "" {
		return false
	}
	expectedHash := sha256.Sum256([]byte(s.bearerToken))
	presentedHash := sha256.Sum256([]byte(presented))
	return subtle.ConstantTimeCompare(expectedHash[:], presentedHash[:]) == 1
}

type authFailureEntry struct {
	windowStart time.Time
	lastSeen    time.Time
	failures    int
}

// authFailureLimiter is a bounded fixed-window limiter. It deliberately keys
// only on Request.RemoteAddr; forwarded headers are caller-controlled and are
// never used for authentication policy.
type authFailureLimiter struct {
	mu         sync.Mutex
	now        func() time.Time
	limit      int
	window     time.Duration
	maxEntries int
	entries    map[string]authFailureEntry
}

func newAuthFailureLimiter(now func() time.Time, limit int, window time.Duration, maxEntries int) *authFailureLimiter {
	return &authFailureLimiter{
		now:        now,
		limit:      limit,
		window:     window,
		maxEntries: maxEntries,
		entries:    make(map[string]authFailureEntry),
	}
}

func (l *authFailureLimiter) blocked(remoteIP string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanupExpiredLocked(now)

	entry, ok := l.entries[remoteIP]
	if !ok {
		return false, 0
	}
	if now.Before(entry.windowStart) {
		delete(l.entries, remoteIP)
		return false, 0
	}
	entry.lastSeen = now
	l.entries[remoteIP] = entry
	if entry.failures < l.limit {
		return false, 0
	}
	return true, entry.windowStart.Add(l.window).Sub(now)
}

func (l *authFailureLimiter) recordFailure(remoteIP string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanupExpiredLocked(now)

	entry, ok := l.entries[remoteIP]
	if !ok || now.Before(entry.windowStart) || !now.Before(entry.windowStart.Add(l.window)) {
		if !ok && len(l.entries) >= l.maxEntries {
			l.evictOldestLocked()
		}
		entry = authFailureEntry{windowStart: now}
	}
	entry.failures++
	entry.lastSeen = now
	l.entries[remoteIP] = entry
}

func (l *authFailureLimiter) clear(remoteIP string) {
	l.mu.Lock()
	delete(l.entries, remoteIP)
	l.mu.Unlock()
}

func (l *authFailureLimiter) cleanupExpiredLocked(now time.Time) {
	for remoteIP, entry := range l.entries {
		if now.Before(entry.windowStart) || !now.Before(entry.windowStart.Add(l.window)) {
			delete(l.entries, remoteIP)
		}
	}
}

func (l *authFailureLimiter) evictOldestLocked() {
	var oldestIP string
	var oldest time.Time
	for remoteIP, entry := range l.entries {
		if oldestIP == "" || entry.lastSeen.Before(oldest) {
			oldestIP = remoteIP
			oldest = entry.lastSeen
		}
	}
	if oldestIP != "" {
		delete(l.entries, oldestIP)
	}
}

func retryAfterHeader(remaining time.Duration) string {
	if remaining <= 0 {
		return "1"
	}
	seconds := int64(remaining / time.Second)
	if remaining%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	return strconv.FormatInt(seconds, 10)
}

func remoteIP(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	remote := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	if parsed := net.ParseIP(strings.Trim(remote, "[]")); parsed != nil {
		return parsed.String()
	}
	return "unknown"
}

func (s *Server) handleLive(w http.ResponseWriter, r *http.Request, correlationID string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet, correlationID)
		return
	}
	if s.readiness != nil {
		if err := s.readiness.Live(r.Context()); err != nil {
			writeError(w, application.NewSafeError("not_live", 503, "The process is not live"), correlationID)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request, correlationID string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet, correlationID)
		return
	}
	if s.readiness == nil {
		writeError(w, application.NewSafeError("not_ready", 503, "The process is not ready"), correlationID)
		return
	}
	if err := s.readiness.Ready(r.Context()); err != nil {
		writeError(w, application.NewSafeError("not_ready", 503, "The process is not ready"), correlationID)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request, correlationID string) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1")
	path = strings.TrimPrefix(path, "/")
	if path == "jobs" || path == "jobs/" {
		if r.Method == http.MethodGet {
			s.handleList(w, r, correlationID)
			return
		}
		methodNotAllowed(w, http.MethodGet, correlationID)
		return
	}
	parts := splitPath(path)
	if len(parts) < 2 || parts[0] != "jobs" || parts[1] == "" {
		writeError(w, application.NewSafeError("not_found", 404, "Route was not found"), correlationID)
		return
	}
	idValue, err := url.PathUnescape(parts[1])
	if err != nil || idValue == "" || strings.Contains(idValue, "/") {
		writeError(w, application.NewSafeError("invalid_input", 400, "job id is invalid"), correlationID)
		return
	}
	id := domain.JobID(idValue)
	switch {
	case len(parts) == 2 && r.Method == http.MethodGet:
		s.handleGet(w, r, id, correlationID)
	case len(parts) == 2:
		methodNotAllowed(w, http.MethodGet, correlationID)
	case len(parts) == 3 && parts[2] == "artifact" && r.Method == http.MethodGet:
		s.handleArtifact(w, r, id, correlationID)
	case len(parts) == 3 && parts[2] == "artifact":
		methodNotAllowed(w, http.MethodGet, correlationID)
	case len(parts) == 3 && parts[2] == "retry" && r.Method == http.MethodPost:
		methodNotAllowed(w, "", correlationID)
	default:
		writeError(w, application.NewSafeError("not_found", 404, "Route was not found"), correlationID)
	}
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request, correlationID string) {
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, application.NewSafeError("invalid_input", 400, "limit is invalid"), correlationID)
			return
		}
		limit = parsed
	}
	page, err := s.archive.ListJobs(r.Context(), application.ListJobsRequest{
		Limit:  limit,
		Cursor: r.URL.Query().Get("cursor"),
		Status: r.URL.Query().Get("status"),
	})
	if err != nil {
		writeError(w, err, correlationID)
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		entry := map[string]any{
			"id":           string(item.ID),
			"status":       string(item.Status),
			"stage":        nullableStage(item.Stage),
			"created_at":   item.CreatedAt.UTC().Format(time.RFC3339),
			"has_artifact": item.HasArtifact,
			"source_host":  item.SourceHost,
		}
		if item.ContentID != "" {
			entry["content_id"] = string(item.ContentID)
		}
		if item.Title != "" {
			entry["title"] = item.Title
		}
		if item.CompletedAt != nil {
			entry["completed_at"] = item.CompletedAt.UTC().Format(time.RFC3339)
		}
		if item.FailureCategory != "" {
			entry["failure_category"] = item.FailureCategory
		}
		items = append(items, entry)
	}
	result := map[string]any{"items": items}
	if page.NextCursor != "" {
		result["next_cursor"] = page.NextCursor
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request, id domain.JobID, correlationID string) {
	job, err := s.archive.GetJob(r.Context(), id)
	if err != nil {
		writeError(w, err, correlationID)
		return
	}
	result := jobDetailJSON(job)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleArtifact(w http.ResponseWriter, r *http.Request, id domain.JobID, correlationID string) {
	artifact, err := s.archive.OpenArtifact(r.Context(), id)
	if err != nil {
		writeError(w, err, correlationID)
		return
	}
	defer artifact.Body.Close()
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeHeaderFilename(artifact.Filename)+`"`)
	if artifact.ByteSize >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(artifact.ByteSize, 10))
	}
	if _, err := io.Copy(w, artifact.Body); err != nil {
		// Headers may already be committed.  The transport cannot safely turn a
		// partial download into a JSON error, so the stream simply terminates.
		return
	}
}

func jobDetailJSON(job application.JobDetail) map[string]any {
	result := map[string]any{
		"id":            string(job.ID),
		"status":        string(job.Status),
		"stage":         nullableStage(job.Stage),
		"attempt_count": job.AttemptCount,
		"created_at":    job.CreatedAt.UTC().Format(time.RFC3339),
		"submitted_url": job.SubmittedURL,
		"canonical_url": job.CanonicalURL,
		"has_artifact":  job.HasArtifact,
	}
	if job.ContentID != "" {
		result["content_id"] = string(job.ContentID)
	}
	if job.Title != "" {
		result["title"] = job.Title
	}
	if job.SourceHost != "" {
		result["source_host"] = job.SourceHost
	}
	if job.CompletedAt != nil {
		result["completed_at"] = job.CompletedAt.UTC().Format(time.RFC3339)
	}
	if job.Metadata != nil {
		result["metadata"] = map[string]string{
			"title":            job.Metadata.Title,
			"author":           job.Metadata.Author,
			"site_name":        job.Metadata.SiteName,
			"publication_date": job.Metadata.PublicationDate,
			"description":      job.Metadata.Description,
			"language":         job.Metadata.Language,
		}
	}
	if job.AIConfidence != nil {
		result["ai_confidence"] = *job.AIConfidence
	}
	if job.Artifact != nil {
		result["artifact"] = map[string]any{
			"filename":   job.Artifact.Filename,
			"media_type": job.Artifact.MediaType,
			"byte_size":  job.Artifact.ByteSize,
			"checksum":   job.Artifact.Checksum,
			"download":   job.Artifact.Download,
		}
	}
	if job.Delivery != nil {
		result["delivery"] = map[string]any{
			"status":        job.Delivery.Status,
			"attempt_count": job.Delivery.AttemptCount,
		}
	}
	if job.Failure != nil {
		result["failure"] = map[string]string{
			"category":       job.Failure.Category,
			"message":        job.Failure.Message,
			"correlation_id": job.Failure.CorrelationID,
		}
	}
	if job.RetryOfJobID != "" {
		result["retry_of_job_id"] = string(job.RetryOfJobID)
	}
	return result
}

func nullableStage(stage domain.Stage) any {
	if stage == "" {
		return nil
	}
	return string(stage)
}

func splitPath(path string) []string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		return nil
	}
	return parts
}

func safeHeaderFilename(filename string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return "attic.pdf"
	}
	var builder strings.Builder
	for _, r := range filename {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' || r == ' ' {
			builder.WriteRune(r)
		}
	}
	result := strings.TrimSpace(builder.String())
	if result == "" {
		return "attic.pdf"
	}
	return result
}

func requestCorrelationID(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-Correlation-ID")); value != "" && len(value) <= 100 && isSafeToken(value) {
		return value
	}
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return "correlation-unknown"
}

func isSafeToken(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("-_.", r) {
			continue
		}
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error, correlationID string) {
	safe := safeTransportError(err).WithCorrelationID(correlationID)
	w.Header().Set("Content-Type", "application/json")
	if safe.Code == "unauthorized" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="attic"`)
	}
	w.WriteHeader(safe.HTTPStatus)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":           safe.Code,
			"message":        safe.Message,
			"correlation_id": safe.CorrelationID,
		},
	})
}

func safeTransportError(err error) *application.SafeError {
	if err == nil {
		return application.NewSafeError("internal_error", 500, "The operation could not be completed")
	}
	var safe *application.SafeError
	if errors.As(err, &safe) && safe != nil {
		return safe
	}
	return application.NewSafeError("internal_error", 500, "The operation could not be completed")
}

func methodNotAllowed(w http.ResponseWriter, allow, correlationID string) {
	w.Header().Set("Allow", allow)
	writeError(w, application.NewSafeError("method_not_allowed", 405, "HTTP method is not allowed"), correlationID)
}
