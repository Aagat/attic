package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"attic/internal/domain"
)

const (
	defaultWorkerLease   = 5 * time.Minute
	maxHeartbeatInterval = 30 * time.Second
)

// Worker is the default Runner implementation.  It deliberately contains no
// transport concerns; it claims durable work and asks an injected processor
// to perform the expensive pipeline.
type Worker struct {
	store        JobStore
	artifacts    ArtifactStore
	processor    Processor
	approve      func(ArticleDraft) (ApprovedArticle, error)
	newContentID func() domain.ContentID
	now          func() time.Time
	lease        time.Duration
	heartbeat    time.Duration
	poll         time.Duration
	maxAttempts  int
	leaseMu      sync.Mutex
}

func NewWorker(archive *Archive, options WorkerOptions) (*Worker, error) {
	if archive == nil {
		return nil, NewSafeError("invalid_configuration", 500, "Job archive is not configured")
	}
	if options.Now == nil {
		options.Now = archive.now
	}
	if options.LeaseDuration <= 0 {
		options.LeaseDuration = defaultWorkerLease
	}
	options.HeartbeatInterval = boundedHeartbeatInterval(options.LeaseDuration, options.HeartbeatInterval)
	if options.PollInterval <= 0 {
		options.PollInterval = time.Second
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = 3
	}
	processor := options.Processor
	if processor == nil {
		processor = archive.processor
	}
	return &Worker{
		store:        archive.store,
		artifacts:    archive.artifacts,
		processor:    processor,
		approve:      archive.approveArticle,
		newContentID: archive.newContentID,
		now:          options.Now,
		lease:        options.LeaseDuration,
		heartbeat:    options.HeartbeatInterval,
		poll:         options.PollInterval,
		maxAttempts:  options.MaxAttempts,
	}, nil
}

var _ Runner = (*Worker)(nil)

// SetProcessor exists for deterministic harnesses that construct the worker
// before deciding which fake pipeline to run. Production callers should inject
// the processor through WorkerOptions or ArchiveOptions before starting Run.
func (w *Worker) SetProcessor(processor Processor) {
	if processor == nil {
		w.processor = unavailableProcessor{}
		return
	}
	w.processor = processor
}

// RunOnce is useful for a deterministic harness and remains outside the
// JobArchive interface.  It returns whether a job was claimed.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	now := w.now().UTC()
	lease, err := w.store.ClaimNext(ctx, now, w.lease)
	if err != nil {
		return false, err
	}
	if lease == nil {
		return false, nil
	}

	processor := w.processor
	if processor == nil {
		processor = unavailableProcessor{}
	}
	processCtx, cancelProcess := context.WithCancel(ctx)
	heartbeatDone := make(chan error, 1)
	go w.renewDuringProcess(processCtx, cancelProcess, lease, heartbeatDone)
	processorContext := ProcessorContext{
		Job:            lease.Job,
		ApproveArticle: w.approve,
		SetStage: func(stage domain.Stage) error {
			w.leaseMu.Lock()
			defer w.leaseMu.Unlock()
			return w.store.SetStage(processCtx, lease, stage)
		},
	}
	result, processErr := processor.Process(processCtx, lease.Job, processorContext)
	cancelProcess()
	heartbeatErr := <-heartbeatDone
	if heartbeatErr != nil && (errors.Is(heartbeatErr, ErrLeaseLost) || processErr == nil || errors.Is(processErr, context.Canceled) || errors.Is(processErr, context.DeadlineExceeded)) {
		processErr = heartbeatErr
	}
	if processErr != nil {
		if errors.Is(processErr, ErrLeaseLost) {
			return true, nil
		}
		return true, w.handleProcessingError(ctx, lease, processErr, now)
	}
	if !result.Article.valid {
		return true, w.handleProcessingError(ctx, lease, NewProcessingError(string(domain.FailureAIInvalidResponse), "The processor did not return AI-approved content", false), now)
	}
	if len(result.PDF) == 0 {
		return true, w.handleProcessingError(ctx, lease, NewProcessingError(string(domain.FailureFormatFailed), "The processor did not return a PDF", false), now)
	}

	if err := w.store.SetStage(ctx, lease, domain.StagePersisting); err != nil {
		if errors.Is(err, ErrLeaseLost) {
			return true, nil
		}
		return true, err
	}
	filename := sanitizeFilename(result.Filename, result.Article.draft.Title, lease.Job.ID)
	artifact, err := w.artifacts.Put(ctx, lease.Job.ID, filename, result.PDF, now)
	if err != nil {
		return true, w.handleProcessingError(ctx, lease, NewProcessingError(string(domain.FailureStorageFailed), "Artifact storage is unavailable", true), now)
	}
	content, ok := result.Article.content(lease.Job, w.newContentID(), now)
	if !ok {
		_ = w.artifacts.Delete(ctx, artifact)
		return true, w.handleProcessingError(ctx, lease, NewProcessingError(string(domain.FailureAIInvalidResponse), "The processor returned invalid article content", false), now)
	}
	if err := w.store.Complete(ctx, lease, Completion{Content: content, Artifact: artifact}, now); err != nil {
		_ = w.artifacts.Delete(ctx, artifact)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return true, err
		}
		handledErr := w.handleProcessingError(ctx, lease, NewProcessingError(
			string(domain.FailureStorageFailed),
			"The completed article could not be committed",
			true,
		), now)
		// A lease can expire between artifact creation and the durable commit.
		// Another worker then owns recovery; do not terminate the runner on the
		// stale worker's private cleanup result.
		if errors.Is(handledErr, ErrLeaseLost) {
			return true, nil
		}
		return true, handledErr
	}
	return true, nil
}

func (w *Worker) handleProcessingError(ctx context.Context, lease *Lease, processErr error, now time.Time) error {
	if lease == nil {
		return ErrLeaseLost
	}
	category := string(domain.FailureInternalError)
	message := "The job could not be completed"
	retryable := false
	var typed *ProcessingError
	if errors.As(processErr, &typed) && typed != nil {
		category = typed.Category
		message = typed.Message
		retryable = typed.Retryable
	}
	failure := domain.Failure{
		Category:      domain.FailureCategory(category),
		Message:       safeJobMessage(category, message),
		CorrelationID: randomOpaqueID(),
		OccurredAt:    now.UTC(),
	}
	if retryable && lease.Job.AttemptCount < w.maxAttempts {
		backoff := time.Duration(lease.Job.AttemptCount) * 250 * time.Millisecond
		return w.store.Requeue(ctx, lease, failure, now.Add(backoff))
	}
	return w.store.Fail(ctx, lease, failure, now)
}

func boundedHeartbeatInterval(leaseDuration, requested time.Duration) time.Duration {
	if leaseDuration <= 0 {
		leaseDuration = defaultWorkerLease
	}
	if requested <= 0 {
		requested = leaseDuration / 3
	}
	if requested <= 0 {
		requested = time.Nanosecond
	}
	if requested > maxHeartbeatInterval {
		requested = maxHeartbeatInterval
	}
	if requested >= leaseDuration {
		requested = leaseDuration / 2
		if requested <= 0 {
			requested = time.Nanosecond
		}
	}
	return requested
}

func (w *Worker) renewDuringProcess(ctx context.Context, cancel context.CancelFunc, lease *Lease, done chan<- error) {
	timer := time.NewTimer(w.heartbeat)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case <-timer.C:
			w.leaseMu.Lock()
			err := w.store.RenewLease(ctx, lease, w.lease)
			w.leaseMu.Unlock()
			if err != nil {
				if ctx.Err() != nil {
					done <- nil
					return
				}
				cancel()
				done <- err
				return
			}
			timer.Reset(w.heartbeat)
		}
	}
}

func safeJobMessage(category, message string) string {
	// A provider or page can put arbitrary text in its error. Keep that text
	// out of durable records and client responses by using fixed category
	// messages instead of echoing adapter details.
	switch category {
	case string(domain.FailureAIUnavailable):
		return "The AI provider is temporarily unavailable"
	case string(domain.FailureAIAuthFailed):
		return "The AI provider rejected the configured credentials"
	case string(domain.FailureAIModelUnsupported):
		return "The configured AI model is unsupported"
	case string(domain.FailureAIInvalidResponse):
		return "The AI provider returned an invalid response"
	case string(domain.FailureUnsupportedContent):
		return "The page is not a supported article"
	case string(domain.FailureInsufficientContent):
		return "The page did not contain enough readable content"
	case string(domain.FailureFormatFailed):
		return "The PDF could not be generated"
	case string(domain.FailureStorageFailed):
		return "Artifact storage is temporarily unavailable"
	case string(domain.FailureFetchFailed):
		return "The page could not be retrieved"
	case string(domain.FailureBlockedTarget):
		return "The destination was blocked by network policy"
	case string(domain.FailureRenderTimeout):
		return "Page rendering exceeded its deadline"
	case string(domain.FailureAccessDenied):
		return "The page denied access"
	case string(domain.FailurePaywallDetected):
		return "The page appears to be behind a paywall"
	case string(domain.FailureDeliveryRejected):
		return "Email delivery was rejected"
	case string(domain.FailureDeliveryTimeout):
		return "Email delivery could not be confirmed"
	default:
		if strings.TrimSpace(message) == "" {
			return "The job could not be completed"
		}
		return "The job could not be completed"
	}
}

func (w *Worker) Run(ctx context.Context) error {
	for {
		_, err := w.RunOnce(ctx)
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		timer := time.NewTimer(w.poll)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}
