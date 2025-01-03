package integrations

import (
	"context"
	"fmt"
	"os/exec"
)

// Puppeteer provides browser automation capabilities
type Puppeteer struct {
	scriptPath string
}

// NewPuppeteer creates a new Puppeteer instance
func NewPuppeteer(scriptPath string) *Puppeteer {
	return &Puppeteer{
		scriptPath: scriptPath,
	}
}

// CaptureScreenshot takes a screenshot of a webpage
func (p *Puppeteer) CaptureScreenshot(ctx context.Context, url string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "node", p.scriptPath, "screenshot", url)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to capture screenshot: %w", err)
	}
	return output, nil
}

// ExtractWithReadability extracts content using Readability.js
func (p *Puppeteer) ExtractWithReadability(ctx context.Context, url string) (string, error) {
	cmd := exec.CommandContext(ctx, "node", p.scriptPath, "extract", url)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to extract content: %w", err)
	}
	return string(output), nil
}
