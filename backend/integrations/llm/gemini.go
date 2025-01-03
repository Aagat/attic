package llm

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

// GeminiClient implements the LLM interface using Google's Gemini API
type GeminiClient struct {
	client *genai.Client
	model  string
}

// NewGeminiClient creates a new Gemini client
func NewGeminiClient(cfg Config) (*GeminiClient, error) {
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey: cfg.APIKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	return &GeminiClient{
		client: client,
		model:  cfg.Model,
	}, nil
}

// ExtractContent implements LLM.ExtractContent
func (g *GeminiClient) ExtractContent(ctx context.Context, screenshot []byte) (string, error) {
	prompt := "What's this image about? Extract the main article content."

	parts := []*genai.Part{
		{Text: prompt},
		{InlineData: &genai.Blob{
			Data:     screenshot,
			MIMEType: "image/png",
		}},
	}

	resp, err := g.client.Models.GenerateContent(ctx, g.model, []*genai.Content{{Parts: parts}}, nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate content: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", ErrNoContent
	}

	return resp.Candidates[0].Content.Parts[0].Text, nil
}

// DetectPaywall implements LLM.DetectPaywall
func (g *GeminiClient) DetectPaywall(ctx context.Context, screenshot []byte) (bool, error) {
	prompt := "Does this webpage contain a paywall? Answer with just 'yes' or 'no'."

	parts := []*genai.Part{
		{Text: prompt},
		{InlineData: &genai.Blob{
			Data:     screenshot,
			MIMEType: "image/png",
		}},
	}

	resp, err := g.client.Models.GenerateContent(ctx, g.model, []*genai.Content{{Parts: parts}}, nil)
	if err != nil {
		return false, fmt.Errorf("failed to detect paywall: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return false, ErrNoContent
	}

	answer := strings.ToLower(resp.Candidates[0].Content.Parts[0].Text)
	return strings.Contains(answer, "yes"), nil
}
