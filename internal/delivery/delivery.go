// Package delivery owns sending stored PDFs, independent of article processing.
package delivery

import (
	"context"
	"io"
	"time"

	"attic/internal/domain"
)

// Claim is a durable, exclusive delivery attempt. Its artifact is already saved.
type Claim struct {
	JobID                                domain.JobID
	Token, MessageID, Destination, Title string
	Artifact                             domain.Artifact
}

type Result struct{ Outcome, Code string }

// Queue owns claims, attempt history and bounded retry scheduling atomically.
type Queue interface {
	ClaimDelivery(context.Context, time.Duration) (*Claim, error)
	FinishDelivery(context.Context, *Claim, Result) error
}

type Artifacts interface {
	Open(context.Context, domain.Artifact) (io.ReadCloser, error)
}
type Sender interface {
	Send(context.Context, *Claim, io.Reader) Result
}

type Worker struct {
	Queue     Queue
	Artifacts Artifacts
	Sender    Sender
	Timeout   time.Duration
}

func (w Worker) RunOnce(ctx context.Context) (bool, error) {
	timeout := w.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	claim, err := w.Queue.ClaimDelivery(ctx, timeout+15*time.Second)
	if err != nil || claim == nil {
		return false, err
	}
	sendCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result := Result{Outcome: "transient_failure"}
	body, err := w.Artifacts.Open(sendCtx, claim.Artifact)
	if err == nil {
		result = w.Sender.Send(sendCtx, claim, body)
		body.Close()
	}
	// Persist known results even when shutdown cancelled the SMTP connection.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	return true, w.Queue.FinishDelivery(finishCtx, claim, result)
}

func (w Worker) Run(ctx context.Context) error {
	for {
		if _, err := w.RunOnce(ctx); err != nil {
			return err
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
