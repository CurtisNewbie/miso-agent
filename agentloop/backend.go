package agentloop

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/curtisnewbie/miso/errs"
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

// FileInfo represents file metadata.
type FileInfo struct {
	Path       string `json:"path"`
	IsDir      bool   `json:"is_dir"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}

// MemFileStore is an in-memory backend implementation.
// Suitable for ephemeral sessions and testing.
type MemFileStore struct {
	mu    sync.RWMutex
	files map[string]*FileEntry
}

// FileEntry represents a file in the memory file backend.
type FileEntry struct {
	Content     []byte
	ModifiedAt  string
	IsDirectory bool
}

// NewMemFileStore creates a new in-memory file backend.
func NewMemFileStore() *MemFileStore {
	return &MemFileStore{
		files: make(map[string]*FileEntry),
	}
}

// ReadFile reads a file from the memory file backend.
func (b *MemFileStore) ReadFile(ctx context.Context, path string) ([]byte, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	normalizedPath := normalizeMemPath(path)
	entry, exists := b.files[normalizedPath]
	if !exists {
		return nil, errs.NewErrf("file not found: %s", path)
	}
	if entry.IsDirectory {
		return nil, errs.NewErrf("cannot read directory: %s", path)
	}
	return entry.Content, nil
}

// WriteFile writes content to a file in the memory file backend.
func (b *MemFileStore) WriteFile(ctx context.Context, path string, content []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	normalizedPath := normalizeMemPath(path)
	dir := filepath.Dir(normalizedPath)

	// Ensure parent directory exists
	if dir != "." && dir != "/" {
		if _, exists := b.files[dir]; !exists {
			b.files[dir] = &FileEntry{
				IsDirectory: true,
				ModifiedAt:  time.Now().Format(time.RFC3339),
			}
		}
	}

	b.files[normalizedPath] = &FileEntry{
		Content:     content,
		ModifiedAt:  time.Now().Format(time.RFC3339),
		IsDirectory: false,
	}

	return nil
}

// ListDirectory lists files and directories in a path.
func (b *MemFileStore) ListDirectory(ctx context.Context, path string) ([]FileInfo, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	normalizedPath := normalizeMemPath(path)
	var result []FileInfo

	for p, entry := range b.files {
		if p == normalizedPath {
			continue
		}

		// Check if this path is a direct child of the requested directory
		dir := filepath.Dir(p)
		if dir == normalizedPath {
			result = append(result, FileInfo{
				Path:       filepath.Base(p),
				IsDir:      entry.IsDirectory,
				Size:       int64(len(entry.Content)),
				ModifiedAt: entry.ModifiedAt,
			})
		}
	}

	return result, nil
}

// FileExists checks if a file exists.
func (b *MemFileStore) FileExists(ctx context.Context, path string) (bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	normalizedPath := normalizeMemPath(path)
	_, exists := b.files[normalizedPath]
	return exists, nil
}

// DeleteFile deletes a file.
func (b *MemFileStore) DeleteFile(ctx context.Context, path string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	normalizedPath := normalizeMemPath(path)
	if _, exists := b.files[normalizedPath]; !exists {
		return errs.NewErrf("file not found: %s", path)
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

// BackendFactory is a function that creates a FileStore.
type BackendFactory func(ctx context.Context) (FileStore, error)
