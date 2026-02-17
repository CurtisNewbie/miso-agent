package skills

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/curtisnewbie/miso-agent/agentloop/backend"
)

// Loader loads skills from a backend.
type Loader struct {
	backend backend.FileBackendProtocol
}

// NewLoader creates a new skill loader.
func NewLoader(backend backend.FileBackendProtocol) *Loader {
	return &Loader{
		backend: backend,
	}
}

// LoadFromSources loads skills from multiple sources.
// Sources are paths to skill directories (e.g., "/skills/user/", "/skills/project/").
// Each source directory should contain skill subdirectories with SKILL.md files.
// Later sources override earlier sources for skills with the same name.
func (l *Loader) LoadFromSources(ctx context.Context, sources []string) (SkillsMap, error) {
	result := make(SkillsMap)

	for _, source := range sources {
		sourceSkills, err := l.LoadFromSource(ctx, source)
		if err != nil {
			return nil, fmt.Errorf("failed to load skills from %s: %w", source, err)
		}

		// Merge skills, later sources override earlier ones
		for name, skill := range sourceSkills {
			result[name] = skill
		}
	}

	return result, nil
}

// LoadFromSource loads skills from a single source directory.
func (l *Loader) LoadFromSource(ctx context.Context, source string) (SkillsMap, error) {
	// Normalize source path
	source = normalizePath(source)

	// List directory contents
	files, err := l.backend.ListDirectory(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("failed to list directory %s: %w", source, err)
	}

	result := make(SkillsMap)

	for _, file := range files {
		if file.IsDir {
			// Try to load SKILL.md from this subdirectory
			skillPath := filepath.Join(source, file.Path, "SKILL.md")
			skill, err := l.LoadSkillFile(ctx, skillPath)
			if err != nil {
				// Log warning but continue loading other skills
				continue
			}
			result[skill.Metadata.Name] = skill
		}
	}

	return result, nil
}

// LoadSkillFile loads a single skill from a SKILL.md file.
func (l *Loader) LoadSkillFile(ctx context.Context, path string) (*Skill, error) {
	// Normalize path
	path = normalizePath(path)

	// Read file
	content, err := l.backend.ReadFile(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to read skill file %s: %w", path, err)
	}

	// Parse skill
	skill, err := LoadSkill(path, content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse skill %s: %w", path, err)
	}

	return skill, nil
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
