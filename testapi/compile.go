package testapi

import (
	"github.com/curtisnewbie/miso-agent/agentloop"
	"github.com/curtisnewbie/miso-agent/agentloop/types"
	"github.com/curtisnewbie/miso-agent/agents"
	"github.com/curtisnewbie/miso-agent/graph"
	"github.com/curtisnewbie/miso/miso"
)

func compileGraph() error {
	rail := miso.EmptyRail()
	model, err := agents.NewOpenAIChatModel("mymodel", "mykey")
	if err != nil {
		return err
	}
	gop := graph.NewGenericOps()
	gop.RepeatPrompt = true
	gop.VisualizeDir = "../doc"
	_, err = agents.NewMemorySummarizer(rail, model, agents.NewMemorySummarizerOps(gop))
	if err != nil {
		return err
	}

	_, err = agents.NewExecutiveSummaryWriter(rail, model, agents.NewExecutiveSummaryWriterOps(gop))
	if err != nil {
		return err
	}

	_, err = agents.NewDeepResearchClarifier(rail, model, agents.NewDeepResearchClarifierOps(gop))
	if err != nil {
		return err
	}

	_, err = agents.NewRuleMatcher(rail, model, agents.NewRuleMatcherOps(gop))
	if err != nil {
		return err
	}

	_, err = agents.NewMaterialExtract(rail, model, agents.NewMaterialExtractOps(gop))
	if err != nil {
		return err
	}

	// Add agentloop agent
	_, err = agentloop.NewAgent(types.AgentConfig{
		Model:                       model,
		MaxSteps:                    100,
		Language:                    "English",
		MaxTokens:                   32000,
		TokenizerModelName:          "gpt-3.5-turbo",
		EvictToolResultsThreshold:   1000,
		EvictToolResultsKeepPreview: 100,
		VisualizeDir:                "../doc",
	})
	return err
}
