package prebuilt

import "testing"

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
