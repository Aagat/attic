// Package domain contains the small, transport-independent vocabulary shared
// by the application module and its adapters.
package domain

import "time"

// Opaque identifiers deliberately have distinct named types.  Callers may
// persist and compare them, but must not infer database layout or meaning from
// their representation.
type JobID string
type ContentID string
type IdempotencyKey string

type Status string

const (
	StatusQueued         Status = "queued"
	StatusProcessing     Status = "processing"
	StatusDelivering     Status = "delivering"
	StatusReady          Status = "ready"
	StatusDelivered      Status = "delivered"
	StatusDeliveryFailed Status = "delivery_failed"
	StatusFailed         Status = "failed"
	StatusCancelled      Status = "cancelled"
)

func (s Status) Valid() bool {
	switch s {
	case StatusQueued, StatusProcessing, StatusDelivering, StatusReady,
		StatusDelivered, StatusDeliveryFailed, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

func (s Status) Terminal() bool {
	switch s {
	case StatusReady, StatusDelivered, StatusDeliveryFailed, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

type Stage string

const (
	StageFetching    Stage = "fetching"
	StageExtracting  Stage = "extracting"
	StageAIAnalyzing Stage = "ai_analyzing"
	StageFormatting  Stage = "formatting"
	StagePersisting  Stage = "persisting"
)

func (s Stage) Valid() bool {
	switch s {
	case StageFetching, StageExtracting, StageAIAnalyzing, StageFormatting, StagePersisting:
		return true
	default:
		return false
	}
}

type FailureCategory string

const (
	FailureInvalidInput        FailureCategory = "invalid_input"
	FailureBlockedTarget       FailureCategory = "blocked_target"
	FailureFetchFailed         FailureCategory = "fetch_failed"
	FailureRenderTimeout       FailureCategory = "render_timeout"
	FailureAccessDenied        FailureCategory = "access_denied"
	FailurePaywallDetected     FailureCategory = "paywall_detected"
	FailureUnsupportedContent  FailureCategory = "unsupported_content"
	FailureInsufficientContent FailureCategory = "insufficient_content"
	FailureAIUnavailable       FailureCategory = "ai_unavailable"
	FailureAIAuthFailed        FailureCategory = "ai_auth_failed"
	FailureAIModelUnsupported  FailureCategory = "ai_model_unsupported"
	FailureAIInvalidResponse   FailureCategory = "ai_invalid_response"
	FailureFormatFailed        FailureCategory = "format_failed"
	FailurePDFQualityFailed    FailureCategory = "pdf_quality_failed"
	FailureStorageFailed       FailureCategory = "storage_failed"
	FailureDeliveryRejected    FailureCategory = "delivery_rejected"
	FailureDeliveryTimeout     FailureCategory = "delivery_timeout"
	FailureInternalError       FailureCategory = "internal_error"
)

type Failure struct {
	Category      FailureCategory
	Message       string
	CorrelationID string
	OccurredAt    time.Time
}

type Artifact struct {
	Key       string
	Filename  string
	MediaType string
	ByteSize  int64
	Checksum  string
	Available bool
	CreatedAt time.Time
}

type Delivery struct {
	Status       string
	AttemptCount int
	MessageID    string
}

// ContentDocument is immutable after a successful job.  Keeping both
// sanitized semantic HTML and normalized plain text makes later UI and search
// modules possible without changing the job interface.
type ContentDocument struct {
	ID               ContentID
	JobID            JobID
	Title            string
	Author           string
	SiteName         string
	PublicationDate  string
	Description      string
	Language         string
	SourceURL        string
	SemanticHTML     string
	PlainText        string
	ExtractionMethod string
	AIConfidence     float64
	AICompleteness   float64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Job struct {
	ID              JobID
	RetryOfJobID    JobID
	Content         *ContentDocument
	SubmittedURL    string
	CanonicalURL    string
	TitleHint       string
	Profile         string
	Status          Status
	Stage           Stage
	AttemptCount    int
	CreatedAt       time.Time
	CompletedAt     *time.Time
	Failure         *Failure
	Artifact        *Artifact
	Delivery        *Delivery
	AIConfidence    float64
	Version         uint64
	NextAttemptAt   time.Time
	IdempotencyKey  IdempotencyKey
	RequestDigest   string
	DeleteRequested bool
	LeaseToken      string
	LeaseUntil      time.Time
}

// CursorPosition is internal to the storage seam.  The HTTP interface only
// exposes an opaque encoded cursor.
type CursorPosition struct {
	CreatedAt time.Time
	ID        JobID
}

// CanRecoverSource permits a new acquisition attempt before artifact persistence.
func CanRecoverSource(from, to Stage) bool {
	return to == StageFetching && (from == StageExtracting || from == StageAIAnalyzing || from == StageFormatting)
}
