package formatter

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var validPDF = []byte("%PDF-1.7\n1 0 obj<</Type/Catalog>>endobj\nstartxref\n0\n%%EOF\n")

type executorFunc func(context.Context, Invocation) error

func (f executorFunc) Run(ctx context.Context, invocation Invocation) error {
	return f(ctx, invocation)
}

func TestPDFUsesPrivateSanitizedA5HTMLAndPandocArguments(t *testing.T) {
	var captured Invocation
	var document, template string
	var htmlMode, dirMode os.FileMode
	executor := executorFunc(func(_ context.Context, invocation Invocation) error {
		captured = invocation
		contents, err := os.ReadFile(invocation.HTMLPath)
		if err != nil {
			return err
		}
		document = string(contents)
		tmpl, err := os.ReadFile(invocation.TemplatePath)
		if err != nil {
			return err
		}
		template = string(tmpl)
		htmlInfo, err := os.Stat(invocation.HTMLPath)
		if err != nil {
			return err
		}
		dirInfo, err := os.Stat(filepath.Dir(invocation.HTMLPath))
		if err != nil {
			return err
		}
		htmlMode, dirMode = htmlInfo.Mode().Perm(), dirInfo.Mode().Perm()
		return os.WriteFile(invocation.OutputPath, validPDF, 0o600)
	})

	result, err := (PDF{Executor: executor, PandocPath: "/test/pandoc"}).Format(context.Background(), Article{
		Profile:         "a5",
		Title:           `Title </h1><script>titleEvil()</script>`,
		Author:          `A & B`,
		SiteName:        "Example",
		PublicationDate: "2026-08-31",
		SourceURL:       "https://example.test/article?a=1&b=2",
		SemanticHTML:    `<article><p>Readable <strong>Unicode 東京</strong>.</p><script>bodyEvil()</script><a href="https://example.test/linked">link</a></article>`,
		GeneratedAt:     time.Date(2026, 8, 31, 12, 0, 0, 0, time.FixedZone("test", 2*60*60)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Profile != "a5" || !bytes.Equal(result.PDF, validPDF) {
		t.Fatalf("result = %#v", result)
	}
	if captured.Executable != "/test/pandoc" {
		t.Fatalf("executable = %q", captured.Executable)
	}
	args := strings.Join(captured.Args, "\n")
	for _, want := range []string{"--from=html", "--sandbox", "--pdf-engine=/usr/bin/xelatex", "--pdf-engine-opt=-no-shell-escape", "--output=" + captured.OutputPath} {
		if !strings.Contains(args, want) {
			t.Errorf("Pandoc args missing %q", want)
		}
	}
	for _, want := range []string{`Readable <strong>Unicode 東京</strong>`, `href="https://example.test/linked"`} {
		if !strings.Contains(document, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	for _, want := range []string{`paperwidth=148mm,paperheight=210mm,margin=12mm`, `Latin Modern Roman`, `breaklines=true`, `A \& B`, `https://example.test/article?a=1\&b=2`} {
		if !strings.Contains(template, want) {
			t.Errorf("template missing %q", want)
		}
	}

	for _, forbidden := range []string{"bodyEvil", "<script"} {
		if strings.Contains(document, forbidden) {
			t.Errorf("rendered HTML contains unsafe %q", forbidden)
		}
	}
	if htmlMode != 0o600 || dirMode != 0o700 {
		t.Fatalf("private modes = html %o, dir %o", htmlMode, dirMode)
	}
	if _, err := os.Stat(filepath.Dir(captured.HTMLPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary workspace remains after Format: %v", err)
	}
}

func TestPDFRejectsUnsafeSourceURL(t *testing.T) {
	executor := executorFunc(func(_ context.Context, invocation Invocation) error {
		document, err := os.ReadFile(invocation.HTMLPath)
		if err != nil {
			return err
		}
		if strings.Contains(string(document), "javascript:") {
			return errors.New("unsafe source URL entered render document")
		}
		return os.WriteFile(invocation.OutputPath, validPDF, 0o600)
	})
	_, err := (PDF{Executor: executor}).Format(context.Background(), Article{SemanticHTML: "<p>text</p>", SourceURL: "javascript:alert(1)"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPDFTimeoutCancelsExecutorAndWaitsForCleanup(t *testing.T) {
	var cleaned atomic.Bool
	executor := executorFunc(func(ctx context.Context, _ Invocation) error {
		<-ctx.Done()
		time.Sleep(10 * time.Millisecond)
		cleaned.Store(true)
		return ctx.Err()
	})

	started := time.Now()
	_, err := (PDF{Executor: executor, Timeout: 20 * time.Millisecond}).Format(context.Background(), Article{SemanticHTML: "<p>text</p>"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if !cleaned.Load() {
		t.Fatal("Format returned before executor cleanup")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("timeout cleanup took %v", elapsed)
	}
}

func TestPDFStopsExecutorWhenOutputGrowsPastLimit(t *testing.T) {
	var cleaned atomic.Bool
	executor := executorFunc(func(ctx context.Context, invocation Invocation) error {
		if err := os.WriteFile(invocation.OutputPath, bytes.Repeat([]byte("x"), 2048), 0o600); err != nil {
			return err
		}
		<-ctx.Done()
		cleaned.Store(true)
		return ctx.Err()
	})
	_, err := (PDF{Executor: executor, MaxBytes: 1024, Timeout: time.Second}).Format(context.Background(), Article{SemanticHTML: "<p>text</p>"})
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("error = %v, want output too large", err)
	}
	if !cleaned.Load() {
		t.Fatal("Format returned before oversized render cleanup")
	}
}

func TestPDFRejectsInvalidOrMissingOutput(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "invalid", data: []byte("not a PDF")},
		{name: "missing", data: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := executorFunc(func(_ context.Context, invocation Invocation) error {
				if test.data == nil {
					return nil
				}
				return os.WriteFile(invocation.OutputPath, test.data, 0o600)
			})
			_, err := (PDF{Executor: executor}).Format(context.Background(), Article{SemanticHTML: "<p>text</p>"})
			if err == nil {
				t.Fatal("expected output verification error")
			}
		})
	}
}

func TestPDFRejectsUnsupportedProfileAndUnsafeArticle(t *testing.T) {
	if _, err := (PDF{}).Format(context.Background(), Article{Profile: "a4", SemanticHTML: "text"}); !errors.Is(err, ErrUnsupportedProfile) {
		t.Fatalf("profile error = %v", err)
	}
	if _, err := (PDF{}).Format(context.Background(), Article{SemanticHTML: "<script>onlyActive()</script>"}); !errors.Is(err, ErrInvalidArticle) {
		t.Fatalf("article error = %v", err)
	}
}

func TestPandocPDFIntegration(t *testing.T) {
	if os.Getenv("ATTIC_LATEX_INTEGRATION") != "1" {
		t.Skip("set ATTIC_LATEX_INTEGRATION=1 to run the real Pandoc PDF test")
	}
	for _, tool := range []string{"/usr/bin/pandoc", "/usr/bin/xelatex", "pdfinfo", "pdftotext"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is unavailable", tool)
		}
	}

	result, err := (PDF{PandocPath: "/usr/bin/pandoc", Timeout: 30 * time.Second, Executor: executorFunc(func(ctx context.Context, inv Invocation) error {
		cmd := exec.CommandContext(ctx, inv.Executable, inv.Args...)
		cmd.Env = append(os.Environ(), "openin_any=p", "openout_any=p")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("synthetic fixture typesetting: %s", output)
		}
		return err
	})}).Format(context.Background(), Article{
		Profile:         "kindle-scribe",
		Title:           "Unicode integration",
		Author:          "Ada & René",
		SiteName:        "Example Journal",
		PublicationDate: "2026-08-23",
		SourceURL:       "https://example.test/clickable",
		SemanticHTML: `<article><header><h1>Unicode integration</h1><p>Unwanted metadata</p></header><h2>Heading</h2><p>Selectable résumé 東京</p><p><a href="https://example.test/linked">linked text</a></p><pre><code>cat README.md | sed 's/a/b/'
    indented_code()
\end{verbatim}
\input{/etc/passwd}
</code></pre></article>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "article.pdf")
	if err := os.WriteFile(path, result.PDF, 0o600); err != nil {
		t.Fatal(err)
	}

	info, err := exec.Command("pdfinfo", path).CombinedOutput()
	if err != nil {
		t.Fatalf("pdfinfo: %v\n%s", err, info)
	}
	if !strings.Contains(string(info), "Page size:") || !strings.Contains(string(info), "446.46 x 595.28 pts") {
		t.Fatalf("PDF does not match Kindle Scribe geometry according to pdfinfo:\n%s", info)
	}
	for _, want := range []string{"Title:           Unicode integration", "Author:          Ada & René", "Subject:", "Example Journal", "2026-08-23", "Creator:         Attic"} {
		if !strings.Contains(string(info), want) {
			t.Errorf("PDF metadata missing %q:\n%s", want, info)
		}
	}
	text, err := exec.Command("pdftotext", path, "-").CombinedOutput()
	if err != nil {
		t.Fatalf("pdftotext: %v\n%s", err, text)
	}
	for _, want := range []string{"Unicode integration", "Selectable résumé", "東京", "linked text", "cat README.md", "indented_code()", `\input{/etc/passwd}`} {
		if !strings.Contains(string(text), want) {
			t.Errorf("selectable PDF text missing %q:\n%s", want, text)
		}
	}
	if strings.Count(string(text), "Unicode integration") != 1 || strings.Contains(string(text), "Unwanted metadata") {
		t.Fatalf("duplicate title or page chrome: %s", text)
	}
	urls, err := exec.Command("pdfinfo", "-url", path).CombinedOutput()
	if err != nil {
		t.Fatalf("pdfinfo -url: %v\n%s", err, urls)
	}
	for _, want := range []string{"https://example.test/clickable", "https://example.test/linked"} {
		if !strings.Contains(string(urls), want) {
			t.Errorf("clickable PDF link missing %q:\n%s", want, urls)
		}
	}
}
