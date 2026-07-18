package agentloop

import (
	"context"
)

// validatingFileStore wraps a FileStore and rejects logical paths containing
// traversal segments ("..") before delegating to the wrapped store. This lets any
// FileStore implementation share the same path validation without duplicating it
// in every implementation's methods.
type validatingFileStore struct {
	inner FileStore
}

func (v *validatingFileStore) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if err := validateMemPath(path); err != nil {
		return nil, err
	}
	return v.inner.ReadFile(ctx, normalizeMemPath(path))
}

func (v *validatingFileStore) WriteFile(ctx context.Context, path string, content []byte) error {
	if err := validateMemPath(path); err != nil {
		return err
	}
	return v.inner.WriteFile(ctx, normalizeMemPath(path), content)
}

func (v *validatingFileStore) ListDirectory(ctx context.Context, path string) ([]FileInfo, error) {
	if err := validateMemPath(path); err != nil {
		return nil, err
	}
	return v.inner.ListDirectory(ctx, normalizeMemPath(path))
}

func (v *validatingFileStore) FileExists(ctx context.Context, path string) (bool, error) {
	if err := validateMemPath(path); err != nil {
		return false, err
	}
	return v.inner.FileExists(ctx, normalizeMemPath(path))
}

func (v *validatingFileStore) DeleteFile(ctx context.Context, path string) error {
	if err := validateMemPath(path); err != nil {
		return err
	}
	return v.inner.DeleteFile(ctx, normalizeMemPath(path))
}

// validatingSessionAwareFileStore adds SessionAware to validatingFileStore by
// promoting the embedded interface's methods (no conflict with FileStore's method
// set since SessionAware declares none of the same names).
type validatingSessionAwareFileStore struct {
	*validatingFileStore
	SessionAware
}

// validatingDirBackedFileStore adds DirBackedFileStore to validatingFileStore.
// RootDir is forwarded explicitly (DirBackedFileStore embeds FileStore, so it can't
// be embedded directly here without an ambiguous-selector conflict with
// validatingFileStore's own FileStore methods).
type validatingDirBackedFileStore struct {
	*validatingFileStore
	db DirBackedFileStore
}

func (v *validatingDirBackedFileStore) RootDir() (string, error) {
	return v.db.RootDir()
}

// validatingFullFileStore adds both SessionAware and DirBackedFileStore.
type validatingFullFileStore struct {
	*validatingFileStore
	SessionAware
	db DirBackedFileStore
}

func (v *validatingFullFileStore) RootDir() (string, error) {
	return v.db.RootDir()
}

// newValidatingFileStore wraps inner so all path-accepting FileStore methods reject
// traversal segments (".."), regardless of inner's implementation. If inner also
// implements SessionAware and/or DirBackedFileStore, the returned FileStore forwards
// those interfaces too, so callers doing type assertions (e.g. agent.go's session
// lifecycle check, the bash tool's DirBackedFileStore check) keep working exactly as
// if inner were used directly.
func newValidatingFileStore(inner FileStore) FileStore {
	base := &validatingFileStore{inner: inner}
	sa, isSessionAware := inner.(SessionAware)
	db, isDirBacked := inner.(DirBackedFileStore)

	switch {
	case isSessionAware && isDirBacked:
		return &validatingFullFileStore{validatingFileStore: base, SessionAware: sa, db: db}
	case isSessionAware:
		return &validatingSessionAwareFileStore{validatingFileStore: base, SessionAware: sa}
	case isDirBacked:
		return &validatingDirBackedFileStore{validatingFileStore: base, db: db}
	default:
		return base
	}
}

var (
	_ FileStore          = (*validatingFileStore)(nil)
	_ SessionAware       = (*validatingSessionAwareFileStore)(nil)
	_ DirBackedFileStore = (*validatingDirBackedFileStore)(nil)
	_ SessionAware       = (*validatingFullFileStore)(nil)
	_ DirBackedFileStore = (*validatingFullFileStore)(nil)
)
