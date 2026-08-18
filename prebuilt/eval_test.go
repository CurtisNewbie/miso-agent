package prebuilt

import (
	"context"
	"testing"

	"github.com/curtisnewbie/miso-agent/agentloop"
)

func TestParseScoreReason(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantScore  int
		wantReason string
		wantErr    bool
	}{
		// --- happy path ---
		{
			name:       "Score and Reason",
			content:    `{"score": 5, "reason": "Perfect answer."}`,
			wantScore:  5,
			wantReason: "Perfect answer.",
		},
		{
			name:       "Reason with embedded newline",
			content:    "{\"score\": 3, \"reason\": \"Partially correct.\\nThe second sentence continues the reason.\"}",
			wantScore:  3,
			wantReason: "Partially correct.\nThe second sentence continues the reason.",
		},
		// --- key order independence ---
		{
			name:       "Reason before score in JSON",
			content:    `{"reason": "Great response.", "score": 5}`,
			wantScore:  5,
			wantReason: "Great response.",
		},
		// --- whitespace / formatting tolerance ---
		{
			name:       "Extra whitespace around JSON",
			content:    "  \n" + `{"score": 3, "reason": "Somewhat relevant."}` + "\n  ",
			wantScore:  3,
			wantReason: "Somewhat relevant.",
		},
		{
			name:       "Wrapped in markdown fence",
			content:    "```json\n" + `{"score": 4, "reason": "Minor issues."}` + "\n```",
			wantScore:  4,
			wantReason: "Minor issues.",
		},
		{
			name:       "Wrapped in think block",
			content:    "<think>reasoning...</think>" + `{"score": 4, "reason": "Minor issues."}`,
			wantScore:  4,
			wantReason: "Minor issues.",
		},
		// --- boundary scores ---
		{
			name:      "Score = 1 (min)",
			content:   `{"score": 1, "reason": "Completely wrong."}`,
			wantScore: 1,
		},
		{
			name:      "Score = 5 (max)",
			content:   `{"score": 5, "reason": "Fully correct."}`,
			wantScore: 5,
		},
		// --- reason optional ---
		{
			name:       "Score only, no reason field",
			content:    `{"score": 5}`,
			wantScore:  5,
			wantReason: "",
		},
		// --- error: invalid JSON ---
		{
			name:    "Empty content",
			content: "",
			wantErr: true,
		},
		{
			name:    "Not JSON at all",
			content: "the score is excellent",
			wantErr: true,
		},
		{
			name:    "Score not a number",
			content: `{"score": "excellent", "reason": "non-numeric"}`,
			wantErr: true,
		},
		// --- error: out of range ---
		{
			name:    "Score = 0 (below range)",
			content: `{"score": 0, "reason": "below range"}`,
			wantErr: true,
		},
		{
			name:    "Score = 6 (above range)",
			content: `{"score": 6, "reason": "above range"}`,
			wantErr: true,
		},
		{
			name:    "Score negative",
			content: `{"score": -1, "reason": "negative"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, reason, err := parseScoreReason(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil (score=%d, reason=%q)", score, reason)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if score != tt.wantScore {
				t.Errorf("score = %d, want %d", score, tt.wantScore)
			}
			if tt.wantReason != "" && reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

func TestScoreReasonOutputCheck(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		attempt  int
		wantOk   bool
		wantHint bool // true => hint must be non-empty; false => hint must be empty
	}{
		// --- valid input always passes, regardless of attempt ---
		{
			name:    "Valid JSON at attempt 0",
			output:  `{"score": 4, "reason": "Well justified."}`,
			attempt: 0,
			wantOk:  true,
		},
		{
			name:    "Valid JSON at attempt 1",
			output:  `{"score": 4, "reason": "Well justified."}`,
			attempt: 1,
			wantOk:  true,
		},
		// --- invalid JSON ---
		{
			name:     "Invalid JSON below maxAttempts rejects with hint",
			output:   "the answer is great",
			attempt:  0,
			wantOk:   false,
			wantHint: true,
		},
		{
			name:    "Invalid JSON at maxAttempts gives up",
			output:  "the answer is great",
			attempt: 2,
			wantOk:  true,
		},
		// --- score out of range ---
		{
			name:     "Score below range rejects with hint",
			output:   `{"score": 0, "reason": "too low"}`,
			attempt:  0,
			wantOk:   false,
			wantHint: true,
		},
		{
			name:     "Score above range rejects with hint",
			output:   `{"score": 6, "reason": "too high"}`,
			attempt:  0,
			wantOk:   false,
			wantHint: true,
		},
		{
			name:    "Score out of range at maxAttempts gives up",
			output:  `{"score": 6, "reason": "too high"}`,
			attempt: 2,
			wantOk:  true,
		},
		// --- empty reason ---
		{
			name:     "Empty reason rejects with hint",
			output:   `{"score": 3, "reason": ""}`,
			attempt:  0,
			wantOk:   false,
			wantHint: true,
		},
		{
			name:     "Whitespace-only reason rejects with hint",
			output:   `{"score": 3, "reason": "   "}`,
			attempt:  0,
			wantOk:   false,
			wantHint: true,
		},
		{
			name:    "Empty reason at maxAttempts gives up",
			output:  `{"score": 3, "reason": ""}`,
			attempt: 2,
			wantOk:  true,
		},
		// --- boundary scores ---
		{
			name:    "Score = 1 (min) with reason passes",
			output:  `{"score": 1, "reason": "Completely wrong."}`,
			attempt: 0,
			wantOk:  true,
		},
		{
			name:    "Score = 5 (max) with reason passes",
			output:  `{"score": 5, "reason": "Fully correct."}`,
			attempt: 0,
			wantOk:  true,
		},
	}

	check := scoreReasonOutputCheck(2)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hint, ok, err := check(context.Background(), agentloop.AgentContext{}, tt.attempt, tt.output)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tt.wantOk {
				t.Errorf("ok = %v, want %v (hint=%q)", ok, tt.wantOk, hint)
			}
			if tt.wantHint && hint == "" {
				t.Errorf("expected non-empty hint, got empty")
			}
			if !tt.wantHint && hint != "" {
				t.Errorf("expected empty hint, got %q", hint)
			}
		})
	}
}
