package types

import (
	"embed"
	"io/fs"
	"strings"
)

// BuildPreloadedSkills builds a PreloadedSkills map from an embedded filesystem.
// The baseDirs are the root directories within the embedded FS to start from.
// File paths in the returned map will be relative to the baseDir and prefixed with '/'.
//
// Example:
//
//	//go:embed skills/*
//	var skillsFS embed.FS
//
//	preloaded := types.BuildPreloadedSkills(skillsFS, "skills")
//	// Returns: map[string]string{
//	//   "/skills/web-research/SKILL.md": "...",
//	//   "/skills/code-analysis/SKILL.md": "...",
//	// }
//
// Multiple base dirs:
//
//	preloaded := types.BuildPreloadedSkills(skillsFS, "skills", "templates")
func BuildPreloadedSkills(efs embed.FS, baseDirs ...string) map[string]string {
	result := make(map[string]string)

	for _, baseDir := range baseDirs {
		// Ensure baseDir doesn't have trailing slash
		baseDir = strings.TrimSuffix(baseDir, "/")

		// Walk through the embedded filesystem
		err := fs.WalkDir(efs, baseDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // Skip files that can't be accessed
			}

			// Skip directories
			if d.IsDir() {
				return nil
			}

			// Read file content
			content, err := efs.ReadFile(path)
			if err != nil {
				return nil // Skip files that can't be read
			}

			// Build the virtual path (key for PreloadedSkills)
			// Convert "skills/web-research/SKILL.md" to "/skills/web-research/SKILL.md"
			virtualPath := "/" + path
			result[virtualPath] = string(content)

			return nil
		})

		if err != nil {
			// If walk fails, return whatever we've collected
			return result
		}
	}

	return result
}

// BuildPreloadedSkillsWithFilter builds a PreloadedSkills map from an embedded filesystem
// with a custom filter function. The filter function receives the file path and should return
// true if the file should be included in the result.
//
// Example:
//
//	//go:embed skills/*
//	var skillsFS embed.FS
//
//	// Only include SKILL.md files
//	preloaded := types.BuildPreloadedSkillsWithFilter(skillsFS, func(path string) bool {
//	    return strings.HasSuffix(path, "SKILL.md")
//	}, "skills")
//
// Multiple base dirs:
//
//	preloaded := types.BuildPreloadedSkillsWithFilter(skillsFS, func(path string) bool {
//	    return strings.HasSuffix(path, "SKILL.md")
//	}, "skills", "templates")
func BuildPreloadedSkillsWithFilter(efs embed.FS, filter func(path string) bool, baseDirs ...string) map[string]string {
	result := make(map[string]string)

	for _, baseDir := range baseDirs {
		// Ensure baseDir doesn't have trailing slash
		baseDir = strings.TrimSuffix(baseDir, "/")

		// Walk through the embedded filesystem
		err := fs.WalkDir(efs, baseDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // Skip files that can't be accessed
			}

			// Skip directories
			if d.IsDir() {
				return nil
			}

			// Apply filter
			if filter != nil && !filter(path) {
				return nil
			}

			// Read file content
			content, err := efs.ReadFile(path)
			if err != nil {
				return nil // Skip files that can't be read
			}

			// Build the virtual path (key for PreloadedSkills)
			virtualPath := "/" + path
			result[virtualPath] = string(content)

			return nil
		})

		if err != nil {
			// If walk fails, return whatever we've collected
			return result
		}
	}

	return result
}
