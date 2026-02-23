package agentloop

import (
	"testing"
)

func TestArtifactManager_AddArtifact(t *testing.T) {
	am := NewArtifactManager()

	artifact := Artifact{
		Path:        "/test/file.txt",
		SizeInBytes: 100,
		Meta:        map[string]string{"title": "Test File"},
	}

	err := am.AddArtifact(artifact)
	if err != nil {
		t.Fatalf("Failed to add artifact: %v", err)
	}

	artifacts := am.ListArtifacts()
	if len(artifacts) != 1 {
		t.Errorf("Expected 1 artifact, got %d", len(artifacts))
	}

	if artifacts[0].Path != artifact.Path {
		t.Errorf("Expected path %q, got %q", artifact.Path, artifacts[0].Path)
	}

	if artifacts[0].SizeInBytes != artifact.SizeInBytes {
		t.Errorf("Expected size %d, got %d", artifact.SizeInBytes, artifacts[0].SizeInBytes)
	}
}

func TestArtifactManager_AddArtifact_EmptyPath(t *testing.T) {
	am := NewArtifactManager()

	artifact := Artifact{
		Path:        "",
		SizeInBytes: 100,
		Meta:        map[string]string{},
	}

	err := am.AddArtifact(artifact)
	if err == nil {
		t.Error("Expected error for empty path, got nil")
	}

	// Verify no artifact was added
	artifacts := am.ListArtifacts()
	if len(artifacts) != 0 {
		t.Errorf("Expected 0 artifacts, got %d", len(artifacts))
	}
}

func TestArtifactManager_ListArtifacts(t *testing.T) {
	am := NewArtifactManager()

	// Add multiple artifacts
	artifacts := []Artifact{
		{Path: "/test/file1.txt", SizeInBytes: 100, Meta: map[string]string{"title": "File 1"}},
		{Path: "/test/file2.txt", SizeInBytes: 200, Meta: map[string]string{"title": "File 2"}},
		{Path: "/test/file3.txt", SizeInBytes: 300, Meta: map[string]string{"title": "File 3"}},
	}

	for _, artifact := range artifacts {
		err := am.AddArtifact(artifact)
		if err != nil {
			t.Fatalf("Failed to add artifact: %v", err)
		}
	}

	// List artifacts
	listed := am.ListArtifacts()
	if len(listed) != len(artifacts) {
		t.Errorf("Expected %d artifacts, got %d", len(artifacts), len(listed))
	}

	// Verify each artifact
	for i, artifact := range artifacts {
		if listed[i].Path != artifact.Path {
			t.Errorf("Expected path %q, got %q", artifact.Path, listed[i].Path)
		}
		if listed[i].SizeInBytes != artifact.SizeInBytes {
			t.Errorf("Expected size %d, got %d", artifact.SizeInBytes, listed[i].SizeInBytes)
		}
	}
}

func TestArtifactManager_ListArtifacts_Empty(t *testing.T) {
	am := NewArtifactManager()

	artifacts := am.ListArtifacts()
	if len(artifacts) != 0 {
		t.Errorf("Expected 0 artifacts, got %d", len(artifacts))
	}
}

func TestArtifactManager_GetArtifacts(t *testing.T) {
	am := NewArtifactManager()

	artifact := Artifact{
		Path:        "/test/file.txt",
		SizeInBytes: 100,
		Meta:        map[string]string{"title": "Test File"},
	}

	am.AddArtifact(artifact)

	// GetArtifacts should return the same as ListArtifacts
	artifacts := am.GetArtifacts()
	if len(artifacts) != 1 {
		t.Errorf("Expected 1 artifact, got %d", len(artifacts))
	}
}
