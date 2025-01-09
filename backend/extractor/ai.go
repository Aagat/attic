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

// newAIExtractor creates a new AI extractor
func newAIExtractor(llm LLMClient, puppeteer integrations.Puppeteer) *aiExtractor {
	return &aiExtractor{
		llm:       llm,
		puppeteer: puppeteer,
		log:       logger.WithComponent("ai_extractor"),
	}
}

// extract extracts content from a URL using AI
func (a *aiExtractor) extract(ctx context.Context, screenshot []byte) (string, error) {
	// Check for paywall
	isPaywall, err := a.llm.DetectPaywall(ctx, screenshot)
	if err != nil {
		a.log.Error().Err(err).Msg("Failed to detect paywall")
		return "", fmt.Errorf("failed to detect paywall: %w", err)
	}

	if isPaywall {
		a.log.Info().Msg("Paywall detected")
		return "", ErrPaywall
	}

	// Extract content using AI
	content, err := a.llm.ExtractContent(ctx, screenshot)
	if err != nil {
		a.log.Error().Err(err).Msg("Failed to extract content")
		return "", fmt.Errorf("failed to extract content: %w", err)
	}

	return content, nil
}
