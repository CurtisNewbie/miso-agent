package agents

type genericOps struct {
	Language     string
	VisualizeDir string
	LogOnStart   bool
	LogInputs    bool
}

func NewGenericOps() *genericOps {
	return &genericOps{
		Language:   "English",
		LogOnStart: true,
		LogInputs:  false,
	}
}
