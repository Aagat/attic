package interfaces

import "context"

// LLMClient defines the interface for LLM-based content extraction
type LLMClient interface {
	ExtractContent(ctx context.Context, screenshot []byte) (string, error)
}
