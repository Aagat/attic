// Package memory provides deterministic adapters for application tests and
// the minimal local foundation binary.  It deliberately mirrors the durable
// JobStore contract rather than exposing map details to callers.
package memory

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"attic/internal/application"
	"attic/internal/domain"
)

type idempotencyRecord struct {
	jobID     domain.JobID
	digest    string
	createdAt time.Time
}

type Store struct {
	mu          sync.RWMutex
	jobs        map[domain.JobID]domain.Job
	idempotency map[domain.IdempotencyKey]idempotencyRecord
	now         func() time.Time
}

func NewStore() *Store {
	return NewStoreWithClock(func() time.Time { return time.Now().UTC() })
}

// NewStoreWithClock is useful for deterministic adapter tests. Production
// callers should use NewStore, which uses the wall clock for lease expiry.
func NewStoreWithClock(now func() time.Time) *Store {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Store{
		jobs:        make(map[domain.JobID]domain.Job),
		idempotency: make(map[domain.IdempotencyKey]idempotencyRecord),
		now:         now,
	}
}

func (s *Store) CreateOrReuse(ctx context.Context, command application.CreateJob) (application.CreateOutcome, error) {
	if err := contextErr(ctx); err != nil {
		return application.CreateOutcome{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if command.IdempotencyKey != "" {
		if previous, ok := s.idempotency[command.IdempotencyKey]; ok {
			if !previous.createdAt.IsZero() && command.CreatedAt.Sub(previous.createdAt) >= 24*time.Hour {
				delete(s.idempotency, command.IdempotencyKey)
				ok = false
			}
		}
		if previous, ok := s.idempotency[command.IdempotencyKey]; ok {
			job, exists := s.jobs[previous.jobID]
			if !exists {
				delete(s.idempotency, command.IdempotencyKey)
			} else if previous.digest != command.RequestDigest {
				return application.CreateOutcome{Conflict: true}, application.ErrIdempotencyConflict
			} else {
				return application.CreateOutcome{Job: cloneJob(job), Replayed: true}, nil
			}
		}
	}
	job := domain.Job{
		ID:             command.ID,
		SubmittedURL:   command.Request.URL,
		TitleHint:      command.Request.Title,
		Profile:        command.Request.Profile,
		Status:         domain.StatusQueued,
		CreatedAt:      command.CreatedAt.UTC(),
		NextAttemptAt:  command.CreatedAt.UTC(),
		Version:        1,
		IdempotencyKey: command.IdempotencyKey,
		RequestDigest:  command.RequestDigest,
	}
	s.jobs[job.ID] = job
	if command.IdempotencyKey != "" {
		s.idempotency[command.IdempotencyKey] = idempotencyRecord{jobID: job.ID, digest: command.RequestDigest, createdAt: command.CreatedAt.UTC()}
	}
	return application.CreateOutcome{Job: cloneJob(job)}, nil
}

func (s *Store) List(ctx context.Context, request application.StoreListRequest) (application.StorePage, error) {
	if err := contextErr(ctx); err != nil {
		return application.StorePage{}, err
	}
	s.mu.RLock()
	jobs := make([]domain.Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		if request.Status != nil && job.Status != *request.Status {
			continue
		}
		if request.After != nil && !beforeCursor(job, *request.After) {
			continue
		}
		jobs = append(jobs, cloneJob(job))
	}
	s.mu.RUnlock()
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].CreatedAt.Equal(jobs[j].CreatedAt) {
			return jobs[i].ID > jobs[j].ID
		}
		return jobs[i].CreatedAt.After(jobs[j].CreatedAt)
	})
	limit := request.Limit
	if limit <= 0 {
		limit = 25
	}
	hasMore := len(jobs) > limit
	if hasMore {
		jobs = jobs[:limit]
	}
	return application.StorePage{Jobs: jobs, HasMore: hasMore}, nil
}

func beforeCursor(job domain.Job, cursor domain.CursorPosition) bool {
	if job.CreatedAt.Before(cursor.CreatedAt) {
		return true
	}
	return job.CreatedAt.Equal(cursor.CreatedAt) && job.ID < cursor.ID
}

func (s *Store) Get(ctx context.Context, id domain.JobID) (domain.Job, error) {
	if err := contextErr(ctx); err != nil {
		return domain.Job{}, err
	}
	s.mu.RLock()
	job, ok := s.jobs[id]
	s.mu.RUnlock()
	if !ok {
		return domain.Job{}, application.ErrNotFound
	}
	return cloneJob(job), nil
}

func (s *Store) CreateRetry(ctx context.Context, sourceID, newID domain.JobID, now time.Time) (domain.Job, error) {
	if err := contextErr(ctx); err != nil {
		return domain.Job{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	source, ok := s.jobs[sourceID]
	if !ok {
		return domain.Job{}, application.ErrNotFound
	}
	if !source.Status.Terminal() {
		return domain.Job{}, application.ErrNotTerminal
	}
	now = now.UTC()
	job := domain.Job{
		ID:            newID,
		RetryOfJobID:  source.ID,
		SubmittedURL:  source.SubmittedURL,
		TitleHint:     source.TitleHint,
		Profile:       source.Profile,
		Status:        domain.StatusQueued,
		CreatedAt:     now,
		NextAttemptAt: now,
		Version:       1,
	}
	s.jobs[job.ID] = job
	return cloneJob(job), nil
}

func (s *Store) RequestDelete(ctx context.Context, id domain.JobID, now time.Time) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return application.ErrNotFound
	}
	// The in-memory adapter can finish cancellation synchronously.  A durable
	// adapter may instead retain a deletion task after making the same logical
	// transition.
	job.Status = domain.StatusCancelled
	job.Stage = ""
	completed := now.UTC()
	job.CompletedAt = &completed
	job.Version++
	if job.IdempotencyKey != "" {
		delete(s.idempotency, job.IdempotencyKey)
	}
	delete(s.jobs, id)
	return nil
}

func (s *Store) ClaimNext(ctx context.Context, now time.Time, leaseDuration time.Duration) (*application.Lease, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	now = now.UTC()
	if leaseDuration <= 0 {
		leaseDuration = 5 * time.Minute
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var selected *domain.Job
	for id, candidate := range s.jobs {
		if candidate.Status == domain.StatusDelivering && !candidate.LeaseUntil.IsZero() && !candidate.LeaseUntil.After(now) {
			completed := now.UTC()
			candidate.Status = domain.StatusDeliveryFailed
			candidate.Stage = ""
			candidate.Failure = &domain.Failure{
				Category:   domain.FailureDeliveryTimeout,
				Message:    "Email delivery could not be confirmed",
				OccurredAt: completed,
			}
			candidate.CompletedAt = &completed
			candidate.LeaseToken = ""
			candidate.LeaseUntil = time.Time{}
			candidate.NextAttemptAt = completed
			candidate.Version++
			s.jobs[id] = candidate
			continue
		}
		if candidate.Status == domain.StatusProcessing && !candidate.LeaseUntil.IsZero() && !candidate.LeaseUntil.After(now) {
			candidate.Status = domain.StatusQueued
			candidate.Stage = ""
			candidate.LeaseToken = ""
			candidate.LeaseUntil = time.Time{}
			candidate.NextAttemptAt = now
			candidate.Failure = nil
			candidate.Version++
			s.jobs[id] = candidate
		}
		if candidate.Status != domain.StatusQueued || candidate.DeleteRequested || candidate.NextAttemptAt.After(now) {
			continue
		}
		copy := cloneJob(candidate)
		if selected == nil || copy.CreatedAt.Before(selected.CreatedAt) || (copy.CreatedAt.Equal(selected.CreatedAt) && copy.ID < selected.ID) {
			selected = &copy
		}
	}
	if selected == nil {
		return nil, nil
	}
	job := s.jobs[selected.ID]
	job.Status = domain.StatusProcessing
	job.Stage = domain.StageFetching
	job.AttemptCount++
	job.Version++
	job.LeaseToken = randomToken()
	leaseBase := job.LeaseUntil
	if leaseBase.Before(now) {
		leaseBase = now
	}
	job.LeaseUntil = leaseBase.Add(leaseDuration)
	s.jobs[job.ID] = job
	return application.NewLease(cloneJob(job), job.LeaseToken, job.LeaseUntil), nil
}

func (s *Store) RenewLease(ctx context.Context, lease *application.Lease, leaseDuration time.Duration) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if lease == nil {
		return application.ErrLeaseLost
	}
	if leaseDuration <= 0 {
		leaseDuration = 5 * time.Minute
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[lease.Job.ID]
	if !ok || (job.Status != domain.StatusProcessing && job.Status != domain.StatusDelivering) || job.LeaseToken != lease.Token || job.Version != lease.CurrentVersion() || !job.LeaseUntil.After(now) {
		return application.ErrLeaseLost
	}
	leaseBase := job.LeaseUntil
	if leaseBase.Before(now) {
		leaseBase = now
	}
	job.LeaseUntil = leaseBase.Add(leaseDuration)
	job.Version++
	job.NextAttemptAt = job.NextAttemptAt.UTC()
	s.jobs[job.ID] = job
	lease.Observe(job.Version, job.LeaseUntil, job.Stage)
	return nil
}

func (s *Store) SetStage(ctx context.Context, lease *application.Lease, stage domain.Stage) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if !stage.Valid() {
		return application.ErrInvalidStage
	}
	if lease == nil {
		return application.ErrLeaseLost
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	job, ok := s.jobs[lease.Job.ID]
	if !ok || job.LeaseToken != lease.Token || job.Status != domain.StatusProcessing || job.Version != lease.CurrentVersion() || !job.LeaseUntil.After(now) {
		return application.ErrLeaseLost
	}
	if stage != job.Stage && nextStage(job.Stage) != stage && !domain.CanRecoverSource(job.Stage, stage) {
		return application.ErrInvalidStage
	}
	job.Stage = stage
	job.Version++
	s.jobs[job.ID] = job
	lease.Observe(job.Version, job.LeaseUntil, job.Stage)
	return nil
}

func (s *Store) Complete(ctx context.Context, lease *application.Lease, completion application.Completion, now time.Time) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if !completion.Artifact.Available || completion.Artifact.ByteSize <= 0 || completion.Content.ID == "" {
		return errors.New("incomplete completion")
	}
	if lease == nil {
		return application.ErrLeaseLost
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[lease.Job.ID]
	if !ok || job.LeaseToken != lease.Token || job.Status != domain.StatusProcessing || job.Version != lease.CurrentVersion() || !job.LeaseUntil.After(s.now().UTC()) {
		return application.ErrLeaseLost
	}
	if job.Stage != domain.StagePersisting {
		return application.ErrInvalidStage
	}
	content := completion.Content
	content.JobID = job.ID
	job.Content = &content
	job.CanonicalURL = completion.CanonicalURL
	artifact := completion.Artifact
	job.Artifact = &artifact
	job.AIConfidence = content.AIConfidence
	job.Status = domain.StatusReady
	job.Stage = ""
	completed := now.UTC()
	job.CompletedAt = &completed
	job.LeaseToken = ""
	job.LeaseUntil = time.Time{}
	job.Version++
	s.jobs[job.ID] = job
	lease.Observe(job.Version, job.LeaseUntil, job.Stage)
	return nil
}

func (s *Store) Requeue(ctx context.Context, lease *application.Lease, failure domain.Failure, next time.Time) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if lease == nil {
		return application.ErrLeaseLost
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[lease.Job.ID]
	if !ok || job.LeaseToken != lease.Token || job.Version != lease.CurrentVersion() || !job.LeaseUntil.After(s.now().UTC()) {
		return application.ErrLeaseLost
	}
	job.Status = domain.StatusQueued
	job.Stage = ""
	job.NextAttemptAt = next.UTC()
	job.LeaseToken = ""
	job.LeaseUntil = time.Time{}
	job.Version++
	s.jobs[job.ID] = job
	lease.Observe(job.Version, job.LeaseUntil, job.Stage)
	return nil
}

func (s *Store) Fail(ctx context.Context, lease *application.Lease, failure domain.Failure, now time.Time) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if lease == nil {
		return application.ErrLeaseLost
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[lease.Job.ID]
	if !ok || job.LeaseToken != lease.Token || job.Version != lease.CurrentVersion() || !job.LeaseUntil.After(s.now().UTC()) {
		return application.ErrLeaseLost
	}
	failure.OccurredAt = now.UTC()
	job.Status = domain.StatusFailed
	job.Stage = ""
	job.Failure = &failure
	completed := now.UTC()
	job.CompletedAt = &completed
	job.LeaseToken = ""
	job.LeaseUntil = time.Time{}
	job.Version++
	s.jobs[job.ID] = job
	lease.Observe(job.Version, job.LeaseUntil, job.Stage)
	return nil
}

func nextStage(stage domain.Stage) domain.Stage {
	switch stage {
	case domain.StageFetching:
		return domain.StageExtracting
	case domain.StageExtracting:
		return domain.StageAIAnalyzing
	case domain.StageAIAnalyzing:
		return domain.StageFormatting
	case domain.StageFormatting:
		return domain.StagePersisting
	default:
		return ""
	}
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func cloneJob(job domain.Job) domain.Job {
	if job.Content != nil {
		content := *job.Content
		job.Content = &content
	}
	if job.CompletedAt != nil {
		value := *job.CompletedAt
		job.CompletedAt = &value
	}
	if job.Failure != nil {
		failure := *job.Failure
		job.Failure = &failure
	}
	if job.Artifact != nil {
		artifact := *job.Artifact
		job.Artifact = &artifact
	}
	if job.Delivery != nil {
		delivery := *job.Delivery
		job.Delivery = &delivery
	}
	return job
}

type ArtifactStore struct {
	mu    sync.RWMutex
	files map[string][]byte
}

func NewArtifactStore() *ArtifactStore {
	return &ArtifactStore{files: make(map[string][]byte)}
}

func (s *ArtifactStore) Put(ctx context.Context, jobID domain.JobID, filename string, data []byte, now time.Time) (domain.Artifact, error) {
	if err := contextErr(ctx); err != nil {
		return domain.Artifact{}, err
	}
	filename = strings.TrimSpace(filename)
	if filename == "" || strings.ContainsAny(filename, `/\\`) || filename == "." || filename == ".." {
		return domain.Artifact{}, errors.New("unsafe artifact filename")
	}
	if len(data) == 0 {
		return domain.Artifact{}, errors.New("empty artifact")
	}
	copyData := append([]byte(nil), data...)
	hash := sha256.Sum256(copyData)
	key := string(jobID) + "/" + filename
	s.mu.Lock()
	s.files[key] = copyData
	s.mu.Unlock()
	return domain.Artifact{
		Key:       key,
		Filename:  filename,
		MediaType: "application/pdf",
		ByteSize:  int64(len(copyData)),
		Checksum:  hex.EncodeToString(hash[:]),
		Available: true,
		CreatedAt: now.UTC(),
	}, nil
}

func (s *ArtifactStore) Open(ctx context.Context, artifact domain.Artifact) (io.ReadCloser, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if !artifact.Available {
		return nil, os.ErrNotExist
	}
	s.mu.RLock()
	data, ok := s.files[artifact.Key]
	copyData := append([]byte(nil), data...)
	s.mu.RUnlock()
	if !ok {
		return nil, os.ErrNotExist
	}
	if int64(len(copyData)) != artifact.ByteSize {
		return nil, errors.New("artifact size mismatch")
	}
	hash := sha256.Sum256(copyData)
	if hex.EncodeToString(hash[:]) != artifact.Checksum {
		return nil, errors.New("artifact checksum mismatch")
	}
	return io.NopCloser(bytes.NewReader(copyData)), nil
}

func (s *ArtifactStore) Delete(ctx context.Context, artifact domain.Artifact) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.files, artifact.Key)
	s.mu.Unlock()
	return nil
}

func randomToken() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(data[:])
}
