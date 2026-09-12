package setup

import (
	"attic/internal/acquisition"
	"attic/internal/formatter"
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Runtime tests the real template and a sandboxed local browser page. It makes
// no provider request and sends no mail. Raw subprocess output stays private.
func Runtime(parent context.Context) map[string]string {
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	result := map[string]string{}
	for _, tool := range []string{"pandoc", "xelatex", "pdfinfo", "pdftotext", "Xvfb", "fc-match"} {
		if _, err := exec.LookPath(tool); err != nil {
			result[tool] = "Missing: install the supported runtime image"
		} else {
			result[tool] = "Installed"
		}
	}
	font, err := exec.CommandContext(ctx, "fc-match", "-f", "%{family}", "Latin Modern Roman").Output()
	if err != nil || !strings.Contains(string(font), "Latin Modern") {
		result["fonts"] = "Latin Modern missing: install lmodern and refresh fontconfig"
	} else {
		result["fonts"] = "Latin Modern available"
	}
	browser := os.Getenv("BROWSER_EXECUTABLE")
	if browser == "" {
		browser = "/usr/bin/chromium-browser"
	}
	renderer, err := acquisition.NewChromiumRenderer(acquisition.ChromiumConfig{Executable: browser, RenderTimeout: 25 * time.Second})
	if err == nil {
		var page acquisition.RenderedPage
		page, err = renderer.RenderSaved(ctx, "https://example.invalid/attic-runtime", []byte("<html><head><title>Attic runtime</title></head><body><article><p>Offline Attic capture fixture.</p></article></body></html>"))
		if err == nil && (len(page.DOM) == 0 || len(page.Screenshot) == 0) {
			err = context.DeadlineExceeded
		}
	}

	if err != nil {
		result["browser"] = "Launch failed: check Chromium, writable /tmp and scoped AppArmor user namespace policy; retain sandbox"
	} else {
		result["browser"] = "Sandboxed browser launch passed"
	}
	text := strings.Repeat("This deterministic article verifies Attic's reading template and PDF runtime. ", 40)
	_, err = (formatter.Checked{Renderer: formatter.PDF{PandocPath: "/usr/bin/pandoc"}}).Format(ctx, formatter.Article{Title: "Attic runtime verification", Profile: "a5", SemanticHTML: "<article><p>" + text + "</p></article>"})
	if err != nil {
		result["pdf"] = "Actual Attic PDF formatting or validation failed: check Pandoc, XeLaTeX, Poppler and Latin Modern fonts"
	} else {
		result["pdf"] = "Attic PDF template and quality validation passed"
	}
	return result
}
