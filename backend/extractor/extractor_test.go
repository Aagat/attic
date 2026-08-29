package extractor

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/aagat/attic/backend/integrations"
	"github.com/aagat/attic/backend/logger"
)

// Mock implementations for testing
type mockPuppeteer struct {
	captureScreenshotFunc      func(ctx context.Context, url string) ([]byte, error)
	extractWithReadabilityFunc func(ctx context.Context, url string) (*integrations.ReadabilityContent, error)
}

func (m *mockPuppeteer) CaptureScreenshot(ctx context.Context, url string) ([]byte, error) {
	return m.captureScreenshotFunc(ctx, url)
}

func (m *mockPuppeteer) ExtractWithReadability(ctx context.Context, url string) (*integrations.ReadabilityContent, error) {
	return m.extractWithReadabilityFunc(ctx, url)
}

type mockLLM struct {
	detectPaywallFunc  func(ctx context.Context, screenshot []byte) (bool, error)
	extractContentFunc func(ctx context.Context, screenshot []byte) (string, error)
}

func (m *mockLLM) DetectPaywall(ctx context.Context, screenshot []byte) (bool, error) {
	return m.detectPaywallFunc(ctx, screenshot)
}

func (m *mockLLM) ExtractContent(ctx context.Context, screenshot []byte) (string, error) {
	return m.extractContentFunc(ctx, screenshot)
}

type mockArchiveExtractor struct {
	getArchiveURLFunc func(url string) (string, error)
}

var _ ArchiveExtractor = (*mockArchiveExtractor)(nil) // Ensure mock implements interface

func (m *mockArchiveExtractor) getArchiveURL(url string) (string, error) {
	return m.getArchiveURLFunc(url)
}

func TestExtractFromURL(t *testing.T) {
	tests := []struct {
		name                  string
		url                   string
		readabilityErr        error
		readabilityRes        *integrations.ReadabilityContent
		screenshotErr         error
		screenshotRes         []byte
		paywallErr            error
		paywallRes            bool
		aiErr                 error
		aiRes                 string
		archiveURLErr         error
		archiveURL            string
		archiveReadabilityErr error
		archiveReadabilityRes *integrations.ReadabilityContent
		archiveAIErr          error
		archiveAIRes          string
		wantErr               error
		wantContent           []byte
		wantMetadata          map[string]string
	}{
		{
			name:           "readability success",
			url:            "https://example.com",
			readabilityErr: nil,
			readabilityRes: &integrations.ReadabilityContent{
				Content:     "<article>test content</article>",
				TextContent: "test content",
				Title:       "Test Title",
				Byline:      "Test Author",
				Excerpt:     "Test Excerpt",
				Length:      100,
				IsReadable:  true,
				SiteName:    "Example Site",
			},
			wantErr:     nil,
			wantContent: []byte("<article>test content</article>"),
			wantMetadata: map[string]string{
				"Title":   "Test Title",
				"Author":  "Test Author",
				"Excerpt": "Test Excerpt",
				"Source":  "https://example.com",
			},
		},
		{
			name:           "readability fails, ai success",
			url:            "https://example.com",
			readabilityErr: errors.New("readability failed"),
			screenshotRes:  []byte("screenshot"),
			aiRes:          "ai content",
			wantErr:        nil,
			wantContent:    []byte("ai content"),
			wantMetadata: map[string]string{
				"Source": "https://example.com",
			},
		},
		{
			name:           "paywall detected, archive success",
			url:            "https://example.com",
			readabilityErr: errors.New("readability failed"),
			screenshotRes:  []byte("screenshot"),
			aiErr:          &ExtractError{Type: ErrPaywall, Reason: "paywall detected"},
			archiveURL:     "https://web.archive.org/example.com",
			archiveReadabilityRes: &integrations.ReadabilityContent{
				Content:     "<article>archived content</article>",
				TextContent: "archived content",
				Title:       "Archived Title",
				Length:      100,
				IsReadable:  true,
			},
			wantErr:     nil,
			wantContent: []byte("<article>archived content</article>"),
			wantMetadata: map[string]string{
				"Title":  "Archived Title",
				"Source": "https://web.archive.org/example.com",
			},
		},
		{
			name:           "paywall detected, archive not available",
			url:            "https://example.com",
			readabilityErr: errors.New("readability failed"),
			screenshotRes:  []byte("screenshot"),
			aiErr:          &ExtractError{Type: ErrPaywall, Reason: "paywall detected"},
			archiveURLErr:  errors.New("no archived version available"),
			wantErr:        ErrPaywall,
		},
		{
			name:                  "paywall detected, archive extraction fails",
			url:                   "https://example.com",
			readabilityErr:        errors.New("readability failed"),
			screenshotRes:         []byte("screenshot"),
			aiErr:                 &ExtractError{Type: ErrPaywall, Reason: "paywall detected"},
			archiveURL:            "https://web.archive.org/example.com",
			archiveReadabilityErr: errors.New("archive extraction failed"),
			archiveAIErr:          errors.New("archive ai failed"),
			wantErr:               ErrPaywall,
		},
		{
			name:           "non-article content",
			url:            "https://example.com",
			readabilityErr: errors.New("readability failed"),
			screenshotRes:  []byte("screenshot"),
			aiErr:          &ExtractError{Type: ErrNonArticle, Reason: "this is a landing page"},
			wantErr:        ErrNonArticle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mocks
			puppeteer := &mockPuppeteer{
				captureScreenshotFunc: func(ctx context.Context, url string) ([]byte, error) {
					if url == tt.archiveURL {
						return tt.screenshotRes, tt.screenshotErr
					}
					return tt.screenshotRes, tt.screenshotErr
				},
				extractWithReadabilityFunc: func(ctx context.Context, url string) (*integrations.ReadabilityContent, error) {
					if url == tt.archiveURL {
						return tt.archiveReadabilityRes, tt.archiveReadabilityErr
					}
					return tt.readabilityRes, tt.readabilityErr
				},
			}

			llm := &mockLLM{
				extractContentFunc: func(ctx context.Context, screenshot []byte) (string, error) {
					if tt.archiveAIRes != "" {
						return tt.archiveAIRes, tt.archiveAIErr
					}
					return tt.aiRes, tt.aiErr
				},
			}

			archiveCalls := 0
			archive := &mockArchiveExtractor{
				getArchiveURLFunc: func(url string) (string, error) {
					if archiveCalls > 0 {
						return "", errors.New("no more archive sources available")
					}
					archiveCalls++
					return tt.archiveURL, tt.archiveURLErr
				},
			}

			extractor := &Extractor{
				puppeteer:   puppeteer,
				llm:         llm,
				ai:          newAIExtractor(llm, puppeteer),
				archive:     archive,
				log:         logger.WithComponent("extractor_test"),
				storagePath: t.TempDir(),
			}

			// Run test
			got, err := extractor.ExtractFromURL(tt.url)

			// Check error
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ExtractFromURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Check content and metadata if no error expected
			if tt.wantErr == nil {
				if !bytes.Equal(got.Content, tt.wantContent) {
					t.Errorf("ExtractFromURL() content = %v, want %v", string(got.Content), string(tt.wantContent))
				}
				for k, v := range tt.wantMetadata {
					if got.Metadata[k] != v {
						t.Errorf("ExtractFromURL() metadata[%s] = %v, want %v", k, got.Metadata[k], v)
					}
				}
			}
		})
	}
}

func TestExtractFromFile(t *testing.T) {
	tests := []struct {
		name         string
		content      []byte
		filename     string
		aiErr        error
		aiRes        string
		wantErr      error
		wantContent  []byte
		wantMetadata map[string]string
	}{
		{
			name:        "pdf file",
			content:     []byte("pdf content"),
			filename:    "test.pdf",
			wantErr:     nil,
			wantContent: []byte("pdf content"),
			wantMetadata: map[string]string{
				"Title":  "test.pdf",
				"Source": "File Upload",
			},
		},
		{
			name:        "text file",
			content:     []byte("text content"),
			filename:    "test.txt",
			wantErr:     nil,
			wantContent: []byte("text content"),
			wantMetadata: map[string]string{
				"Title":  "test.txt",
				"Source": "File Upload",
			},
		},
		{
			name:        "image file",
			content:     []byte("image data"),
			filename:    "test.png",
			aiRes:       "extracted text from image",
			wantErr:     nil,
			wantContent: []byte("extracted text from image"),
			wantMetadata: map[string]string{
				"Title":  "test.png",
				"Source": "File Upload",
			},
		},
		{
			name:     "image extraction fails",
			content:  []byte("image data"),
			filename: "test.jpg",
			aiErr:    errors.New("some error"),
			wantErr:  ErrImageExtraction,
		},
		{
			name:     "unsupported file type",
			content:  []byte("binary data"),
			filename: "test.exe",
			wantErr:  ErrUnsupportedFileType,
		},
		{
			name:        "empty file",
			content:     []byte{},
			filename:    "test.txt",
			wantErr:     nil,
			wantContent: []byte{},
			wantMetadata: map[string]string{
				"Title":  "test.txt",
				"Source": "File Upload",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mocks
			llm := &mockLLM{
				extractContentFunc: func(ctx context.Context, screenshot []byte) (string, error) {
					return tt.aiRes, tt.aiErr
				},
			}

			extractor := &Extractor{
				llm: llm,
				log: logger.WithComponent("extractor_test"),
			}

			// Run test
			got, err := extractor.ExtractFromFile(bytes.NewReader(tt.content), tt.filename)

			// Check error
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("ExtractFromFile() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			// Check content and metadata if no error expected
			if err != nil {
				t.Errorf("ExtractFromFile() unexpected error: %v", err)
				return
			}
			if !bytes.Equal(got.Content, tt.wantContent) {
				t.Errorf("ExtractFromFile() content = %v, want %v", string(got.Content), string(tt.wantContent))
			}
			for k, v := range tt.wantMetadata {
				if got.Metadata[k] != v {
					t.Errorf("ExtractFromFile() metadata[%s] = %v, want %v", k, got.Metadata[k], v)
				}
			}
		})
	}
}
