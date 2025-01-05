package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/aagat/attic/backend/logger"
	"github.com/rs/zerolog"
)

// ReadabilityContent represents the content and metadata extracted from a webpage using Readability.js
type ReadabilityContent struct {
	Content string `json:"content"`
	Title   string `json:"title"`
	Byline  string `json:"byline"`
	Excerpt string `json:"excerpt"`
	Error   string `json:"error,omitempty"`
}

// Puppeteer defines the interface for browser automation and content extraction
type Puppeteer interface {
	ExtractWithReadability(ctx context.Context, url string) (*ReadabilityContent, error)
	CaptureScreenshot(ctx context.Context, url string) ([]byte, error)
}

// puppeteerImpl provides PDF manipulation functionality using poppler-utils
type puppeteerImpl struct {
	scriptPath string
	log        zerolog.Logger
}

// NewPuppeteer creates a new Puppeteer instance
func NewPuppeteer() (Puppeteer, error) {
	return &puppeteerImpl{
		scriptPath: filepath.Join("scripts", "puppeteer", "extract.js"),
		log:        logger.WithComponent("puppeteer"),
	}, nil
}

// ExtractWithReadability extracts content from a URL using Readability.js
func (p *puppeteerImpl) ExtractWithReadability(ctx context.Context, url string) (*ReadabilityContent, error) {
	p.log.Info().Str("url", url).Msg("Extracting content using Readability")

	cmd := exec.CommandContext(ctx, "node", p.scriptPath, "--url", url)
	output, err := cmd.Output()
	if err != nil {
		var stderr string
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = string(exitErr.Stderr)
		}
		p.log.Error().Err(err).Str("stderr", stderr).Msg("Failed to extract content")
		return nil, fmt.Errorf("puppeteer failed: %w", err)
	}

	var content ReadabilityContent
	if err := json.Unmarshal(output, &content); err != nil {
		p.log.Error().Err(err).Msg("Failed to parse extracted content")
		return nil, fmt.Errorf("failed to parse content: %w", err)
	}

	p.log.Info().
		Str("url", url).
		Str("title", content.Title).
		Int("content_length", len(content.Content)).
		Msg("Successfully extracted content")

	return &content, nil
}

// CaptureScreenshot captures a screenshot of a URL
func (p *puppeteerImpl) CaptureScreenshot(ctx context.Context, url string) ([]byte, error) {
	p.log.Info().Str("url", url).Msg("Capturing screenshot")

	cmd := exec.CommandContext(ctx, "node", p.scriptPath, "--url", url, "--screenshot")
	output, err := cmd.Output()
	if err != nil {
		var stderr string
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = string(exitErr.Stderr)
		}
		p.log.Error().Err(err).Str("stderr", stderr).Msg("Failed to capture screenshot")
		return nil, fmt.Errorf("screenshot failed: %w", err)
	}

	p.log.Info().
		Str("url", url).
		Int("size", len(output)).
		Msg("Successfully captured screenshot")

	return output, nil
}
