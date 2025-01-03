package llm

import (
	"context"
	"errors"
)

// ErrNoContent is returned when no content could be extracted
var ErrNoContent = errors.New("no content could be extracted")

// LLM defines the interface for interacting with language models
type LLM interface {
	// ExtractContent takes a webpage screenshot and returns the main content
	ExtractContent(ctx context.Context, screenshot []byte) (string, error)
	// DetectPaywall determines if a webpage has a paywall
	DetectPaywall(ctx context.Context, screenshot []byte) (bool, error)
}

// Config holds configuration for LLM clients
type Config struct {
	APIKey string
	Model  string
}
