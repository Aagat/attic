package capture

import (
	"bytes"
	"errors"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/url"
	"strings"

	"attic/internal/acquisition"
)

const MaxBrowserCaptureBytes = 32 << 20

var ErrBlocked = errors.New("page requires browser verification before capture")

// FromMHTML converts a browser snapshot without fetching anything from its source.
// Its root location must match the tab URL to avoid attaching a navigated tab to
// the wrong bookmark. The normal static sanitizer handles all embedded resources.
func FromMHTML(rawURL, title string, data []byte) (Result, error) {
	if len(data) == 0 || len(data) > MaxBrowserCaptureBytes {
		return Result{}, errors.New("invalid browser snapshot size")
	}
	expected, err := browserURL(rawURL)
	if err != nil {
		return Result{}, err
	}
	message, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return Result{}, err
	}
	kind, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil || kind != "multipart/related" || params["boundary"] == "" {
		return Result{}, errors.New("invalid browser snapshot format")
	}
	parts := multipart.NewReader(message.Body, params["boundary"])
	part, err := parts.NextPart()
	if err != nil {
		return Result{}, err
	}
	rootKind, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
	location, err := browserURL(part.Header.Get("Content-Location"))
	part.Close()
	if err != nil || rootKind != "text/html" || location != expected {
		return Result{}, errors.New("snapshot root does not match tab URL")
	}
	// Prefer the archived document title, including for challenge detection.
	result, err := convert(acquisition.RenderedPage{FinalURL: expected, Status: 200, MHTML: data})
	if err != nil {
		return Result{}, err
	}
	if result.Status == "blocked" {
		return Result{}, ErrBlocked
	}
	if result.Title == "" {
		result.Title = strings.TrimSpace(title)
	}
	result.OriginalURL = expected
	return result, nil
}
func browserURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("invalid snapshot URL")
	}
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}
