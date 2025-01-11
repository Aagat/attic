package integrations

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"

	"github.com/aagat/attic/backend/config"
)

type Pandoc struct {
	profiles       map[string]config.PDFProfile
	defaultProfile string
}

// NewPandoc creates a new Pandoc instance
func NewPandoc(cfg *config.Config) *Pandoc {
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
		"--from", from + "+raw_html",
		"--to", "pdf",
		"--pdf-engine", "xelatex",
		"--standalone",
		"--variable", fmt.Sprintf("geometry:margin=%s", profile.MarginSize),
		"--variable", fmt.Sprintf("fontsize=%s", profile.FontSize),
		"--variable", fmt.Sprintf("papersize=%s", profile.PaperSize),
		"--variable", "documentclass=article",
		"--variable", "block-headings",
		"--variable", "title-meta=true",
		"--variable", "header-includes=\\usepackage{titling}\\pretitle{\\begin{center}\\huge\\bfseries}\\posttitle{\\par\\vspace{2.5em}\\end{center}}",
		"--pdf-engine-opt=-shell-escape",
	}

	// Handle page numbers based on profile
	if !profile.ShowNumbers {
		args = append(args,
			"--variable", "pagestyle=empty",
			"--variable", "header-includes=\\pagenumbering{gobble}",
		)
	}

	// Add metadata arguments
	for key, value := range metadata {
		args = append(args, "--metadata", fmt.Sprintf("%s=%s", key, value))
	}

	cmd := exec.Command("pandoc", args...)

	// Capture stderr for better error reporting
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

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
		// Include stderr in error message for better debugging
		return nil, fmt.Errorf("pandoc failed: %w\nDetails: %s", err, stderr.String())
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
