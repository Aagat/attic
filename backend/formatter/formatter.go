package formatter

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
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
		metadata = make(map[string]string)
	}

	// Clean up and standardize metadata
	standardMetadata := make(map[string]string)

	// Copy over the metadata fields we want to preserve
	if title := metadata["Title"]; title != "" {
		// Use lowercase keys for consistency with pandoc
		standardMetadata["title"] = title // Used by pandoc for document title and PDF metadata
	}
	if author := metadata["Author"]; author != "" {
		standardMetadata["author"] = author
	}
	if excerpt := metadata["Excerpt"]; excerpt != "" {
		standardMetadata["subject"] = excerpt
	}
	if siteName := metadata["SiteName"]; siteName != "" {
		standardMetadata["publisher"] = siteName
	}
	if source := metadata["Source"]; source != "" && source != "File Upload" {
		standardMetadata["source"] = source
	}

	// Convert content to PDF with metadata
	pdf, err := f.pandoc.ConvertHTMLToPDF(content, standardMetadata)
	if err != nil {
		// Save the content for debugging
		debugDir := filepath.Join(f.storagePath, "debug")
		if err := os.MkdirAll(debugDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create debug directory: %w", err)
		}

		timestamp := time.Now().Format("20060102-150405")
		title := sanitizeFilename(standardMetadata["title"])
		if title == "" {
			title = "untitled"
		}

		// Save HTML content
		htmlPath := filepath.Join(debugDir, fmt.Sprintf("%s-%s.html", timestamp, title))
		if err := ioutil.WriteFile(htmlPath, content, 0644); err != nil {
			return "", fmt.Errorf("failed to save debug content: %w", err)
		}

		// Save metadata
		metadataPath := filepath.Join(debugDir, fmt.Sprintf("%s-%s.json", timestamp, title))
		metadataJSON, _ := json.MarshalIndent(standardMetadata, "", "  ")
		if err := ioutil.WriteFile(metadataPath, metadataJSON, 0644); err != nil {
			return "", fmt.Errorf("failed to save debug metadata: %w", err)
		}

		return "", fmt.Errorf("failed to convert to PDF (debug content saved to %s): %w", htmlPath, err)
	}

	// Add metadata using Poppler as well for better compatibility
	pdf, err = f.poppler.AddMetadata(pdf, standardMetadata)
	if err != nil {
		return "", fmt.Errorf("failed to add metadata: %w", err)
	}

	// Generate a unique filename using timestamp and metadata
	timestamp := time.Now().Format("20060102-150405")
	title := sanitizeFilename(standardMetadata["title"])
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

	// Check for rsvg-convert
	_, err = exec.LookPath("rsvg-convert")
	if err != nil {
		return fmt.Errorf("librsvg (rsvg-convert) is not installed")
	}

	// Check for svg.sty
	cmd := exec.Command("kpsewhich", "svg.sty")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("LaTeX svg package is not installed (texlive-svg)")
	}

	return nil
}
