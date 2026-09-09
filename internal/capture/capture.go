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
	renderer SnapshotRenderer
	archives ArchiveSources
}

func New(renderer SnapshotRenderer, archives ArchiveSources) *Capturer {
	return &Capturer{renderer, archives}
}

// Capture returns the best usable snapshot, including a blocked/partial result when
// preservation is incomplete. Errors are reserved for cancellation or total failure.
func (c *Capturer) Capture(ctx context.Context, original string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	best := Result{OriginalURL: original, Status: "blocked"}
	var attempts []Attempt
	var lastErr error
	sources := []string{original}
	for i := 0; i < len(sources); i++ {
		if ctx.Err() != nil {
			return best, ctx.Err()
		}
		page, err := c.renderer.Snapshot(ctx, sources[i])
		status := "failed"
		if err == nil {
			var result Result
			result, err = convert(page)
			if err == nil {
				result.OriginalURL = original
				status = result.Status
				if len(best.HTML) == 0 || (best.Status == "blocked" && result.Status != "blocked") {
					best = result
				}
			}
		}
		if err != nil {
			lastErr = err
		}
		attempts = append(attempts, Attempt{sources[i], status})
		if best.Status != "blocked" && len(best.HTML) > 0 {
			best.Attempts = attempts
			return best, nil
		}
		if i == 0 && c.archives != nil {
			sources = append(sources, c.archives.Candidates(ctx, original)...)
			if len(sources) > 4 {
				sources = sources[:4]
			}
		}
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
