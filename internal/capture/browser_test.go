package capture

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func browserFixture(title, body string) []byte {
	return []byte(fmt.Sprintf("MIME-Version: 1.0\r\nContent-Type: multipart/related; boundary=attic\r\n\r\n--attic\r\nContent-Type: text/html\r\nContent-Location: https://source.invalid/story\r\n\r\n<html><head><title>%s</title></head><body>%s</body></html>\r\n--attic--\r\n", title, body))
}
func TestBrowserCaptureUsesStaticSanitizer(t *testing.T) {
	p := fixture()
	result, err := FromMHTML(p.FinalURL+"#section", "ignored tab title", p.MHTML)
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "Saved fixture" || result.OriginalURL != p.FinalURL || !strings.Contains(string(result.HTML), "data:image/png;base64,") || strings.Contains(string(result.HTML), "<script") {
		t.Fatalf("unexpected snapshot %+v", result)
	}
}
func TestBrowserCaptureRejectsInvalidOrMismatchedArchive(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("<html>not MHTML</html>"), []byte("Content-Type: multipart/related; boundary=empty\r\n\r\n--empty--\r\n"), make([]byte, MaxBrowserCaptureBytes+1)} {
		if _, err := FromMHTML("https://source.invalid/story", "", data); err == nil {
			t.Fatal("accepted invalid archive")
		}
	}
	if _, err := FromMHTML("https://source.invalid/other", "", fixture().MHTML); err == nil {
		t.Fatal("accepted different page")
	}
}
func TestBrowserCaptureRejectsChallengeButAcceptsArticleAboutCaptcha(t *testing.T) {
	for _, sample := range []struct{ title, body string }{{"CAPTCHA", "Please complete verification"}, {"Just a moment...", "Loading"}, {"Article", "Please verify you are human to continue"}} {
		if _, err := FromMHTML("https://source.invalid/story", "", browserFixture(sample.title, sample.body)); !errors.Is(err, ErrBlocked) {
			t.Fatalf("challenge accepted: %v", err)
		}
	}
	if _, err := FromMHTML("https://source.invalid/story", "", browserFixture("How CAPTCHA works", strings.Repeat("Article content about captcha systems. ", 100)+"verify you are human")); err != nil {
		t.Fatal(err)
	}
}
