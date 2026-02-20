package agentloop

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple path",
			input:    "skills/web-research",
			expected: "skills/web-research",
		},
		{
			name:     "path with leading slash",
			input:    "/skills/web-research",
			expected: "skills/web-research",
		},
		{
			name:     "path with trailing slash",
			input:    "skills/web-research/",
			expected: "skills/web-research",
		},
		{
			name:     "path with both leading and trailing slashes",
			input:    "/skills/web-research/",
			expected: "skills/web-research",
		},
		{
			name:     "empty path",
			input:    "",
			expected: ".",
		},
		{
			name:     "just slashes",
			input:    "/",
			expected: ".",
		},
		{
			name:     "multiple slashes",
			input:    "///skills///web-research///",
			expected: "skills///web-research",
		},
		{
			name:     "single component with slash",
			input:    "skills/",
			expected: "skills",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizePath(tt.input)
			if result != tt.expected {
				t.Errorf("normalizePath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSkillLoader_LoadSkillFile(t *testing.T) {
	ctx := context.Background()
	backend := NewMemFileStore()
	loader := NewSkillLoader(backend)

	// Write a valid skill file
	validSkillContent := `---
name: web-research
description: Structured approach to conducting thorough web research
---

# Web Research Skill

## When to Use
- User asks you to research a topic
`
	backend.WriteFile(ctx, "skills/web-research/SKILL.md", []byte(validSkillContent))

	t.Run("load valid skill", func(t *testing.T) {
		skill, err := loader.LoadSkillFile(ctx, "skills/web-research/SKILL.md")
		if err != nil {
			t.Fatalf("LoadSkillFile failed: %v", err)
		}

		if skill.Metadata.Name != "web-research" {
			t.Errorf("skill name = %q, want 'web-research'", skill.Metadata.Name)
		}
		if skill.Metadata.Description != "Structured approach to conducting thorough web research" {
			t.Errorf("skill description = %q, want 'Structured approach to conducting thorough web research'", skill.Metadata.Description)
		}
		if skill.Path != "skills/web-research/SKILL.md" {
			t.Errorf("skill path = %q, want 'skills/web-research/SKILL.md'", skill.Path)
		}
		if skill.Content == "" {
			t.Error("skill content is empty, want non-empty content")
		}
	})

	t.Run("load non-existent file", func(t *testing.T) {
		_, err := loader.LoadSkillFile(ctx, "skills/non-existent/SKILL.md")
		if err == nil {
			t.Error("LoadSkillFile should return error for non-existent file")
		}
	})

	t.Run("load invalid skill format", func(t *testing.T) {
		invalidContent := `no yaml frontmatter here`
		backend.WriteFile(ctx, "skills/invalid/SKILL.md", []byte(invalidContent))

		_, err := loader.LoadSkillFile(ctx, "skills/invalid/SKILL.md")
		if err == nil {
			t.Error("LoadSkillFile should return error for invalid skill format")
		}
	})

	t.Run("load skill with missing name", func(t *testing.T) {
		missingNameContent := `---
description: A skill without a name
---
# Content
`
		backend.WriteFile(ctx, "skills/missing-name/SKILL.md", []byte(missingNameContent))

		_, err := loader.LoadSkillFile(ctx, "skills/missing-name/SKILL.md")
		if err == nil {
			t.Error("LoadSkillFile should return error for missing name")
		}
	})

	t.Run("load skill with missing description", func(t *testing.T) {
		missingDescContent := `---
name: test-skill
---
# Content
`
		backend.WriteFile(ctx, "skills/missing-desc/SKILL.md", []byte(missingDescContent))

		_, err := loader.LoadSkillFile(ctx, "skills/missing-desc/SKILL.md")
		if err == nil {
			t.Error("LoadSkillFile should return error for missing description")
		}
	})

	t.Run("load skill with invalid name", func(t *testing.T) {
		invalidNameContent := `---
name: Invalid_Name
description: A skill with invalid name
---
# Content
`
		backend.WriteFile(ctx, "skills/invalid-name/SKILL.md", []byte(invalidNameContent))

		_, err := loader.LoadSkillFile(ctx, "skills/invalid-name/SKILL.md")
		if err == nil {
			t.Error("LoadSkillFile should return error for invalid name")
		}
	})
}

func TestSkillLoader_LoadFromSource(t *testing.T) {
	ctx := context.Background()
	backend := NewMemFileStore()
	loader := NewSkillLoader(backend)

	// Create skill files in the source directory
	webResearchContent := `---
name: web-research
description: Web research skill
---
# Web Research
`
	dataAnalysisContent := `---
name: data-analysis
description: Data analysis skill
---
# Data Analysis
`

	backend.WriteFile(ctx, "skills/web-research/SKILL.md", []byte(webResearchContent))
	backend.WriteFile(ctx, "skills/data-analysis/SKILL.md", []byte(dataAnalysisContent))

	// Create a directory without SKILL.md (should be ignored)
	backend.WriteFile(ctx, "skills/ignored/README.md", []byte("readme"))

	t.Run("load from valid source", func(t *testing.T) {
		skills, err := loader.LoadFromSource(ctx, "skills")
		if err != nil {
			t.Fatalf("LoadFromSource failed: %v", err)
		}

		if len(skills) != 2 {
			t.Errorf("got %d skills, want 2", len(skills))
		}

		if _, ok := skills["web-research"]; !ok {
			t.Error("skills map missing 'web-research'")
		}
		if _, ok := skills["data-analysis"]; !ok {
			t.Error("skills map missing 'data-analysis'")
		}
	})

	t.Run("load from non-existent source", func(t *testing.T) {
		// MemFileStore returns empty slice for non-existent directories (no error)
		skills, err := loader.LoadFromSource(ctx, "non-existent")
		if err != nil {
			t.Fatalf("LoadFromSource failed: %v", err)
		}
		if len(skills) != 0 {
			t.Errorf("got %d skills, want 0", len(skills))
		}
	})

	t.Run("load from empty source", func(t *testing.T) {
		backend.WriteFile(ctx, "empty/.gitkeep", []byte(""))

		skills, err := loader.LoadFromSource(ctx, "empty")
		if err != nil {
			t.Fatalf("LoadFromSource failed: %v", err)
		}

		if len(skills) != 0 {
			t.Errorf("got %d skills, want 0", len(skills))
		}
	})
}

func TestSkillLoader_LoadFromSources(t *testing.T) {
	ctx := context.Background()
	backend := NewMemFileStore()
	loader := NewSkillLoader(backend)

	// Create skills in source1
	source1WebResearch := `---
name: web-research
description: Web research from source1
---
# Web Research
`
	source1DataAnalysis := `---
name: data-analysis
description: Data analysis from source1
---
# Data Analysis
`

	backend.WriteFile(ctx, "source1/web-research/SKILL.md", []byte(source1WebResearch))
	backend.WriteFile(ctx, "source1/data-analysis/SKILL.md", []byte(source1DataAnalysis))

	// Create skills in source2 (web-research will override)
	source2WebResearch := `---
name: web-research
description: Web research from source2 (override)
---
# Web Research v2
`
	source2Coding := `---
name: coding
description: Coding skill
---
# Coding
`

	backend.WriteFile(ctx, "source2/web-research/SKILL.md", []byte(source2WebResearch))
	backend.WriteFile(ctx, "source2/coding/SKILL.md", []byte(source2Coding))

	t.Run("load from multiple sources", func(t *testing.T) {
		sources := []string{"source1", "source2"}
		skills, err := loader.LoadFromSources(ctx, sources)
		if err != nil {
			t.Fatalf("LoadFromSources failed: %v", err)
		}

		// Should have 3 skills (web-research, data-analysis, coding)
		if len(skills) != 3 {
			t.Errorf("got %d skills, want 3", len(skills))
		}

		// web-research should be from source2 (override)
		webResearch := skills["web-research"]
		if webResearch.Metadata.Description != "Web research from source2 (override)" {
			t.Errorf("web-research description = %q, want 'Web research from source2 (override)'", webResearch.Metadata.Description)
		}

		// data-analysis should be from source1
		dataAnalysis := skills["data-analysis"]
		if dataAnalysis.Metadata.Description != "Data analysis from source1" {
			t.Errorf("data-analysis description = %q, want 'Data analysis from source1'", dataAnalysis.Metadata.Description)
		}

		// coding should be from source2
		coding := skills["coding"]
		if coding.Metadata.Description != "Coding skill" {
			t.Errorf("coding description = %q, want 'Coding skill'", coding.Metadata.Description)
		}
	})

	t.Run("load with one non-existent source", func(t *testing.T) {
		sources := []string{"source1", "non-existent"}
		// MemFileStore returns empty slice for non-existent directories (no error)
		skills, err := loader.LoadFromSources(ctx, sources)
		if err != nil {
			t.Fatalf("LoadFromSources failed: %v", err)
		}
		// Should still load skills from source1
		if len(skills) != 2 {
			t.Errorf("got %d skills, want 2 (from source1)", len(skills))
		}
	})

	t.Run("load from empty sources list", func(t *testing.T) {
		sources := []string{}
		skills, err := loader.LoadFromSources(ctx, sources)
		if err != nil {
			t.Fatalf("LoadFromSources failed: %v", err)
		}

		if len(skills) != 0 {
			t.Errorf("got %d skills, want 0", len(skills))
		}
	})
}

func TestSkillLoader_FormatMetadataOnly(t *testing.T) {
	t.Run("format skill with all metadata", func(t *testing.T) {
		skill := &Skill{
			Metadata: SkillMetadata{
				Name:         "web-research",
				Description:  "Web research skill",
				License:      "MIT",
				Compatible:   "v1.0+",
				AllowedTools: []string{"search", "read"},
			},
			Path: "/skills/web-research/SKILL.md",
		}

		result := skill.FormatMetadataOnly()

		// Check that name and description are present
		if !strings.Contains(result, "web-research") {
			t.Error("result should contain skill name")
		}
		if !strings.Contains(result, "Web research skill") {
			t.Error("result should contain skill description")
		}
		if !strings.Contains(result, "License: MIT") {
			t.Error("result should contain license")
		}
		if !strings.Contains(result, "Compatibility: v1.0+") {
			t.Error("result should contain compatibility")
		}
		if !strings.Contains(result, "Allowed tools: search, read") {
			t.Error("result should contain allowed tools")
		}
		if !strings.Contains(result, "/skills/web-research/SKILL.md") {
			t.Error("result should contain skill path")
		}
	})

	t.Run("format skill with minimal metadata", func(t *testing.T) {
		skill := &Skill{
			Metadata: SkillMetadata{
				Name:        "simple",
				Description: "Simple skill",
			},
			Path: "/skills/simple/SKILL.md",
		}

		result := skill.FormatMetadataOnly()

		if !strings.Contains(result, "simple") {
			t.Error("result should contain skill name")
		}
		if !strings.Contains(result, "Simple skill") {
			t.Error("result should contain skill description")
		}
	})
}

func TestSkillsMap_FormatMetadataOnly(t *testing.T) {
	t.Run("format multiple skills", func(t *testing.T) {
		skills := SkillsMap{
			"web-research": &Skill{
				Metadata: SkillMetadata{
					Name:        "web-research",
					Description: "Web research skill",
				},
				Path: "/skills/web-research/SKILL.md",
			},
			"data-analysis": &Skill{
				Metadata: SkillMetadata{
					Name:        "data-analysis",
					Description: "Data analysis skill",
				},
				Path: "/skills/data-analysis/SKILL.md",
			},
		}

		result := skills.FormatMetadata()

		if !strings.Contains(result, "web-research") {
			t.Error("result should contain web-research")
		}
		if !strings.Contains(result, "data-analysis") {
			t.Error("result should contain data-analysis")
		}
	})

	t.Run("format empty skills map", func(t *testing.T) {
		skills := SkillsMap{}
		result := skills.FormatMetadata()

		if result != "" {
			t.Errorf("result should be empty, got %q", result)
		}
	})
}
