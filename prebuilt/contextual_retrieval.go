package prebuilt

import (
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/curtisnewbie/miso-agent/agentloop"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
	"github.com/curtisnewbie/miso/util/llm"
	"github.com/curtisnewbie/miso/util/retry"
	"github.com/curtisnewbie/miso/util/strutil"
)

var contextualRetrievalExtractor = llm.MustTagExtractor("final_response")

// ContextualRetrievalOption configures a ContextualRetrievalAgent.
type ContextualRetrievalOption func(o *contextualRetrievalConfig)

type contextualRetrievalConfig struct {
	// SystemPrompt is an optional system prompt prepended before the task prompt.
	// If empty, no system message is sent.
	SystemPrompt string
	// RetryCount is the number of additional attempts when the model response is empty.
	// Defaults to 2 (up to 3 total attempts).
	RetryCount int
}

// WithContextualRetrievalSystemPrompt sets an optional system prompt for the contextual retrieval agent.
func WithContextualRetrievalSystemPrompt(prompt string) ContextualRetrievalOption {
	return func(o *contextualRetrievalConfig) {
		o.SystemPrompt = prompt
	}
}

// WithContextualRetrievalRetry sets the number of additional retry attempts when the model
// response is empty. The default is 2 (up to 3 total attempts).
func WithContextualRetrievalRetry(n int) ContextualRetrievalOption {
	return func(o *contextualRetrievalConfig) {
		o.RetryCount = n
	}
}

// ContextualRetrievalInput holds the inputs for a single contextual retrieval call.
type ContextualRetrievalInput struct {
	// Content is the full document text from which the chunk was extracted.
	Content string

	// Chunk is the specific text chunk to situate within the document.
	Chunk string
}

// ContextualRetrievalResult holds the generated context for the chunk.
type ContextualRetrievalResult struct {
	// Context is a short succinct description that situates the chunk within the overall document,
	// intended to improve search retrieval quality when prepended to the chunk.
	Context string
}

// contextualRetrievalPromptInput is the named template substitution struct for [contextualRetrievalTaskPrompt].
// Field names must match the ${...} placeholders in the template.
type contextualRetrievalPromptInput struct {
	Content string
	Chunk   string
}

// ContextualRetrievalAgent generates a short context snippet for a document chunk,
// situating it within the overall document to improve RAG retrieval quality.
//
// The agent implements the Contextual Retrieval pattern: given a full document and a
// specific chunk, it produces a succinct context that describes where the chunk fits
// in the broader text. This context can then be prepended to the chunk before indexing
// to significantly improve retrieval recall.
//
// The response language follows the document language automatically — no language
// configuration is required.
//
// Use [NewContextualRetrievalAgent] to create an instance, then call
// [ContextualRetrievalAgent.Retrieve] to generate context for a chunk.
type ContextualRetrievalAgent struct {
	agent  *agentloop.Agent
	config *contextualRetrievalConfig
}

// NewContextualRetrievalAgent creates a new ContextualRetrievalAgent backed by the given chat model.
//
// Example:
//
//	agent, err := prebuilt.NewContextualRetrievalAgent(chatModel)
//	result, err := agent.Retrieve(rail, prebuilt.ContextualRetrievalInput{
//	    Content: "...full document text...",
//	    Chunk:   "...a specific paragraph or section...",
//	})
func NewContextualRetrievalAgent(chatModel model.ToolCallingChatModel, opts ...ContextualRetrievalOption) (*ContextualRetrievalAgent, error) {
	cfg := &contextualRetrievalConfig{RetryCount: 2}
	for _, o := range opts {
		o(cfg)
	}

	agent, err := agentloop.NewAgent(agentloop.AgentConfig{
		Name:         "ContextualRetrievalAgent",
		Model:        chatModel,
		MaxRunSteps:  2,
		SystemPrompt: cfg.SystemPrompt,
	})
	if err != nil {
		return nil, errs.Wrapf(err, "failed to create ContextualRetrievalAgent")
	}

	return &ContextualRetrievalAgent{agent: agent, config: cfg}, nil
}

// Retrieve generates a short succinct context that situates the given chunk within the
// overall document. The response language follows the document language automatically.
//
// The model produces chain-of-thought reasoning inside <thinking> tags followed by the
// final answer inside <final_response> tags. Only the <final_response> content is returned.
// If the <final_response> tag is absent the call is retried up to [contextualRetrievalConfig.RetryCount] times.
func (a *ContextualRetrievalAgent) Retrieve(rail flow.Rail, input ContextualRetrievalInput) (ContextualRetrievalResult, error) {
	userPrompt := strutil.NamedSprintfv(contextualRetrievalTaskPrompt, contextualRetrievalPromptInput{
		Content: input.Content,
		Chunk:   input.Chunk,
	})

	return retry.GetOne(a.config.RetryCount, func() (ContextualRetrievalResult, error) {
		out, err := a.agent.Execute(rail, agentloop.AgentRequest{
			UserInput: userPrompt,
		})
		if err != nil {
			return ContextualRetrievalResult{}, errs.Wrapf(err, "ContextualRetrievalAgent execution failed")
		}
		ctx, err := parseContextualRetrievalResponse(out.Response)
		if err != nil {
			return ContextualRetrievalResult{}, errs.Wrapf(err, "failed to parse ContextualRetrievalAgent response")
		}
		return ContextualRetrievalResult{Context: ctx}, nil
	})
}

// parseContextualRetrievalResponse extracts content from the <final_response> tag.
// Returns an error when the tag is absent or yields an empty value, triggering retry.
func parseContextualRetrievalResponse(content string) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", errs.NewErrf("empty response from ContextualRetrievalAgent")
	}
	result := contextualRetrievalExtractor.Content(content)
	if strings.TrimSpace(result) == "" {
		return "", errs.NewErrf("missing <final_response> tag in ContextualRetrievalAgent response")
	}
	return strings.TrimSpace(result), nil
}

// contextualRetrievalTaskPrompt is the task prompt template sent as the user message.
// Placeholders ${Content} and ${Chunk} are substituted at call time via strutil.NamedSprintfv.
const contextualRetrievalTaskPrompt = `<document>
${Content}
</document>

Here is the chunk to situate within the document above:
<chunk>
${Chunk}
</chunk>

Your task: Write a short context (1-3 sentences) that situates this chunk within the overall document. This context will be prepended to the chunk to improve vector search retrieval quality.

Requirements:
- Use the same language as the document
- Describe where the chunk sits in the document and what broader topic it belongs to
- Do NOT summarize the chunk itself — describe its position and role within the document
- 1-3 sentences only

Think through the chunk's position in the document, then output your answer strictly in the format below.

<thinking>
[Think: what section is this chunk from? what broader topic does it belong to? how does it relate to the document's main subject?]
</thinking>

<final_response>
[Your 1-3 sentence context, written in the document's language]
</final_response>`
