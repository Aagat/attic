package integrations

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractWithReadabilityPreservesScreenshotOnScriptError(t *testing.T) {
	screenshot := []byte("screenshot")
	nodeScript := "#!/bin/sh\n" +
		"printf '%s\\n' '" +
		`{"error":"content is not readable","screenshot":"` +
		base64.StdEncoding.EncodeToString(screenshot) +
		`","isReadable":false}` +
		"'\n" +
		"exit 1\n"

	binDir := t.TempDir()
	nodePath := filepath.Join(binDir, "node")
	if err := os.WriteFile(nodePath, []byte(nodeScript), 0755); err != nil {
		t.Fatalf("failed to write node fixture: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	puppeteer := &puppeteerImpl{scriptPath: "extract.js"}
	content, err := puppeteer.ExtractWithReadability(context.Background(), "https://example.com")
	if err == nil {
		t.Fatal("ExtractWithReadability() error = nil, want an extraction error")
	}
	if content == nil {
		t.Fatal("ExtractWithReadability() content = nil, want structured error content")
	}
	if content.Screenshot != base64.StdEncoding.EncodeToString(screenshot) {
		t.Fatalf("ExtractWithReadability() screenshot = %q, want encoded screenshot", content.Screenshot)
	}

	gotScreenshot, err := puppeteer.CaptureScreenshot(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("CaptureScreenshot() error = %v, want nil when a screenshot is available", err)
	}
	if !bytes.Equal(gotScreenshot, screenshot) {
		t.Fatalf("CaptureScreenshot() = %q, want %q", gotScreenshot, screenshot)
	}
}
