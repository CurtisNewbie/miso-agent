package agentloop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
)

// FileStore defines the interface for file operations.
// This abstraction allows different storage backends (filesystem, in-memory, etc.)
type FileStore interface {
	// ReadFile reads a file from the backend.
	ReadFile(ctx context.Context, path string) ([]byte, error)

	// WriteFile writes content to a file in the backend.
	WriteFile(ctx context.Context, path string, content []byte) error

	// ListDirectory lists files and directories in a path.
	ListDirectory(ctx context.Context, path string) ([]FileInfo, error)

	// FileExists checks if a file exists.
	FileExists(ctx context.Context, path string) (bool, error)

	// DeleteFile deletes a file.
	DeleteFile(ctx context.Context, path string) error
}

// SessionAware is implemented by FileStore backends that need lifecycle management
// tied to an agent session.
type SessionAware interface {
	// OnSessionStart is called when an agent session begins, before any file operations.
	// Implementations should perform any initialisation here (e.g. creating a tmp directory).
	OnSessionStart(rail flow.Rail) error

	// OnSessionEnd is called when an agent session ends.
	// Implementations should release resources here (e.g. removing the tmp directory).
	OnSessionEnd(rail flow.Rail) error
}

// DirBackedFileStore is implemented by FileStore backends that expose a real
// on-disk directory whose relative paths mirror logical file paths, enabling
// tools (like the bash tool) to operate on the same files via a real filesystem.
type DirBackedFileStore interface {
	FileStore
	// RootDir returns the real directory backing this store, creating it if needed.
	RootDir() (string, error)
}

// FileInfo represents file metadata.
type FileInfo struct {
	Path       string    `json:"path"`
	IsDir      bool      `json:"is_dir"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

// fileRef holds a reference to a tmp file on disk for a single logical file entry.
type fileRef struct {
	TmpPath     string // absolute path to the local tmp file; empty for directory entries
	ModifiedAt  time.Time
	IsDirectory bool
	Size        int64
}

// TmpFileStore is a tmp-file-backed FileStore implementation.
// Each session gets its own tmp directory; all files written during the session are
// stored as individual tmp files inside that directory.
// The tmp directory is created lazily on the first WriteFile call.
// Call OnSessionEnd when the session is done to clean up.
//
// TmpFileStore expects paths to already be normalized (forward slashes, no trailing
// slash) and free of traversal segments ("..") — it does not normalize or validate
// paths itself. Callers should go through [newValidatingFileStore] (applied
// automatically by Agent.Execute), which handles both centrally for any FileStore.
type TmpFileStore struct {
	mu    sync.RWMutex
	files map[string]fileRef // logical path -> reference to tmp file on disk
	dir   string             // session tmp directory, created lazily on first WriteFile
}

// NewTmpFileStore creates a new TmpFileStore.
func NewTmpFileStore() *TmpFileStore {
	return &TmpFileStore{
		files: make(map[string]fileRef),
	}
}

// compile-time check that *TmpFileStore satisfies DirBackedFileStore.
var _ DirBackedFileStore = (*TmpFileStore)(nil)

// ensureDir creates the session tmp directory if it has not been created yet.
// Callers must hold b.mu (write lock) before calling this method.
func (b *TmpFileStore) ensureDir() error {
	if b.dir != "" {
		return nil
	}
	dir, err := os.MkdirTemp("", "miso-agent-*")
	if err != nil {
		return errs.Wrapf(err, "failed to create session tmp directory")
	}
	b.dir = dir
	return nil
}

// RootDir returns the real on-disk directory backing this store, creating it if it
// doesn't exist yet. Files inside this directory mirror their logical paths.
func (b *TmpFileStore) RootDir() (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ensureDir(); err != nil {
		return "", err
	}
	return b.dir, nil
}

// OnSessionStart is a no-op for TmpFileStore.
// The tmp directory is created lazily on the first WriteFile call.
func (b *TmpFileStore) OnSessionStart(rail flow.Rail) error {
	return nil
}

// OnSessionEnd removes the session tmp directory and all files inside it.
func (b *TmpFileStore) OnSessionEnd(rail flow.Rail) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.dir == "" {
		return nil
	}
	if err := os.RemoveAll(b.dir); err != nil {
		return errs.Wrapf(err, "failed to remove session tmp directory: %s", b.dir)
	}
	rail.Infof("TmpFileStore session ended, removed tmp dir: %s", b.dir)
	b.dir = ""
	b.files = make(map[string]fileRef)
	return nil
}

// ReadFile reads a file from the tmp-file-backed store.
func (b *TmpFileStore) ReadFile(ctx context.Context, path string) ([]byte, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	ref, exists := b.files[path]
	if !exists {
		return nil, errs.NewErrf("file not found: %s", path)
	}
	if ref.IsDirectory {
		return nil, errs.NewErrf("cannot read directory: %s", path)
	}
	content, err := os.ReadFile(ref.TmpPath)
	if err != nil {
		return nil, errs.Wrapf(err, "failed to read tmp file for %s", path)
	}
	return content, nil
}

// WriteFile writes content to a new tmp file inside the session directory.
// The session tmp directory is created lazily on the first call if OnSessionStart
// has not been called explicitly.
func (b *TmpFileStore) WriteFile(ctx context.Context, path string, content []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.ensureDir(); err != nil {
		return err
	}

	// Ensure parent directory entry exists in the map.
	dir := filepath.Dir(path)
	if dir != "." && dir != "/" {
		if _, exists := b.files[dir]; !exists {
			b.files[dir] = fileRef{
				IsDirectory: true,
				ModifiedAt:  time.Now(),
			}
		}
	}

	// Materialize the file at a real path mirroring its logical path inside the
	// session tmp directory, e.g. logical path /foo/bar.txt -> <dir>/foo/bar.txt.
	realPath := filepath.Join(b.dir, path)
	if err := confineToDir(b.dir, realPath); err != nil {
		return errs.Wrapf(err, "invalid path: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(realPath), 0o700); err != nil {
		return errs.Wrapf(err, "failed to create parent dir for %s", path)
	}
	if err := os.WriteFile(realPath, content, 0o600); err != nil {
		return errs.Wrapf(err, "failed to write file for %s", path)
	}

	b.files[path] = fileRef{
		TmpPath:    realPath,
		ModifiedAt: time.Now(),
		Size:       int64(len(content)),
	}
	return nil
}

// ListDirectory lists direct children of the given path.
func (b *TmpFileStore) ListDirectory(ctx context.Context, path string) ([]FileInfo, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var result []FileInfo

	for p, ref := range b.files {
		if p == path {
			continue
		}
		dir := filepath.Dir(p)
		if dir == path {
			result = append(result, FileInfo{
				Path:       filepath.Base(p),
				IsDir:      ref.IsDirectory,
				Size:       ref.Size,
				ModifiedAt: ref.ModifiedAt,
			})
		}
	}

	return result, nil
}

// FileExists checks whether a file or directory exists.
func (b *TmpFileStore) FileExists(ctx context.Context, path string) (bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	_, exists := b.files[path]
	return exists, nil
}

// DeleteFile removes a file entry and its underlying tmp file.
func (b *TmpFileStore) DeleteFile(ctx context.Context, path string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	ref, exists := b.files[path]
	if !exists {
		return errs.NewErrf("file not found: %s", path)
	}

	// Remove the underlying tmp file (directories have no tmp file).
	if !ref.IsDirectory && ref.TmpPath != "" {
		if err := os.Remove(ref.TmpPath); err != nil && !os.IsNotExist(err) {
			return errs.Wrapf(err, "failed to remove tmp file for %s", path)
		}
	}

	delete(b.files, path)
	return nil
}

// normalizeMemPath normalizes a path to use forward slashes and remove trailing slashes.
func normalizeMemPath(path string) string {
	path = filepath.ToSlash(path)
	path = strings.TrimRight(path, "/")
	if path == "" {
		return "."
	}
	return path
}

// validateMemPath rejects logical paths containing traversal segments ("..").
// Applied uniformly across all FileStore methods so no path-accepting method can
// be used to reference entries outside the logical store, regardless of whether
// that method also touches the real filesystem.
func validateMemPath(path string) error {
	for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
		if seg == ".." {
			return errs.NewErrf("path must not contain traversal segments (..): %s", path)
		}
	}
	return nil
}

// confineToDir returns an error if resolvedPath does not lie within root (or equal
// root). Used to reject logical paths (e.g. containing "..") that would otherwise
// resolve to a real path escaping the store's on-disk root directory.
func confineToDir(root, resolvedPath string) error {
	root = filepath.Clean(root)
	resolvedPath = filepath.Clean(resolvedPath)
	if resolvedPath == root {
		return nil
	}
	if !strings.HasPrefix(resolvedPath, root+string(os.PathSeparator)) {
		return errs.NewErrf("path escapes store root directory")
	}
	return nil
}
