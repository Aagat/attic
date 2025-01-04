package extractor

import (
	"context"
	"fmt"
	"log"

	"github.com/aagat/attic/backend/integrations"
	"github.com/aagat/attic/backend/integrations/llm"
)

type aiExtractor struct {
	llm       llm.LLM
	puppeteer integrations.Puppeteer
}

func newAIExtractor(llm llm.LLM, puppeteer integrations.Puppeteer) *aiExtractor {
	return &aiExtractor{
		llm:       llm,
		puppeteer: puppeteer,
	}
}

func (a *aiExtractor) extract(ctx context.Context, url string, content ...[]byte) ([]byte, error) {
	var screenshot []byte
	var err error

	if len(content) > 0 {
		// Content provided directly (e.g., for image processing)
		screenshot = content[0]
	} else {
		// Take screenshot of the URL
		log.Printf("Taking screenshot of %s", url)
		screenshot, err = a.puppeteer.CaptureScreenshot(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("failed to capture screenshot: %w", err)
		}
	}

	// Check for paywall
	log.Printf("Checking for paywall")
	hasPaywall, err := a.llm.DetectPaywall(ctx, screenshot)
	if err != nil {
		log.Printf("Failed to detect paywall: %v", err)
		// Continue with content extraction even if paywall detection fails
	} else if hasPaywall {
		log.Printf("Paywall detected")
		return nil, ErrPaywall
	}

	// Extract content using LLM
	log.Printf("Extracting content using LLM")
	extractedContent, err := a.llm.ExtractContent(ctx, screenshot)
	if err != nil {
		return nil, fmt.Errorf("failed to extract content: %w", err)
	}

	if extractedContent == "" {
		return nil, fmt.Errorf("no content extracted")
	}

	return []byte(extractedContent), nil
}
