package integrations

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
)

// Poppler provides PDF manipulation capabilities
type Poppler struct{}

// NewPoppler creates a new Poppler instance
func NewPoppler() *Poppler {
	return &Poppler{}
}

// AddMetadata adds metadata to a PDF file
func (p *Poppler) AddMetadata(pdf []byte, metadata map[string]string) ([]byte, error) {
	// Create temporary files
	tmpDir, err := ioutil.TempDir("", "poppler")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	inPath := filepath.Join(tmpDir, "input.pdf")
	outPath := filepath.Join(tmpDir, "output.pdf")

	if err := ioutil.WriteFile(inPath, pdf, 0644); err != nil {
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}

	// Build pdftk command
	args := []string{
		inPath,
		"update_info",
		"-",
		"output",
		outPath,
	}

	cmd := exec.Command("pdftk", args...)

	// Prepare metadata input
	var metadataInput bytes.Buffer
	for key, value := range metadata {
		fmt.Fprintf(&metadataInput, "InfoKey: %s\nInfoValue: %s\n", key, value)
	}

	cmd.Stdin = &metadataInput

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pdftk failed: %w", err)
	}

	// Read the output file
	output, err := ioutil.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read output file: %w", err)
	}

	return output, nil
}

// ExtractText extracts text content from a PDF
func (p *Poppler) ExtractText(pdf []byte) (string, error) {
	// Create temporary file
	tmpFile, err := ioutil.TempFile("", "pdf")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.Write(pdf); err != nil {
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	cmd := exec.Command("pdftotext", tmpFile.Name(), "-")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pdftotext failed: %w", err)
	}

	return string(output), nil
}
