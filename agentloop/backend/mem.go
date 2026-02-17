package backend

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MemFileBackend is an in-memory backend implementation.
// Suitable for ephemeral sessions and testing.
type MemFileBackend struct {
	mu    sync.RWMutex
	files map[string]*FileEntry
}

// FileEntry represents a file in the memory file backend.
type FileEntry struct {
	Content     []byte
	ModifiedAt  time.Time
	IsDirectory bool
}

// NewMemFileBackend creates a new in-memory file backend.
func NewMemFileBackend() *MemFileBackend {
	return &MemFileBackend{
		files: make(map[string]*FileEntry),
	}
}

// ReadFile reads a file from the memory file backend.
func (b *MemFileBackend) ReadFile(ctx context.Context, path string) ([]byte, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	normalizedPath := normalizePath(path)
	entry, exists := b.files[normalizedPath]
	if !exists {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	if entry.IsDirectory {
		return nil, fmt.Errorf("cannot read directory: %s", path)
	}
	return entry.Content, nil
}

// WriteFile writes content to a file in the memory file backend.
func (b *MemFileBackend) WriteFile(ctx context.Context, path string, content []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	normalizedPath := normalizePath(path)
	dir := filepath.Dir(normalizedPath)

	// Ensure parent directory exists
	if dir != "." && dir != "/" {
		if _, exists := b.files[dir]; !exists {
			b.files[dir] = &FileEntry{
				IsDirectory: true,
				ModifiedAt:  time.Now(),
			}
		}
	}

	b.files[normalizedPath] = &FileEntry{
		Content:     content,
		ModifiedAt:  time.Now(),
		IsDirectory: false,
	}

	return nil
}

// ListDirectory lists files and directories in a path.
func (b *MemFileBackend) ListDirectory(ctx context.Context, path string) ([]FileInfo, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	normalizedPath := normalizePath(path)
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
func (b *MemFileBackend) FileExists(ctx context.Context, path string) (bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	normalizedPath := normalizePath(path)
	_, exists := b.files[normalizedPath]
	return exists, nil
}

// DeleteFile deletes a file.
func (b *MemFileBackend) DeleteFile(ctx context.Context, path string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	normalizedPath := normalizePath(path)
	if _, exists := b.files[normalizedPath]; !exists {
		return fmt.Errorf("file not found: %s", path)
	}

	delete(b.files, normalizedPath)
	return nil
}

// normalizePath normalizes a path to use forward slashes and remove leading/trailing slashes.
func normalizePath(path string) string {
	path = filepath.ToSlash(path)
	path = strings.Trim(path, "/")
	if path == "" {
		return "."
	}
	return path
}
