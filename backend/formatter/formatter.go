package formatter

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"

	"github.com/aagat/attic/backend/integrations"
)

// Formatter handles content formatting and PDF generation
type Formatter struct {
	pandoc  *integrations.Pandoc
	poppler *integrations.Poppler
}

// NewFormatter creates a new Formatter instance
func NewFormatter() (*Formatter, error) {
	if err := checkDependencies(); err != nil {
		return nil, err
	}

	return &Formatter{
		pandoc:  integrations.NewPandoc(),
		poppler: integrations.NewPoppler(),
	}, nil
}

// FormatToPDF converts content to PDF format using Pandoc and saves it to a file
func (f *Formatter) FormatToPDF(content []byte, metadata map[string]string) (string, error) {
	// Convert content to PDF
	pdf, err := f.pandoc.ConvertHTMLToPDF(content)
	if err != nil {
		return "", fmt.Errorf("failed to convert to PDF: %w", err)
	}

	// Add metadata
	if metadata == nil {
		metadata = map[string]string{
			"Creator": "Attic Kindle Converter",
		}
	}

	pdf, err = f.poppler.AddMetadata(pdf, metadata)
	if err != nil {
		return "", fmt.Errorf("failed to add metadata: %w", err)
	}

	// Save PDF to a temporary file
	tmpFile, err := ioutil.TempFile("", "attic-*.pdf")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	if _, err := tmpFile.Write(pdf); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write PDF file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to close PDF file: %w", err)
	}

	return tmpFile.Name(), nil
}

// checkDependencies verifies that required external tools are available
func checkDependencies() error {
	// Check for pandoc
	_, err := exec.LookPath("pandoc")
	if err != nil {
		return fmt.Errorf("pandoc is not installed")
	}

	// Check for poppler-utils
	_, err = exec.LookPath("pdfinfo")
	if err != nil {
		return fmt.Errorf("poppler-utils is not installed")
	}

	return nil
}
