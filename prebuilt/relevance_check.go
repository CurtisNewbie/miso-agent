package prebuilt

import (
	"github.com/cloudwego/eino/components/model"
	"github.com/curtisnewbie/miso-agent/agentloop"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
	"github.com/curtisnewbie/miso/util/retry"
	"github.com/curtisnewbie/miso/util/strutil"
)

// RelevanceCheckOption configures a RelevanceCheckAgent.
type RelevanceCheckOption func(o *relevanceCheckConfig)

type relevanceCheckConfig struct {
	// SystemPrompt is an optional system prompt prepended before the relevance task prompt.
	// If empty, no system message is sent.
	SystemPrompt string
	// Language specifies the response language for the agent.
	// If empty, defaults to "English".
	Language string
	// RetryCount is the number of additional attempts when the response is missing Score or Reason.
	// Defaults to 2 (up to 3 total attempts).
	RetryCount int
}

// WithRelevanceCheckSystemPrompt sets an optional system prompt for the relevance-check agent.
func WithRelevanceCheckSystemPrompt(prompt string) RelevanceCheckOption {
	return func(o *relevanceCheckConfig) {
		o.SystemPrompt = prompt
	}
}

// WithRelevanceCheckLanguage sets the response language for the relevance-check agent.
func WithRelevanceCheckLanguage(lang string) RelevanceCheckOption {
	return func(o *relevanceCheckConfig) {
		o.Language = lang
	}
}

// WithRelevanceCheckRetry sets the number of additional retry attempts when the model response
// is missing a Score or Reason field. The default is 2 (up to 3 total attempts).
func WithRelevanceCheckRetry(n int) RelevanceCheckOption {
	return func(o *relevanceCheckConfig) {
		o.RetryCount = n
	}
}

// RelevanceCheckResult holds the numeric relevance score and textual reason returned by the agent.
type RelevanceCheckResult struct {
	// Score is the relevance score on a 1-5 scale:
	//   1 = Completely off-topic — addresses a different subject than what was asked
	//   2 = Mostly irrelevant — on the right topic but misses the core ask or ignores available context
	//   3 = Somewhat relevant — on-topic but with noticeable gaps (e.g. correctly abstains when context has no info)
	//   4 = Mostly relevant with minor omissions or issues
	//   5 = Fully relevant — directly and completely answers the question
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
	cfg := &relevanceCheckConfig{RetryCount: 2}
	for _, o := range opts {
		o(cfg)
	}

	agent, err := agentloop.NewAgent(agentloop.AgentConfig{
		Name:         "RelevanceCheckAgent",
		Model:        chatModel,
		MaxRunSteps:  5,
		Language:     cfg.Language,
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
// The model response is parsed for "Score:" and "Reason:" fields. If either field is missing
// the call is retried up to [relevanceCheckConfig.RetryCount] additional times.
func (a *RelevanceCheckAgent) Check(rail flow.Rail, input RelevanceCheckInput) (RelevanceCheckResult, error) {
	userPrompt := strutil.NamedSprintfv(relevanceCheckTaskPrompt, relevanceCheckPromptInput{
		Question:        input.Question,
		Context:         input.Context,
		Output:          input.Output,
		ReferenceAnswer: input.ReferenceAnswer,
	})

	return retry.GetOne(a.config.RetryCount, func() (RelevanceCheckResult, error) {
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
		if reason == "" {
			return RelevanceCheckResult{}, errs.NewErrf("missing Reason field in RelevanceCheckAgent response")
		}
		return RelevanceCheckResult{Score: score, Reason: reason}, nil
	})
}

// relevanceCheckTaskPrompt is the evaluation prompt template sent as the user message.
// Placeholders ${Question}, ${Context}, ${Output}, ${ReferenceAnswer} are substituted
// at call time via strutil.NamedSprintfv.
const relevanceCheckTaskPrompt = `You are an expert judge. Rate how directly and completely the LLM response addresses the user question.

CRITICAL DISTINCTION — relevance vs. factual accuracy:
- RELEVANCE (this task): Does the response answer the question that was asked?
- FACTUAL ACCURACY (separate task): Is the response truthful and grounded in the context?

These are independent dimensions. A hallucinated response can score 5 on relevance if it directly answers the question. A perfectly truthful abstention can score 2 on relevance if the context had the answer. Do NOT penalize relevance because a response contradicts or ignores the knowledge context — that is factual accuracy, not relevance.

When a reference answer is provided, use it to assess completeness. It defines the expected scope and key points a full answer should cover. If the response is on-topic but omits aspects that the reference answer covers, reduce the score accordingly.

Score scale:
1 = Completely off-topic — response addresses a different subject than what was asked
2 = Mostly irrelevant — on the right topic but misses the core ask, or context contains the answer but the response ignores it
3 = Somewhat relevant — on-topic and acknowledges the question, but with noticeable gaps (e.g. correctly abstains when context lacks relevant information, or covers only part of what the reference answer requires)
4 = Mostly relevant — addresses the question directly with minor omissions or issues
5 = Fully relevant — directly and completely answers the question; if a reference answer is provided, all key points are covered

Important:
- A response that says "no information available" is NOT automatically score 1. Reserve score 1 only for responses that address a completely different subject.
- A factually WRONG or hallucinated answer can still score 4-5 if it directly addresses the user question. Factual errors are penalized by the fact-checking dimension, not here.
- Use score 2 when context has the answer but the response ignores it or falsely claims no info is available.
- Use score 3 when context has no relevant information and the response correctly abstains.
- When reference_answer is provided: if the response covers only some of the expected key points, downgrade to 3-4 depending on how much is missing.

--- EXAMPLES ---

Example 1:
<user_question>How do I reset my password?</user_question>
<knowledge_context>To reset your password, go to the login page and click "Forgot Password". Enter your registered email and follow the link sent to your inbox.</knowledge_context>
<llm_response>Click "Forgot Password" on the login page, enter your email address, then follow the reset link in your inbox.</llm_response>
<reference_answer></reference_answer>
Score: 5
Reason: The response directly answers the question and accurately follows all steps described in the context without omission or addition.

Example 2:
<user_question>How do I reset my password?</user_question>
<knowledge_context>To reset your password, go to the login page and click "Forgot Password".</knowledge_context>
<llm_response>You can reach our support team by emailing help@example.com for any account issues.</llm_response>
<reference_answer></reference_answer>
Score: 1
Reason: The response redirects to customer support instead of answering the password reset question. It is completely off-topic relative to both the question and the context.

Example 3:
<user_question>What are the shipping options and their delivery times?</user_question>
<knowledge_context>We offer standard shipping (5-7 business days) and express shipping (1-2 business days). Orders over $50 qualify for free standard shipping.</knowledge_context>
<llm_response>We have fast and slow shipping options available.</llm_response>
<reference_answer></reference_answer>
Score: 3
Reason: The response acknowledges that multiple shipping speeds exist but omits the specific delivery timeframes and the free shipping threshold, which were directly asked about.

Example 4:
<user_question>Can customer invitation codes be changed?</user_question>
<knowledge_context>This section covers payment information modification, permission settings, and member verification code updates.</knowledge_context>
<llm_response>I'm sorry, there is currently no information available about whether customer invitation codes can be changed. If you have other questions, feel free to ask.</llm_response>
<reference_answer></reference_answer>
Score: 3
Reason: The response is on-topic — it directly acknowledges the question and honestly states no information is available. However, the question remains unanswered, which is a significant gap. This scores 3, not 1, because the response is not off-topic; it appropriately reflects the limits of available knowledge.

Example 5:
<user_question>What is the deadline to request a refund?</user_question>
<knowledge_context>Customers may request a refund within 7 days of purchase. After 7 days, no refunds will be issued.</knowledge_context>
<llm_response>I'm sorry, I don't have any information about the refund deadline. Please contact customer support for details.</llm_response>
<reference_answer></reference_answer>
Score: 2
Reason: The response is on the right topic but falsely claims no information is available, ignoring the directly relevant context that explicitly states the 7-day deadline.

Example 6:
<user_question>How do I cancel my subscription?</user_question>
<knowledge_context>Our platform offers monthly and annual subscription plans. Annual plans come with a 20% discount compared to monthly billing.</knowledge_context>
<llm_response>To cancel your subscription, go to Account Settings, click Subscription, then select Cancel Plan and choose a cancellation reason.</llm_response>
<reference_answer></reference_answer>
Score: 5
Reason: The response directly and completely answers the cancellation question. The specific steps are not found in the context — this is a factual accuracy concern, not a relevance concern. The response is fully relevant to the question regardless of whether the steps are correct.

Example 7:
<user_question>What should I prepare before my onboarding meeting?</user_question>
<knowledge_context>The onboarding meeting covers account setup, API integration, compliance documentation, and pricing confirmation.</knowledge_context>
<llm_response>You should prepare your company registration documents and a list of questions you have about the platform.</llm_response>
<reference_answer>Prepare the following: company registration documents, signed compliance agreements, your technical team contact for API integration, and confirmation of your selected pricing plan.</reference_answer>
Score: 3
Reason: The response is on-topic and mentions one valid preparation item (company registration documents), but the reference answer reveals three additional required items (signed compliance agreements, technical contact for API integration, pricing confirmation) that were not covered. The response partially addresses the question but has significant gaps relative to the expected complete answer.

--- END EXAMPLES ---

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
Reason: <concise justification explaining why the response is or is not relevant>`
