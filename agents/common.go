package agents

type genericOps struct {
	Language     string
	VisualizeDir string
}

func NewGenericOps() *genericOps {
	return &genericOps{
		Language: "English",
	}
}
