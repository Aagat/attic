package extractor

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// Mock implementations for testing
type mockPuppeteer struct {
	captureScreenshotFunc      func(ctx context.Context, url string) ([]byte, error)
	extractWithReadabilityFunc func(ctx context.Context, url string) (string, error)
}

func (m *mockPuppeteer) CaptureScreenshot(ctx context.Context, url string) ([]byte, error) {
	return m.captureScreenshotFunc(ctx, url)
}

func (m *mockPuppeteer) ExtractWithReadability(ctx context.Context, url string) (string, error) {
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
	extractFunc func(ctx context.Context, url string) ([]byte, error)
}

func (m *mockArchiveExtractor) extract(ctx context.Context, url string) ([]byte, error) {
	return m.extractFunc(ctx, url)
}

func TestExtractFromURL(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		readabilityErr error
		readabilityRes string
		screenshotErr  error
		screenshotRes  []byte
		paywallErr     error
		paywallRes     bool
		aiErr          error
		aiRes          string
		archiveErr     error
		archiveRes     []byte
		wantErr        error
		wantContent    []byte
	}{
		{
			name:           "readability success",
			url:            "https://example.com",
			readabilityErr: nil,
			readabilityRes: "test content",
			wantErr:        nil,
			wantContent:    []byte("test content"),
		},
		{
			name:           "readability fails, ai success",
			url:            "https://example.com",
			readabilityErr: errors.New("readability failed"),
			screenshotRes:  []byte("screenshot"),
			paywallRes:     false,
			aiRes:          "ai content",
			wantErr:        nil,
			wantContent:    []byte("ai content"),
		},
		{
			name:           "paywall detected",
			url:            "https://example.com",
			readabilityErr: errors.New("readability failed"),
			screenshotRes:  []byte("screenshot"),
			paywallRes:     true,
			wantErr:        ErrPaywall,
		},
		{
			name:           "all methods fail",
			url:            "https://example.com",
			readabilityErr: errors.New("readability failed"),
			screenshotRes:  []byte("screenshot"),
			paywallRes:     false,
			aiErr:          errors.New("ai failed"),
			archiveErr:     errors.New("archive failed"),
			wantErr:        ErrNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mocks
			puppeteer := &mockPuppeteer{
				captureScreenshotFunc: func(ctx context.Context, url string) ([]byte, error) {
					return tt.screenshotRes, tt.screenshotErr
				},
				extractWithReadabilityFunc: func(ctx context.Context, url string) (string, error) {
					return tt.readabilityRes, tt.readabilityErr
				},
			}

			llm := &mockLLM{
				detectPaywallFunc: func(ctx context.Context, screenshot []byte) (bool, error) {
					return tt.paywallRes, tt.paywallErr
				},
				extractContentFunc: func(ctx context.Context, screenshot []byte) (string, error) {
					return tt.aiRes, tt.aiErr
				},
			}

			archive := &mockArchiveExtractor{
				extractFunc: func(ctx context.Context, url string) ([]byte, error) {
					return tt.archiveRes, tt.archiveErr
				},
			}

			extractor := &Extractor{
				puppeteer: puppeteer,
				llm:       llm,
				ai:        newAIExtractor(llm, puppeteer),
				archive:   archive,
			}

			// Run test
			got, err := extractor.ExtractFromURL(tt.url)

			// Check error
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ExtractFromURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Check content if no error expected
			if tt.wantErr == nil {
				if string(got) != string(tt.wantContent) {
					t.Errorf("ExtractFromURL() = %v, want %v", string(got), string(tt.wantContent))
				}
			}
		})
	}
}

func TestExtractFromFile(t *testing.T) {
	tests := []struct {
		name        string
		content     []byte
		filename    string
		aiErr       error
		aiRes       string
		wantErr     error
		wantContent []byte
	}{
		{
			name:        "pdf file",
			content:     []byte("pdf content"),
			filename:    "test.pdf",
			wantErr:     nil,
			wantContent: []byte("pdf content"),
		},
		{
			name:        "text file",
			content:     []byte("text content"),
			filename:    "test.txt",
			wantErr:     nil,
			wantContent: []byte("text content"),
		},
		{
			name:        "image file",
			content:     []byte("image data"),
			filename:    "test.png",
			aiRes:       "extracted text from image",
			wantErr:     nil,
			wantContent: []byte("extracted text from image"),
		},
		{
			name:        "image extraction fails",
			content:     []byte("image data"),
			filename:    "test.jpg",
			aiErr:       errors.New("failed to extract text"),
			wantErr:     errors.New("failed to extract text"),
			wantContent: nil,
		},
		{
			name:        "unsupported file type",
			content:     []byte("binary data"),
			filename:    "test.exe",
			wantErr:     ErrUnsupportedFileType,
			wantContent: nil,
		},
		{
			name:        "empty file",
			content:     []byte{},
			filename:    "test.txt",
			wantErr:     nil,
			wantContent: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mocks
			puppeteer := &mockPuppeteer{
				captureScreenshotFunc: func(ctx context.Context, url string) ([]byte, error) {
					return nil, errors.New("not used")
				},
				extractWithReadabilityFunc: func(ctx context.Context, url string) (string, error) {
					return "", errors.New("not used")
				},
			}

			llm := &mockLLM{
				detectPaywallFunc: func(ctx context.Context, screenshot []byte) (bool, error) {
					return false, errors.New("not used")
				},
				extractContentFunc: func(ctx context.Context, screenshot []byte) (string, error) {
					return tt.aiRes, tt.aiErr
				},
			}

			extractor := &Extractor{
				puppeteer: puppeteer,
				llm:       llm,
				ai:        newAIExtractor(llm, puppeteer),
			}

			// Run test
			got, err := extractor.ExtractFromFile(bytes.NewReader(tt.content), tt.filename)

			// Check error
			if tt.wantErr != nil && err == nil {
				t.Errorf("ExtractFromFile() expected error %v, got nil", tt.wantErr)
				return
			}
			if tt.wantErr == nil && err != nil {
				t.Errorf("ExtractFromFile() unexpected error: %v", err)
				return
			}
			if tt.wantErr != nil && err != nil && tt.wantErr.Error() != err.Error() {
				t.Errorf("ExtractFromFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Check content if no error expected
			if tt.wantErr == nil {
				if !bytes.Equal(got, tt.wantContent) {
					t.Errorf("ExtractFromFile() = %v, want %v", got, tt.wantContent)
				}
			}
		})
	}
}
