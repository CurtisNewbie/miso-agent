package agentloop

import (
	"context"

	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso-agent/agentloop/backend"
	"github.com/curtisnewbie/miso/errs"
)

// Skills injects loaded skills into the system prompt.
type Skills struct {
	loader *SkillLoader
	skills SkillsMap
}

// NewSkills creates a new skills manager.
func NewSkills(backend backend.FileBackend) *Skills {
	return &Skills{
		loader: NewSkillLoader(backend),
	}
}

// Load loads skills from the configured sources.
func (s *Skills) Load(ctx context.Context, sources []string) error {
	skills, err := s.loader.LoadFromSources(ctx, sources)
	if err != nil {
		return errs.Wrapf(err, "failed to load skills")
	}
	s.skills = skills
	return nil
}

// InjectSkills injects the loaded skills into the system prompt.
// This is typically called during the graph construction phase.
func (s *Skills) InjectSkills(basePrompt string) string {
	if s.skills == nil || len(s.skills) == 0 {
		return basePrompt
	}

	skillsPrompt := s.skills.FormatForPrompt()
	if skillsPrompt == "" {
		return basePrompt
	}

	return basePrompt + "\n\n" + skillsPrompt
}

// InjectMetadataOnly injects only skill metadata for progressive disclosure.
// The LLM is instructed to read full skill content on-demand using tools.
func (s *Skills) InjectMetadataOnly(basePrompt string) string {
	if s.skills == nil || len(s.skills) == 0 {
		return basePrompt
	}

	skillsPrompt := s.skills.FormatMetadataOnly()
	if skillsPrompt == "" {
		return basePrompt
	}

	return basePrompt + "\n\n" + skillsPrompt
}

// GetSkills returns the loaded skills map.
func (s *Skills) GetSkills() SkillsMap {
	return s.skills
}

// PrepareSystemMessage prepares a system message with skills injected.
func PrepareSystemMessage(skills *Skills, basePrompt string) *schema.Message {
	if skills == nil {
		return schema.SystemMessage(basePrompt)
	}

	skillsPrompt := skills.InjectSkills(basePrompt)
	return schema.SystemMessage(skillsPrompt)
}
