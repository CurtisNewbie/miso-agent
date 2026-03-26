package prebuilt

// @author yongj.zhuang

import (
	"strconv"
	"strings"

	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/util/strutil"
)

// parseScoreReason parses a "Score:\nReason:" model response.
// Reason may span multiple lines; Score may appear before or after Reason.
// Prefix matching is case-insensitive.
// Returns score in [1,5] and the trimmed reason string.
func parseScoreReason(content string) (score int, reason string, err error) {
	lines := strings.Split(content, "\n")
	var scoreStr string
	reasonStart := -1
	reasonEnd := -1

	for i, l := range lines {
		l = strings.TrimSpace(l)
		if v, ok := strutil.CutPrefixIgnoreCase(l, "Score:"); ok {
			scoreStr = strings.TrimSpace(v)
			if reasonStart > -1 && reasonEnd < 0 {
				reasonEnd = i
			}
		} else if strutil.HasPrefixIgnoreCase(l, "Reason:") {
			if reasonStart < 0 {
				reasonStart = i
			}
		}
	}

	if scoreStr == "" {
		return 0, "", errs.NewErrf("missing Score field in response")
	}
	n, convErr := strconv.Atoi(scoreStr)
	if convErr != nil {
		return 0, "", errs.NewErrf("invalid score value %q: %v", scoreStr, convErr)
	}
	if n < 1 || n > 5 {
		return 0, "", errs.NewErrf("score out of range [1,5]: %d", n)
	}

	if reasonStart > -1 {
		if reasonEnd < 0 {
			reasonEnd = len(lines)
		}
		raw := strings.TrimSpace(strings.Join(lines[reasonStart:reasonEnd], "\n"))
		raw, _ = strutil.CutPrefixIgnoreCase(raw, "Reason:")
		reason = strings.TrimSpace(raw)
	}

	return n, reason, nil
}
