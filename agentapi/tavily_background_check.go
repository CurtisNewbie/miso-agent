package agentapi

import (
	"strings"

	"github.com/curtisnewbie/miso-tavily/tavily"
	"github.com/curtisnewbie/miso/miso"
	"github.com/curtisnewbie/miso/util/slutil"
	"github.com/curtisnewbie/miso/util/strutil"
)

type TavilyBackgroundCheckAspect struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Example     string `json:"example"`
}

type TavilyBackgroundCheckReq struct {
	Language string                        `json:"language"`
	Entity   string                        `json:"entity"`  // entity name
	Context  string                        `json:"context"` // additional context about the entity or the research
	Asepcts  []TavilyBackgroundCheckAspect `json:"asepcts"` // aspects of the entity that should be answered
	Model    string                        `json:"model"`   // mini, pro, auto
}

type TavilyBackgroundCheckRes struct {
	Report  string   `json:"report"`
	Sources []Source `json:"sources"`
}

// Run Tavily background check with predefined prompt.
//
// If you don't want the prompt, just call Tvaily's API yourself, or use [tavily.StreamResearch] directly.
func TavilBackgroundCheck(rail miso.Rail, apiKey string, req TavilyBackgroundCheckReq, ops ...tavily.StreamResearchOpFunc) (TavilyBackgroundCheckRes, error) {
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
	sources := make([]tavily.Source, 0, 10)
	query := strutil.NamedSprintf(`
# Task
Conduct a focused investigation of the provided entity based primarily on the specified aspects.
Generate a practical, fact-based report that provides background information about the entity and directly answers the questions (IMPORTANT).

# Context
- Entity Name: ${entity}
- Aspects to Research:
${aspects}
- Additional Context:
${context}

# Core Instructions:
- Report must be written in ${language}.
- Strict Scope Adherence: Research the listed Aspects. List other information about the entity if found, but avoid further investigation unless the information is related to the entity or listed aspects.
- Practical & Direct: Write concisely. Present findings as clear statements, bullet points, or short summaries. Avoid theoretical frameworks.
- Fact-Based Reporting: Prioritize verified data from credible sources (official records, reputable news, financial filings). Clearly distinguish between confirmed facts and widespread public claims.
- Integrated Reasoning (CRITICAL): For each finding or answer provided, you must append a brief, practical reason in parentheses. This reason should explain the basis for the information (e.g., source type, logic of deduction, or acknowledgment of data limitation).
- Gap Statement: If a specific Aspect cannot be answered due to a complete lack of publicly available information, state this simply (e.g., "No public record found."). Do not elaborate on the reasons for the gap.
`, map[string]any{
		"language": req.Language,
		"entity":   req.Entity,
		"aspects":  ab.String(),
		"context":  req.Context,
	})

	rail.Infof("TavilBackgroundCheck Prompt: %v", query)
	report, err := tavily.StreamResearch(rail, apiKey,
		tavily.InitResearchReq{
			Input:  query,
			Model:  req.Model,
			Stream: true,
		},
		append(ops, tavily.WithSourceHook(func(s []tavily.Source) error {
			sources = append(sources, s...)
			return nil
		}))...)
	if err != nil {
		return TavilyBackgroundCheckRes{}, err
	}
	return TavilyBackgroundCheckRes{
		Report:  report,
		Sources: slutil.MapTo(sources, func(s tavily.Source) Source { return Source(s) }),
	}, nil
}
