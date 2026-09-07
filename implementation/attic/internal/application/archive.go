package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"attic/internal/domain"
	"attic/internal/sanitize"
)

const (
	defaultProfile       = "a5"
	defaultMinChars      = 500
	maxURLLength         = 8192
	maxTitleLength       = 500
	maxProfileLength     = 100
	maxIdempotencyLength = 255
)

// Archive is the default JobArchive implementation.  It owns request
// validation and projections while delegating durable state and bytes to
// injected seams.
type Archive struct {
	store          JobStore
	artifacts      ArtifactStore
	processor      Processor
	defaultProfile string
	profiles       map[string]struct{}
	minContent     int
	now            func() time.Time
	newJobID       func() domain.JobID
	newContentID   func() domain.ContentID
}

func NewArchive(store JobStore, artifacts ArtifactStore, options ArchiveOptions) (*Archive, error) {
	if store == nil {
		return nil, NewSafeError("invalid_configuration", 500, "Job storage is not configured")
	}
	if artifacts == nil {
		return nil, NewSafeError("invalid_configuration", 500, "Artifact storage is not configured")
	}

	profile := strings.TrimSpace(options.DefaultProfile)
	if profile == "" {
		profile = defaultProfile
	}
	profiles := make(map[string]struct{}, len(options.Profiles)+1)
	for name := range options.Profiles {
		name = strings.TrimSpace(name)
		if name != "" {
			if name != defaultProfile && name != "kindle-scribe" {
				return nil, NewSafeError("invalid_configuration", 500, "The configured PDF profile is not supported")
			}
			profiles[name] = struct{}{}
		}
	}
	if len(profiles) == 0 {
		if profile != defaultProfile && profile != "kindle-scribe" {
			return nil, NewSafeError("invalid_configuration", 500, "The configured PDF profile is not supported")
		}
		profiles[profile] = struct{}{}
	}
	if _, ok := profiles[profile]; !ok {
		return nil, NewSafeError("invalid_configuration", 500, "The configured PDF profile is not supported")
	}

	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	newJobID := options.NewJobID
	if newJobID == nil {
		newJobID = randomJobID
	}
	newContentID := options.NewContentID
	if newContentID == nil {
		newContentID = func() domain.ContentID { return domain.ContentID(randomOpaqueID()) }
	}
	minContent := options.MinContentChars
	if minContent <= 0 {
		minContent = defaultMinChars
	}

	processor := options.Processor
	if processor == nil {
		processor = unavailableProcessor{}
	}
	return &Archive{
		store:          store,
		artifacts:      artifacts,
		processor:      processor,
		defaultProfile: profile,
		profiles:       profiles,
		minContent:     minContent,
		now:            now,
		newJobID:       newJobID,
		newContentID:   newContentID,
	}, nil
}

var _ JobArchive = (*Archive)(nil)

func (a *Archive) SetProcessor(processor Processor) {
	if processor == nil {
		a.processor = unavailableProcessor{}
		return
	}
	a.processor = processor
}

func (a *Archive) SubmitURL(ctx context.Context, request SubmitURLRequest, idempotencyKey string) (AcceptedJob, error) {
	normalized, err := a.normalizeSubmit(request, idempotencyKey)
	if err != nil {
		return AcceptedJob{}, err
	}
	createdAt := a.now().UTC()
	command := CreateJob{
		ID:             a.newJobID(),
		Request:        normalized.request,
		CreatedAt:      createdAt,
		IdempotencyKey: domain.IdempotencyKey(normalized.idempotencyKey),
		RequestDigest:  normalized.digest,
	}
	outcome, err := a.store.CreateOrReuse(ctx, command)
	if err != nil {
		return AcceptedJob{}, mapStoreError(err)
	}
	if outcome.Conflict {
		return AcceptedJob{}, NewSafeError("idempotency_conflict", 409, "The idempotency key was already used with a different request")
	}
	return acceptedJob(outcome.Job), nil
}

func (a *Archive) ListJobs(ctx context.Context, request ListJobsRequest) (JobPage, error) {
	limit := request.Limit
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 100 {
		return JobPage{}, NewSafeError("invalid_input", 400, "limit must be between 1 and 100")
	}

	var after *CursorPosition
	if strings.TrimSpace(request.Cursor) != "" {
		position, err := decodeCursor(request.Cursor)
		if err != nil {
			return JobPage{}, NewSafeError("invalid_input", 400, "cursor is invalid")
		}
		after = &position
	}

	var status *domain.Status
	if strings.TrimSpace(request.Status) != "" {
		candidate := domain.Status(strings.TrimSpace(request.Status))
		if !candidate.Valid() {
			return JobPage{}, NewSafeError("invalid_input", 400, "status is invalid")
		}
		status = &candidate
	}

	page, err := a.store.List(ctx, StoreListRequest{Limit: limit, After: after, Status: status})
	if err != nil {
		return JobPage{}, mapStoreError(err)
	}
	result := JobPage{Items: make([]JobSummary, 0, len(page.Jobs))}
	for _, job := range page.Jobs {
		result.Items = append(result.Items, summarize(job))
	}
	if page.HasMore && len(page.Jobs) > 0 {
		last := page.Jobs[len(page.Jobs)-1]
		result.NextCursor = encodeCursor(CursorPosition{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return result, nil
}

func (a *Archive) GetJob(ctx context.Context, id domain.JobID) (JobDetail, error) {
	if strings.TrimSpace(string(id)) == "" {
		return JobDetail{}, NewSafeError("invalid_input", 400, "job id is required")
	}
	job, err := a.store.Get(ctx, id)
	if err != nil {
		return JobDetail{}, mapStoreError(err)
	}
	return detail(job), nil
}

func (a *Archive) OpenArtifact(ctx context.Context, id domain.JobID) (ArtifactDownload, error) {
	if strings.TrimSpace(string(id)) == "" {
		return ArtifactDownload{}, NewSafeError("invalid_input", 400, "job id is required")
	}
	job, err := a.store.Get(ctx, id)
	if err != nil {
		return ArtifactDownload{}, mapStoreError(err)
	}
	if job.Artifact == nil || !job.Artifact.Available {
		return ArtifactDownload{}, NewSafeError("artifact_not_found", 404, "No complete artifact exists for this job")
	}
	body, err := a.artifacts.Open(ctx, *job.Artifact)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ArtifactDownload{}, NewSafeError("artifact_not_found", 404, "No complete artifact exists for this job")
		}
		return ArtifactDownload{}, mapStorageError(err)
	}
	return ArtifactDownload{
		Filename:  job.Artifact.Filename,
		MediaType: "application/pdf",
		ByteSize:  job.Artifact.ByteSize,
		Checksum:  job.Artifact.Checksum,
		Body:      body,
	}, nil
}

func (a *Archive) RetryJob(ctx context.Context, id domain.JobID) (AcceptedJob, error) {
	if strings.TrimSpace(string(id)) == "" {
		return AcceptedJob{}, NewSafeError("invalid_input", 400, "job id is required")
	}
	job, err := a.store.CreateRetry(ctx, id, a.newJobID(), a.now().UTC())
	if err != nil {
		if errors.Is(err, ErrNotTerminal) {
			return AcceptedJob{}, NewSafeError("job_not_terminal", 409, "Only terminal jobs can be retried")
		}
		return AcceptedJob{}, mapStoreError(err)
	}
	return acceptedJob(job), nil
}

func (a *Archive) DeleteJob(ctx context.Context, id domain.JobID) error {
	if strings.TrimSpace(string(id)) == "" {
		return NewSafeError("invalid_input", 400, "job id is required")
	}
	job, err := a.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return mapStoreError(err)
	}
	if err := a.store.RequestDelete(ctx, id, a.now().UTC()); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return mapStoreError(err)
	}
	// RequestDelete must durably enqueue the deletion before the physical
	// bytes disappear. That ordering leaves an operator-retryable task if the
	// filesystem is temporarily unavailable and never strands metadata that
	// points at a missing artifact after a database failure.
	if job.Artifact != nil {
		if err := a.artifacts.Delete(ctx, *job.Artifact); err != nil {
			return mapStorageError(err)
		}
	}
	return nil
}

type normalizedSubmit struct {
	request        SubmitURLRequest
	idempotencyKey string
	digest         string
}

func (a *Archive) normalizeSubmit(request SubmitURLRequest, idempotencyKey string) (normalizedSubmit, error) {
	rawURL := strings.TrimSpace(request.URL)
	if rawURL == "" || len(rawURL) > maxURLLength {
		return normalizedSubmit{}, NewSafeError("invalid_input", 400, "url must be a bounded HTTP or HTTPS URL")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return normalizedSubmit{}, NewSafeError("invalid_input", 400, "url must be a bounded HTTP or HTTPS URL")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return normalizedSubmit{}, NewSafeError("invalid_input", 400, "url must use HTTP or HTTPS")
	}
	if parsed.Hostname() == "" {
		return normalizedSubmit{}, NewSafeError("invalid_input", 400, "url must contain a host")
	}
	parsed.Fragment = ""
	normalizedURL := parsed.String()

	title := strings.TrimSpace(request.Title)
	if len(title) > maxTitleLength {
		return normalizedSubmit{}, NewSafeError("invalid_input", 400, "title is too long")
	}
	profile := strings.TrimSpace(request.Profile)
	if profile == "" {
		profile = a.defaultProfile
	}
	if len(profile) > maxProfileLength {
		return normalizedSubmit{}, NewSafeError("invalid_input", 400, "profile is invalid")
	}
	if _, ok := a.profiles[profile]; !ok {
		return normalizedSubmit{}, NewSafeError("invalid_input", 400, "profile is not configured")
	}

	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if len(idempotencyKey) > maxIdempotencyLength {
		return normalizedSubmit{}, NewSafeError("invalid_input", 400, "idempotency key is too long")
	}
	canonical := normalizedURL + "\x00" + title + "\x00" + profile
	digest := sha256.Sum256([]byte(canonical))
	return normalizedSubmit{
		request:        SubmitURLRequest{URL: normalizedURL, Title: title, Profile: profile},
		idempotencyKey: idempotencyKey,
		digest:         hex.EncodeToString(digest[:]),
	}, nil
}

func acceptedJob(job domain.Job) AcceptedJob {
	return AcceptedJob{
		ID:        job.ID,
		Status:    job.Status,
		CreatedAt: job.CreatedAt.UTC(),
		Self:      "/api/v1/jobs/" + string(job.ID),
	}
}

func summarize(job domain.Job) JobSummary {
	var title string
	if job.Content != nil {
		title = job.Content.Title
	}
	if title == "" {
		title = job.TitleHint
	}
	var completedAt *time.Time
	if job.CompletedAt != nil {
		value := job.CompletedAt.UTC()
		completedAt = &value
	}
	var failure string
	if job.Failure != nil {
		failure = string(job.Failure.Category)
	}
	return JobSummary{
		ID:              job.ID,
		ContentID:       contentID(job),
		Title:           title,
		SourceHost:      sourceHost(job.SubmittedURL),
		Status:          job.Status,
		Stage:           job.Stage,
		CreatedAt:       job.CreatedAt.UTC(),
		CompletedAt:     completedAt,
		FailureCategory: failure,
		HasArtifact:     job.Artifact != nil && job.Artifact.Available,
	}
}

func detail(job domain.Job) JobDetail {
	result := JobDetail{
		JobSummary:   summarize(job),
		SubmittedURL: redactURL(job.SubmittedURL),
		CanonicalURL: redactURL(job.CanonicalURL),
		AttemptCount: job.AttemptCount,
		RetryOfJobID: job.RetryOfJobID,
	}
	if job.Content != nil {
		confidence := job.Content.AIConfidence
		result.AIConfidence = &confidence
		result.Metadata = &JobMetadata{
			Title:           job.Content.Title,
			Author:          job.Content.Author,
			SiteName:        job.Content.SiteName,
			PublicationDate: job.Content.PublicationDate,
			Description:     job.Content.Description,
			Language:        job.Content.Language,
		}
	}
	if job.Artifact != nil && job.Artifact.Available {
		result.Artifact = &ArtifactInfo{
			Filename:  job.Artifact.Filename,
			MediaType: job.Artifact.MediaType,
			ByteSize:  job.Artifact.ByteSize,
			Checksum:  job.Artifact.Checksum,
			Download:  "/api/v1/jobs/" + string(job.ID) + "/artifact",
		}
	}
	if job.Delivery != nil {
		result.Delivery = &DeliveryInfo{Status: job.Delivery.Status, AttemptCount: job.Delivery.AttemptCount}
	}
	if job.Failure != nil {
		result.Failure = &FailureInfo{
			Category:      string(job.Failure.Category),
			Message:       job.Failure.Message,
			CorrelationID: job.Failure.CorrelationID,
		}
	}
	return result
}

func contentID(job domain.Job) domain.ContentID {
	if job.Content == nil {
		return ""
	}
	return job.Content.ID
}

func sourceHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func redactURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "[redacted]"
	}
	if parsed.User != nil {
		parsed.User = nil
	}
	if parsed.RawQuery != "" {
		query := parsed.Query()
		for key := range query {
			query[key] = []string{"[redacted]"}
		}
		parsed.RawQuery = query.Encode()
	}
	parsed.Fragment = ""
	return parsed.String()
}

func encodeCursor(position CursorPosition) string {
	payload := strconv.FormatInt(position.CreatedAt.UTC().UnixNano(), 10) + ":" + string(position.ID)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeCursor(value string) (CursorPosition, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return CursorPosition{}, err
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return CursorPosition{}, errors.New("malformed cursor")
	}
	nanos, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return CursorPosition{}, err
	}
	return CursorPosition{CreatedAt: time.Unix(0, nanos).UTC(), ID: domain.JobID(parts[1])}, nil
}

func mapStoreError(err error) *SafeError {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		return NewSafeError("not_found", 404, "Job was not found")
	case errors.Is(err, ErrIdempotencyConflict):
		return NewSafeError("idempotency_conflict", 409, "The idempotency key was already used with a different request")
	case errors.Is(err, ErrNotTerminal):
		return NewSafeError("job_not_terminal", 409, "Only terminal jobs can be retried")
	default:
		return internalError(err)
	}
}

func mapStorageError(err error) *SafeError {
	if err == nil {
		return nil
	}
	return &SafeError{Code: "storage_failed", HTTPStatus: 503, Message: "Artifact storage is temporarily unavailable", cause: err}
}

func randomOpaqueID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		// crypto/rand failure is extraordinarily unusual.  The timestamp keeps
		// the fallback opaque and collision-resistant enough for a single process.
		return hex.EncodeToString([]byte(strconv.FormatInt(time.Now().UnixNano(), 10)))
	}
	return hex.EncodeToString(bytes[:])
}

func randomJobID() domain.JobID {
	return domain.JobID(randomOpaqueID())
}

func safeFilename(title string, id domain.JobID) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return filenameWithoutParentTraversal("attic-" + string(id) + ".pdf")
	}
	var builder strings.Builder
	for _, r := range title {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == ' ' || r == '.' {
			builder.WriteRune(r)
		} else {
			builder.WriteRune('_')
		}
	}
	name := strings.TrimSpace(builder.String())
	name = strings.Trim(name, ".")
	if name == "" {
		return filenameWithoutParentTraversal("attic-" + string(id) + ".pdf")
	}
	name = truncateRunes(name, 100)
	return filenameWithoutParentTraversal(name + ".pdf")
}

func filenameWithoutParentTraversal(value string) string {
	return strings.ReplaceAll(value, "..", "__")
}

func truncateRunes(value string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max])
}

func (a *Archive) approveArticle(draft ArticleDraft) (ApprovedArticle, error) {
	if draft.Classification != "article" || (draft.Decision != "accept_candidate" && draft.Decision != "replace_candidate") {
		return ApprovedArticle{}, NewProcessingError(string(domain.FailureUnsupportedContent), "The page was not classified as an article", false)
	}
	draft.Title = strings.TrimSpace(draft.Title)
	draft.PlainText = strings.TrimSpace(draft.PlainText)
	draft.SemanticHTML = strings.TrimSpace(draft.SemanticHTML)
	if draft.Title == "" {
		return ApprovedArticle{}, NewProcessingError(string(domain.FailureInsufficientContent), "The article has no title", false)
	}
	if utf8.RuneCountInString(draft.PlainText) < a.minContent {
		return ApprovedArticle{}, NewProcessingError(string(domain.FailureInsufficientContent), "The article does not contain enough readable content", false)
	}
	if draft.SemanticHTML == "" {
		return ApprovedArticle{}, NewProcessingError(string(domain.FailureInsufficientContent), "The article has no semantic content", false)
	}
	if draft.AIConfidence < 0 || draft.AIConfidence > 1 {
		return ApprovedArticle{}, NewProcessingError(string(domain.FailureAIInvalidResponse), "The AI confidence value is invalid", false)
	}
	if draft.AICompleteness < 0 || draft.AICompleteness > 1 {
		return ApprovedArticle{}, NewProcessingError(string(domain.FailureAIInvalidResponse), "The AI completeness value is invalid", false)
	}
	draft.AIAttemptID = strings.TrimSpace(draft.AIAttemptID)
	if draft.AIAttemptID == "" {
		return ApprovedArticle{}, NewProcessingError(string(domain.FailureAIInvalidResponse), "The AI attempt is not recorded", false)
	}
	sanitizedHTML, sanitizeErr := sanitize.SanitizeHTML(draft.SemanticHTML)
	if sanitizeErr != nil {
		if errors.Is(sanitizeErr, sanitize.ErrNoSemanticContent) {
			return ApprovedArticle{}, NewProcessingError(string(domain.FailureInsufficientContent), "The article has no semantic content", false)
		}
		return ApprovedArticle{}, NewProcessingError(string(domain.FailureAIInvalidResponse), "The AI content is not safe HTML", false)
	}
	draft.SemanticHTML = sanitizedHTML
	if draft.ExtractionMethod == "" {
		draft.ExtractionMethod = "ai"
	}
	return ApprovedArticle{draft: draft, valid: true}, nil
}

func (a ApprovedArticle) content(job domain.Job, id domain.ContentID, now time.Time) (domain.ContentDocument, bool) {
	if !a.valid {
		return domain.ContentDocument{}, false
	}
	sourceURL := job.CanonicalURL
	if sourceURL == "" {
		sourceURL = job.SubmittedURL
	}
	return domain.ContentDocument{
		ID:               id,
		JobID:            job.ID,
		Title:            a.draft.Title,
		Author:           strings.TrimSpace(a.draft.Author),
		SiteName:         strings.TrimSpace(a.draft.SiteName),
		PublicationDate:  strings.TrimSpace(a.draft.PublicationDate),
		Description:      strings.TrimSpace(a.draft.Description),
		Language:         strings.TrimSpace(a.draft.Language),
		SourceURL:        redactURL(sourceURL),
		SemanticHTML:     a.draft.SemanticHTML,
		PlainText:        a.draft.PlainText,
		ExtractionMethod: a.draft.ExtractionMethod,
		AIConfidence:     a.draft.AIConfidence,
		AICompleteness:   a.draft.AICompleteness,
		CreatedAt:        now.UTC(),
		UpdatedAt:        now.UTC(),
	}, true
}

type unavailableProcessor struct{}

func (unavailableProcessor) Process(context.Context, domain.Job, ProcessorContext) (ProcessResult, error) {
	return ProcessResult{}, NewProcessingError(string(domain.FailureAIUnavailable), "The AI provider is unavailable", true)
}

func sanitizeFilename(value, title string, id domain.JobID) string {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(strings.ToLower(value), ".pdf") {
		value = value[:len(value)-len(".pdf")]
	}
	if value == "" {
		return safeFilename(title, id)
	}
	return safeFilename(value, id)
}
