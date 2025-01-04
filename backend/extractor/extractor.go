package extractor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/aagat/attic/backend/integrations"
	"github.com/aagat/attic/backend/integrations/llm"
)

var (
	// ErrNoContent is returned when no content could be extracted
	ErrNoContent = errors.New("failed to extract content from URL")
	// ErrPaywall is returned when content is behind a paywall
	ErrPaywall = errors.New("content is behind a paywall")
	// ErrUnsupportedFileType is returned when the file type is not supported
	ErrUnsupportedFileType = errors.New("unsupported file type")
)

// archiveExtractorInterface defines the interface for archive content extraction
type archiveExtractorInterface interface {
	extract(ctx context.Context, url string) ([]byte, error)
}

// Extractor handles content extraction from various sources
type Extractor struct {
	puppeteer integrations.Puppeteer
	llm       llm.LLM
	ai        *aiExtractor
	archive   archiveExtractorInterface
}

// NewExtractor creates a new Extractor instance
func NewExtractor(puppeteer integrations.Puppeteer, llm llm.LLM) *Extractor {
	return &Extractor{
		puppeteer: puppeteer,
		llm:       llm,
		ai:        newAIExtractor(llm, puppeteer),
		archive:   newArchiveExtractor(),
	}
}

// ExtractedContent represents the content and metadata extracted from a source
type ExtractedContent struct {
	Content  []byte
	Metadata map[string]string
}

// ExtractFromURL extracts content from a given URL
func (e *Extractor) ExtractFromURL(url string) (*ExtractedContent, error) {
	ctx := context.Background()

	// Try Readability first
	log.Printf("Attempting to extract content using Readability from %s", url)
	result, err := e.puppeteer.ExtractWithReadability(ctx, url)
	if err == nil && result.Content != "" {
		log.Printf("Successfully extracted content using Readability from %s", url)
		metadata := map[string]string{
			"Title":   result.Title,
			"Author":  result.Byline,
			"Excerpt": result.Excerpt,
			"Source":  url,
		}
		return &ExtractedContent{
			Content:  []byte(result.Content),
			Metadata: metadata,
		}, nil
	}
	if err != nil {
		log.Printf("Readability extraction failed for %s: %v", url, err)
	}

	// If Readability fails, try AI extraction
	log.Printf("Attempting to extract content using AI from %s", url)
	aiContent, err := e.ai.extract(ctx, url)
	if err == nil {
		log.Printf("Successfully extracted content using AI from %s", url)
		metadata := map[string]string{
			"Source": url,
		}
		return &ExtractedContent{
			Content:  aiContent,
			Metadata: metadata,
		}, nil
	}
	if err != nil {
		if errors.Is(err, ErrPaywall) {
			log.Printf("Content from %s is behind a paywall", url)
			return nil, ErrPaywall
		}
		log.Printf("AI extraction failed for %s: %v", url, err)
	}

	// If AI fails for other reasons, try archive
	log.Printf("Attempting to extract content from archive for %s", url)
	archiveContent, err := e.archive.extract(ctx, url)
	if err == nil {
		log.Printf("Successfully extracted content from archive for %s", url)
		metadata := map[string]string{
			"Source": url,
		}
		return &ExtractedContent{
			Content:  archiveContent,
			Metadata: metadata,
		}, nil
	}
	if err != nil {
		log.Printf("Archive extraction failed for %s: %v", url, err)
	}

	// All methods failed
	log.Printf("All extraction methods failed for %s", url)
	return nil, ErrNoContent
}

// ExtractFromFile extracts content from an uploaded file
func (e *Extractor) ExtractFromFile(file io.Reader, filename string) (*ExtractedContent, error) {
	// Read the entire file
	content, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Detect file type
	ext := filepath.Ext(filename)
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		// Try to detect from content
		mimeType = http.DetectContentType(content)
	}

	log.Printf("Processing file %s with MIME type %s", filename, mimeType)

	metadata := map[string]string{
		"Title":  filename,
		"Source": "File Upload",
	}

	switch {
	case mimeType == "application/pdf":
		// PDF files can be passed through directly
		return &ExtractedContent{
			Content:  content,
			Metadata: metadata,
		}, nil
	case mimeType == "application/epub+zip":
		// TODO: Implement EPUB conversion
		return nil, fmt.Errorf("EPUB conversion not implemented yet")
	case strings.HasPrefix(mimeType, "text/"):
		// Text files can be passed through directly
		return &ExtractedContent{
			Content:  content,
			Metadata: metadata,
		}, nil
	case strings.HasPrefix(mimeType, "image/"):
		// For images, use AI to extract any text content
		log.Printf("Attempting to extract text from image %s", filename)
		aiContent, err := e.ai.extract(context.Background(), "", content)
		if err != nil {
			return nil, err
		}
		return &ExtractedContent{
			Content:  aiContent,
			Metadata: metadata,
		}, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFileType, mimeType)
	}
}
