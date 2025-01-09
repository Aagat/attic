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
	HasPaywall           bool   `json:"hasPaywall"`
	ContentType          string `json:"contentType"`
	Article              string `json:"article"`
	CategorizationReason string `json:"categorizationReason"`
	Description          string `json:"description"`
	HasPartialContent    bool   `json:"hasPartialContent"`
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
	prompt := `You are an expert content extraction tool designed to analyze webpage screenshots and determine if their main content is suitable for conversion into a readable PDF for an e-reader like Kindle.
** Your primary goal is to extract the main textual content of the page ONLY if it is an article or blog post. Consider content like news articles, blog entries, or in-depth explanations as fitting this category.
** If the page's main content is primarily interactive (e.g., visualizations, videos, forms, maps, code playgrounds) or is dominated by user interface elements, DO NOT extract the content. Instead, categorize the page's content type and explain your reasoning for not extracting.

** If the page has a paywall restricting access to the main content, DO NOT attempt to extract any content. Provide a clear reason stating that a paywall was detected.
	`

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
				"hasPaywall":           {Type: genai.TypeBoolean, Description: "Indicates if the page has a paywall."},
				"contentType":          {Type: genai.TypeString, Description: "The type of content on the page.", Enum: []string{"article", "video", "tweet", "code", "visualization", "other"}},
				"article":              {Type: genai.TypeString, Description: "The extracted article content, present only if contentType is 'article'."},
				"categorizationReason": {Type: genai.TypeString, Description: "The reasoning behind the assigned contentType."},
				"description":          {Type: genai.TypeString, Description: "A concise summary of the page's content."},
				"hasPartialContent":    {Type: genai.TypeBoolean, Description: "Indicates if only a portion of the article content was extracted."},
			},
			Required: []string{"hasPaywall", "contentType", "categorizationReason", "description", "hasPartialContent"},
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
		Str("categorization_reason", result.CategorizationReason).
		Int("content_length", len(result.Article)).
		Bool("has_partial_content", result.HasPartialContent).
		Str("description", result.Description).
		Str("content_type", result.ContentType).
		Str("article", result.Article).
		Msg("Successfully analyzed content")

	if result.HasPaywall {
		g.log.Info().Str("categorization_reason", result.CategorizationReason).Msg("Detected paywall")
		return "", &extractor.ExtractError{
			Type:   extractor.ErrPaywall,
			Reason: result.CategorizationReason,
		}
	}

	if result.ContentType != "article" {
		g.log.Info().Str("categorization_reason", result.CategorizationReason).Msg("Detected non-article content")
		return "", &extractor.ExtractError{
			Type:   extractor.ErrNonArticle,
			Reason: result.CategorizationReason,
		}
	}

	if result.HasPartialContent {
		g.log.Info().Msg("Detected partial content")
		return "", &extractor.ExtractError{
			Type:   extractor.ErrPartialContent,
			Reason: result.Description,
		}
	}

	return result.Article, nil
}
