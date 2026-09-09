package processing

import (
	"context"
	"errors"
	"testing"

	"attic/internal/acquisition"
	"attic/internal/ai"
	"attic/internal/application"
	"attic/internal/capture"
	"attic/internal/domain"
	"attic/internal/formatter"
)

type staticRenderer struct{ called bool }

func (r *staticRenderer) RenderSaved(_ context.Context, raw string, body []byte) (acquisition.RenderedPage, error) {
	r.called = true
	return acquisition.RenderedPage{FinalURL: raw, DOM: body, Screenshot: []byte("offline viewport"), Status: 200}, nil
}

type rejectStatic struct {
	t      *testing.T
	called bool
	err    error
}

func (a *rejectStatic) Approve(_ context.Context, _ domain.JobID, in ai.ApprovalInput, _ func(application.ArticleDraft) (application.ApprovedArticle, error)) (application.ApprovedArticle, error) {
	a.called = true
	if in.SourceURL != "https://x.com/sample/status/12345" || in.RetrievedURL != "https://xcancel.com/sample/status/12345" || in.ScreenshotDataURL == "" {
		a.t.Fatalf("static provenance or offline screenshot lost: %+v", in)
	}
	return application.ApprovedArticle{}, a.err
}

type noStaticPDF struct{ t *testing.T }

func (f noStaticPDF) Format(context.Context, formatter.Article) (formatter.Result, error) {
	f.t.Fatal("unapproved static content reached formatter")
	return formatter.Result{}, nil
}

func TestStaticSourceRequiresOfflineRenderingAndApproval(t *testing.T) {
	renderer := &staticRenderer{}
	rejection := application.NewProcessingError(string(domain.FailureInsufficientContent), "incomplete post", false)
	approver := &rejectStatic{t: t, err: rejection}
	p := &Processor{savedRenderer: renderer, approver: approver, formatter: noStaticPDF{t}}
	source := capture.Source{URL: "https://xcancel.com/sample/status/12345", Static: &capture.Result{FinalURL: "https://xcancel.com/sample/status/12345", HTML: []byte("<article><p>Preserved main post.</p></article>"), Status: "partial"}}
	_, err := p.processAttempt(context.Background(), domain.Job{SubmittedURL: "https://x.com/sample/status/12345"}, application.ProcessorContext{SetStage: func(domain.Stage) error { return nil }}, source)
	if !renderer.called || !approver.called || !errors.Is(err, rejection) {
		t.Fatalf("rendered=%v approved=%v err=%v", renderer.called, approver.called, err)
	}
}
