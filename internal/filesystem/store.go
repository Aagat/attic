// Package filesystem implements the local-substitutable artifact seam.
//
// The store keeps only opaque relative keys in domain.Artifact. The configured
// root is never returned to callers, and every path component is checked before
// it is used. Writes are staged in the destination directory and committed by
// atomic rename after both the file and (where supported) directory have been
// synchronized.
package filesystem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"attic/internal/application"
	"attic/internal/domain"
)

const (
	defaultDirectoryMode = 0o750
	defaultFileMode      = 0o600
	maxKeyLength         = 1024
	maxSegmentLength     = 255
	writeChunkSize       = 32 * 1024
)

var (
	ErrInvalidRoot      = errors.New("invalid artifact root")
	ErrUnsafeKey        = errors.New("unsafe artifact key")
	ErrEmptyArtifact    = errors.New("empty artifact")
	ErrChecksumMismatch = errors.New("artifact checksum mismatch")
	ErrSizeMismatch     = errors.New("artifact size mismatch")
)

// Store is a filesystem-backed implementation of application.ArtifactStore.
// root is canonicalized once during construction and is intentionally private.
type Store struct {
	root string
}

// ArtifactStore names the concrete filesystem adapter for composition code;
// the behavioral seam it satisfies is application.ArtifactStore.
type ArtifactStore = Store

var _ application.ArtifactStore = (*Store)(nil)

// New creates an artifact store rooted at root. The root is created when it
// does not exist. A configured root that is itself a symlink is rejected so a
// deployment cannot silently redirect all artifacts elsewhere.
func New(root string) (*Store, error) {
	root = strings.TrimSpace(root)
	if root == "" || strings.IndexByte(root, 0) >= 0 {
		return nil, ErrInvalidRoot
	}

	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, operationError("resolve root", err)
	}
	absolute = filepath.Clean(absolute)

	info, err := os.Lstat(absolute)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(absolute, defaultDirectoryMode); err != nil {
			return nil, operationError("create root", err)
		}
		info, err = os.Lstat(absolute)
	}
	if err != nil {
		return nil, operationError("inspect root", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, ErrInvalidRoot
	}
	if err := os.Chmod(absolute, defaultDirectoryMode); err != nil {
		return nil, operationError("secure root", err)
	}

	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, operationError("canonicalize root", err)
	}
	canonicalInfo, err := os.Lstat(canonical)
	if err != nil {
		return nil, operationError("inspect canonical root", err)
	}
	if canonicalInfo.Mode()&os.ModeSymlink != 0 || !canonicalInfo.IsDir() {
		return nil, ErrInvalidRoot
	}
	return &Store{root: canonical}, nil
}

// NewArtifactStore is an explicit constructor alias for composition code that
// names adapters by their role.
func NewArtifactStore(root string) (*Store, error) {
	return New(root)
}

// Put atomically stores a non-empty PDF payload under jobID/filename and
// returns metadata suitable for persistence. The key is relative and contains
// no host path. Existing regular artifacts with the same key are replaced by a
// complete new file in one rename operation.
func (s *Store) Put(ctx context.Context, jobID domain.JobID, filename string, data []byte, now time.Time) (domain.Artifact, error) {
	if err := contextError(ctx); err != nil {
		return domain.Artifact{}, err
	}
	if s == nil || s.root == "" {
		return domain.Artifact{}, ErrInvalidRoot
	}
	if len(data) == 0 {
		return domain.Artifact{}, ErrEmptyArtifact
	}
	if err := validateSegment(string(jobID)); err != nil {
		return domain.Artifact{}, err
	}
	if err := validateSegment(filename); err != nil {
		return domain.Artifact{}, err
	}

	jobDir, err := s.ensureJobDirectory(string(jobID))
	if err != nil {
		return domain.Artifact{}, err
	}
	key := string(jobID) + "/" + filename
	finalPath, parent, err := s.resolveParent(key)
	if err != nil {
		return domain.Artifact{}, err
	}
	if parent != jobDir {
		return domain.Artifact{}, ErrUnsafeKey
	}

	temporary, err := os.CreateTemp(parent, ".attic-artifact-*")
	if err != nil {
		return domain.Artifact{}, operationError("create temporary artifact", err)
	}
	temporaryName := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(defaultFileMode); err != nil {
		_ = temporary.Close()
		return domain.Artifact{}, operationError("secure temporary artifact", err)
	}

	hash := sha256.New()
	if err := writeContext(ctx, temporary, hash, data); err != nil {
		_ = temporary.Close()
		return domain.Artifact{}, err
	}
	if err := contextError(ctx); err != nil {
		_ = temporary.Close()
		return domain.Artifact{}, err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return domain.Artifact{}, operationError("synchronize artifact", err)
	}
	if err := temporary.Close(); err != nil {
		return domain.Artifact{}, operationError("close temporary artifact", err)
	}

	// A final symlink is never followed or replaced. Replacing a regular file
	// is safe and preserves idempotent retry behavior for a deterministic key.
	if info, statErr := os.Lstat(finalPath); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return domain.Artifact{}, ErrUnsafeKey
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return domain.Artifact{}, operationError("inspect artifact destination", statErr)
	}
	if err := contextError(ctx); err != nil {
		return domain.Artifact{}, err
	}
	if err := os.Rename(temporaryName, finalPath); err != nil {
		return domain.Artifact{}, operationError("commit artifact", err)
	}
	removeTemporary = false
	if err := syncDirectory(parent); err != nil {
		return domain.Artifact{}, err
	}

	checksum := hex.EncodeToString(hash.Sum(nil))
	artifact := domain.Artifact{
		Key:       key,
		Filename:  filename,
		MediaType: "application/pdf",
		ByteSize:  int64(len(data)),
		Checksum:  checksum,
		Available: true,
		CreatedAt: now.UTC(),
	}
	if err := s.verifyCommitted(ctx, finalPath, artifact.ByteSize, artifact.Checksum); err != nil {
		_ = removeFile(finalPath)
		return domain.Artifact{}, err
	}
	return artifact, nil
}

// Open validates metadata against the current regular file before returning a
// read-only, context-aware stream. Missing or unavailable artifacts preserve
// os.ErrNotExist for the application seam's 404 mapping.
func (s *Store) Open(ctx context.Context, artifact domain.Artifact) (io.ReadCloser, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.root == "" {
		return nil, ErrInvalidRoot
	}
	if !artifact.Available {
		return nil, os.ErrNotExist
	}
	if err := validateKey(artifact.Key); err != nil {
		return nil, err
	}
	if artifact.Filename != "" {
		if err := validateSegment(artifact.Filename); err != nil {
			return nil, err
		}
	}
	if artifact.ByteSize < 0 {
		return nil, ErrSizeMismatch
	}
	if !validChecksum(artifact.Checksum) {
		return nil, ErrChecksumMismatch
	}

	path, _, err := s.resolveParent(artifact.Key)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, operationError("inspect artifact", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, ErrUnsafeKey
	}

	file, err := openReadOnlyNoFollow(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		if errors.Is(err, syscall.ELOOP) {
			return nil, ErrUnsafeKey
		}
		return nil, operationError("open artifact", err)
	}
	if err := verifyFile(ctx, file, artifact.ByteSize, artifact.Checksum); err != nil {
		_ = file.Close()
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, operationError("rewind artifact", err)
	}
	return &contextReadCloser{ctx: ctx, file: file}, nil
}

// Delete removes an artifact by key and is idempotent. A final symlink is
// removed as a directory entry without following its target; symlinked parent
// directories are rejected by resolveParent.
func (s *Store) Delete(ctx context.Context, artifact domain.Artifact) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.root == "" {
		return ErrInvalidRoot
	}
	if err := validateKey(artifact.Key); err != nil {
		return err
	}
	path, parent, err := s.resolveParent(artifact.Key)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return operationError("inspect artifact for deletion", err)
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return ErrUnsafeKey
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return operationError("delete artifact", err)
	}
	return syncDirectory(parent)
}

func (s *Store) ensureJobDirectory(jobID string) (string, error) {
	if err := validateSegment(jobID); err != nil {
		return "", err
	}
	path := filepath.Join(s.root, jobID)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, defaultDirectoryMode); err != nil && !errors.Is(err, os.ErrExist) {
			return "", operationError("create artifact directory", err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return "", operationError("inspect artifact directory", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", ErrUnsafeKey
	}
	if err := os.Chmod(path, defaultDirectoryMode); err != nil {
		return "", operationError("secure artifact directory", err)
	}
	return path, nil
}

// resolveParent checks every existing directory component without following a
// symlink and returns the final path plus its verified parent. Missing final
// files are allowed so Put can create them; missing parent components remain
// os.ErrNotExist for Open/Delete.
func (s *Store) resolveParent(key string) (string, string, error) {
	parts, err := splitKey(key)
	if err != nil {
		return "", "", err
	}
	parent := s.root
	for _, part := range parts[:len(parts)-1] {
		next := filepath.Join(parent, part)
		info, statErr := os.Lstat(next)
		if errors.Is(statErr, os.ErrNotExist) {
			return "", "", os.ErrNotExist
		}
		if statErr != nil {
			return "", "", operationError("inspect artifact path", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", "", ErrUnsafeKey
		}
		parent = next
	}
	return filepath.Join(parent, parts[len(parts)-1]), parent, nil
}

func splitKey(key string) ([]string, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	parts := strings.Split(key, "/")
	for _, part := range parts {
		if err := validateSegment(part); err != nil {
			return nil, err
		}
	}
	return parts, nil
}

func validateKey(key string) error {
	if len(key) == 0 || len(key) > maxKeyLength || strings.IndexByte(key, 0) >= 0 {
		return ErrUnsafeKey
	}
	if filepath.IsAbs(key) || filepath.VolumeName(key) != "" || strings.Contains(key, `\`) {
		return ErrUnsafeKey
	}
	if filepath.Clean(filepath.FromSlash(key)) != filepath.FromSlash(key) {
		return ErrUnsafeKey
	}
	return nil
}

func validateSegment(segment string) error {
	if len(segment) == 0 || len(segment) > maxSegmentLength || strings.TrimSpace(segment) == "" {
		return ErrUnsafeKey
	}
	if segment == "." || segment == ".." || strings.IndexByte(segment, 0) >= 0 {
		return ErrUnsafeKey
	}
	if strings.ContainsAny(segment, `/\`) || filepath.IsAbs(segment) || filepath.VolumeName(segment) != "" {
		return ErrUnsafeKey
	}
	return nil
}

func writeContext(ctx context.Context, file *os.File, hash io.Writer, data []byte) error {
	for offset := 0; offset < len(data); {
		if err := contextError(ctx); err != nil {
			return err
		}
		end := offset + writeChunkSize
		if end > len(data) {
			end = len(data)
		}
		written, err := file.Write(data[offset:end])
		if written > 0 {
			if _, hashErr := hash.Write(data[offset : offset+written]); hashErr != nil {
				return operationError("hash artifact", hashErr)
			}
			offset += written
		}
		if err != nil {
			return operationError("write artifact", err)
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func (s *Store) verifyCommitted(ctx context.Context, path string, expectedSize int64, checksum string) error {
	file, err := openReadOnlyNoFollow(path)
	if err != nil {
		return operationError("verify artifact", err)
	}
	defer file.Close()
	return verifyFile(ctx, file, expectedSize, checksum)
}

func verifyFile(ctx context.Context, file *os.File, expectedSize int64, checksum string) error {
	if expectedSize < 0 {
		return ErrSizeMismatch
	}
	if !validChecksum(checksum) {
		return ErrChecksumMismatch
	}
	info, err := file.Stat()
	if err != nil {
		return operationError("stat artifact", err)
	}
	if !info.Mode().IsRegular() {
		return ErrUnsafeKey
	}
	if info.Size() != expectedSize {
		return ErrSizeMismatch
	}

	hash := sha256.New()
	buffer := make([]byte, writeChunkSize)
	var total int64
	for {
		if err := contextError(ctx); err != nil {
			return err
		}
		read, readErr := file.Read(buffer)
		if read > 0 {
			if _, hashErr := hash.Write(buffer[:read]); hashErr != nil {
				return operationError("hash artifact", hashErr)
			}
			total += int64(read)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return operationError("read artifact", readErr)
		}
	}
	if total != expectedSize {
		return ErrSizeMismatch
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), checksum) {
		return ErrChecksumMismatch
	}
	return nil
}

func validChecksum(checksum string) bool {
	if len(checksum) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(checksum)
	return err == nil
}

func openReadOnlyNoFollow(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), filepath.Base(path))
	if file == nil {
		_ = syscall.Close(fd)
		return nil, errors.New("could not create artifact file")
	}
	return file, nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return operationError("open artifact directory", err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil && !directorySyncUnsupported(syncErr) {
		return operationError("synchronize artifact directory", syncErr)
	}
	if closeErr != nil {
		return operationError("close artifact directory", closeErr)
	}
	return nil
}

func directorySyncUnsupported(err error) bool {
	return errors.Is(err, syscall.EINVAL) ||
		errors.Is(err, syscall.EISDIR) ||
		errors.Is(err, syscall.ENOTSUP) ||
		errors.Is(err, syscall.EOPNOTSUPP)
}

func removeFile(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return operationError("remove incomplete artifact", err)
	}
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

type contextReadCloser struct {
	ctx  context.Context
	file *os.File
	once sync.Once
	err  error
}

func (r *contextReadCloser) Read(p []byte) (int, error) {
	if err := contextError(r.ctx); err != nil {
		_ = r.Close()
		return 0, err
	}
	return r.file.Read(p)
}

func (r *contextReadCloser) Close() error {
	r.once.Do(func() { r.err = r.file.Close() })
	return r.err
}

type operationFailure struct {
	operation string
	cause     error
}

func (e *operationFailure) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("artifact %s failed", e.operation)
}

func (e *operationFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func operationError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrInvalidRoot) || errors.Is(err, ErrUnsafeKey) ||
		errors.Is(err, ErrEmptyArtifact) || errors.Is(err, ErrChecksumMismatch) ||
		errors.Is(err, ErrSizeMismatch) || errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrNotExist) {
		return err
	}
	return &operationFailure{operation: operation, cause: err}
}
