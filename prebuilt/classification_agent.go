package prebuilt

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

// ClassificationOption configures a [ClassificationAgent].
type ClassificationOption func(o *classificationConfig)

type classificationConfig struct {
	// SystemPrompt is an optional system prompt prepended to the default classification prompt.
	SystemPrompt string
	// Language specifies the response language. If empty, defaults to "English".
	Language string
}

// WithClassificationSystemPrompt sets an optional system prompt for the classification agent.
func WithClassificationSystemPrompt(prompt string) ClassificationOption {
	return func(o *classificationConfig) {
		o.SystemPrompt = prompt
	}
}

// WithClassificationLanguage sets the response language for the classification agent.
func WithClassificationLanguage(lang string) ClassificationOption {
	return func(o *classificationConfig) {
		o.Language = lang
	}
}

// ClassificationInput holds the inputs for a batch classification call.
type ClassificationInput struct {
	// Categories is the list of candidate category names to match against.
	Categories []string

	// Subjects is the batch of subjects to classify.
	Subjects []string

	// TaskExplanation is an optional context/hint prepended to the user prompt.
	TaskExplanation string
}

// ClassificationResult holds the classification result for a single subject.
type ClassificationResult struct {
	// Subject is the subject that was classified.
	Subject string `json:"subject"`

	// Reason is the step-by-step reasoning behind the classification result.
	Reason string `json:"reason"`

	// Categories contains the matched category names. Contains ["Unknown"] if none match.
	Categories []string `json:"categories"`
}

// ClassificationOutput is the JSON response returned by the classification agent.
type ClassificationOutput struct {
	// Results contains one classification result per subject, in input order.
	Results []ClassificationResult `json:"results"`
}

// classificationPromptInput is the named template substitution struct for [classificationUserPrompt].
type classificationPromptInput struct {
	Categories string
	Subjects   string
}

// ClassificationAgent classifies a batch of subjects against a given list of categories.
// It uses a single-shot [agentloop.Agent] call and parses the JSON response.
//
// Use [NewClassificationAgent] to create an instance, then call [ClassificationAgent.Classify].
type ClassificationAgent struct {
	agent *agentloop.Agent
}

// NewClassificationAgent creates a new ClassificationAgent backed by the given chat model.
//
// Example:
//
//	agent, err := prebuilt.NewClassificationAgent(chatModel)
//	result, err := agent.Classify(rail, prebuilt.ClassificationInput{
//	    Categories: []string{"Goods trade-Alcohol", "Service trade-Logistics"},
//	    Subjects:   []string{"物流", "进口葡萄酒"},
//	})
func NewClassificationAgent(chatModel model.ToolCallingChatModel, opts ...ClassificationOption) (*ClassificationAgent, error) {
	cfg := &classificationConfig{}
	for _, o := range opts {
		o(cfg)
	}

	systemPrompt := classificationSystemPrompt
	if cfg.SystemPrompt != "" {
		systemPrompt = cfg.SystemPrompt
	}

	agent, err := agentloop.NewAgent(agentloop.AgentConfig{
		Name:         "ClassificationAgent",
		Model:        chatModel,
		MaxRunSteps:  30,
		Language:     cfg.Language,
		SystemPrompt: systemPrompt,
	})
	if err != nil {
		return nil, errs.Wrapf(err, "failed to create ClassificationAgent")
	}

	return &ClassificationAgent{agent: agent}, nil
}

// Classify runs the classification agent over all subjects and returns per-subject results.
func (a *ClassificationAgent) Classify(rail flow.Rail, input ClassificationInput) (ClassificationOutput, error) {
	numbered := make([]string, len(input.Subjects))
	for i, s := range input.Subjects {
		numbered[i] = fmt.Sprintf("%d. %s", i+1, s)
	}

	userPrompt := strutil.NamedSprintfv(classificationUserPrompt, classificationPromptInput{
		Categories: strings.Join(input.Categories, "\n"),
		Subjects:   strings.Join(numbered, "\n"),
	})
	if input.TaskExplanation != "" {
		userPrompt = input.TaskExplanation + "\n\n" + userPrompt
	}

	out, err := a.agent.Execute(rail, agentloop.AgentRequest{UserInput: userPrompt})
	if err != nil {
		return ClassificationOutput{}, errs.Wrapf(err, "ClassificationAgent execution failed")
	}
	_, content := llm.ParseThink(out.Response)
	result, err := llm.ParseLLMJsonAs[ClassificationOutput](content)
	if err != nil {
		return ClassificationOutput{}, errs.Wrapf(err, "failed to parse ClassificationAgent response")
	}
	for i, r := range result.Results {
		for _, c := range r.Categories {
			if strings.EqualFold(c, "Unknown") {
				result.Results[i].Categories = []string{"Unknown"}
				break
			}
		}
	}
	return result, nil
}

// classificationSystemPrompt establishes the classifier role, constraints, output schema,
// and few-shot examples. Kept in the system turn so it is cached across calls.
const classificationSystemPrompt = `You are a classification engine. Given a list of subjects and a list of categories, classify each subject against the categories.

Rules:
- Select ONLY from the provided categories. Do not invent or paraphrase category names.
- A subject may match one or multiple categories.
- If no category fits, set categories to ["Unknown"].
- Output strictly valid JSON — no markdown, no prose, no trailing commas.
- Produce one result object per subject, in the same order as the input.

Output schema:
{"results": [{"subject": "<subject text>", "reason": "<concise chain-of-thought>", "categories": ["<matched category>", ...]}, ...]}

Examples:

Subjects:
1. 物流
2. organic apples import
3. birthday party planning

Categories:
Goods trade-Agricultural Products
Goods trade-Alcohol
Service trade-Logistics

Output:
{"results": [{"subject": "物流", "reason": "物流 directly maps to Service trade-Logistics.", "categories": ["Service trade-Logistics"]}, {"subject": "organic apples import", "reason": "Organic apples are agricultural goods; import is a goods-trade activity.", "categories": ["Goods trade-Agricultural Products"]}, {"subject": "birthday party planning", "reason": "Birthday party planning does not correspond to any of the provided trade categories.", "categories": ["Unknown"]}]}`

// classificationUserPrompt is the per-call template carrying the runtime inputs.
// Placeholders ${Categories} and ${Subjects} are substituted via strutil.NamedSprintfv.
const classificationUserPrompt = `Categories:
${Categories}

Subjects:
${Subjects}`
