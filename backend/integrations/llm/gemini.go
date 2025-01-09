package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aagat/attic/backend/extractor"
	"github.com/aagat/attic/backend/interfaces"
	"google.golang.org/genai"
)

// GeminiClient implements the LLM interface using Google's Gemini API
type GeminiClient struct {
	client *genai.Client
	model  string
}

// Ensure GeminiClient implements interfaces.LLMClient
var _ interfaces.LLMClient = (*GeminiClient)(nil)

// AnalysisResult represents the structured output from Gemini
type AnalysisResult struct {
	HasPaywall bool   `json:"hasPaywall"`
	Content    string `json:"content"`
	Reason     string `json:"reason"`
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

// ExtractContent implements interfaces.LLMClient
func (g *GeminiClient) ExtractContent(ctx context.Context, screenshot []byte) (string, error) {
	prompt := `Analyze this webpage screenshot and extract the main content. If there's a paywall, extract whatever content is visible.
If the page is not an article, describe what you see.`

	content := []*genai.Content{
		{
			Parts: []*genai.Part{
				{Text: prompt},
				{InlineData: &genai.Blob{
					Data:     screenshot,
					MIMEType: "image/jpeg",
				}},
			},
		},
	}

	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"hasPaywall": {Type: genai.TypeBoolean},
				"content":    {Type: genai.TypeString},
				"reason":     {Type: genai.TypeString},
			},
			Required: []string{"hasPaywall", "content", "reason"},
		},
	}

	resp, err := g.client.Models.GenerateContent(ctx, g.model, content, config)
	if err != nil {
		return "", fmt.Errorf("failed to generate content: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", extractor.ErrNoContent
	}

	// Parse the JSON response
	var result AnalysisResult
	if err := json.Unmarshal([]byte(resp.Candidates[0].Content.Parts[0].Text), &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if result.HasPaywall {
		return "", extractor.ErrPaywall
	}

	return result.Content, nil
}
