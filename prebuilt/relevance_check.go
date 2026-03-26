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

// RelevanceCheckOption configures a RelevanceCheckAgent.
type RelevanceCheckOption func(o *relevanceCheckConfig)

type relevanceCheckConfig struct {
	// SystemPrompt is an optional system prompt prepended before the relevance task prompt.
	// If empty, no system message is sent.
	SystemPrompt string
}

// WithRelevanceCheckSystemPrompt sets an optional system prompt for the relevance-check agent.
func WithRelevanceCheckSystemPrompt(prompt string) RelevanceCheckOption {
	return func(o *relevanceCheckConfig) {
		o.SystemPrompt = prompt
	}
}

// RelevanceCheckResult holds the numeric relevance score and textual reason returned by the agent.
type RelevanceCheckResult struct {
	// Score is the relevance score on a 1-5 scale:
	//   1 = Completely irrelevant
	//   2 = Mostly irrelevant
	//   3 = Somewhat relevant but with noticeable issues
	//   4 = Mostly relevant with minor issues
	//   5 = Fully correct and accurate
	Score int

	// Reason is a brief textual justification for the score.
	Reason string
}

// relevanceCheckPromptInput is the named template substitution struct for [relevanceCheckTaskPrompt].
// Field names must match the ${...} placeholders in the template.
type relevanceCheckPromptInput struct {
	Question        string
	Context         string
	Output          string
	ReferenceAnswer string
}

// RelevanceCheckAgent evaluates how relevant an LLM response is to the user question,
// knowledge context, and optional reference answer.
//
// The agent performs a single-shot call using [agentloop.Agent] with no tools.
//
// Use [NewRelevanceCheckAgent] to create an instance, then call [RelevanceCheckAgent.Check]
// to score a response.
type RelevanceCheckAgent struct {
	agent  *agentloop.Agent
	config *relevanceCheckConfig
}

// NewRelevanceCheckAgent creates a new RelevanceCheckAgent backed by the given chat model.
//
// Example:
//
//	agent, err := prebuilt.NewRelevanceCheckAgent(chatModel)
//	result, err := agent.Check(rail, prebuilt.RelevanceCheckInput{
//	    Question: "What is the capital of France?",
//	    Context:  "France is a country in Western Europe. Its capital is Paris.",
//	    Output:   "The capital of France is Paris.",
//	})
func NewRelevanceCheckAgent(chatModel model.ToolCallingChatModel, opts ...RelevanceCheckOption) (*RelevanceCheckAgent, error) {
	cfg := &relevanceCheckConfig{}
	for _, o := range opts {
		o(cfg)
	}

	agent, err := agentloop.NewAgent(agentloop.AgentConfig{
		Name:         "RelevanceCheckAgent",
		Model:        chatModel,
		MaxRunSteps:  2,
		SystemPrompt: cfg.SystemPrompt,
	})
	if err != nil {
		return nil, errs.Wrapf(err, "failed to create RelevanceCheckAgent")
	}

	return &RelevanceCheckAgent{agent: agent, config: cfg}, nil
}

// RelevanceCheckInput holds all the inputs required for a single relevance-check call.
type RelevanceCheckInput struct {
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

// Check evaluates the relevance of an LLM response and returns a [RelevanceCheckResult].
//
// The prompt template is substituted with the provided inputs using strutil.NamedSprintfv.
// The agent uses [agentloop.NewThinkTool] for structured reflection, then the model response
// is parsed for "Score:" and "Reason:" fields.
func (a *RelevanceCheckAgent) Check(rail flow.Rail, input RelevanceCheckInput) (RelevanceCheckResult, error) {
	userPrompt := strutil.NamedSprintfv(relevanceCheckTaskPrompt, relevanceCheckPromptInput{
		Question:        input.Question,
		Context:         input.Context,
		Output:          input.Output,
		ReferenceAnswer: input.ReferenceAnswer,
	})

	out, err := a.agent.Execute(rail, agentloop.AgentRequest{
		UserInput: userPrompt,
	})
	if err != nil {
		return RelevanceCheckResult{}, errs.Wrapf(err, "RelevanceCheckAgent execution failed")
	}

	score, reason, err := parseScoreReason(out.Response)
	if err != nil {
		return RelevanceCheckResult{}, errs.Wrapf(err, "failed to parse RelevanceCheckAgent response")
	}
	return RelevanceCheckResult{Score: score, Reason: reason}, nil
}

// CheckCtx is like [RelevanceCheckAgent.Check] but accepts a plain context.Context.
func (a *RelevanceCheckAgent) CheckCtx(ctx context.Context, input RelevanceCheckInput) (RelevanceCheckResult, error) {
	return a.Check(flow.NewRail(ctx), input)
}

// relevanceCheckTaskPrompt is the evaluation prompt template sent as the user message.
// Placeholders ${Question}, ${Context}, ${Output}, ${ReferenceAnswer} are substituted
// at call time via strutil.NamedSprintfv.
const relevanceCheckTaskPrompt = `You are an expert judge. Your task is to rate how relevant the following LLM response is based on the provided input. Rate on a scale from 1 to 5, where:

1 = Completely irrelevant
2 = Mostly irrelevant
3 = Somewhat relevant but with noticeable issues
4 = Mostly relevant with minor issues
5 = Fully correct and accurate

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
