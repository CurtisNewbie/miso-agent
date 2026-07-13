package agentloop

import (
	"context"
	"fmt"

	"github.com/curtisnewbie/miso/util/llm"
)

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
