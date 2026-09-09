package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"attic/internal/capture"
)

var ErrBrowserCaptureBlocked = errors.New("Complete verification in your browser, then save the page again")

// ImportBrowserCapture preserves the loaded tab before queued server work can
// replace it, while keeping earlier versions and making upload retries harmless.
func (l *Library) ImportBrowserCapture(ctx context.Context, id, rawURL, title string, data []byte) error {
	normalized, err := normalize(rawURL)
	if err != nil || len(title) > 2000 {
		return ErrInvalid
	}
	item, err := l.Get(ctx, id)
	if err != nil {
		return err
	}
	if item.Kind != "bookmark" || normalized != item.URL {
		return ErrInvalid
	}
	result, err := capture.FromMHTML(normalized, title, data)
	if errors.Is(err, capture.ErrBlocked) {
		return ErrBrowserCaptureBlocked
	}
	if err != nil {
		return ErrInvalid
	}
	sum := sha256.Sum256(append([]byte(id+"\x00"), data...))
	return l.persistCapture(ctx, id, "", hex.EncodeToString(sum[:16]), "browser", result)
}
