package formatter

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aagat/attic/backend/config"
)

// Formatter handles content formatting and PDF generation
type Formatter struct {
	pandoc      *Pandoc
	poppler     *Poppler
	storagePath string
}

// NewFormatter creates a new Formatter instance
func NewFormatter(cfg *config.Config) (*Formatter, error) {
	if err := checkDependencies(); err != nil {
		return nil, err
	}

	return &Formatter{
		pandoc:      newPandoc(cfg),
		poppler:     newPoppler(),
		storagePath: cfg.StoragePath,
	}, nil
}

// FormatToPDF converts content to PDF format using Pandoc and saves it to a file
func (f *Formatter) FormatToPDF(content []byte, metadata map[string]string, profile string) (string, error) {
	// Ensure metadata exists
	if metadata == nil {
		metadata = map[string]string{
			"Creator": "Attic Kindle Converter",
		}
	} else {
		// Ensure Creator is set
		if _, ok := metadata["Creator"]; !ok {
			metadata["Creator"] = "Attic Kindle Converter"
		}
		// Set profile if provided
		if profile != "" {
			metadata["Profile"] = profile
		}
	}

	// Convert content to PDF with metadata
	pdf, err := f.pandoc.ConvertHTMLToPDF(content, metadata)
	if err != nil {
		return "", fmt.Errorf("failed to convert to PDF: %w", err)
	}

	// Add metadata using Poppler as well for better compatibility
	pdf, err = f.poppler.AddMetadata(pdf, metadata)
	if err != nil {
		return "", fmt.Errorf("failed to add metadata: %w", err)
	}

	// Generate a unique filename using timestamp and metadata
	timestamp := time.Now().Format("20060102-150405")
	title := sanitizeFilename(metadata["Title"])
	if title == "" {
		title = "untitled"
	}
	filename := fmt.Sprintf("%s-%s.pdf", timestamp, title)
	pdfPath := filepath.Join(f.storagePath, filename)

	// Save PDF to file
	if err := ioutil.WriteFile(pdfPath, pdf, 0644); err != nil {
		return "", fmt.Errorf("failed to write PDF file: %w", err)
	}

	return pdfPath, nil
}

// sanitizeFilename removes or replaces characters that are not safe for filenames
func sanitizeFilename(name string) string {
	// Replace unsafe characters with underscores
	unsafe := []string{"/", "\\", "?", "%", "*", ":", "|", "\"", "<", ">", ".", " "}
	safe := name
	for _, char := range unsafe {
		safe = filepath.Clean(strings.ReplaceAll(safe, char, "_"))
	}
	// Limit the length
	if len(safe) > 50 {
		safe = safe[:50]
	}
	return safe
}

// checkDependencies verifies that required external tools are available
func checkDependencies() error {
	// Check for pandoc
	_, err := exec.LookPath("pandoc")
	if err != nil {
		return fmt.Errorf("pandoc is not installed")
	}

	// Check for xelatex
	_, err = exec.LookPath("xelatex")
	if err != nil {
		return fmt.Errorf("xelatex is not installed")
	}

	// Check for poppler-utils
	_, err = exec.LookPath("pdfinfo")
	if err != nil {
		return fmt.Errorf("poppler-utils is not installed")
	}

	return nil
}
