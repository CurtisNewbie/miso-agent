package agentloop

import (
	"context"
	"fmt"

	"github.com/curtisnewbie/miso/util/llm"
)

// JsonOutputCheck returns an OutputCheckFunc that rejects assistant responses that cannot be
// parsed as valid JSON of type T (after stripping any <think>...</think> block).
//
// maxAttempts caps how many times the check will reject a response. Once attempt exceeds
// maxAttempts the check passes unconditionally, accepting whatever the model produced.
//
// Example:
//
//	agent, _ := agentloop.NewAgent(agentloop.AgentConfig{
//	    OutputCheck: agentloop.JsonOutputCheck[ClassificationOutput](2),
//	})
func JsonOutputCheck[T any](maxAttempts int) OutputCheckFunc {
	return func(_ context.Context, _ AgentContext, attempt int, output string) (string, bool, error) {
		_, content := llm.ParseThink(output)
		if _, err := llm.ParseLLMJsonAs[T](content); err != nil {
			if attempt >= maxAttempts {
				return "", true, nil
			}
			return fmt.Sprintf(
				"[Attempt %d] Your response could not be parsed as valid JSON: %v. "+
					"Rewrite your response as a valid JSON object with no markdown fences or extra commentary.",
				attempt, err,
			), false, nil
		}
		return "", true, nil
	}
}

// FinalResponseExtractor extracts content enclosed in <final_response>...</final_response> tags.
var FinalResponseExtractor = llm.MustTagExtractor("final_response")

// FinalResponseTagOutputCheck returns an OutputCheckFunc that rejects assistant responses
// not wrapped in <final_response>...</final_response> tags.
//
// maxAttempts caps how many times the check will reject a response. Once attempt exceeds
// maxAttempts the check passes unconditionally, accepting whatever the model produced.
//
// Example:
//
//	agent, _ := agentloop.NewAgent(agentloop.AgentConfig{
//	    OutputCheck: agentloop.FinalResponseTagOutputCheck(2),
//	})
func FinalResponseTagOutputCheck(maxAttempts int) OutputCheckFunc {
	return func(_ context.Context, _ AgentContext, attempt int, output string) (string, bool, error) {
		if FinalResponseExtractor.Content(output) != "" {
			return "", true, nil
		}
		if attempt >= maxAttempts {
			return "", true, nil
		}
		return fmt.Sprintf(
			"[Attempt %d] Your response is missing the required <final_response> wrapper. "+
				"Rewrite your response using exactly this format:\n\n"+
				"<final_response>\n[your complete answer]\n</final_response>\n\n"+
				"Do not use alternative tag names such as <final_answer>, <answer>, or <response>.",
			attempt,
		), false, nil
	}
}
