package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aagat/attic/backend/extractor"
	"github.com/aagat/attic/backend/interfaces"
	"github.com/aagat/attic/backend/logger"
	"github.com/rs/zerolog"
	"google.golang.org/genai"
)

// GeminiClient implements the LLM interface using Google's Gemini API
type GeminiClient struct {
	client *genai.Client
	model  string
	log    zerolog.Logger
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
		log:    logger.WithComponent("gemini"),
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
				"hasPaywall": {Type: genai.TypeBoolean, Description: "Whether the page has a paywall"},
				"content":    {Type: genai.TypeString, Description: "The main content of the page"},
				"reason":     {Type: genai.TypeString, Description: "The reason for the paywall or content extraction"},
			},
			Required: []string{"hasPaywall", "content", "reason"},
		},
	}

	g.log.Info().Msg("Sending request to Gemini")
	resp, err := g.client.Models.GenerateContent(ctx, g.model, content, config)
	if err != nil {
		g.log.Error().Err(err).Msg("Failed to get response from Gemini")
		return "", fmt.Errorf("failed to generate content: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		g.log.Error().Msg("No content in Gemini response")
		return "", extractor.ErrNoContent
	}

	rawResponse := resp.Candidates[0].Content.Parts[0].Text
	g.log.Debug().Str("raw_response", rawResponse).Msg("Got response from Gemini")

	// Parse the JSON response
	var result AnalysisResult
	if err := json.Unmarshal([]byte(rawResponse), &result); err != nil {
		g.log.Error().Err(err).Str("raw_response", rawResponse).Msg("Failed to parse Gemini response")
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	g.log.Info().
		Bool("has_paywall", result.HasPaywall).
		Str("reason", result.Reason).
		Int("content_length", len(result.Content)).
		Msg("Successfully analyzed content")

	if result.HasPaywall {
		g.log.Info().Str("reason", result.Reason).Msg("Detected paywall")
		return "", extractor.ErrPaywall
	}

	return result.Content, nil
}
