package library

import (
	"context"
	"io"
	"os"
)

// RestoreUpload stages a seekable ZIP on persistent storage, not the small browser
// tmpfs. The temporary file is private and removed on success or failure.
func (l *Library) RestoreUpload(ctx context.Context, input io.Reader) (int, error) {
	file, err := os.CreateTemp(l.root, ".restore-*.zip")
	if err != nil {
		return 0, ErrStorage
	}
	defer os.Remove(file.Name())
	defer file.Close()
	n, err := io.Copy(file, io.LimitReader(input, maxTransferBytes+1))
	if err != nil {
		return 0, safe(err)
	}
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if n > maxTransferBytes {
		return 0, ErrInvalid
	}
	return l.Restore(ctx, file, n)
}
