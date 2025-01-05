package integrations

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aagat/attic/backend/logger"
	"github.com/rs/zerolog"
)

// Poppler provides PDF manipulation functionality using poppler-utils
type Poppler struct {
	log zerolog.Logger
}

// NewPoppler creates a new Poppler instance
func NewPoppler() *Poppler {
	return &Poppler{
		log: logger.WithComponent("poppler"),
	}
}

// AddMetadata adds metadata to a PDF file
func (p *Poppler) AddMetadata(pdfData []byte, metadata map[string]string) ([]byte, error) {
	// Create temporary directory
	tempDir, err := ioutil.TempDir("", "poppler-*")
	if err != nil {
		p.log.Error().Err(err).Msg("Failed to create temp directory")
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Write input PDF to temp file
	inputPath := filepath.Join(tempDir, "input.pdf")
	if err := ioutil.WriteFile(inputPath, pdfData, 0600); err != nil {
		p.log.Error().Err(err).Str("path", inputPath).Msg("Failed to write temp file")
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}

	// Create metadata file
	var metadataInput strings.Builder
	for key, value := range metadata {
		metadataInput.WriteString(fmt.Sprintf("InfoKey: %s\nInfoValue: %s\n", key, value))
	}

	// Run pdftk to add metadata
	outputPath := filepath.Join(tempDir, "output.pdf")
	cmd := exec.Command("pdftk",
		inputPath,
		"update_info_utf8",
		"-",
		"output",
		outputPath,
	)
	cmd.Stdin = strings.NewReader(metadataInput.String())

	if err := cmd.Run(); err != nil {
		p.log.Error().Err(err).Msg("Failed to add metadata with pdftk")
		return nil, fmt.Errorf("pdftk failed: %w", err)
	}

	// Read output file
	output, err := ioutil.ReadFile(outputPath)
	if err != nil {
		p.log.Error().Err(err).Str("path", outputPath).Msg("Failed to read output file")
		return nil, fmt.Errorf("failed to read output file: %w", err)
	}

	p.log.Info().Int("size", len(output)).Msg("Successfully added metadata to PDF")
	return output, nil
}

// ExtractText extracts text content from a PDF file
func (p *Poppler) ExtractText(pdfData []byte) (string, error) {
	// Create temporary file for input
	tempFile, err := ioutil.TempFile("", "poppler-*.pdf")
	if err != nil {
		p.log.Error().Err(err).Msg("Failed to create temp file")
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())

	// Write PDF data to temp file
	if err := ioutil.WriteFile(tempFile.Name(), pdfData, 0600); err != nil {
		p.log.Error().Err(err).Str("path", tempFile.Name()).Msg("Failed to write temp file")
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	// Run pdftotext
	output, err := exec.Command("pdftotext", tempFile.Name(), "-").Output()
	if err != nil {
		p.log.Error().Err(err).Msg("Failed to extract text with pdftotext")
		return "", fmt.Errorf("pdftotext failed: %w", err)
	}

	text := string(output)
	p.log.Info().Int("length", len(text)).Msg("Successfully extracted text from PDF")
	return text, nil
}
