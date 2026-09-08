package application

import (
	"context"
	"io"
	"sync"
	"time"

	"attic/internal/domain"
)

// JobArchive is the small interface intended for HTTP, CLI, or future UI
// callers.  It intentionally contains no SQL, filesystem paths, browser
// details, or provider-specific values.
type JobArchive interface {
	SubmitURL(context.Context, SubmitURLRequest, string) (AcceptedJob, error)
	ListJobs(context.Context, ListJobsRequest) (JobPage, error)
	GetJob(context.Context, domain.JobID) (JobDetail, error)
	OpenArtifact(context.Context, domain.JobID) (ArtifactDownload, error)
	RetryJob(context.Context, domain.JobID) (AcceptedJob, error)
	DeleteJob(context.Context, domain.JobID) error
}

// Runner is deliberately separate from JobArchive.  HTTP callers only submit
// and inspect work; the runtime owns worker lifetime and scheduling.
type Runner interface {
	Run(context.Context) error
}

// Readiness is separate from both the job interface and the worker.  Liveness
// must remain useful while external AI dependencies are unavailable.
type Readiness interface {
	Live(context.Context) error
	Ready(context.Context) error
}

type SubmitURLRequest struct {
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	Profile string `json:"profile,omitempty"`
}

type AcceptedJob struct {
	ID        domain.JobID
	Status    domain.Status
	CreatedAt time.Time
	Self      string
}

type ListJobsRequest struct {
	Limit  int
	Cursor string
	Status string
}

type JobSummary struct {
	ID              domain.JobID
	ContentID       domain.ContentID
	Title           string
	SourceHost      string
	Status          domain.Status
	Stage           domain.Stage
	CreatedAt       time.Time
	CompletedAt     *time.Time
	FailureCategory string
	HasArtifact     bool
}

type JobPage struct {
	Items      []JobSummary
	NextCursor string
}

type JobMetadata struct {
	Title           string
	Author          string
	SiteName        string
	PublicationDate string
	Description     string
	Language        string
}

type ArtifactInfo struct {
	Filename  string
	MediaType string
	ByteSize  int64
	Checksum  string
	Download  string
}

type DeliveryInfo struct {
	Status       string
	AttemptCount int
}

type FailureInfo struct {
	Category      string
	Message       string
	CorrelationID string
}

type JobDetail struct {
	JobSummary
	SubmittedURL string
	CanonicalURL string
	AttemptCount int
	Metadata     *JobMetadata
	AIConfidence *float64
	Artifact     *ArtifactInfo
	Delivery     *DeliveryInfo
	Failure      *FailureInfo
	RetryOfJobID domain.JobID
}

type ArtifactDownload struct {
	Filename  string
	MediaType string
	ByteSize  int64
	Checksum  string
	Body      io.ReadCloser
}

type CursorPosition = domain.CursorPosition

type CreateJob struct {
	ID             domain.JobID
	Request        SubmitURLRequest
	CreatedAt      time.Time
	IdempotencyKey domain.IdempotencyKey
	RequestDigest  string
}

type CreateOutcome struct {
	Job      domain.Job
	Replayed bool
	Conflict bool
}

type StoreListRequest struct {
	Limit  int
	After  *CursorPosition
	Status *domain.Status
}

type StorePage struct {
	Jobs    []domain.Job
	HasMore bool
}

type Lease struct {
	Job       domain.Job
	Token     string
	ExpiresAt time.Time
	state     *leaseState
}

// leaseState keeps the optimistic version and expiry coherent when a worker
// renews a lease while the processor reports a stage transition. The pointer
// also makes copied lease values observe the same durable revision.
type leaseState struct {
	mu        sync.Mutex
	version   uint64
	expiresAt time.Time
}

func NewLease(job domain.Job, token string, expiresAt time.Time) *Lease {
	return &Lease{
		Job:       job,
		Token:     token,
		ExpiresAt: expiresAt,
		state:     &leaseState{version: job.Version, expiresAt: expiresAt},
	}
}

func (l *Lease) CurrentVersion() uint64 {
	if l == nil {
		return 0
	}
	if l.state == nil {
		return l.Job.Version
	}
	l.state.mu.Lock()
	defer l.state.mu.Unlock()
	return l.state.version
}

func (l *Lease) Observe(version uint64, expiresAt time.Time, stage domain.Stage) {
	if l == nil {
		return
	}
	if l.state == nil {
		l.state = &leaseState{version: l.Job.Version, expiresAt: l.ExpiresAt}
	}
	l.state.mu.Lock()
	l.state.version = version
	l.state.expiresAt = expiresAt
	l.Job.Version = version
	l.Job.LeaseUntil = expiresAt
	l.ExpiresAt = expiresAt
	l.Job.Stage = stage
	l.state.mu.Unlock()
}

type Completion struct {
	Content      domain.ContentDocument
	Artifact     domain.Artifact
	CanonicalURL string
}

// AIAttempt is the provider-safe durable record for one model request. Page
// content, prompts, screenshots, response bodies, credentials, and headers do
// not belong at this seam.
type AIAttempt struct {
	Purpose           string
	Model             string
	PromptVersion     string
	Latency           time.Duration
	ProviderRequestID string
	InputTokens       int
	OutputTokens      int
	UsageReported     bool
	Status            string
	ErrorCategory     string
	CreatedAt         time.Time
}

// AIAttemptRecorder durably records a complete provider call sequence. The
// returned IDs correspond positionally to attempts and are created by the
// adapter. Implementations must commit the sequence atomically.
type AIAttemptRecorder interface {
	RecordAIAttempts(context.Context, domain.JobID, []AIAttempt) ([]string, error)
}

// JobStore is the PostgreSQL seam.  The implementation owns transactions,
// locking, migrations, leases, and durable checkpoints; none leak through
// JobArchive.
type JobStore interface {
	CreateOrReuse(context.Context, CreateJob) (CreateOutcome, error)
	List(context.Context, StoreListRequest) (StorePage, error)
	Get(context.Context, domain.JobID) (domain.Job, error)
	CreateRetry(context.Context, domain.JobID, domain.JobID, time.Time) (domain.Job, error)
	RequestDelete(context.Context, domain.JobID, time.Time) error
	ClaimNext(context.Context, time.Time, time.Duration) (*Lease, error)
	RenewLease(context.Context, *Lease, time.Duration) error
	SetStage(context.Context, *Lease, domain.Stage) error
	Complete(context.Context, *Lease, Completion, time.Time) error
	Requeue(context.Context, *Lease, domain.Failure, time.Time) error
	Fail(context.Context, *Lease, domain.Failure, time.Time) error
}

type ArtifactStore interface {
	Put(context.Context, domain.JobID, string, []byte, time.Time) (domain.Artifact, error)
	Open(context.Context, domain.Artifact) (io.ReadCloser, error)
	Delete(context.Context, domain.Artifact) error
}

// Processor is the internal pipeline seam.  A production implementation will
// render, extract, invoke AI, validate, format, and return a complete artifact.
// The application constructs ApprovedArticle only through ApproveArticle.
type Processor interface {
	Process(context.Context, domain.Job, ProcessorContext) (ProcessResult, error)
}

type ProcessorFunc func(context.Context, domain.Job, ProcessorContext) (ProcessResult, error)

func (f ProcessorFunc) Process(ctx context.Context, job domain.Job, pc ProcessorContext) (ProcessResult, error) {
	return f(ctx, job, pc)
}

type ProcessorContext struct {
	Job            domain.Job
	ApproveArticle func(ArticleDraft) (ApprovedArticle, error)
	SetStage       func(domain.Stage) error
}

type ProcessResult struct {
	Article      ApprovedArticle
	CanonicalURL string
	Filename     string
	PDF          []byte
}

// ArticleDraft is the validated logical result returned by an AI-aware
// processor.  It is not itself sufficient to complete a job.
type ArticleDraft struct {
	Classification   string
	Decision         string
	Title            string
	Author           string
	SiteName         string
	PublicationDate  string
	Description      string
	Language         string
	SemanticHTML     string
	PlainText        string
	ExtractionMethod string
	AIConfidence     float64
	AICompleteness   float64
	AIAttemptID      string
}

// ApprovedArticle has unexported fields and can only be constructed by the
// application package.  Its zero value is invalid.
type ApprovedArticle struct {
	draft ArticleDraft
	valid bool
}

// Snapshot returns the immutable, AI-approved article data needed by trusted
// downstream adapters such as the PDF formatter. The zero value remains
// unusable, so formatting cannot accidentally consume an unapproved draft.
func (a ApprovedArticle) Snapshot() (ArticleDraft, bool) {
	return a.draft, a.valid
}

type ArchiveOptions struct {
	DefaultProfile  string
	Profiles        map[string]struct{}
	MinContentChars int
	Now             func() time.Time
	NewJobID        func() domain.JobID
	NewContentID    func() domain.ContentID
	CursorSecret    []byte
	Processor       Processor
}

type WorkerOptions struct {
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	PollInterval      time.Duration
	MaxAttempts       int
	Now               func() time.Time
	Processor         Processor
}
