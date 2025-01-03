package extractor

import (
	"context"
	"fmt"

	"github.com/aagat/attic/backend/integrations"
	"github.com/aagat/attic/backend/integrations/llm"
)

type aiExtractor struct {
	llm       llm.LLM
	puppeteer *integrations.Puppeteer
}

func newAIExtractor(llm llm.LLM, puppeteer *integrations.Puppeteer) *aiExtractor {
	return &aiExtractor{
		llm:       llm,
		puppeteer: puppeteer,
	}
}

func (a *aiExtractor) extract(ctx context.Context, url string) ([]byte, error) {
	// Take screenshot of the page
	screenshot, err := a.puppeteer.CaptureScreenshot(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to capture screenshot: %w", err)
	}

	// Check for paywall
	hasPaywall, err := a.llm.DetectPaywall(ctx, screenshot)
	if err != nil {
		return nil, fmt.Errorf("failed to detect paywall: %w", err)
	}

	if hasPaywall {
		return nil, fmt.Errorf("content is behind a paywall")
	}

	// Extract content using LLM
	content, err := a.llm.ExtractContent(ctx, screenshot)
	if err != nil {
		return nil, fmt.Errorf("failed to extract content: %w", err)
	}

	return []byte(content), nil
}
