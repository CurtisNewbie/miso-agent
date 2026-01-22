package agentapi

import (
	"fmt"

	"github.com/curtisnewbie/miso-tavily/tavily"
	"github.com/curtisnewbie/miso/miso"
	"github.com/curtisnewbie/miso/util/slutil"
	"github.com/curtisnewbie/miso/util/strutil"
)

type InitTavilyResearchReq struct {
	Topic            string               `json:"topic"`
	PreviousResearch string               `json:"previousResearch"`
	CitationFormat   string               `json:"citation_format"` // numbered, mla, apa, chicago
	Model            string               `json:"model"`           // mini, pro, auto
	OutputSchema     *tavily.OutputSchema `json:"output_schema"`
}

type Source struct {
	Favicon string `json:"favicon"`
	Title   string `json:"title"`
	URL     string `json:"url"`
}

type TavilyDeepResearchRes struct {
	Report  string   `json:"report"`
	Sources []Source `json:"sources"`
}

// Run Tavily Deep Research with predefined prompt.
//
// If you don't want the prompt, just call Tvaily's API yourself, or use [tavily.StreamResearch] directly.
func TavilyDeepResearch(rail miso.Rail, apiKey string, req InitTavilyResearchReq, ops ...tavily.StreamResearchOpFunc) (TavilyDeepResearchRes, error) {
	var previousResearch string
	if req.PreviousResearch != "" {
		previousResearch = fmt.Sprintf(`
# Previous Research History Retrieval
<previous_research>
%s
</previous_research>`, req.PreviousResearch)
	}

	sources := make([]tavily.Source, 0, 10)
	query := strutil.NamedSprintfkv(`
# Research Topic
${query}

# Core Research Requirements
- Practical Focus: Prioritize real-world application over theoretical concepts. Avoid jargon, abstract discussions, or overly verbose explanations. Explain necessary terms plainly.
- Problem-Solution Alignment: Clearly connect findings to specific, solvable problems or decisions the user faces.
- Scope & Constraints: Acknowledge limitations (e.g., cost, time, feasibility) in any recommendations.
- Actionable Output: Provide clear, concrete steps, alternatives, or criteria for decision-making.
- Evidence-Based: Ground insights in credible data, case studies, or proven examples—not just trends.
- Clarity on Gaps: If critical information is unavailable or uncertain, state this simply and move on. Do not over-elaborate on research process shortcomings.
- Readability: Use simple, easy to understand language, try not to over-complicate things.
- Formatting:
	- Report should be well organized. Avoid using too many bulletpoints.
	- Avoid LaTeX syntax, LaTeX markdown can't be rendered properly in the generated report.
	- Avoid Citation in graphs, it hurts readability.
	- Simplify graphs, graphs are rendered in plain text, make sure they are readable, avoid fancy formatting.

${previousResearch}
`, "query", req.Topic, "previousResearch", previousResearch)

	rail.Infof("TavilyDeepResearch Prompt: %v", query)
	report, err := tavily.StreamResearch(rail, apiKey,
		tavily.InitResearchReq{
			CitationFormat: req.CitationFormat,
			Input:          query,
			Model:          req.Model,
			OutputSchema:   req.OutputSchema,
			Stream:         true,
		},
		append(ops, tavily.WithSourceHook(func(s []tavily.Source) error {
			sources = append(sources, s...)
			return nil
		}))...)

	if err != nil {
		return TavilyDeepResearchRes{}, err
	}
	return TavilyDeepResearchRes{
		Report:  report,
		Sources: slutil.MapTo(sources, func(s tavily.Source) Source { return Source(s) }),
	}, nil
}
