package prebuilt

import (
	"context"
	"fmt"
	"strings"

	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/util/llm"

	"github.com/curtisnewbie/miso-agent/agentloop"
)

// scoreReasonResponse is the JSON schema shared by evaluator agents (fact-check, accuracy-check,
// relevance-check) that report a 1-5 score with a textual justification.
type scoreReasonResponse struct {
	Score  int    `json:"score"`
	Reason string `json:"reason"`
}

// parseScoreReason parses a `{"score": <1-5>, "reason": "..."}` JSON model response.
// Any <think>...</think> block is stripped before parsing.
// Returns score in [1,5] and the trimmed reason string.
func parseScoreReason(content string) (score int, reason string, err error) {
	_, body := llm.ParseThink(content)
	parsed, perr := llm.ParseLLMJsonAs[scoreReasonResponse](body)
	if perr != nil {
		return 0, "", errs.Wrapf(perr, "failed to parse JSON response")
	}
	if parsed.Score < 1 || parsed.Score > 5 {
		return 0, "", errs.NewErrf("score out of range [1,5]: %d", parsed.Score)
	}
	return parsed.Score, strings.TrimSpace(parsed.Reason), nil
}

// scoreReasonOutputCheck returns an OutputCheckFunc that rejects assistant responses that fail
// semantic compliance of scoreReasonResponse: they must parse as valid JSON (after stripping any
//
//	thinking... response block) AND contain a score in [1,5] and a non-empty "reason" field.
//
// This mirrors agentloop.JsonOutputCheck's give-up-after-maxAttempts semantics: once attempt
// exceeds maxAttempts the check passes unconditionally, accepting whatever the model produced.
// Unlike plain agentloop.JsonOutputCheck, which only verifies JSON parseability, this check also
// validates the score range and that the reason field is non-empty.
//
// Example:
//
//	agent, _ := agentloop.NewAgent(agentloop.AgentConfig{
//	    OutputCheck: scoreReasonOutputCheck(2),
//	})
func scoreReasonOutputCheck(maxAttempts int) agentloop.OutputCheckFunc {
	return func(_ context.Context, _ agentloop.AgentContext, attempt int, output string) (string, bool, error) {
		_, body := llm.ParseThink(output)
		parsed, err := llm.ParseLLMJsonAs[scoreReasonResponse](body)
		if err != nil {
			if attempt >= maxAttempts {
				return "", true, nil
			}
			return fmt.Sprintf(
				"[Attempt %d] Your response could not be parsed as valid JSON: %v. "+
					"Rewrite your response as a valid JSON object with no markdown fences or extra commentary.",
				attempt, err,
			), false, nil
		}
		if parsed.Score < 1 || parsed.Score > 5 {
			if attempt >= maxAttempts {
				return "", true, nil
			}
			return fmt.Sprintf(
				"[Attempt %d] Your score %d is out of range. Score must be an integer between 1 and 5 (inclusive).",
				attempt, parsed.Score,
			), false, nil
		}
		if strings.TrimSpace(parsed.Reason) == "" {
			if attempt >= maxAttempts {
				return "", true, nil
			}
			return fmt.Sprintf(
				"[Attempt %d] The \"reason\" field is empty. Provide a non-empty textual justification for your score.",
				attempt,
			), false, nil
		}
		return "", true, nil
	}
}
