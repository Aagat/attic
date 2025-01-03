package integrations

import (
	"context"
	"fmt"
	"os/exec"
)

// Puppeteer defines the interface for browser automation
type Puppeteer interface {
	ExtractWithReadability(ctx context.Context, url string) (string, error)
	CaptureScreenshot(ctx context.Context, url string) ([]byte, error)
}

// puppeteerImpl provides browser automation capabilities
type puppeteerImpl struct {
	scriptPath string
}

// NewPuppeteer creates a new Puppeteer instance
func NewPuppeteer(scriptPath string) Puppeteer {
	return &puppeteerImpl{
		scriptPath: scriptPath,
	}
}

// CaptureScreenshot takes a screenshot of a webpage
func (p *puppeteerImpl) CaptureScreenshot(ctx context.Context, url string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "node", p.scriptPath, "screenshot", url)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to capture screenshot: %w", err)
	}
	return output, nil
}

// ExtractWithReadability extracts content using Readability.js
func (p *puppeteerImpl) ExtractWithReadability(ctx context.Context, url string) (string, error) {
	cmd := exec.CommandContext(ctx, "node", p.scriptPath, "extract", url)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to extract content: %w", err)
	}
	return string(output), nil
}
