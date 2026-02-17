package skills

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso-agent/agentloop/backend"
)

// Middleware injects loaded skills into the system prompt.
type Middleware struct {
	loader *Loader
	skills SkillsMap
}

// NewMiddleware creates a new skills middleware.
func NewMiddleware(backend backend.FileBackendProtocol, sources []string) *Middleware {
	return &Middleware{
		loader: NewLoader(backend),
	}
}

// Load loads skills from the configured sources.
func (m *Middleware) Load(ctx context.Context, sources []string) error {
	skills, err := m.loader.LoadFromSources(ctx, sources)
	if err != nil {
		return fmt.Errorf("failed to load skills: %w", err)
	}
	m.skills = skills
	return nil
}

// InjectSkills injects the loaded skills into the system prompt.
// This is typically called during the graph construction phase.
func (m *Middleware) InjectSkills(basePrompt string) string {
	if m.skills == nil || len(m.skills) == 0 {
		return basePrompt
	}

	skillsPrompt := m.skills.FormatForPrompt()
	if skillsPrompt == "" {
		return basePrompt
	}

	return basePrompt + "\n\n" + skillsPrompt
}

// InjectMetadataOnly injects only skill metadata for progressive disclosure.
// The LLM is instructed to read full skill content on-demand using tools.
func (m *Middleware) InjectMetadataOnly(basePrompt string) string {
	if m.skills == nil || len(m.skills) == 0 {
		return basePrompt
	}

	skillsPrompt := m.skills.FormatMetadataOnly()
	if skillsPrompt == "" {
		return basePrompt
	}

	return basePrompt + "\n\n" + skillsPrompt
}

// GetSkills returns the loaded skills map.
func (m *Middleware) GetSkills() SkillsMap {
	return m.skills
}

// PrepareSystemMessage prepares a system message with skills injected.
func PrepareSystemMessage(middleware *Middleware, basePrompt string) *schema.Message {
	if middleware == nil {
		return schema.SystemMessage(basePrompt)
	}

	skillsPrompt := middleware.InjectSkills(basePrompt)
	return schema.SystemMessage(skillsPrompt)
}
