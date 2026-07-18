package agentloop

import (
	"context"
	"testing"

	"github.com/curtisnewbie/miso/flow"
)

// fakeFileStore is a minimal FileStore that implements neither SessionAware nor
// DirBackedFileStore, used to verify the validating wrapper doesn't fabricate
// support for capabilities the inner store doesn't have.
type fakeFileStore struct {
	written map[string][]byte
}

func newFakeFileStore() *fakeFileStore {
	return &fakeFileStore{written: make(map[string][]byte)}
}

func (f *fakeFileStore) ReadFile(ctx context.Context, path string) ([]byte, error) {
	return f.written[path], nil
}
func (f *fakeFileStore) WriteFile(ctx context.Context, path string, content []byte) error {
	f.written[path] = content
	return nil
}
func (f *fakeFileStore) ListDirectory(ctx context.Context, path string) ([]FileInfo, error) {
	return nil, nil
}
func (f *fakeFileStore) FileExists(ctx context.Context, path string) (bool, error) {
	_, ok := f.written[path]
	return ok, nil
}
func (f *fakeFileStore) DeleteFile(ctx context.Context, path string) error {
	delete(f.written, path)
	return nil
}

func TestValidatingFileStore_RejectsTraversal_AllMethods(t *testing.T) {
	ctx := context.Background()
	store := newValidatingFileStore(newFakeFileStore())

	badPath := "../escape.txt"

	if err := store.WriteFile(ctx, badPath, []byte("x")); err == nil {
		t.Error("WriteFile: expected traversal to be rejected")
	}
	if _, err := store.ReadFile(ctx, badPath); err == nil {
		t.Error("ReadFile: expected traversal to be rejected")
	}
	if _, err := store.ListDirectory(ctx, badPath); err == nil {
		t.Error("ListDirectory: expected traversal to be rejected")
	}
	if _, err := store.FileExists(ctx, badPath); err == nil {
		t.Error("FileExists: expected traversal to be rejected")
	}
	if err := store.DeleteFile(ctx, badPath); err == nil {
		t.Error("DeleteFile: expected traversal to be rejected")
	}

	// Sanity: a normal path still works through the wrapper.
	if err := store.WriteFile(ctx, "/ok.txt", []byte("data")); err != nil {
		t.Fatalf("WriteFile with valid path failed: %v", err)
	}
	got, err := store.ReadFile(ctx, "/ok.txt")
	if err != nil || string(got) != "data" {
		t.Errorf("ReadFile with valid path failed: got=%q err=%v", got, err)
	}
}

func TestValidatingFileStore_DoesNotFabricateCapabilities(t *testing.T) {
	store := newValidatingFileStore(newFakeFileStore())

	if _, ok := store.(SessionAware); ok {
		t.Error("wrapped fakeFileStore must not appear SessionAware")
	}
	if _, ok := store.(DirBackedFileStore); ok {
		t.Error("wrapped fakeFileStore must not appear DirBackedFileStore")
	}
}

func TestValidatingFileStore_ForwardsSessionAwareAndDirBacked(t *testing.T) {
	inner := NewTmpFileStore()
	store := newValidatingFileStore(inner)

	sa, ok := store.(SessionAware)
	if !ok {
		t.Fatal("wrapped TmpFileStore must implement SessionAware")
	}
	rail := flow.NewRail(context.Background())
	if err := sa.OnSessionStart(rail); err != nil {
		t.Fatalf("OnSessionStart failed: %v", err)
	}

	ctx := context.Background()
	if err := store.WriteFile(ctx, "/a.txt", []byte("hi")); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	db, ok := store.(DirBackedFileStore)
	if !ok {
		t.Fatal("wrapped TmpFileStore must implement DirBackedFileStore")
	}
	dir, err := db.RootDir()
	if err != nil || dir == "" {
		t.Fatalf("RootDir failed: dir=%q err=%v", dir, err)
	}
	if dir != inner.dir {
		t.Errorf("RootDir() = %q, want %q (inner's real dir)", dir, inner.dir)
	}

	if err := sa.OnSessionEnd(rail); err != nil {
		t.Fatalf("OnSessionEnd failed: %v", err)
	}
}
