package extractor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aagat/attic/backend/integrations"
	"github.com/aagat/attic/backend/integrations/llm"
)

// MockPuppeteer implements integrations.Puppeteer interface for testing
type MockPuppeteer struct {
	readabilityContent string
	readabilityErr     error
	screenshotData     []byte
	screenshotErr      error
}

var _ integrations.Puppeteer = (*MockPuppeteer)(nil) // Verify interface implementation

func (m *MockPuppeteer) ExtractWithReadability(ctx context.Context, url string) (string, error) {
	return m.readabilityContent, m.readabilityErr
}

func (m *MockPuppeteer) CaptureScreenshot(ctx context.Context, url string) ([]byte, error) {
	return m.screenshotData, m.screenshotErr
}

// MockLLM implements llm.LLM interface for testing
type MockLLM struct {
	extractContent string
	extractErr     error
	hasPaywall     bool
	paywallErr     error
}

var _ llm.LLM = (*MockLLM)(nil) // Verify interface implementation

func (m *MockLLM) ExtractContent(ctx context.Context, screenshot []byte) (string, error) {
	return m.extractContent, m.extractErr
}

func (m *MockLLM) DetectPaywall(ctx context.Context, screenshot []byte) (bool, error) {
	return m.hasPaywall, m.paywallErr
}

func TestExtractFromURL(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		puppeteer      *MockPuppeteer
		llm            *MockLLM
		expectedOutput []byte
		expectError    bool
	}{
		{
			name: "successful readability extraction",
			url:  "https://example.com",
			puppeteer: &MockPuppeteer{
				readabilityContent: "test content",
				readabilityErr:     nil,
			},
			llm:            &MockLLM{},
			expectedOutput: []byte("test content"),
			expectError:    false,
		},
		{
			name: "readability fails, AI extraction succeeds",
			url:  "https://example.com",
			puppeteer: &MockPuppeteer{
				readabilityContent: "",
				readabilityErr:     errors.New("readability failed"),
				screenshotData:     []byte("screenshot"),
				screenshotErr:      nil,
			},
			llm: &MockLLM{
				extractContent: "AI extracted content",
				extractErr:     nil,
				hasPaywall:     false,
				paywallErr:     nil,
			},
			expectedOutput: []byte("AI extracted content"),
			expectError:    false,
		},
		{
			name: "paywall detected",
			url:  "https://example.com",
			puppeteer: &MockPuppeteer{
				readabilityContent: "",
				readabilityErr:     errors.New("readability failed"),
				screenshotData:     []byte("screenshot"),
				screenshotErr:      nil,
			},
			llm: &MockLLM{
				hasPaywall: true,
				paywallErr: nil,
			},
			expectedOutput: nil,
			expectError:    true,
		},
		{
			name: "all extraction methods fail",
			url:  "https://example.com",
			puppeteer: &MockPuppeteer{
				readabilityContent: "",
				readabilityErr:     errors.New("readability failed"),
				screenshotData:     []byte("screenshot"),
				screenshotErr:      nil,
			},
			llm: &MockLLM{
				extractContent: "",
				extractErr:     errors.New("AI extraction failed"),
				hasPaywall:     false,
				paywallErr:     nil,
			},
			expectedOutput: nil,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extractor := NewExtractor(tt.puppeteer, tt.llm)
			output, err := extractor.ExtractFromURL(tt.url)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if string(output) != string(tt.expectedOutput) {
					t.Errorf("expected output %q, got %q", tt.expectedOutput, output)
				}
			}
		})
	}
}

func TestExtractFromFile(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
	}{
		{
			name:        "simple text file",
			input:       "test content",
			expectError: false,
		},
		// Add more test cases for different file types when implemented
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			puppeteer := &MockPuppeteer{}
			llm := &MockLLM{}
			extractor := NewExtractor(puppeteer, llm)
			reader := strings.NewReader(tt.input)
			output, err := extractor.ExtractFromFile(reader)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if string(output) != tt.input {
					t.Errorf("expected output %q, got %q", tt.input, string(output))
				}
			}
		})
	}
}
