package agentapi

import (
	"strings"

	"github.com/curtisnewbie/miso-tavily/tavily"
	"github.com/curtisnewbie/miso/miso"
	"github.com/curtisnewbie/miso/util/strutil"
)

type TavilyBackgroundCheckAspect struct {
	Name        string
	Description string
	Example     string
}

type InitTavilyBackgroundCheckReq struct {
	Language string                        `json:"language"`
	Entity   string                        `json:"entity"`  // entity name
	Context  string                        `json:"context"` // additional context about the entity or the research
	Asepcts  []TavilyBackgroundCheckAspect `json:"asepcts"` // aspects of the entity that should be answered
	Model    string                        `json:"model"`   // mini, pro, auto
}

// Run Tavily background check with predefined prompt.
//
// If you don't want the prompt, just call Tvaily's API yourself, or use [tavily.StreamResearch] directly.
func TavilBackgroundCheck(rail miso.Rail, apiKey string, req InitTavilyBackgroundCheckReq, ops ...tavily.StreamResearchOpFunc) (string, error) {
	if req.Language == "" {
		req.Language = "English"
	}

	ab := strutil.NewBuilder()
	ab.WithIndent("  ", 1)
	for _, asp := range req.Asepcts {
		ab.Printlnf("- %v", asp.Name)
		if asp.Description != "" {
			ab.Printf(", %v", asp.Description)
		}
		if asp.Example != "" {
			ab.StepIn(func(b *strutil.Builder) {
				b.Println("E.g.,")
				for _, l := range strings.Split(asp.Example, "\n") {
					b.Println(l)
				}
			})
		}
	}

	query := strutil.NamedSprintf(`
# Task
Conduct a focused investigation of the provided Entity based solely on the specified Aspects.
Generate a practical, fact-based report that directly answers the questions.
Do not research, analyze, or comment on areas outside the defined scope.

# Context
- Entity Name: ${entity}
- Aspects to Research:
${aspects}
- Additional Context:
${context}

# Core Instructions:
- Report must be written in ${language}.
- Strict Scope Adherence: Research only the listed Aspects. Ignore all other information about the entity, no matter how notable it seems.
- Practical & Direct: Write concisely. Present findings as clear statements, bullet points, or short summaries. Avoid introductions, theoretical frameworks, and concluding summaries.
- Fact-Based Reporting: Prioritize verified data from credible sources (official records, reputable news, financial filings). Clearly distinguish between confirmed facts and widespread public claims.
- Gap Statement: If a specific Aspect cannot be answered due to a complete lack of publicly available information, state this simply (e.g., "No public record found."). Do not elaborate on the reasons for the gap.

`, map[string]any{
		"language": req.Language,
		"entity":   req.Entity,
		"aspects":  ab.String(),
		"context":  req.Context,
	})

	rail.Infof("TavilBackgroundCheck Prompt: %v", query)
	return tavily.StreamResearch(rail, apiKey,
		tavily.InitResearchReq{
			Input:  query,
			Model:  req.Model,
			Stream: true,
		},
		ops...)
}
