package formatter

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"

	"github.com/aagat/attic/backend/config"
)

// Pandoc provides document conversion capabilities
type Pandoc struct {
	profiles       map[string]config.PDFProfile
	defaultProfile string
}

// newPandoc creates a new Pandoc instance
func newPandoc(cfg *config.Config) *Pandoc {
	// Create profiles map for quick lookup
	profiles := make(map[string]config.PDFProfile)
	for _, profile := range cfg.PDFProfiles {
		profiles[profile.Name] = profile
	}

	return &Pandoc{
		profiles:       profiles,
		defaultProfile: cfg.DefaultProfile,
	}
}

// ConvertToPDF converts content to PDF format
func (p *Pandoc) ConvertToPDF(content []byte, from string, metadata map[string]string) ([]byte, error) {
	// Get profile name from metadata or use default
	profileName := p.defaultProfile
	if name, ok := metadata["Profile"]; ok && name != "" {
		profileName = name
	}

	// Get profile configuration
	profile, ok := p.profiles[profileName]
	if !ok {
		return nil, fmt.Errorf("PDF profile not found: %s", profileName)
	}

	args := []string{
		"--from", from,
		"--to", "pdf",
		"--pdf-engine", "xelatex",
		"--standalone",
		"--variable", fmt.Sprintf("geometry:margin=%s", profile.MarginSize),
		"--variable", fmt.Sprintf("fontsize=%s", profile.FontSize),
		"--variable", fmt.Sprintf("papersize=%s", profile.PaperSize),
		"--variable", "documentclass=article",
		"--variable", "block-headings",
	}

	// Handle page numbers based on profile
	if !profile.ShowNumbers {
		args = append(args,
			"--variable", "pagestyle=empty",
			"--variable", "header-includes=\\pagenumbering{gobble}",
		)
	}

	// Add metadata arguments
	if title, ok := metadata["Title"]; ok && title != "" {
		args = append(args, "--metadata", fmt.Sprintf("title=%s", title))
	}
	if author, ok := metadata["Author"]; ok && author != "" {
		args = append(args, "--metadata", fmt.Sprintf("author=%s", author))
	}

	cmd := exec.Command("pandoc", args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	var output bytes.Buffer
	cmd.Stdout = &output

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start pandoc: %w", err)
	}

	if _, err := io.Copy(stdin, bytes.NewReader(content)); err != nil {
		return nil, fmt.Errorf("failed to write to stdin: %w", err)
	}
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("pandoc failed: %w", err)
	}

	return output.Bytes(), nil
}

// ConvertEPUBToPDF converts an EPUB file to PDF
func (p *Pandoc) ConvertEPUBToPDF(epub []byte) ([]byte, error) {
	return p.ConvertToPDF(epub, "epub", nil)
}

// ConvertHTMLToPDF converts HTML content to PDF
func (p *Pandoc) ConvertHTMLToPDF(html []byte, metadata map[string]string) ([]byte, error) {
	return p.ConvertToPDF(html, "html", metadata)
}
