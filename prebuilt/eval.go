package prebuilt

import (
	"strings"

	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/util/llm"
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
