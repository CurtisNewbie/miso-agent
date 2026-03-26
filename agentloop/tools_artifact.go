package agentloop

import (
	"sync"

	"github.com/curtisnewbie/miso/errs"
)

// ArtifactManager manages artifacts collected during agent execution.
type ArtifactManager struct {
	mu        sync.RWMutex
	artifacts []Artifact
}

// NewArtifactManager creates a new artifact manager.
func NewArtifactManager() *ArtifactManager {
	return &ArtifactManager{
		artifacts: make([]Artifact, 0),
	}
}

// AddArtifact adds a new artifact.
func (am *ArtifactManager) AddArtifact(artifact Artifact) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if artifact.Path == "" {
		return errs.NewErrf("artifact path cannot be empty")
	}

	am.artifacts = append(am.artifacts, artifact)
	return nil
}

// ListArtifacts returns all artifacts.
func (am *ArtifactManager) ListArtifacts() []Artifact {
	am.mu.RLock()
	defer am.mu.RUnlock()

	result := make([]Artifact, len(am.artifacts))
	copy(result, am.artifacts)
	return result
}

// GetArtifacts returns all artifacts (alias for ListArtifacts).
func (am *ArtifactManager) GetArtifacts() []Artifact {
	return am.ListArtifacts()
}
