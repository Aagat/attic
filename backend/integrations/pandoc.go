package integrations

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
)

// Pandoc provides document conversion capabilities
type Pandoc struct{}

// NewPandoc creates a new Pandoc instance
func NewPandoc() *Pandoc {
	return &Pandoc{}
}

// ConvertToPDF converts content to PDF format
func (p *Pandoc) ConvertToPDF(content []byte, from string, metadata map[string]string) ([]byte, error) {
	args := []string{
		"--from", from,
		"--to", "pdf",
		"--pdf-engine", "xelatex",
		"--standalone",
		"--variable", "geometry:margin=0.5in",
		"--variable", "fontsize=12pt",
		"--variable", "papersize=letter",
		"--variable", "classoption=article",
		"--variable", "block-headings",
		"--variable", "papersize=letter",
		"--variable", "documentclass=article",
		"--variable", "pagestyle=empty",
		"--variable", "header-includes=\\pagenumbering{gobble}",
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
