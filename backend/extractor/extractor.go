package extractor

import (
	"context"
	"errors"
	"io"
	"io/ioutil"

	"github.com/aagat/attic/backend/integrations"
	"github.com/aagat/attic/backend/integrations/llm"
)

// Extractor handles content extraction from various sources
type Extractor struct {
	puppeteer *integrations.Puppeteer
	llm       llm.LLM
	ai        *aiExtractor
	archive   *archiveExtractor
}

// NewExtractor creates a new Extractor instance
func NewExtractor(puppeteer *integrations.Puppeteer, llm llm.LLM) *Extractor {
	return &Extractor{
		puppeteer: puppeteer,
		llm:       llm,
		ai:        newAIExtractor(llm, puppeteer),
		archive:   newArchiveExtractor(),
	}
}

// ExtractFromURL extracts content from a given URL
func (e *Extractor) ExtractFromURL(url string) ([]byte, error) {
	ctx := context.Background()

	// Try Readability first
	content, err := e.puppeteer.ExtractWithReadability(ctx, url)
	if err == nil {
		return []byte(content), nil
	}

	// If Readability fails, try AI extraction
	aiContent, err := e.ai.extract(ctx, url)
	if err == nil {
		return aiContent, nil
	}

	// If AI fails, try archive
	archiveContent, err := e.archive.extract(ctx, url)
	if err == nil {
		return archiveContent, nil
	}

	return nil, errors.New("failed to extract content from URL")
}

// ExtractFromFile extracts content from an uploaded file
func (e *Extractor) ExtractFromFile(file io.Reader) ([]byte, error) {
	content, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	}

	// TODO: Implement file type detection and appropriate extraction
	return content, nil
}

// Private helper functions to be implemented in their respective files
func extractWithReadability(url string) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func extractWithAI(url string) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func extractFromArchive(url string) ([]byte, error) {
	return nil, errors.New("not implemented")
}
