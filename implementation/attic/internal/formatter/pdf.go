// Package formatter renders sanitized articles as PDFs through Pandoc and XeLaTeX.
package formatter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	MaxOutputBytes = 16 << 20
	defaultTimeout = 45 * time.Second
	defaultPandoc  = "/usr/bin/pandoc"
)

var (
	ErrUnsupportedProfile = errors.New("unsupported PDF profile")
	ErrInvalidArticle     = errors.New("invalid semantic article")
	ErrOutputTooLarge     = errors.New("PDF output exceeds configured limit")
	ErrInvalidPDF         = errors.New("Pandoc produced an invalid PDF")
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
// starting a typesetter. Executors must return after ctx is cancelled.
type Invocation struct {
	Executable   string
	Args         []string
	HTMLPath     string
	OutputPath   string
	TemplatePath string
}

type Executor interface {
	Run(context.Context, Invocation) error
}

// CommandExecutor starts Pandoc in its own process group. Cancelling the
// context kills the entire group and still waits for the process to be reaped.
type CommandExecutor struct{}

func (CommandExecutor) Run(ctx context.Context, invocation Invocation) error {
	cmd := exec.Command(invocation.Executable, invocation.Args...)
	cmd.Env = append(os.Environ(), "openin_any=p", "openout_any=p")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Pandoc: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("Pandoc render: %w", err)
		}
		return nil
	case <-ctx.Done():
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			<-done
			return fmt.Errorf("terminate Pandoc: %w", err)
		}
		<-done
		return ctx.Err()
	}
}

type PDF struct {
	MaxBytes   int
	Timeout    time.Duration
	PandocPath string
	TempDir    string
	Executor   Executor
	MarginMM   float64
	BodyFontPT float64
	LineHeight float64
}

func (p PDF) Format(ctx context.Context, article Article) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if article.Profile == "" {
		article.Profile = "a5"
	}
	if article.Profile != "a5" && article.Profile != "kindle-scribe" {
		return Result{}, ErrUnsupportedProfile
	}

	clean, err := sanitize.ArticleHTML(article.SemanticHTML, article.Title)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidArticle, err)
	}
	margin, fontSize, lineHeight := p.MarginMM, p.BodyFontPT, p.LineHeight
	if margin <= 0 {
		margin = 12
	}
	if fontSize <= 0 {
		fontSize = 11
	}
	if lineHeight <= 0 {
		lineHeight = 1.25
	}
	document := "<!doctype html><html><head><meta charset=\"utf-8\"></head><body>" + clean + "</body></html>"

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
	templatePath := filepath.Join(tempDir, "template.tex")
	if err := os.WriteFile(templatePath, []byte(renderTemplate(article, margin, fontSize, lineHeight)), 0o600); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(htmlPath, []byte(document), 0o600); err != nil {
		return Result{}, fmt.Errorf("write private article HTML: %w", err)
	}

	timeout := p.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	renderCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	executable := p.PandocPath
	if executable == "" {
		executable = defaultPandoc
	}
	invocation := Invocation{
		Executable:   executable,
		HTMLPath:     htmlPath,
		TemplatePath: templatePath,
		OutputPath:   outputPath,
		Args:         []string{"--from=html", "--to=latex", "--standalone", "--sandbox", "--no-highlight", "--template=" + templatePath, "--pdf-engine=/usr/bin/xelatex", "--pdf-engine-opt=-no-shell-escape", "--pdf-engine-opt=-halt-on-error", "--output=" + outputPath, htmlPath},
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
		return nil, fmt.Errorf("open Pandoc PDF: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect Pandoc PDF: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, ErrInvalidPDF
	}
	if info.Size() > int64(maxBytes) {
		return nil, ErrOutputTooLarge
	}
	pdf, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	if err != nil {
		return nil, fmt.Errorf("read Pandoc PDF: %w", err)
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

func renderTemplate(article Article, marginMM, bodyFontPT, lineHeight float64) string {
	width := "148"
	if article.Profile == "kindle-scribe" {
		width = "157.5"
	}
	var metadata []string
	for _, v := range []string{article.Author, article.SiteName, article.PublicationDate} {
		if strings.TrimSpace(v) != "" {
			metadata = append(metadata, latexEscape(v))
		}
	}
	if safeURL(article.SourceURL) {
		metadata = append(metadata, `\href{`+latexEscape(article.SourceURL)+`}{Original article}`)
	}
	return strings.NewReplacer("@@WIDTH@@", width, "@@MARGIN@@", number(marginMM), "@@FONT@@", number(bodyFontPT), "@@LEADING@@", number(bodyFontPT*lineHeight), "@@TITLE@@", latexEscape(article.Title), "@@AUTHOR@@", latexEscape(article.Author), "@@SUBJECT@@", latexEscape(strings.Join([]string{article.SiteName, article.PublicationDate, article.SourceURL}, " | ")), "@@META@@", strings.Join(metadata, ` \textperiodcentered{} `)).Replace(latexTemplate)
}

// Template dollar signs are escaped separately from TeX's special characters.
func latexEscape(value string) string {
	return strings.NewReplacer(`\`, `\textbackslash{}`, `{`, `\{`, `}`, `\}`, `$`, `\$$`, `&`, `\&`, `#`, `\#`, `%`, `\%`, `_`, `\_`, `~`, `\textasciitilde{}`, `^`, `\textasciicircum{}`).Replace(strings.TrimSpace(value))
}
func number(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

const latexTemplate = `\documentclass[11pt]{article}
\usepackage[paperwidth=@@WIDTH@@mm,paperheight=210mm,margin=@@MARGIN@@mm,includefoot,footskip=7mm]{geometry}
\usepackage{fontspec}
\setmainfont{Latin Modern Roman}
\setsansfont{Latin Modern Sans}
\setmonofont{Latin Modern Mono}
\usepackage{xeCJK}
\setCJKmainfont{Noto Serif CJK JP}
\usepackage{microtype,amsmath,amssymb,graphicx,longtable,booktabs,array,calc,xcolor,fvextra}
\usepackage{hyperref}
\hypersetup{hidelinks,unicode=true,pdftitle={@@TITLE@@},pdfauthor={@@AUTHOR@@},pdfsubject={@@SUBJECT@@},pdfcreator={Attic}}
\usepackage{bookmark,needspace,etoolbox}
\usepackage{adjustbox}
\usepackage{titlesec}
\titleformat{\section}{\large\bfseries}{}{0pt}{}
\titleformat{\subsection}{\normalsize\bfseries}{}{0pt}{}
\titlespacing*{\section}{0pt}{1.2em}{.4em}
\titlespacing*{\subsection}{0pt}{1em}{.3em}
\setcounter{secnumdepth}{0}
\setlength{\parindent}{1em}
\setlength{\parskip}{.3em}
\setlength{\emergencystretch}{2em}
\providecommand{\tightlist}{\setlength{\itemsep}{0pt}\setlength{\parskip}{0pt}}
\providecommand{\pandocbounded}[1]{\begin{adjustbox}{max width=\linewidth,max totalheight=.85\textheight}#1\end{adjustbox}}
\newenvironment{Shaded}{}{}
\DefineVerbatimEnvironment{Highlighting}{Verbatim}{commandchars=\\\{\}}
\newcommand{\NormalTok}[1]{#1}
\fvset{fontsize=\footnotesize,breaklines=true,breakanywhere=true,breakautoindent=true,breakindent=1em}
\RecustomVerbatimEnvironment{verbatim}{Verbatim}{fontsize=\footnotesize,breaklines=true,breakanywhere=true,breakautoindent=true,breakindent=1em,frame=leftline,framesep=5pt,rulecolor=\color{gray},baselinestretch=1}
\BeforeBeginEnvironment{verbatim}{\Needspace{60pt}}
\BeforeBeginEnvironment{Shaded}{\Needspace{60pt}}
\begin{document}
\fontsize{@@FONT@@}{@@LEADING@@}\selectfont
\begin{center}
{\LARGE\bfseries @@TITLE@@\par}
\vspace{.7em}
{\small @@META@@\par}
\end{center}
\vspace{.5em}
$body$
\end{document}
`

func safeURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
