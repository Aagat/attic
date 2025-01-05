package extractor

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aagat/attic/backend/integrations"
	"github.com/aagat/attic/backend/logger"
	"github.com/rs/zerolog"
)

var (
	ErrPaywall             = errors.New("content is behind a paywall")
	ErrNoContent           = errors.New("no content could be extracted")
	ErrUnsupportedFileType = errors.New("unsupported file type")
	ErrImageExtraction     = errors.New("failed to extract text from image")
)

// LLMClient defines the interface for LLM-based content extraction
type LLMClient interface {
	DetectPaywall(ctx context.Context, screenshot []byte) (bool, error)
	ExtractContent(ctx context.Context, screenshot []byte) (string, error)
}

// ExtractedContent represents the extracted content and its metadata
type ExtractedContent struct {
	Content    []byte            // The extracted content
	Metadata   map[string]string // Metadata about the content
	Screenshot []byte            // Screenshot of the content source
}

// Extractor handles content extraction from various sources
type Extractor struct {
	puppeteer   integrations.Puppeteer
	llm         LLMClient
	ai          *aiExtractor
	archive     ArchiveExtractor
	log         zerolog.Logger
	storagePath string
}

// NewExtractor creates a new Extractor instance
func NewExtractor(puppeteer integrations.Puppeteer, llm LLMClient, storagePath string) *Extractor {
	return &Extractor{
		puppeteer:   puppeteer,
		llm:         llm,
		ai:          newAIExtractor(llm, puppeteer),
		archive:     newArchiveExtractor(),
		log:         logger.WithComponent("extractor"),
		storagePath: storagePath,
	}
}

// saveScreenshot saves the screenshot to disk and returns its path
func (e *Extractor) saveScreenshot(screenshot []byte, url string) (string, error) {
	// Create screenshots directory if it doesn't exist
	screenshotsDir := filepath.Join(e.storagePath, "screenshots")
	if err := os.MkdirAll(screenshotsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create screenshots directory: %w", err)
	}

	// Generate a unique filename using timestamp and sanitized URL
	timestamp := time.Now().Format("20060102-150405")
	urlHash := fmt.Sprintf("%x", sha256.Sum256([]byte(url)))[:8]
	filename := fmt.Sprintf("%s-%s.jpg", timestamp, urlHash)
	screenshotPath := filepath.Join(screenshotsDir, filename)

	// Save screenshot
	if err := ioutil.WriteFile(screenshotPath, screenshot, 0644); err != nil {
		return "", fmt.Errorf("failed to save screenshot: %w", err)
	}

	return screenshotPath, nil
}

// ExtractFromURL extracts content from a URL
func (e *Extractor) ExtractFromURL(url string) (*ExtractedContent, error) {
	ctx := context.Background()
	var screenshot []byte

	// Try Readability first
	e.log.Info().Str("url", url).Msg("Attempting to extract content using Readability")
	content, err := e.puppeteer.ExtractWithReadability(ctx, url)
	if err == nil {
		// Decode and save screenshot
		var screenshotErr error
		screenshot, screenshotErr = base64.StdEncoding.DecodeString(content.Screenshot)
		if screenshotErr != nil {
			e.log.Warn().Err(screenshotErr).Str("url", url).Msg("Failed to decode screenshot")
		} else {
			screenshotPath, err := e.saveScreenshot(screenshot, url)
			if err != nil {
				e.log.Warn().Err(err).Str("url", url).Msg("Failed to save screenshot")
			} else {
				e.log.Info().Str("path", screenshotPath).Msg("Saved screenshot")
			}
		}

		e.log.Info().
			Str("url", url).
			Str("title", content.Title).
			Int("content_length", len(content.Content)).
			Msg("Successfully extracted content using Readability")
		return &ExtractedContent{
			Content: []byte(content.Content),
			Metadata: map[string]string{
				"Title":   content.Title,
				"Author":  content.Byline,
				"Excerpt": content.Excerpt,
				"Source":  url,
			},
			Screenshot: screenshot,
		}, nil
	}
	e.log.Warn().Err(err).Str("url", url).Msg("Readability extraction failed")

	// Try AI extraction
	e.log.Info().Str("url", url).Msg("Attempting to extract content using AI")
	aiContent, err := e.ai.extract(ctx, url)
	if err == nil {
		e.log.Info().
			Str("url", url).
			Int("content_length", len(aiContent)).
			Msg("Successfully extracted content using AI")
		return &ExtractedContent{
			Content: []byte(aiContent),
			Metadata: map[string]string{
				"Source": url,
			},
			Screenshot: screenshot,
		}, nil
	}
	if errors.Is(err, ErrPaywall) {
		e.log.Warn().Str("url", url).Msg("Content is behind a paywall")
		return nil, ErrPaywall
	}
	e.log.Warn().Err(err).Str("url", url).Msg("AI extraction failed")

	// Try archive as last resort
	e.log.Info().Str("url", url).Msg("Attempting to extract content from archive")
	archiveContent, err := e.archive.extract(ctx, url)
	if err == nil {
		e.log.Info().
			Str("url", url).
			Int("content_length", len(archiveContent)).
			Msg("Successfully extracted content from archive")
		return &ExtractedContent{
			Content: archiveContent,
			Metadata: map[string]string{
				"Source": url,
			},
			Screenshot: screenshot,
		}, nil
	}
	e.log.Warn().Err(err).Str("url", url).Msg("Archive extraction failed")

	return nil, ErrNoContent
}

// ExtractFromFile extracts content from a file
func (e *Extractor) ExtractFromFile(file io.Reader, filename string) (*ExtractedContent, error) {
	// Read the entire file
	content, err := io.ReadAll(file)
	if err != nil {
		e.log.Error().Err(err).Str("filename", filename).Msg("Failed to read file")
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Detect file type
	ext := filepath.Ext(filename)
	mimeType := mime.TypeByExtension(ext)

	// Process based on file type
	switch {
	case ext == ".pdf":
		e.log.Info().Str("filename", filename).Msg("Processing PDF file")
		return &ExtractedContent{
			Content: content,
			Metadata: map[string]string{
				"Title":  filename,
				"Source": "File Upload",
			},
		}, nil

	case ext == ".txt" || ext == ".md" || ext == ".html":
		e.log.Info().Str("filename", filename).Msg("Processing text file")
		return &ExtractedContent{
			Content: content,
			Metadata: map[string]string{
				"Title":  filename,
				"Source": "File Upload",
			},
		}, nil

	case ext == ".epub":
		e.log.Error().Str("filename", filename).Msg("EPUB format not supported yet")
		return nil, fmt.Errorf("EPUB conversion not implemented yet")

	case strings.HasPrefix(mimeType, "image/"):
		e.log.Info().Str("filename", filename).Msg("Processing image file")
		text, err := e.llm.ExtractContent(context.Background(), content)
		if err != nil {
			e.log.Error().Err(err).Str("filename", filename).Msg("Failed to extract text from image")
			return nil, fmt.Errorf("%w: %v", ErrImageExtraction, err)
		}
		return &ExtractedContent{
			Content: []byte(text),
			Metadata: map[string]string{
				"Title":  filename,
				"Source": "File Upload",
			},
		}, nil

	default:
		e.log.Error().
			Str("filename", filename).
			Str("mime_type", mimeType).
			Msg("Unsupported file type")
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFileType, mimeType)
	}
}
