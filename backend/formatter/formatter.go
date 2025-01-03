package formatter

import (
	"errors"
	"os/exec"
)

// FormatToPDF converts content to PDF format using Pandoc
func FormatToPDF(content []byte) ([]byte, error) {
	// TODO: Implement PDF conversion using Pandoc
	// This is a placeholder implementation
	return nil, errors.New("PDF formatting not implemented")
}

// addMetadata adds metadata to the PDF using poppler-utils
func addMetadata(pdf []byte, metadata map[string]string) ([]byte, error) {
	// TODO: Implement metadata addition using poppler-utils
	return nil, errors.New("metadata addition not implemented")
}

// checkDependencies verifies that required external tools are available
func checkDependencies() error {
	// Check for pandoc
	if _, err := exec.LookPath("pandoc"); err != nil {
		return errors.New("pandoc is not installed")
	}

	// Check for poppler-utils
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		return errors.New("poppler-utils is not installed")
	}

	return nil
}
