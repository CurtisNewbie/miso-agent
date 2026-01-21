package testapi

import (
	"github.com/curtisnewbie/miso-agent/agents"
	"github.com/curtisnewbie/miso/miso"
)

func compileGraph() error {
	rail := miso.EmptyRail()
	model, err := agents.NewOpenAIChatModel("mymodel", "mykey")
	if err != nil {
		return err
	}
	gop := agents.NewGenericOps()
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
	return err
}
