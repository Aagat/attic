// Package formatter renders sanitized articles as PDFs through headless Chromium.
package formatter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"attic/internal/sanitize"
)

const (
	MaxOutputBytes  = 16 << 20
	defaultTimeout  = 45 * time.Second
	defaultChromium = "/usr/bin/chromium"
)

var (
	ErrUnsupportedProfile = errors.New("unsupported PDF profile")
	ErrInvalidArticle     = errors.New("invalid semantic article")
	ErrOutputTooLarge     = errors.New("PDF output exceeds configured limit")
	ErrInvalidPDF         = errors.New("Chromium produced an invalid PDF")
)

type Article struct {
	Profile         string
	Title           string
	Author          string
	SiteName        string
	PublicationDate string
	SourceURL       string
	SemanticHTML    string
	GeneratedAt     time.Time
}

type Result struct {
	PDF     []byte
	Profile string
}

type Formatter interface {
	Format(context.Context, Article) (Result, error)
}

// Invocation is deliberately small so tests can provide an executor without
// starting a browser. Executors must return after ctx is cancelled.
type Invocation struct {
	Executable string
	Args       []string
	HTMLPath   string
	OutputPath string
}

type Executor interface {
	Run(context.Context, Invocation) error
}

// CommandExecutor starts Chromium in its own process group. Cancelling the
// context kills the entire group and still waits for the process to be reaped.
type CommandExecutor struct{}

func (CommandExecutor) Run(ctx context.Context, invocation Invocation) error {
	cmd := exec.Command(invocation.Executable, invocation.Args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Chromium: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("Chromium render: %w", err)
		}
		return nil
	case <-ctx.Done():
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			<-done
			return fmt.Errorf("terminate Chromium: %w", err)
		}
		<-done
		return ctx.Err()
	}
}

type PDF struct {
	MaxBytes     int
	Timeout      time.Duration
	ChromiumPath string
	TempDir      string
	Executor     Executor
	MarginMM     float64
	BodyFontPT   float64
	LineHeight   float64
}

func (p PDF) Format(ctx context.Context, article Article) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if article.Profile == "" {
		article.Profile = "a5"
	}
	if article.Profile != "a5" {
		return Result{}, ErrUnsupportedProfile
	}

	clean, err := sanitize.SanitizeHTML(article.SemanticHTML)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidArticle, err)
	}
	margin, fontSize, lineHeight := p.MarginMM, p.BodyFontPT, p.LineHeight
	if margin <= 0 {
		margin = 10
	}
	if fontSize <= 0 {
		fontSize = 11
	}
	if lineHeight <= 0 {
		lineHeight = 1.4
	}
	document := renderHTML(article, clean, margin, fontSize, lineHeight)

	tempDir, err := os.MkdirTemp(p.TempDir, "attic-pdf-*")
	if err != nil {
		return Result{}, fmt.Errorf("create private PDF workspace: %w", err)
	}
	defer os.RemoveAll(tempDir)
	if err := os.Chmod(tempDir, 0o700); err != nil {
		return Result{}, fmt.Errorf("secure PDF workspace: %w", err)
	}

	htmlPath := filepath.Join(tempDir, "article.html")
	outputPath := filepath.Join(tempDir, "article.pdf")
	if err := os.WriteFile(htmlPath, []byte(document), 0o600); err != nil {
		return Result{}, fmt.Errorf("write private article HTML: %w", err)
	}

	timeout := p.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	renderCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	executable := p.ChromiumPath
	if executable == "" {
		executable = defaultChromium
	}
	invocation := Invocation{
		Executable: executable,
		HTMLPath:   htmlPath,
		OutputPath: outputPath,
		Args:       chromiumArgs(tempDir, htmlPath, outputPath),
	}
	executor := p.Executor
	if executor == nil {
		executor = CommandExecutor{}
	}
	maxBytes := p.MaxBytes
	if maxBytes <= 0 {
		maxBytes = MaxOutputBytes
	}
	if err := runBounded(renderCtx, cancel, executor, invocation, outputPath, int64(maxBytes)); err != nil {
		return Result{}, err
	}

	pdf, err := readAndVerifyPDF(outputPath, maxBytes)
	if err != nil {
		return Result{}, err
	}
	return Result{PDF: pdf, Profile: article.Profile}, nil
}

func chromiumArgs(tempDir, htmlPath, outputPath string) []string {
	return []string{
		"--headless",
		"--disable-dev-shm-usage",
		"--disable-gpu",
		"--disable-background-networking",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-javascript",
		"--disable-sync",
		"--metrics-recording-only",
		"--no-first-run",
		"--no-default-browser-check",
		"--no-pdf-header-footer",
		"--user-data-dir=" + filepath.Join(tempDir, "chromium-profile"),
		"--print-to-pdf=" + outputPath,
		fileURL(htmlPath),
	}
}

func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

func runBounded(ctx context.Context, cancel context.CancelFunc, executor Executor, invocation Invocation, outputPath string, maxBytes int64) error {
	done := make(chan error, 1)
	go func() { done <- executor.Run(ctx, invocation) }()

	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				return err
			}
			return nil
		case <-ticker.C:
			info, err := os.Stat(outputPath)
			if err == nil && info.Size() > maxBytes {
				cancel()
				<-done
				return ErrOutputTooLarge
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				cancel()
				<-done
				return fmt.Errorf("inspect PDF output: %w", err)
			}
		case <-ctx.Done():
			<-done
			return ctx.Err()
		}
	}
}

func readAndVerifyPDF(path string, maxBytes int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open Chromium PDF: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect Chromium PDF: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, ErrInvalidPDF
	}
	if info.Size() > int64(maxBytes) {
		return nil, ErrOutputTooLarge
	}
	pdf, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	if err != nil {
		return nil, fmt.Errorf("read Chromium PDF: %w", err)
	}
	if len(pdf) > maxBytes {
		return nil, ErrOutputTooLarge
	}
	tail := pdf[len(pdf)-min(len(pdf), 1024):]
	if len(pdf) < 24 || !bytes.HasPrefix(pdf, []byte("%PDF-")) || !bytes.Contains(tail, []byte("startxref")) || !bytes.Contains(tail, []byte("%%EOF")) {
		return nil, ErrInvalidPDF
	}
	return pdf, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func renderHTML(article Article, clean string, marginMM, bodyFontPT, lineHeight float64) string {
	var metadata []string
	for _, value := range []string{article.Author, article.SiteName, article.PublicationDate} {
		if value = strings.TrimSpace(value); value != "" {
			metadata = append(metadata, html.EscapeString(value))
		}
	}

	var source string
	if safeURL(article.SourceURL) {
		escaped := html.EscapeString(strings.TrimSpace(article.SourceURL))
		source = `<p class="source">Source: <a href="` + escaped + `">` + escaped + `</a></p>`
	}
	var generated string
	if !article.GeneratedAt.IsZero() {
		generated = `<p class="generated">Generated ` + html.EscapeString(article.GeneratedAt.UTC().Format(time.RFC3339)) + `</p>`
	}

	return `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'; form-action 'none'; base-uri 'none'">
<style>
@page { size: A5 portrait; margin: ` + cssNumber(marginMM) + `mm; }
html { font-family: Georgia, "Noto Serif", "DejaVu Serif", serif; font-size: ` + cssNumber(bodyFontPT) + `pt; line-height: ` + cssNumber(lineHeight) + `; color: #111; }
body { margin: 0; overflow-wrap: anywhere; }
h1 { font-size: 22pt; line-height: 1.15; margin: 0 0 4mm; }
h2 { font-size: 16pt; } h3 { font-size: 13pt; }
h1, h2, h3, h4, h5, h6 { break-after: avoid; }
p, blockquote, figure, table, pre { orphans: 3; widows: 3; }
img { display: block; max-width: 100%; max-height: 240mm; height: auto; object-fit: contain; break-inside: avoid; }
table { width: 100%; border-collapse: collapse; } th, td { border: .2mm solid #aaa; padding: 1.5mm; }
pre, code { font-family: "DejaVu Sans Mono", monospace; white-space: pre-wrap; }
a { color: inherit; text-decoration: underline; }
.metadata, .source, .generated { color: #555; font-size: 9pt; }
.metadata { margin: 0 0 6mm; } .source { margin-top: 8mm; } .generated { margin-top: 2mm; }
</style></head><body><header><h1>` + html.EscapeString(strings.TrimSpace(article.Title)) + `</h1>` +
		conditionalParagraph("metadata", strings.Join(metadata, " · ")) + `</header><main>` + clean + `</main>` + source + generated + `</body></html>`
}

func cssNumber(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

func conditionalParagraph(class, value string) string {
	if value == "" {
		return ""
	}
	return `<p class="` + class + `">` + value + `</p>`
}

func safeURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
