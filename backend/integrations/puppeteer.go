package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
)

// Puppeteer provides methods for browser automation and content extraction
type Puppeteer interface {
	// ExtractWithReadability extracts content from a URL using Readability.js
	ExtractWithReadability(ctx context.Context, url string) (string, error)
	// CaptureScreenshot takes a screenshot of a webpage
	CaptureScreenshot(ctx context.Context, url string) ([]byte, error)
}

// puppeteerImpl implements the Puppeteer interface using Node.js and Puppeteer
type puppeteerImpl struct {
	scriptPath string
}

// NewPuppeteer creates a new Puppeteer instance
func NewPuppeteer() (Puppeteer, error) {
	// Check if Node.js is installed
	if _, err := exec.LookPath("node"); err != nil {
		return nil, fmt.Errorf("node.js is not installed: %w", err)
	}

	// Get the absolute path to the script directory
	scriptPath, err := filepath.Abs("scripts/puppeteer")
	if err != nil {
		return nil, fmt.Errorf("failed to get script path: %w", err)
	}

	return &puppeteerImpl{
		scriptPath: scriptPath,
	}, nil
}

// ExtractWithReadability extracts content from a URL using Readability.js
func (p *puppeteerImpl) ExtractWithReadability(ctx context.Context, url string) (string, error) {
	// Run the Node.js script
	cmd := exec.CommandContext(ctx, "node",
		filepath.Join(p.scriptPath, "extract.js"),
		"--url", url,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to run extract script: %w, stderr: %s", err, stderr.String())
	}

	// Parse the JSON response
	var result struct {
		Content string `json:"content"`
		Error   string `json:"error,omitempty"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return "", fmt.Errorf("failed to parse script output: %w", err)
	}

	if result.Error != "" {
		return "", fmt.Errorf("script error: %s", result.Error)
	}

	return result.Content, nil
}

// CaptureScreenshot takes a screenshot of a webpage
func (p *puppeteerImpl) CaptureScreenshot(ctx context.Context, url string) ([]byte, error) {
	// Run the Node.js script
	cmd := exec.CommandContext(ctx, "node",
		filepath.Join(p.scriptPath, "screenshot.js"),
		"--url", url,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to run screenshot script: %w, stderr: %s", err, stderr.String())
	}

	// The script outputs the base64-encoded screenshot
	return stdout.Bytes(), nil
}
