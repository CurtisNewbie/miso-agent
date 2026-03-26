package prebuilt

// @author yongj.zhuang

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/curtisnewbie/miso-agent/agentloop"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
	"github.com/curtisnewbie/miso/util/strutil"
)

// FactCheckOption configures a FactCheckAgent.
type FactCheckOption func(o *factCheckConfig)

type factCheckConfig struct {
	// SystemPrompt is an optional system prompt prepended before the fact-check task prompt.
	// If empty, no system message is sent.
	SystemPrompt string
}

// WithFactCheckSystemPrompt sets an optional system prompt for the fact-check agent.
func WithFactCheckSystemPrompt(prompt string) FactCheckOption {
	return func(o *factCheckConfig) {
		o.SystemPrompt = prompt
	}
}

// FactCheckResult holds the numeric score and textual reason returned by the agent.
type FactCheckResult struct {
	// Score is the factual accuracy score on a 1-5 scale:
	//   1 = Major factual errors or hallucinations
	//   2 = Significant inaccuracies affecting core meaning
	//   3 = Partially correct but with key mistakes
	//   4 = Minor inaccuracies in non-critical details
	//   5 = Fully factually correct with no errors
	Score int

	// Reason is a brief textual justification for the score.
	Reason string
}

// factCheckPromptInput is the named template substitution struct for [factCheckTaskPrompt].
// Field names must match the ${...} placeholders in the template.
type factCheckPromptInput struct {
	Question        string
	Context         string
	Output          string
	ReferenceAnswer string
}

// FactCheckAgent evaluates the factual accuracy of an LLM response against
// a knowledge context, a user question, and an optional reference answer.
//
// The agent performs a single-shot call using [agentloop.Agent] with no tools.
//
// Use [NewFactCheckAgent] to create an instance, then call [FactCheckAgent.Check]
// to score a response.
type FactCheckAgent struct {
	agent  *agentloop.Agent
	config *factCheckConfig
}

// NewFactCheckAgent creates a new FactCheckAgent backed by the given chat model.
//
// Example:
//
//	agent, err := prebuilt.NewFactCheckAgent(chatModel)
//	result, err := agent.Check(rail, prebuilt.FactCheckInput{
//	    Question:        "What is the capital of France?",
//	    Context:         "France is a country in Western Europe. Its capital is Paris.",
//	    Output:          "The capital of France is Paris.",
//	    ReferenceAnswer: "Paris",
//	})
func NewFactCheckAgent(chatModel model.ToolCallingChatModel, opts ...FactCheckOption) (*FactCheckAgent, error) {
	cfg := &factCheckConfig{}
	for _, o := range opts {
		o(cfg)
	}

	agent, err := agentloop.NewAgent(agentloop.AgentConfig{
		Name:         "FactCheckAgent",
		Model:        chatModel,
		MaxRunSteps:  2,
		SystemPrompt: cfg.SystemPrompt,
	})
	if err != nil {
		return nil, errs.Wrapf(err, "failed to create FactCheckAgent")
	}

	return &FactCheckAgent{agent: agent, config: cfg}, nil
}

// FactCheckInput holds all the inputs required for a single fact-checking call.
type FactCheckInput struct {
	// Question is the user question that prompted the LLM response.
	Question string

	// Context is the knowledge context retrieved for the question.
	Context string

	// Output is the LLM response to be evaluated.
	Output string

	// ReferenceAnswer is the ground-truth answer used for comparison.
	// May be left empty if no reference is available; the agent will rely on Context alone.
	ReferenceAnswer string
}

// Check evaluates the factual accuracy of an LLM response and returns a [FactCheckResult].
//
// The prompt template is substituted with the provided inputs using strutil.NamedSprintfv.
// The agent uses [agentloop.NewThinkTool] for structured reflection, then the model response
// is parsed for "Score:" and "Reason:" fields.
func (a *FactCheckAgent) Check(rail flow.Rail, input FactCheckInput) (FactCheckResult, error) {
	userPrompt := strutil.NamedSprintfv(factCheckTaskPrompt, factCheckPromptInput{
		Question:        input.Question,
		Context:         input.Context,
		Output:          input.Output,
		ReferenceAnswer: input.ReferenceAnswer,
	})

	out, err := a.agent.Execute(rail, agentloop.AgentRequest{
		UserInput: userPrompt,
	})
	if err != nil {
		return FactCheckResult{}, errs.Wrapf(err, "FactCheckAgent execution failed")
	}

	result, err := parseFactCheckResponse(out.Response)
	if err != nil {
		return FactCheckResult{}, errs.Wrapf(err, "failed to parse FactCheckAgent response")
	}
	return result, nil
}

// CheckCtx is like [FactCheckAgent.Check] but accepts a plain context.Context.
func (a *FactCheckAgent) CheckCtx(ctx context.Context, input FactCheckInput) (FactCheckResult, error) {
	return a.Check(flow.NewRail(ctx), input)
}

// parseFactCheckResponse parses the model response into a [FactCheckResult].
func parseFactCheckResponse(content string) (FactCheckResult, error) {
	score, reason, err := parseScoreReason(content)
	if err != nil {
		return FactCheckResult{}, err
	}
	return FactCheckResult{Score: score, Reason: reason}, nil
}

// factCheckTaskPrompt is the evaluation prompt template sent as the user message.
// Placeholders ${Question}, ${Context}, ${Output}, ${ReferenceAnswer} are substituted
// at call time via strutil.NamedSprintfv.
const factCheckTaskPrompt = `You are a fact-checking expert. Strictly evaluate the accuracy of the LLM response against the provided knowledge and question.
If the context is irrelevant or the context does not support the response, the response is considered hallucinated.

1 = Major factual errors or hallucinations
2 = Significant inaccuracies affecting core meaning
3 = Partially correct but with key mistakes
4 = Minor inaccuracies in non-critical details
5 = Fully factually correct with no errors

<user_question>
${Question}
</user_question>

<knowledge_context>
${Context}
</knowledge_context>

<llm_response>
${Output}
</llm_response>

<reference_answer>
${ReferenceAnswer}
</reference_answer>

Use the think_tool to reflect on the evidence before concluding. Then return the numeric score (1-5) and reason in following format:
Score:
Reason: `
