// Package capture preserves public pages independently of article approval and PDF generation.
package capture

import (
	"context"
	"errors"
	"strings"
	"time"

	"attic/internal/acquisition"
)

type SnapshotRenderer interface {
	Snapshot(context.Context, string) (acquisition.RenderedPage, error)
}
type ArchiveSources interface {
	Candidates(context.Context, string) []string
}
type Result struct {
	HTML                                            []byte
	PlainText, Title, OriginalURL, FinalURL, Status string
	MissingResources                                []string
	Attempts                                        []Attempt
}
type Attempt struct {
	URL    string `json:"url"`
	Status string `json:"status"`
}
type Capturer struct {
	renderer    SnapshotRenderer
	archives    ArchiveSources
	fetchJSON   func(context.Context, string) (acquisition.Page, error)
	fetchMirror func(context.Context, string) (acquisition.Page, error)
}

func New(renderer SnapshotRenderer, archives ArchiveSources) *Capturer {
	return &Capturer{renderer: renderer, archives: archives}
}

// Capture returns the best usable snapshot, including a blocked/partial result when
// preservation is incomplete. Errors are reserved for cancellation or total failure.
func (c *Capturer) Capture(ctx context.Context, original string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	best := Result{OriginalURL: original, Status: "blocked"}
	var attempts []Attempt
	var lastErr error
	for source := range c.Sources(ctx, original) {
		result, err := source.Result()
		status := "failed"
		if err == nil {
			status = result.Status
			if len(best.HTML) == 0 || result.Status != "blocked" {
				best = result
			}
		} else {
			lastErr = err
		}
		attempts = append(attempts, Attempt{source.URL, status})
		if err == nil && status != "blocked" && len(best.HTML) > 0 && !source.Fallback {
			best.Attempts = attempts
			return best, nil
		}
	}
	if ctx.Err() != nil {
		best.Attempts = attempts
		return best, ctx.Err()
	}
	best.Attempts = attempts
	if len(best.HTML) > 0 {
		return best, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no usable page capture")
	}
	return best, lastErr
}
func blocked(page acquisition.RenderedPage, text string) bool {
	if page.Status >= 400 {
		return true
	}
	title := strings.ToLower(strings.TrimSpace(page.Title))
	body := strings.ToLower(text)
	for _, marker := range []string{"just a moment", "access denied", "captcha", "security verification", "robot check"} {
		if title == marker || title == marker+"..." || title == marker+" – archive.today" {
			return true
		}
	}
	if len(strings.Fields(body)) < 100 {
		for _, marker := range []string{"something went wrong. try reloading", "something went wrong, but don’t fret", "something went wrong, but don't fret", "sign in to x", "log in to x", "too many requests"} {
			if strings.Contains(body, marker) {
				return true
			}
		}
	}
	// Challenge instructions in a short page are meaningful; an article discussing
	// CAPTCHAs or quoting these phrases should remain readable.
	if len(strings.Fields(body)) < 300 {
		for _, marker := range []string{"verify you are human", "verify that you are human", "enable javascript and cookies to continue", "complete the captcha", "please solve the captcha", "why do i have to complete a captcha", "please complete the security check", "subscribe to continue reading", "subscribe to read the full", "sign in to continue reading"} {
			if strings.Contains(body, marker) {
				return true
			}
		}
	}
	return strings.TrimSpace(text) == ""
}
