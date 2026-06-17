package prebuilt

import (
	"github.com/cloudwego/eino/components/model"
	"github.com/curtisnewbie/miso-agent/agentloop"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
	"github.com/curtisnewbie/miso/util/retry"
	"github.com/curtisnewbie/miso/util/strutil"
)

// AccuracyCheckOption configures an AccuracyCheckAgent.
type AccuracyCheckOption func(o *accuracyCheckConfig)

type accuracyCheckConfig struct {
	// SystemPrompt is an optional system prompt prepended before the accuracy task prompt.
	// If empty, no system message is sent.
	SystemPrompt string
	// Language specifies the response language for the agent.
	// If empty, defaults to "English".
	Language string
	// RetryCount is the number of additional attempts when the response is missing Score or Reason.
	// Defaults to 2 (up to 3 total attempts).
	RetryCount int
}

// WithAccuracyCheckSystemPrompt sets an optional system prompt for the accuracy-check agent.
func WithAccuracyCheckSystemPrompt(prompt string) AccuracyCheckOption {
	return func(o *accuracyCheckConfig) {
		o.SystemPrompt = prompt
	}
}

// WithAccuracyCheckLanguage sets the response language for the accuracy-check agent.
func WithAccuracyCheckLanguage(lang string) AccuracyCheckOption {
	return func(o *accuracyCheckConfig) {
		o.Language = lang
	}
}

// WithAccuracyCheckRetry sets the number of additional retry attempts when the model response
// is missing a Score or Reason field. The default is 2 (up to 3 total attempts).
func WithAccuracyCheckRetry(n int) AccuracyCheckOption {
	return func(o *accuracyCheckConfig) {
		o.RetryCount = n
	}
}

// AccuracyCheckResult holds the numeric score and textual reason returned by the agent.
type AccuracyCheckResult struct {
	// Score is the answer accuracy score on a 1-5 scale measured against the reference answer:
	//   1 = Completely wrong or contradicts the reference answer
	//   2 = Significant divergence from the reference answer affecting core meaning
	//   3 = Partially matches the reference answer but missing key points
	//   4 = Mostly matches the reference answer with minor omissions
	//   5 = Fully matches the reference answer in all key points
	Score int

	// Reason is a brief textual justification for the score.
	Reason string
}

// accuracyCheckPromptInput is the named template substitution struct for [accuracyCheckTaskPrompt].
// Field names must match the ${...} placeholders in the template.
type accuracyCheckPromptInput struct {
	Question        string
	Output          string
	ReferenceAnswer string
}

// AccuracyCheckAgent evaluates how accurately an LLM response matches a ground-truth reference
// answer, independently of the retrieved knowledge context.
//
// This is distinct from FactCheckAgent (which checks context faithfulness / hallucination) and
// RelevanceCheckAgent (which checks whether the question was answered). AccuracyCheckAgent
// answers: "Given we know the correct answer, did the LLM get it right?"
//
// The agent performs a single-shot call using [agentloop.Agent] with no tools.
//
// Use [NewAccuracyCheckAgent] to create an instance, then call [AccuracyCheckAgent.Check]
// to score a response.
type AccuracyCheckAgent struct {
	agent  *agentloop.Agent
	config *accuracyCheckConfig
}

// NewAccuracyCheckAgent creates a new AccuracyCheckAgent backed by the given chat model.
//
// Example:
//
//	agent, err := prebuilt.NewAccuracyCheckAgent(chatModel)
//	result, err := agent.Check(rail, prebuilt.AccuracyCheckInput{
//	    Question:        "What is the capital of France?",
//	    Output:          "The capital of France is Paris.",
//	    ReferenceAnswer: "Paris",
//	})
func NewAccuracyCheckAgent(chatModel model.ToolCallingChatModel, opts ...AccuracyCheckOption) (*AccuracyCheckAgent, error) {
	cfg := &accuracyCheckConfig{RetryCount: 2}
	for _, o := range opts {
		o(cfg)
	}

	systemPrompt := accuracyCheckSystemPrompt
	if cfg.SystemPrompt != "" {
		systemPrompt = cfg.SystemPrompt + "\n\n" + accuracyCheckSystemPrompt
	}

	agent, err := agentloop.NewAgent(agentloop.AgentConfig{
		Name:         "AccuracyCheckAgent",
		Model:        chatModel,
		MaxRunSteps:  5,
		Language:     cfg.Language,
		SystemPrompt: systemPrompt,
	})
	if err != nil {
		return nil, errs.Wrapf(err, "failed to create AccuracyCheckAgent")
	}

	return &AccuracyCheckAgent{agent: agent, config: cfg}, nil
}

// AccuracyCheckInput holds all the inputs required for a single accuracy-check call.
type AccuracyCheckInput struct {
	// Question is the user question that prompted the LLM response.
	Question string

	// Output is the LLM response to be evaluated.
	Output string

	// ReferenceAnswer is the ground-truth answer to compare against.
	// This field is required; if empty the agent will return a low score.
	ReferenceAnswer string
}

// Check evaluates how accurately the LLM response matches the reference answer and returns
// an [AccuracyCheckResult].
//
// The prompt template is substituted with the provided inputs using strutil.NamedSprintfv.
// The model response is parsed for "Score:" and "Reason:" fields. If either field is missing
// the call is retried up to [accuracyCheckConfig.RetryCount] additional times.
func (a *AccuracyCheckAgent) Check(rail flow.Rail, input AccuracyCheckInput) (AccuracyCheckResult, error) {
	userPrompt := strutil.NamedSprintfv(accuracyCheckUserPrompt, accuracyCheckPromptInput{
		Question:        input.Question,
		Output:          input.Output,
		ReferenceAnswer: input.ReferenceAnswer,
	})

	return retry.GetOne(a.config.RetryCount, func() (AccuracyCheckResult, error) {
		out, err := a.agent.Execute(rail, agentloop.AgentRequest{
			UserInput: userPrompt,
		})
		if err != nil {
			return AccuracyCheckResult{}, errs.Wrapf(err, "AccuracyCheckAgent execution failed")
		}
		score, reason, err := parseScoreReason(out.Response)
		if err != nil {
			return AccuracyCheckResult{}, errs.Wrapf(err, "failed to parse AccuracyCheckAgent response")
		}
		if reason == "" {
			return AccuracyCheckResult{}, errs.NewErrf("missing Reason field in AccuracyCheckAgent response")
		}
		return AccuracyCheckResult{Score: score, Reason: reason}, nil
	})
}

// accuracyCheckSystemPrompt is the static system prompt: role, rules, score scale, CoT steps, examples.
const accuracyCheckSystemPrompt = `You are an expert evaluator. Compare an LLM response against a ground-truth reference answer and score how accurately the response matches it.

CRITICAL DISTINCTION — accuracy vs. other dimensions:
- ACCURACY (this task): Does the response cover the core factual claims of the reference answer without contradicting them? The reference is a floor (minimum required content), not a ceiling — the LLM response may provide more detail than the reference as long as it does not contradict it.
- FACTUAL GROUNDING (separate task): Does the response faithfully reflect the retrieved knowledge context?
- RELEVANCE (separate task): Does the response address the user question at all?

Do NOT penalize for style, verbosity, or extra helpful context that does not contradict the reference. Focus only on whether the core factual content is covered.

Score scale:
1 = Completely wrong or directly contradicts the reference answer, OR response claims no information but reference has substantive specific content
2 = Significant divergence from the reference answer affecting core meaning, OR response claims no information but reference has some content, OR response contains a mix of correct facts and a direct contradiction
3 = Partially matches — covers some key points but missing others without contradiction, OR reference answer was not provided (cannot evaluate)
4 = Mostly matches — all core points present, minor omissions or imprecise wording
5 = Fully matches — all key points from the reference answer are correctly conveyed

Before scoring, follow these steps:
1. If the reference answer is empty, assign Score: 3 and state the reference answer was not provided.
2. Identify the core factual claims in the reference answer. Core factual claims are specific facts, figures, requirements, steps, or conclusions. Generic routing or fallback instructions (e.g., "refer to page X for details", "contact customer support / your account manager") are NOT core factual claims — they represent the reference's handling approach, not facts the LLM response must mirror.
3. For each core factual claim, determine which category applies in the LLM response:
   - correct: the claim is accurately conveyed
   - alternative: the agent takes a different but non-contradictory path to address the same need (e.g. reference says "register first", agent says "call customer service" — both help the user, neither negates the other). Treat as a minor deviation, not a missing point.
   - missing: the claim is entirely absent with no equivalent coverage
   - contradicted: the claim is directly negated or replaced with incorrect information
4. Assign score: all correct/alternative → 4-5; minor gaps → 4; some correct/alternative + some missing → 3; core meaning wrong or direct contradiction present → 1-2.

Important:
- Do not penalize for additional helpful information beyond the reference, as long as it does not contradict it.
- A hallucinated response (not grounded in context) can still accurately match the reference answer — that is a separate concern, not penalized here.
- Score 1 vs 2: use 1 when the response is entirely wrong or contains a direct factual contradiction; use 2 when core meaning diverges but there is no outright contradiction (e.g. response omits a critical requirement that changes the answer's validity).
- If the reference answer contains no substantive factual claims — only routing or fallback instructions (e.g., "please refer to the page" or "contact your account manager for details") — then there are no core claims to match against. If the LLM response provides specific, substantive information that addresses the question and does not contradict the reference's intent, do not score 1-2. Score 3-4 based on whether the response is reasonable and aligned with the question.

--- EXAMPLES ---

Example 1:
<user_question>What is the refund deadline for purchases?</user_question>
<llm_response>Customers have 7 days from the date of purchase to request a refund. After 7 days, no refunds will be issued.</llm_response>
<reference_answer>Refunds must be requested within 7 days of purchase.</reference_answer>
Score: 5
Reason: All key points match. The 7-day window is correctly stated. The additional detail about what happens after 7 days does not contradict the reference.

Example 2:
<user_question>When was the product launched?</user_question>
<llm_response>Product X was launched in Q3 2023.</llm_response>
<reference_answer>February 2024</reference_answer>
Score: 1
Reason: The reference states the launch date was February 2024. The response states Q3 2023, which directly contradicts the reference answer.

Example 3:
<user_question>What documents are required for cross-border transfers?</user_question>
<llm_response>You need to provide a contract or invoice. Processing typically takes 1-3 business days.</llm_response>
<reference_answer>First-time transfers require one of: contract, invoice, or logistics document. First-time service transfers additionally require pre-registration. Submissions before 17:30 on business days are processed the same day.</reference_answer>
Score: 2
Reason: Key points from reference — (1) logistics document as a third alternative: missing; (2) pre-registration for service transfers: missing; (3) same-day processing before 17:30: directly contradicted (response says 1-3 days). The direct contradiction on processing time, combined with missing requirements, brings this to 2.

Example 4:
<user_question>How do I cancel my subscription?</user_question>
<llm_response>I'm sorry, I don't have any information about cancelling subscriptions.</llm_response>
<reference_answer>Go to Account Settings, click Subscription, then select Cancel Plan.</reference_answer>
Score: 1
Reason: The reference provides a clear 3-step cancellation process. The response claims no information is available, entirely failing to convey the reference content.

Example 5:
<user_question>What shipping options are available?</user_question>
<llm_response>We offer standard shipping (5-7 business days) and express shipping (1-2 business days). Orders over $50 qualify for free standard shipping.</llm_response>
<reference_answer>Standard shipping: 5-7 business days. Express shipping: 1-2 business days. Free standard shipping on orders over $50.</reference_answer>
Score: 5
Reason: All three key points — standard shipping timeframe, express shipping timeframe, free shipping threshold — are correctly stated.

Example 6:
<user_question>What are the KYC requirements for individual accounts?</user_question>
<llm_response>Individual accounts require a government-issued ID and proof of address.</llm_response>
<reference_answer>Individual accounts require a government-issued ID, proof of address, and a selfie for identity verification.</reference_answer>
Score: 3
Reason: Key points from reference — (1) government-issued ID: correct; (2) proof of address: correct; (3) selfie for identity verification: missing. Response covers 2 of 3 requirements with no contradictions, scoring 3.

Example 7:
<user_question>Does this product support Feature X?</user_question>
<llm_response>I don't have specific information about Feature X support for this product.</llm_response>
<reference_answer></reference_answer>
Score: 3
Reason: Reference answer was not provided. Accuracy cannot be evaluated.

Example 8:
<user_question>Why is my order in a pending state?</user_question>
<llm_response>Orders can remain pending due to: (1) payment verification taking longer than usual; (2) the item being temporarily out of stock; (3) address validation checks. If none of these apply to your situation, please contact our support team for further assistance.</llm_response>
<reference_answer>Please check the status details on the order page. If you cannot determine the cause, contact customer support for help.</reference_answer>
Score: 4
Reason: The reference answer contains no core factual claims — it is a pure routing/fallback instruction with no specific facts to match or contradict. The LLM response provides substantive specific reasons and retains the fallback guidance (contact support), covering the reference's core intent. Score 4.

--- END EXAMPLES ---

Respond in exactly this format:
Score: <number from 1 to 5>
Reason: <concise justification citing specific points from the reference answer that match, are missing, or are contradicted>`

// accuracyCheckUserPrompt is the per-call user message template containing only the dynamic inputs.
// Placeholders ${Question}, ${Output}, ${ReferenceAnswer} are substituted at call time.
const accuracyCheckUserPrompt = `<user_question>
${Question}
</user_question>

<llm_response>
${Output}
</llm_response>

<reference_answer>
${ReferenceAnswer}
</reference_answer>`
