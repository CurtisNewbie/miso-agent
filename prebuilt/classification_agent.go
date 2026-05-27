package prebuilt

// @author yongj.zhuang

import (
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

// ClassificationInput holds the inputs for a single classification call.
type ClassificationInput struct {
	// Categories is the list of candidate category names to match against.
	Categories []string

	// Description is the subject description to classify.
	Description string
}

// ClassificationOutput is the JSON response returned by the classification agent.
type ClassificationOutput struct {
	// Reason is the step-by-step reasoning behind the classification result.
	Reason string `json:"reason"`

	// Categories contains the matched category names. Empty if none match.
	Categories []string `json:"categories"`
}

// classificationPromptInput is the named template substitution struct for [classificationTaskPrompt].
type classificationPromptInput struct {
	Categories  string
	Description string
}

// ClassificationAgent classifies a subject description against a given list of categories.
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
//	    Categories:  []string{"Goods trade-Alcohol", "Service trade-Logistics"},
//	    Description: "物流",
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

// Classify runs the classification agent and returns the matched categories with reasoning.
func (a *ClassificationAgent) Classify(rail flow.Rail, input ClassificationInput) (ClassificationOutput, error) {
	userPrompt := strutil.NamedSprintfv(classificationUserPrompt, classificationPromptInput{
		Categories:  strings.Join(input.Categories, "\n"),
		Description: input.Description,
	})

	out, err := a.agent.Execute(rail, agentloop.AgentRequest{UserInput: userPrompt})
	if err != nil {
		return ClassificationOutput{}, errs.Wrapf(err, "ClassificationAgent execution failed")
	}
	_, content := llm.ParseThink(out.Response)
	result, err := llm.ParseLLMJsonAs[ClassificationOutput](content)
	if err != nil {
		return ClassificationOutput{}, errs.Wrapf(err, "failed to parse ClassificationAgent response")
	}
	return result, nil
}

// classificationSystemPrompt establishes the classifier role, constraints, output schema,
// and few-shot examples. Kept in the system turn so it is cached across calls.
const classificationSystemPrompt = `You are a classification engine. Given a subject description and a list of categories, identify every category the subject belongs to.

Rules:
- Select ONLY from the provided categories. Do not invent or paraphrase category names.
- A subject may match zero, one, or multiple categories.
- Output strictly valid JSON — no markdown, no prose, no trailing commas.

Output schema:
{"reason": "<concise chain-of-thought>", "categories": ["<matched category>", ...]}

Examples:

Subject: 物流
Categories:
Goods trade-Agricultural Products
Goods trade-Alcohol

Output:
{"reason": "物流 (logistics) does not match any goods-trade category listed.", "categories": []}

---

Subject: 物流
Categories:
Goods trade-Agricultural Products
Goods trade-Alcohol
Service trade-Logistics

Output:
{"reason": "物流 directly maps to Service trade-Logistics.", "categories": ["Service trade-Logistics"]}

---

Subject: organic apples import
Categories:
Goods trade-Agricultural Products
Goods trade-Alcohol
Service trade-Logistics

Output:
{"reason": "Organic apples are agricultural goods; import is a goods-trade activity.", "categories": ["Goods trade-Agricultural Products"]}`

// classificationUserPrompt is the per-call template carrying the runtime inputs.
// Placeholders ${Categories} and ${Description} are substituted via strutil.NamedSprintfv.
const classificationUserPrompt = `Categories:
${Categories}

Subject: ${Description}`
