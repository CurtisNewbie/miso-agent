package prebuilt

import (
	"github.com/cloudwego/eino/components/model"
	"github.com/curtisnewbie/miso-agent/agentloop"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
	"github.com/curtisnewbie/miso/util/retry"
	"github.com/curtisnewbie/miso/util/strutil"
)

// FactCheckOption configures a FactCheckAgent.
type FactCheckOption func(o *factCheckConfig)

type factCheckConfig struct {
	// SystemPrompt is an optional system prompt prepended before the fact-check task prompt.
	// If empty, no system message is sent.
	SystemPrompt string
	// Language specifies the response language for the agent.
	// If empty, defaults to "English".
	Language string
	// RetryCount is the number of additional attempts when the response is missing Score or Reason.
	// Defaults to 2 (up to 3 total attempts).
	RetryCount int
}

// WithFactCheckSystemPrompt sets an optional system prompt for the fact-check agent.
func WithFactCheckSystemPrompt(prompt string) FactCheckOption {
	return func(o *factCheckConfig) {
		o.SystemPrompt = prompt
	}
}

// WithFactCheckLanguage sets the response language for the fact-check agent.
func WithFactCheckLanguage(lang string) FactCheckOption {
	return func(o *factCheckConfig) {
		o.Language = lang
	}
}

// WithFactCheckRetry sets the number of additional retry attempts when the model response
// is missing a Score or Reason field. The default is 2 (up to 3 total attempts).
func WithFactCheckRetry(n int) FactCheckOption {
	return func(o *factCheckConfig) {
		o.RetryCount = n
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
	cfg := &factCheckConfig{RetryCount: 2}
	for _, o := range opts {
		o(cfg)
	}

	agent, err := agentloop.NewAgent(agentloop.AgentConfig{
		Name:         "FactCheckAgent",
		Model:        chatModel,
		MaxRunSteps:  5,
		Language:     cfg.Language,
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
// The model response is parsed for "Score:" and "Reason:" fields. If either field is missing
// the call is retried up to [factCheckConfig.RetryCount] additional times.
func (a *FactCheckAgent) Check(rail flow.Rail, input FactCheckInput) (FactCheckResult, error) {
	userPrompt := strutil.NamedSprintfv(factCheckTaskPrompt, factCheckPromptInput{
		Question:        input.Question,
		Context:         input.Context,
		Output:          input.Output,
		ReferenceAnswer: input.ReferenceAnswer,
	})

	return retry.GetOne(a.config.RetryCount, func() (FactCheckResult, error) {
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
		if result.Reason == "" {
			return FactCheckResult{}, errs.NewErrf("missing Reason field in FactCheckAgent response")
		}
		return result, nil
	})
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
const factCheckTaskPrompt = `You are a fact-checking expert. Evaluate the factual accuracy of an LLM response by comparing it against the provided knowledge context and user question.

Evaluation rules:
- If the context CONTAINS relevant information: check whether the response accurately reflects it. Fabricated or contradicted facts are hallucinations.
- If the context does NOT contain relevant information: check whether the response correctly abstains (e.g., says "I don't have information about this"). Correct abstention is NOT a hallucination — it is the expected behavior.
- If a reference answer is provided: treat it as the authoritative ground truth. Compare the LLM response against the reference answer first. If the response contradicts the reference answer, that is a factual error regardless of what the context says. If the response aligns with the reference answer, it is factually correct even if the context is incomplete.

Score scale:
1 = Major factual errors or hallucinations: invents facts not in the context, OR context contains the answer but the response falsely claims no information is available
2 = Significant inaccuracies affecting core meaning
3 = Partially correct but contains key mistakes
4 = Minor inaccuracies in non-critical details
5 = Fully factually correct with no errors, OR correctly abstains when the context contains no relevant information

--- EXAMPLES ---

Example 1:
<user_question>What is the refund policy for digital products?</user_question>
<knowledge_context>All digital products are non-refundable once downloaded. Physical products can be returned within 30 days.</knowledge_context>
<llm_response>Digital products cannot be refunded after download. Physical items are eligible for return within 30 days.</llm_response>
<reference_answer></reference_answer>
Score: 5
Reason: The response exactly matches the context. Both the no-refund rule for digital products and the 30-day return window for physical products are correctly stated.

Example 2:
<user_question>How many support tiers does the service offer?</user_question>
<knowledge_context>The service provides two support plans: Standard (email only, 48h response) and Premium (24/7 phone and email, 4h response).</knowledge_context>
<llm_response>The service offers three support tiers: Basic, Standard, and Premium, each with different response times.</llm_response>
<reference_answer></reference_answer>
Score: 1
Reason: The context describes exactly two support plans. The response invents a third "Basic" tier that does not exist in the context, which is a hallucination.

Example 3:
<user_question>When was the product launched?</user_question>
<knowledge_context>Product X was announced in Q3 2023 and became available to select beta customers in late 2023. General availability launched in February 2024.</knowledge_context>
<llm_response>Product X was launched in Q3 2023.</llm_response>
<reference_answer>February 2024</reference_answer>
Score: 2
Reason: The reference answer establishes "February 2024" as the correct launch date. The response states "Q3 2023", which was the announcement date per the context — not the launch. The response conflates announcement with general availability, contradicting the ground-truth reference answer.

Example 4:
<user_question>Can customer invitation codes be changed?</user_question>
<knowledge_context>This section covers payment information modification, permission settings, and member verification code updates.</knowledge_context>
<llm_response>I'm sorry, there is currently no information available about whether customer invitation codes can be changed. If you have other questions, feel free to ask.</llm_response>
<reference_answer></reference_answer>
Score: 5
Reason: The context contains no information about customer invitation codes. The response correctly abstains by acknowledging the knowledge gap rather than fabricating an answer. Correct abstention when context lacks relevant information is accurate behavior.

Example 5:
<user_question>What is the deadline to request a refund?</user_question>
<knowledge_context>Customers may request a refund within 7 days of purchase. After 7 days, no refunds will be issued.</knowledge_context>
<llm_response>I'm sorry, I don't have any information about the refund deadline. Please contact customer support for details.</llm_response>
<reference_answer></reference_answer>
Score: 1
Reason: The context explicitly states that refunds must be requested within 7 days of purchase. The response falsely claims no information is available, which directly contradicts the context and misleads the user.

--- END EXAMPLES ---

Before scoring, follow these steps:
1. Determine whether the context contains information relevant to the question.
2. If YES: Check if a reference answer is provided — if so, compare the response against it first as the authoritative ground truth. Then verify consistency with the context. Look for fabrications, contradictions, or key omissions.
3. If NO: Assess whether the response correctly abstains. If it honestly says "no information available", assign Score: 5.

Now evaluate:

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

Respond in exactly this format:
Score: <number from 1 to 5>
Reason: <concise justification referencing specific evidence from the context>`
