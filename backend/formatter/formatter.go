package formatter

import (
	"fmt"
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

// FormatToPDF converts content to PDF format using Pandoc
func (f *Formatter) FormatToPDF(content []byte) ([]byte, error) {
	// Convert content to PDF
	pdf, err := f.pandoc.ConvertHTMLToPDF(content)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to PDF: %w", err)
	}

	// Add metadata
	metadata := map[string]string{
		"Creator": "Attic Kindle Converter",
		"Title":   "Web Article",
	}

	pdf, err = f.poppler.AddMetadata(pdf, metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to add metadata: %w", err)
	}

	return pdf, nil
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
