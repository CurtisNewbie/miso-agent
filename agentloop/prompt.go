package agentloop

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso-agent/agentloop/skills"
	"github.com/curtisnewbie/miso/util/strutil"
)

// BasePrompt is the base system prompt for the agent.
const BasePrompt = `You are a ReAct agent, an AI assistant that helps users accomplish tasks using tools. You respond with text and tool calls.

## Core Behavior

- Be concise and direct. Don't over-explain unless asked.
- NEVER add unnecessary preamble ("Sure!", "Great question!", "I'll now...").
- Don't say "I'll now do X" — just do it.
- If the request is ambiguous, ask questions before acting.
- If asked how to approach something, explain first, then act.

## ReAct Pattern

Follow the ReAct (Reasoning + Acting) pattern:

1. **Think**: Analyze the task and plan your approach
2. **Act**: Use tools to gather information or perform actions
3. **Observe**: Review the tool outputs
4. **Iterate**: Continue until the task is complete

## Tool Usage

- Use specialized tools when available
- When performing multiple independent operations, make all tool calls in a single response
- Always check tool outputs and handle errors appropriately

## File Operations

- Read files before editing — understand existing content before making changes
- Mimic existing style, naming conventions, and patterns
- Use absolute paths for all file operations

## Progress Updates

For longer tasks, provide brief progress updates at reasonable intervals — a concise sentence recapping what you've done and what's next.

## Completion

Keep working until the task is fully complete. Don't stop partway and explain what you would do — just do it. Only yield back to the user when the task is done or you're genuinely blocked.
`

// PromptBuilder builds the system prompt for the agent.
type PromptBuilder struct {
	basePrompt   string
	customPrompt string
	taskPrompt   string
	skills       *skills.Middleware
	language     string
	currentTime  string
}

// NewPromptBuilder creates a new prompt builder.
func NewPromptBuilder() *PromptBuilder {
	return &PromptBuilder{
		basePrompt: BasePrompt,
		language:   "English",
	}
}

// WithCustomPrompt sets a custom prompt that will be prepended to the base prompt.
func (pb *PromptBuilder) WithCustomPrompt(prompt string) *PromptBuilder {
	pb.customPrompt = prompt
	return pb
}

// WithTaskPrompt sets a task prompt that provides task-specific guidance.
func (pb *PromptBuilder) WithTaskPrompt(prompt string) *PromptBuilder {
	pb.taskPrompt = prompt
	return pb
}

// WithSkills sets the skills middleware.
func (pb *PromptBuilder) WithSkills(skills *skills.Middleware) *PromptBuilder {
	pb.skills = skills
	return pb
}

// WithLanguage sets the language for the agent.
func (pb *PromptBuilder) WithLanguage(language string) *PromptBuilder {
	pb.language = language
	return pb
}

// WithCurrentTime sets the current time.
func (pb *PromptBuilder) WithCurrentTime(time string) *PromptBuilder {
	pb.currentTime = time
	return pb
}

// Build builds the system prompt.
func (pb *PromptBuilder) Build(ctx context.Context) (*schema.Message, error) {
	sb := strutil.NewBuilder()

	// Add custom prompt if provided
	if pb.customPrompt != "" {
		sb.WriteString(pb.customPrompt)
		sb.WriteString("\n\n")
	}

	// Add task prompt if provided
	if pb.taskPrompt != "" {
		sb.WriteString("## Task\n\n")
		sb.WriteString(pb.taskPrompt)
		sb.WriteString("\n\n")
	}

	// Add base prompt
	sb.WriteString(pb.basePrompt)

	// Add language instruction
	if pb.language != "" {
		sb.WriteString(fmt.Sprintf("\n\n**Language:** You must respond in %s.\n", pb.language))
	}

	// Add current time
	if pb.currentTime != "" {
		sb.WriteString(fmt.Sprintf("\n\n**Current Time:** %s\n", pb.currentTime))
	}

	// Inject skills with progressive disclosure
	if pb.skills != nil {
		skillsMetadata := pb.skills.InjectMetadataOnly("")
		if skillsMetadata != "" {
			sb.WriteString("\n\n## Skills System\n\n")
			sb.WriteString("You have access to a skills library that provides specialized capabilities and domain knowledge.\n\n")
			sb.WriteString("**Available Skills:**\n\n")
			sb.WriteString(skillsMetadata)
			sb.WriteString("\n**How to Use Skills (Progressive Disclosure):**\n\n")
			sb.WriteString("Skills follow a **progressive disclosure** pattern - you see their name and description above, but only read full instructions when needed:\n\n")
			sb.WriteString("1. **Recognize when a skill applies**: Check if the user's task matches a skill's description\n")
			sb.WriteString("2. **Read the skill's full instructions**: Use the `read_file` tool with the path shown in the skill list\n")
			sb.WriteString("3. **Follow the skill's instructions**: SKILL.md contains step-by-step workflows, best practices, and examples\n\n")
			sb.WriteString("**When to Use Skills:**\n")
			sb.WriteString("- User's request matches a skill's domain (e.g., \"research X\" -> web-research skill)\n")
			sb.WriteString("- You need specialized knowledge or structured workflows\n")
			sb.WriteString("- A skill provides proven patterns for complex tasks\n\n")
		}
	}

	return schema.SystemMessage(sb.String()), nil
}

// GetCurrentTime returns the current time formatted for display.
func GetCurrentTime(timezone float64) string {
	now := time.Now()
	if timezone != 0 {
		// Apply timezone offset
		offset := time.Duration(timezone * float64(time.Hour))
		now = now.Add(offset)
	}
	return now.Format("2006-01-02 15:04:05")
}
