package agentapi

import (
	"fmt"

	"github.com/curtisnewbie/miso-tavily/tavily"
	"github.com/curtisnewbie/miso/miso"
	"github.com/curtisnewbie/miso/util/strutil"
)

type InitTavilyResearchReq struct {
	Topic            string
	PreviousResearch string
	CitationFormat   string               `json:"citation_format"` // numbered, mla, apa, chicago
	Model            string               `json:"model"`           // mini, pro, auto
	OutputSchema     *tavily.OutputSchema `json:"output_schema"`
}

// Run Tavily Deep Research with predefined prompt.
//
// If you don't want the prompt, just call Tvaily's API yourself, or use [tavily.StreamResearch] directly.
func TavilDeepResearch(rail miso.Rail, apiKey string, req InitTavilyResearchReq, ops ...tavily.StreamResearchOpFunc) (string, error) {
	var previousResearch string
	if req.PreviousResearch != "" {
		previousResearch = fmt.Sprintf(`
# Previous Research History Retrieval
<previous_research>
%s
</previous_research>`, req.PreviousResearch)
	}
	query := strutil.NamedSprintfkv(`
# Core Research Requirements:
 - Practical Focus: Prioritize real-world application over theoretical concepts. Avoid jargon; explain necessary terms plainly.
 - Problem-Solution Alignment: Clearly connect findings to specific, solvable problems or decisions the user faces.
 - Scope & Constraints: Acknowledge limitations (e.g., cost, time, feasibility) in any recommendations.
 - Actionable Output: Provide clear, concrete steps, alternatives, or criteria for decision-making.
 - Evidence-Based: Ground insights in credible data, case studies, or proven examples—not just trends.

 Avoid:
  - Purely academic or abstract discussions.
  - Overly verbose explanations or unnecessary background.
  - Generic advice without clear implementation paths.

# Formatting Requirements
 - Avoid LaTeX syntax, LaTeX markdown can't be rendered properly in the generated report.
 - Avoid Citation in graphs, it hurts readability.
 - Simplify graphs, graphs are rendered in plain text, make sure they are readable, avoid fancy formatting.

# Research Topic
${query}
${previousResearch}
`, "query", req.Topic, "previousResearch", previousResearch)

	rail.Debugf("TavilyDeepResearch Prompt: %v", query)

	return tavily.StreamResearch(rail, apiKey,
		tavily.InitResearchReq{
			CitationFormat: req.CitationFormat,
			Input:          query,
			Model:          req.Model,
			OutputSchema:   req.OutputSchema,
			Stream:         true,
		},
		ops...)
}
