package skills

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/curtisnewbie/miso/util/strutil"
	"gopkg.in/yaml.v2"
)

// SkillMetadata represents the metadata of a skill from YAML frontmatter.
type SkillMetadata struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	License      string   `yaml:"license,omitempty"`
	Compatible   string   `yaml:"compatibility,omitempty"`
	Metadata     string   `yaml:"metadata,omitempty"`
	AllowedTools []string `yaml:"allowed_tools,omitempty"`
}

// Skill represents a loaded skill with its metadata and content.
type Skill struct {
	Metadata SkillMetadata
	Path     string
	Content  string // The markdown content (excluding YAML frontmatter)
}

// LoadSkill loads a skill from markdown content with YAML frontmatter.
// Format:
// ---
// name: skill-name
// description: skill description
// ---
// # Skill Content
func LoadSkill(path string, content []byte) (*Skill, error) {
	contentStr := string(content)

	// Split YAML frontmatter and content
	parts := strings.SplitN(contentStr, "---", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid skill format: missing YAML frontmatter")
	}

	yamlContent := parts[1]
	markdownContent := strings.TrimSpace(parts[2])

	// Parse YAML frontmatter
	var metadata SkillMetadata
	if err := yaml.Unmarshal([]byte(yamlContent), &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse skill metadata: %w", err)
	}

	// Validate metadata
	if err := validateMetadata(metadata); err != nil {
		return nil, err
	}

	return &Skill{
		Metadata: metadata,
		Path:     path,
		Content:  markdownContent,
	}, nil
}

// validateMetadata validates the skill metadata according to Agent Skills specification.
func validateMetadata(metadata SkillMetadata) error {
	// Name validation (per Agent Skills spec)
	// 1-64 chars, lowercase alphanumeric and hyphens, must not start/end with hyphen
	if metadata.Name == "" {
		return fmt.Errorf("skill name is required")
	}
	if len(metadata.Name) > 64 {
		return fmt.Errorf("skill name must be 64 characters or less")
	}
	nameRegex := regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	if !nameRegex.MatchString(metadata.Name) {
		return fmt.Errorf("skill name must be lowercase alphanumeric with hyphens, not starting/ending with hyphen")
	}

	// Description validation
	if metadata.Description == "" {
		return fmt.Errorf("skill description is required")
	}
	if len(metadata.Description) > 1024 {
		return fmt.Errorf("skill description must be 1024 characters or less")
	}

	// Compatibility validation (optional, max 500 chars)
	if len(metadata.Compatible) > 500 {
		return fmt.Errorf("skill compatibility must be 500 characters or less")
	}

	return nil
}

// FormatForPrompt formats the skill for injection into the system prompt.
func (s *Skill) FormatForPrompt() string {
	sb := strutil.NewBuilder()

	sb.Printlnf("## %s", s.Metadata.Name)
	sb.Printlnf("%s", s.Metadata.Description)

	if s.Metadata.Compatible != "" {
		sb.Printlnf("**Compatibility:** %s", s.Metadata.Compatible)
	}

	if len(s.Metadata.AllowedTools) > 0 {
		sb.Printlnf("**Allowed Tools:** %s", strings.Join(s.Metadata.AllowedTools, ", "))
	}

	sb.WriteRune('\n')
	sb.WriteString(s.Content)
	sb.WriteRune('\n')

	return sb.String()
}

// FormatMetadataOnly formats only the skill metadata for progressive disclosure.
// This is used in the system prompt to show available skills without loading full content.
func (s *Skill) FormatMetadataOnly() string {
	sb := strutil.NewBuilder()

	annotations := []string{}
	if s.Metadata.License != "" {
		annotations = append(annotations, fmt.Sprintf("License: %s", s.Metadata.License))
	}
	if s.Metadata.Compatible != "" {
		annotations = append(annotations, fmt.Sprintf("Compatibility: %s", s.Metadata.Compatible))
	}

	descLine := fmt.Sprintf("- **%s**: %s", s.Metadata.Name, s.Metadata.Description)
	if len(annotations) > 0 {
		descLine += fmt.Sprintf(" (%s)", strings.Join(annotations, ", "))
	}
	sb.Println(descLine)

	if len(s.Metadata.AllowedTools) > 0 {
		sb.Printlnf("  -> Allowed tools: %s", strings.Join(s.Metadata.AllowedTools, ", "))
	}
	sb.Printlnf("  -> Read `%s` for full instructions", s.Path)

	return sb.String()
}

// SkillsMap is a collection of skills keyed by name.
type SkillsMap map[string]*Skill

// Add adds a skill to the map, overwriting if it exists.
func (sm SkillsMap) Add(skill *Skill) {
	sm[skill.Metadata.Name] = skill
}

// Get retrieves a skill by name.
func (sm SkillsMap) Get(name string) (*Skill, bool) {
	skill, ok := sm[name]
	return skill, ok
}

// List returns all skills in the map.
func (sm SkillsMap) List() []*Skill {
	result := make([]*Skill, 0, len(sm))
	for _, skill := range sm {
		result = append(result, skill)
	}
	return result
}

// FormatForPrompt formats all skills for injection into the system prompt.
func (sm SkillsMap) FormatForPrompt() string {
	if len(sm) == 0 {
		return ""
	}

	sb := strutil.NewBuilder()
	sb.Println("# Available Skills")
	sb.Println("You have access to the following skills. Use them when appropriate:")
	sb.WriteRune('\n')

	for _, skill := range sm.List() {
		sb.WriteString(skill.FormatForPrompt())
		sb.WriteRune('\n')
	}

	return sb.String()
}

// FormatMetadataOnly formats all skills with only metadata for progressive disclosure.
func (sm SkillsMap) FormatMetadataOnly() string {
	if len(sm) == 0 {
		return ""
	}

	sb := strutil.NewBuilder()
	for _, skill := range sm.List() {
		sb.WriteString(skill.FormatMetadataOnly())
		sb.WriteRune('\n')
	}

	return sb.String()
}
