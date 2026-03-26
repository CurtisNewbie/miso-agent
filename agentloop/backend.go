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
// Call OnSessionStart before any file operations and OnSessionEnd when done.
type TmpFileStore struct {
	mu    sync.RWMutex
	files map[string]fileRef // logical path -> reference to tmp file on disk
	dir   string             // session tmp directory, created in OnSessionStart
}

// NewTmpFileStore creates a new TmpFileStore.
// Call OnSessionStart before writing any files.
func NewTmpFileStore() *TmpFileStore {
	return &TmpFileStore{
		files: make(map[string]fileRef),
	}
}

// OnSessionStart creates the session tmp directory.
// Must be called before any file operations.
func (b *TmpFileStore) OnSessionStart(rail flow.Rail) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	dir, err := os.MkdirTemp("", "miso-agent-*")
	if err != nil {
		return errs.Wrapf(err, "failed to create session tmp directory")
	}
	b.dir = dir
	rail.Infof("TmpFileStore session started, tmp dir: %s", dir)
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

	normalizedPath := normalizeMemPath(path)
	ref, exists := b.files[normalizedPath]
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
func (b *TmpFileStore) WriteFile(ctx context.Context, path string, content []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	normalizedPath := normalizeMemPath(path)

	// Ensure parent directory entry exists in the map.
	dir := filepath.Dir(normalizedPath)
	if dir != "." && dir != "/" {
		if _, exists := b.files[dir]; !exists {
			b.files[dir] = fileRef{
				IsDirectory: true,
				ModifiedAt:  time.Now(),
			}
		}
	}

	// If a tmp file already exists for this path, overwrite it in place.
	if existing, exists := b.files[normalizedPath]; exists && !existing.IsDirectory && existing.TmpPath != "" {
		if err := os.WriteFile(existing.TmpPath, content, 0o600); err != nil {
			return errs.Wrapf(err, "failed to overwrite tmp file for %s", path)
		}
		b.files[normalizedPath] = fileRef{
			TmpPath:    existing.TmpPath,
			ModifiedAt: time.Now(),
			Size:       int64(len(content)),
		}
		return nil
	}

	// Create a new tmp file inside the session directory.
	f, err := os.CreateTemp(b.dir, "file-*")
	if err != nil {
		return errs.Wrapf(err, "failed to create tmp file for %s", path)
	}
	tmpPath := f.Name()
	if _, err := f.Write(content); err != nil {
		f.Close()
		return errs.Wrapf(err, "failed to write tmp file for %s", path)
	}
	f.Close()

	b.files[normalizedPath] = fileRef{
		TmpPath:    tmpPath,
		ModifiedAt: time.Now(),
		Size:       int64(len(content)),
	}
	return nil
}

// ListDirectory lists direct children of the given path.
func (b *TmpFileStore) ListDirectory(ctx context.Context, path string) ([]FileInfo, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	normalizedPath := normalizeMemPath(path)
	var result []FileInfo

	for p, ref := range b.files {
		if p == normalizedPath {
			continue
		}
		dir := filepath.Dir(p)
		if dir == normalizedPath {
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

	normalizedPath := normalizeMemPath(path)
	_, exists := b.files[normalizedPath]
	return exists, nil
}

// DeleteFile removes a file entry and its underlying tmp file.
func (b *TmpFileStore) DeleteFile(ctx context.Context, path string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	normalizedPath := normalizeMemPath(path)
	ref, exists := b.files[normalizedPath]
	if !exists {
		return errs.NewErrf("file not found: %s", path)
	}

	// Remove the underlying tmp file (directories have no tmp file).
	if !ref.IsDirectory && ref.TmpPath != "" {
		if err := os.Remove(ref.TmpPath); err != nil && !os.IsNotExist(err) {
			return errs.Wrapf(err, "failed to remove tmp file for %s", path)
		}
	}

	delete(b.files, normalizedPath)
	return nil
}

// normalizeMemPath normalizes a path to use forward slashes and remove leading/trailing slashes.
func normalizeMemPath(path string) string {
	path = filepath.ToSlash(path)
	path = strings.Trim(path, "/")
	if path == "" {
		return "."
	}
	return path
}
