package agentloop

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/curtisnewbie/miso/errs"
)

// SkillLoader loads skills from a backend.
type SkillLoader struct {
	backend FileStore
}

// NewSkillLoader creates a new skill loader.
func NewSkillLoader(backend FileStore) *SkillLoader {
	return &SkillLoader{
		backend: backend,
	}
}

// LoadFromSources loads skills from multiple sources.
// Sources are paths to skill directories (e.g., "/skills/user/", "/skills/project/").
// Each source directory should contain skill subdirectories with SKILL.md files.
// Later sources override earlier sources for skills with the same name.
func (l *SkillLoader) LoadFromSources(ctx context.Context, sources []string) (SkillsMap, error) {
	result := make(SkillsMap)

	for _, source := range sources {
		sourceSkills, err := l.LoadFromSource(ctx, source)
		if err != nil {
			return nil, errs.Wrapf(err, "failed to load skills from %s", source)
		}

		// Merge skills, later sources override earlier ones
		for name, skill := range sourceSkills {
			result[name] = skill
		}
	}

	return result, nil
}

// LoadFromSource loads skills from a single source directory.
func (l *SkillLoader) LoadFromSource(ctx context.Context, source string) (SkillsMap, error) {
	// Normalize source path
	source = normalizePath(source)

	// List directory contents
	files, err := l.backend.ListDirectory(ctx, source)
	if err != nil {
		return nil, errs.Wrapf(err, "failed to list directory %s", source)
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
func (l *SkillLoader) LoadSkillFile(ctx context.Context, path string) (*Skill, error) {
	// Normalize path
	path = normalizePath(path)

	// Read file
	content, err := l.backend.ReadFile(ctx, path)
	if err != nil {
		return nil, errs.Wrapf(err, "failed to read skill file %s", path)
	}

	// Parse skill
	skill, err := LoadSkill(path, content)
	if err != nil {
		return nil, errs.Wrapf(err, "failed to parse skill %s", path)
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
