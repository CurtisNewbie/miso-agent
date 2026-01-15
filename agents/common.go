package agents

type genericOps struct {
	Language     string
	VisualizeDir string
	LogOnStart   bool
}

func NewGenericOps() *genericOps {
	return &genericOps{
		Language:   "English",
		LogOnStart: true,
	}
}
