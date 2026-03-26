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
		// --- happy path: score before reason ---
		{
			name:       "Score then Reason single line",
			content:    "Score: 5\nReason: Perfect answer.",
			wantScore:  5,
			wantReason: "Perfect answer.",
		},
		{
			name:       "Score then Reason multi-line",
			content:    "Score: 3\nReason: Partially correct.\nThe second sentence continues the reason.",
			wantScore:  3,
			wantReason: "Partially correct.\nThe second sentence continues the reason.",
		},
		// --- happy path: reason before score ---
		{
			name:       "Reason then Score",
			content:    "Reason: Great response.\nScore: 5",
			wantScore:  5,
			wantReason: "Great response.",
		},
		{
			name:       "Reason multi-line then Score",
			content:    "Reason: Line one.\nLine two.\nScore: 4",
			wantScore:  4,
			wantReason: "Line one.\nLine two.",
		},
		// --- case insensitivity ---
		{
			name:       "Uppercase SCORE and REASON",
			content:    "SCORE: 2\nREASON: Major issues.",
			wantScore:  2,
			wantReason: "Major issues.",
		},
		{
			name:       "Mixed case score:",
			content:    "Score: 4\nreason: Minor issues.",
			wantScore:  4,
			wantReason: "Minor issues.",
		},
		// --- whitespace tolerance ---
		{
			name:       "Extra spaces around values",
			content:    "  Score:  3  \n  Reason:  Somewhat relevant.  ",
			wantScore:  3,
			wantReason: "Somewhat relevant.",
		},
		{
			name:       "Blank lines around entries",
			content:    "\n\nScore: 4\n\nReason: Minor issues.\n\n",
			wantScore:  4,
			wantReason: "Minor issues.",
		},
		// --- boundary scores ---
		{
			name:      "Score = 1 (min)",
			content:   "Score: 1\nReason: Completely wrong.",
			wantScore: 1,
		},
		{
			name:      "Score = 5 (max)",
			content:   "Score: 5\nReason: Fully correct.",
			wantScore: 5,
		},
		// --- reason optional ---
		{
			name:       "Score only, no Reason line",
			content:    "Score: 5",
			wantScore:  5,
			wantReason: "",
		},
		// --- Windows CRLF ---
		{
			name:       "CRLF line endings",
			content:    "Score: 2\r\nReason: CRLF endings.",
			wantScore:  2,
			wantReason: "CRLF endings.",
		},
		// --- error: missing score ---
		{
			name:    "Missing Score line",
			content: "Reason: No score provided.",
			wantErr: true,
		},
		{
			name:    "Empty content",
			content: "",
			wantErr: true,
		},
		// --- error: invalid score value ---
		{
			name:    "Score not a number",
			content: "Score: excellent\nReason: non-numeric",
			wantErr: true,
		},
		// --- error: out of range ---
		{
			name:    "Score = 0 (below range)",
			content: "Score: 0\nReason: below range",
			wantErr: true,
		},
		{
			name:    "Score = 6 (above range)",
			content: "Score: 6\nReason: above range",
			wantErr: true,
		},
		{
			name:    "Score negative",
			content: "Score: -1\nReason: negative",
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
