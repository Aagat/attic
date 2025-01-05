package extractor

import (
	"context"
	"fmt"

	"github.com/aagat/attic/backend/integrations"
	"github.com/aagat/attic/backend/logger"
	"github.com/rs/zerolog"
)

// aiExtractor handles content extraction using AI
type aiExtractor struct {
	llm       LLMClient
	puppeteer integrations.Puppeteer
	log       zerolog.Logger
}

// newAIExtractor creates a new AI extractor instance
func newAIExtractor(llm LLMClient, puppeteer integrations.Puppeteer) *aiExtractor {
	return &aiExtractor{
		llm:       llm,
		puppeteer: puppeteer,
		log:       logger.WithComponent("ai_extractor"),
	}
}

// extract attempts to extract content from a URL using AI
func (a *aiExtractor) extract(ctx context.Context, url string) (string, error) {
	a.log.Info().Str("url", url).Msg("Taking screenshot for AI processing")

	// Take a screenshot of the page
	screenshot, err := a.puppeteer.CaptureScreenshot(ctx, url)
	if err != nil {
		a.log.Error().Err(err).Str("url", url).Msg("Failed to capture screenshot")
		return "", fmt.Errorf("failed to capture screenshot: %w", err)
	}

	// Check for paywall
	isPaywall, err := a.llm.DetectPaywall(ctx, screenshot)
	if err != nil {
		a.log.Error().Err(err).Str("url", url).Msg("Failed to detect paywall")
		return "", fmt.Errorf("failed to detect paywall: %w", err)
	}
	if isPaywall {
		a.log.Warn().Str("url", url).Msg("Paywall detected")
		return "", ErrPaywall
	}

	// Extract content using AI
	content, err := a.llm.ExtractContent(ctx, screenshot)
	if err != nil {
		a.log.Error().Err(err).Str("url", url).Msg("Failed to extract content using AI")
		return "", fmt.Errorf("failed to extract content: %w", err)
	}

	if content == "" {
		a.log.Warn().Str("url", url).Msg("AI returned empty content")
		return "", fmt.Errorf("no content extracted")
	}

	a.log.Info().
		Str("url", url).
		Int("content_length", len(content)).
		Msg("Successfully extracted content using AI")

	return content, nil
}
