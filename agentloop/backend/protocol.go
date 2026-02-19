package backend

import (
	"context"
	"time"
)

// FileBackend defines the interface for file operations.
// This abstraction allows different storage backends (filesystem, in-memory, etc.)
type FileBackend interface {
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
	Path       string    `json:"path"`
	IsDir      bool      `json:"is_dir"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

// BackendFactory is a function that creates a FileBackend.
type BackendFactory func(ctx context.Context) (FileBackend, error)
