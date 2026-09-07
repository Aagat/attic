package httpapi

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestExtensionDownloadContainsOnlyPackagedSources(t *testing.T) {
	server, _, _ := testServer(t)
	response := request(server, "GET", "/attic-chromium.zip", "", "")
	if response.Code != 200 || response.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("download response: %d", response.Code)
	}
	if head := request(server, "HEAD", "/attic-chromium.zip", "", ""); head.Code != 200 || head.Body.Len() != 0 {
		t.Fatal("invalid HEAD response")
	}
	if post := request(server, "POST", "/attic-chromium.zip", "", ""); post.Code != 405 {
		t.Fatal("extension endpoint accepts writes")
	}
	reader, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range reader.File {
		if !strings.HasPrefix(file.Name, "attic/") || strings.Contains(file.Name, "..") || strings.Contains(file.Name, ".env") {
			t.Fatalf("unsafe archive entry: %s", file.Name)
		}
		if file.Name == "attic/manifest.json" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing extension manifest")
	}
}
