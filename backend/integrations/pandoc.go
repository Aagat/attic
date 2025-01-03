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
func (p *Pandoc) ConvertToPDF(content []byte, from string) ([]byte, error) {
	cmd := exec.Command("pandoc",
		"--from", from,
		"--to", "pdf",
		"--pdf-engine", "wkhtmltopdf",
		"--standalone")

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
	return p.ConvertToPDF(epub, "epub")
}

// ConvertHTMLToPDF converts HTML content to PDF
func (p *Pandoc) ConvertHTMLToPDF(html []byte) ([]byte, error) {
	return p.ConvertToPDF(html, "html")
}
