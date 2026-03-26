package agentloop

import (
	"context"
	"os"
	"testing"

	"github.com/curtisnewbie/miso/flow"
)

// newTestMemFileStore creates a TmpFileStore with OnSessionStart already called.
// Suitable for use in unit tests; panics if session start fails.
func newTestMemFileStore() *TmpFileStore {
	be := NewTmpFileStore()
	rail := flow.NewRail(context.Background())
	if err := be.OnSessionStart(rail); err != nil {
		panic(err)
	}
	return be
}

func TestTmpFileStore_SessionLifecycle(t *testing.T) {
	be := NewTmpFileStore()
	rail := flow.NewRail(context.Background())

	// Before OnSessionStart the tmp dir should be empty.
	if be.dir != "" {
		t.Fatalf("expected empty dir before OnSessionStart, got %q", be.dir)
	}

	// OnSessionStart must create the tmp directory.
	if err := be.OnSessionStart(rail); err != nil {
		t.Fatalf("OnSessionStart failed: %v", err)
	}
	dir := be.dir
	if dir == "" {
		t.Fatal("expected non-empty dir after OnSessionStart")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("tmp dir %q should exist after OnSessionStart: %v", dir, err)
	}

	// Write a file so there is something in the tmp dir.
	ctx := context.Background()
	if err := be.WriteFile(ctx, "/hello.txt", []byte("hello")); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// OnSessionEnd must remove the tmp directory and all its contents.
	if err := be.OnSessionEnd(rail); err != nil {
		t.Fatalf("OnSessionEnd failed: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("tmp dir %q should have been removed after OnSessionEnd", dir)
	}

	// dir field must be reset.
	if be.dir != "" {
		t.Errorf("expected empty dir after OnSessionEnd, got %q", be.dir)
	}

	// Calling OnSessionEnd again must be a no-op (no error).
	if err := be.OnSessionEnd(rail); err != nil {
		t.Errorf("second OnSessionEnd should be a no-op, got error: %v", err)
	}
}

func TestTmpFileStore_WriteRead_TmpFile(t *testing.T) {
	be := newTestMemFileStore()
	defer be.OnSessionEnd(flow.NewRail(context.Background()))

	ctx := context.Background()
	path := "/data/report.txt"
	content := []byte("session tmp file content")

	if err := be.WriteFile(ctx, path, content); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Confirm the data lands in the session tmp directory on disk.
	normalized := normalizeMemPath(path)
	ref, ok := be.files[normalized]
	if !ok {
		t.Fatal("expected fileRef in map after WriteFile")
	}
	if ref.TmpPath == "" {
		t.Fatal("expected non-empty TmpPath in fileRef")
	}
	if ref.IsDirectory {
		t.Error("file entry must not be marked as directory")
	}
	if ref.Size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), ref.Size)
	}

	// The tmp file must live inside the session dir.
	if !startsWithDir(ref.TmpPath, be.dir) {
		t.Errorf("tmp file %q is not inside session dir %q", ref.TmpPath, be.dir)
	}

	// ReadFile must return the original content.
	got, err := be.ReadFile(ctx, path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("expected %q, got %q", content, got)
	}
}

func TestTmpFileStore_Overwrite_TmpFile(t *testing.T) {
	be := newTestMemFileStore()
	defer be.OnSessionEnd(flow.NewRail(context.Background()))

	ctx := context.Background()
	path := "/note.txt"

	if err := be.WriteFile(ctx, path, []byte("first")); err != nil {
		t.Fatalf("first WriteFile failed: %v", err)
	}
	normalized := normalizeMemPath(path)
	firstTmpPath := be.files[normalized].TmpPath

	// Overwrite must reuse the same tmp file.
	if err := be.WriteFile(ctx, path, []byte("second")); err != nil {
		t.Fatalf("second WriteFile failed: %v", err)
	}
	secondTmpPath := be.files[normalized].TmpPath
	if firstTmpPath != secondTmpPath {
		t.Errorf("expected same tmp file on overwrite, got %q vs %q", firstTmpPath, secondTmpPath)
	}

	got, err := be.ReadFile(ctx, path)
	if err != nil {
		t.Fatalf("ReadFile after overwrite failed: %v", err)
	}
	if string(got) != "second" {
		t.Errorf("expected %q after overwrite, got %q", "second", got)
	}
}

func TestTmpFileStore_DeleteFile_RemovesTmpFile(t *testing.T) {
	be := newTestMemFileStore()
	defer be.OnSessionEnd(flow.NewRail(context.Background()))

	ctx := context.Background()
	path := "/delete-me.txt"

	if err := be.WriteFile(ctx, path, []byte("bye")); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	normalized := normalizeMemPath(path)
	tmpPath := be.files[normalized].TmpPath

	if err := be.DeleteFile(ctx, path); err != nil {
		t.Fatalf("DeleteFile failed: %v", err)
	}

	// Underlying tmp file must be gone.
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("expected tmp file %q to be removed after DeleteFile", tmpPath)
	}

	// Map entry must be gone.
	if _, ok := be.files[normalized]; ok {
		t.Error("expected map entry to be removed after DeleteFile")
	}
}

// startsWithDir reports whether path is located inside dir.
func startsWithDir(path, dir string) bool {
	rel, err := os.Open(dir)
	if err != nil {
		return false
	}
	rel.Close()
	// Simple prefix check after normalisation.
	return len(path) > len(dir) && path[:len(dir)] == dir
}
