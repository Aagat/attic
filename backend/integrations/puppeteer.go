package integrations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/aagat/attic/backend/logger"
	"github.com/rs/zerolog"
)

// ReadabilityContent represents the content and metadata extracted from a webpage using Readability.js
type ReadabilityContent struct {
	Content     string `json:"content"`
	Title       string `json:"title"`
	Byline      string `json:"byline"`
	Excerpt     string `json:"excerpt"`
	TextContent string `json:"textContent"` // Plain text content for length comparison
	Length      int    `json:"length"`      // Length of the article content
	SiteName    string `json:"siteName"`    // Site name from metadata
	IsReadable  bool   `json:"isReadable"`  // Readability's assessment of article parseability
	Screenshot  string `json:"screenshot"`  // Base64 encoded screenshot
	Error       string `json:"error,omitempty"`
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

	if content.Error != "" {
		p.log.Error().Str("error", content.Error).Msg("Extraction returned error")
		// Return the content anyway since it might contain a screenshot
		return &content, fmt.Errorf("extraction failed: %s", content.Error)
	}

	// Check if Readability successfully parsed the article
	if !content.IsReadable {
		p.log.Warn().
			Str("url", url).
			Int("content_length", len(content.Content)).
			Msg("Readability reports article is not parseable")
		// Return the content anyway since it might contain a screenshot
		return &content, fmt.Errorf("content not parseable by readability")
	}

	// Additional heuristic: Check if the extracted content is too short
	// compared to the full text content (indicating possible extraction failure)
	if len(content.TextContent) > 0 && len(content.Content) > 0 {
		textRatio := float64(len(content.TextContent)) / float64(len(content.Content))
		if textRatio < 0.1 { // Less than 10% of HTML content is text
			p.log.Warn().
				Str("url", url).
				Float64("text_ratio", textRatio).
				Int("text_length", len(content.TextContent)).
				Int("html_length", len(content.Content)).
				Msg("Extracted content has too little text compared to HTML")
			// Return the content anyway since it might contain a screenshot
			return &content, fmt.Errorf("extracted content has insufficient text")
		}
	}

	// Check if the content is too short in absolute terms
	if len(content.TextContent) < 100 { // Minimum text content length threshold
		p.log.Warn().
			Str("url", url).
			Int("text_length", len(content.TextContent)).
			Msg("Extracted text content is too short")
		// Return the content anyway since it might contain a screenshot
		return &content, fmt.Errorf("content too short")
	}

	p.log.Info().
		Str("url", url).
		Str("title", content.Title).
		Int("text_length", len(content.TextContent)).
		Int("html_length", len(content.Content)).
		Bool("is_readable", content.IsReadable).
		Str("site_name", content.SiteName).
		Msg("Successfully extracted content")

	return &content, nil
}

// CaptureScreenshot captures a screenshot of a URL
func (p *puppeteerImpl) CaptureScreenshot(ctx context.Context, url string) ([]byte, error) {
	p.log.Info().Str("url", url).Msg("Capturing screenshot")

	// Get content which includes screenshot
	content, err := p.ExtractWithReadability(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to capture screenshot: %w", err)
	}

	// Decode base64 screenshot
	screenshot, err := base64.StdEncoding.DecodeString(content.Screenshot)
	if err != nil {
		p.log.Error().Err(err).Msg("Failed to decode screenshot")
		return nil, fmt.Errorf("failed to decode screenshot: %w", err)
	}

	p.log.Info().
		Str("url", url).
		Int("size", len(screenshot)).
		Msg("Successfully captured screenshot")

	return screenshot, nil
}
