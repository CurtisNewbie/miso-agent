package prebuilt

// @author yongj.zhuang

import (
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/curtisnewbie/miso-agent/agentloop"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
	"github.com/curtisnewbie/miso/util/llm"
	"github.com/curtisnewbie/miso/util/strutil"
)

// CategoryAnalyzeOption configures a [CategoryAnalyzeAgent].
type CategoryAnalyzeOption func(o *categoryAnalyzeConfig)

type categoryAnalyzeConfig struct {
	// Language specifies the response language. If empty, defaults to "English".
	Language string
}

// WithCategoryAnalyzeLanguage sets the response language for the category analysis agent.
func WithCategoryAnalyzeLanguage(lang string) CategoryAnalyzeOption {
	return func(o *categoryAnalyzeConfig) {
		o.Language = lang
	}
}

// CategoryAnalyzeInput holds the inputs for a single category analysis call.
type CategoryAnalyzeInput struct {
	// TaskExplanation is additional context about the task/domain.
	TaskExplanation string

	// Categories is the list of existing candidate category names.
	Categories []string

	// Subjects is the batch of subjects to analyze.
	Subjects []string
}

// CategoryAnalysisResult is the aggregated JSON response returned by the category analysis agent.
type CategoryAnalysisResult struct {
	// Reason is the step-by-step reasoning across all subjects.
	Reason string `json:"reason"`

	// CategoryMatched contains existing categories selected from the provided list.
	CategoryMatched []string `json:"categoryMatched"`

	// NewCategory contains new category names invented by the agent for subjects that did not fit any existing category.
	NewCategory []string `json:"newCategory"`
}

// categoryAnalyzePromptInput is the named template substitution struct for [categoryAnalyzeUserPrompt].
type categoryAnalyzePromptInput struct {
	TaskExplanation string
	Categories      string
	Subjects        string
}

// CategoryAnalyzeAgent identifies which categories a description belongs to.
// It may select from existing categories or create new ones.
// Uses a single-shot [agentloop.Agent] call and parses the JSON response.
//
// Use [NewCategoryAnalyzeAgent] to create an instance, then call [CategoryAnalyzeAgent.Analyze].
type CategoryAnalyzeAgent struct {
	agent *agentloop.Agent
}

// NewCategoryAnalyzeAgent creates a new CategoryAnalyzeAgent backed by the given chat model.
//
// Example:
//
//	agent, err := prebuilt.NewCategoryAnalyzeAgent(chatModel)
//	result, err := agent.Analyze(rail, prebuilt.CategoryAnalyzeInput{
//	    TaskExplanation: "Categorize trade activities for customs declaration.",
//	    Categories:      []string{"Goods trade-Agricultural", "Service trade-Logistics"},
//	    Descriptions:    []string{"区块链咨询服务"},
//	})
func NewCategoryAnalyzeAgent(chatModel model.ToolCallingChatModel, opts ...CategoryAnalyzeOption) (*CategoryAnalyzeAgent, error) {
	cfg := &categoryAnalyzeConfig{}
	for _, o := range opts {
		o(cfg)
	}

	agent, err := agentloop.NewAgent(agentloop.AgentConfig{
		Name:         "CategoryAnalyzeAgent",
		Model:        chatModel,
		MaxRunSteps:  30,
		Language:     cfg.Language,
		SystemPrompt: categoryAnalyzeSystemPrompt,
	})
	if err != nil {
		return nil, errs.Wrapf(err, "failed to create CategoryAnalyzeAgent")
	}

	return &CategoryAnalyzeAgent{agent: agent}, nil
}

// Analyze runs the category analysis agent and returns the matched or newly created categories with reasoning.
func (a *CategoryAnalyzeAgent) Analyze(rail flow.Rail, input CategoryAnalyzeInput) (CategoryAnalysisResult, error) {
	var numberedDescs []string
	for i, desc := range input.Subjects {
		numberedDescs = append(numberedDescs, fmt.Sprintf("%d. %s", i+1, desc))
	}
	descStr := strings.Join(numberedDescs, "\n")

	userPrompt := strutil.NamedSprintfv(categoryAnalyzeUserPrompt, categoryAnalyzePromptInput{
		TaskExplanation: input.TaskExplanation,
		Categories:      strings.Join(input.Categories, "\n"),
		Subjects:        descStr,
	})

	out, err := a.agent.Execute(rail, agentloop.AgentRequest{UserInput: userPrompt})
	if err != nil {
		return CategoryAnalysisResult{}, errs.Wrapf(err, "CategoryAnalyzeAgent execution failed")
	}
	_, content := llm.ParseThink(out.Response)
	result, err := llm.ParseLLMJsonAs[CategoryAnalysisResult](content)
	if err != nil {
		return CategoryAnalysisResult{}, errs.Wrapf(err, "failed to parse CategoryAnalyzeAgent response")
	}
	return result, nil
}

// categoryAnalyzeSystemPrompt establishes the category analysis engine role, constraints, output schema,
// and few-shot examples. Kept in the system turn so it is cached across calls.
const categoryAnalyzeSystemPrompt = `You are a category analysis engine. Given a list of subjects and a list of existing categories, identify which categories each subject belongs to.

Rules:
- categoryMatched: select from the provided categories list when they are a good match. Include only existing category names here.
- newCategory: invent a new category name only when none of the existing categories are suitable for a subject.
- A subject may match zero, one, or multiple categories.
- Both categoryMatched and newCategory must be deduplicated — no duplicate entries.
- Output strictly valid JSON — no markdown, no prose, no trailing commas. Escape characters if necessary.

Output schema:
{"reason": "<step-by-step reasoning across all subjects>", "categoryMatched": ["<existing category>", ...], "newCategory": ["<new category>", ...]}

Examples:

Subjects:
1. 有机苹果进口
2. 区块链咨询服务
3. 进口葡萄酒

Categories:
Goods trade-Agricultural Products
Goods trade-Alcohol
Service trade-Logistics

Output:
{"reason": "Subject 1 maps to agricultural goods trade. Subject 2 does not fit any existing category — new category created. Subject 3 maps to alcohol goods trade.", "categoryMatched": ["Goods trade-Agricultural Products", "Goods trade-Alcohol"], "newCategory": ["Service trade-Technology Consulting"]}

---

Subjects:
1. 国际海运报关服务
2. 加密货币投资顾问

Categories:
Service trade-Logistics
Service trade-Financial

Output:
{"reason": "Subject 1 maps to logistics service trade. Subject 2 maps to financial service trade.", "categoryMatched": ["Service trade-Logistics", "Service trade-Financial"], "newCategory": []}`

// categoryAnalyzeUserPrompt is the per-call template carrying the runtime inputs.
// Placeholders ${TaskExplanation}, ${Categories}, and ${Subjects} are substituted via strutil.NamedSprintfv.
const categoryAnalyzeUserPrompt = `<task_context>
${TaskExplanation}
</task_context>

<categories>
${Categories}
</categories>

<subjects>
${Subjects}
</subjects>

Respond with JSON only:`
