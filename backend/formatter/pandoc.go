package formatter

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"

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
		"--from", from + "+raw_html",
		"--to", "pdf",
		"--pdf-engine", "xelatex",
		"--standalone",
		"--wrap=preserve",
		"--variable", "graphics=true",
		"--variable", fmt.Sprintf("geometry:margin=%s,includeheadfoot,heightrounded,width=6.5in,textwidth=6.5in", profile.MarginSize),
		"--variable", fmt.Sprintf("fontsize=%s", profile.FontSize),
		"--variable", fmt.Sprintf("papersize=%s", profile.PaperSize),
		"--variable", "documentclass=extarticle",
		"--variable", "block-headings",
		"--variable", "header-includes=\\usepackage[export]{adjustbox}\\usepackage{graphicx}\\makeatletter\\let\\oldincludegraphics\\includegraphics\\renewcommand{\\includegraphics}[2][]{\\oldincludegraphics[width=\\textwidth,keepaspectratio,center]{#2}}\\makeatother\\setlength{\\parindent}{0pt}\\usepackage{float}\\floatplacement{figure}{H}",
		"--variable", "linkcolor=blue",
		"--pdf-engine-opt=-shell-escape",
		"--pdf-engine-opt=-halt-on-error",
		"--pdf-engine-opt=-interaction=nonstopmode",
		"--pdf-engine-opt=-extra-mem-top=10000000",
		"--pdf-engine-opt=-extra-mem-bot=10000000",
		"--pdf-engine-opt=-pool-size=10000000",
		"--pdf-engine-opt=-main-memory=100000000",
		"--pdf-engine-opt=-save-size=100000",
	}

	// Handle page numbers based on profile
	if !profile.ShowNumbers {
		args = append(args,
			"--variable", "pagestyle=empty",
		)
	}

	// Add metadata arguments with proper escaping
	for key, value := range metadata {
		// Escape special LaTeX characters in metadata
		escaped := strings.NewReplacer(
			"\\", "\\textbackslash{}",
			"&", "\\&",
			"%", "\\%",
			"$", "\\$",
			"#", "\\#",
			"_", "\\_",
			"{", "\\{",
			"}", "\\}",
			"~", "\\textasciitilde{}",
			"^", "\\textasciicircum{}",
		).Replace(value)
		args = append(args, "--metadata", fmt.Sprintf("%s=%s", key, escaped))
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
