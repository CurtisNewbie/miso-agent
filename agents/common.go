package agents

type GenericOps struct {
	RepeatPrompt bool // Propmt Repeation: https://arxiv.org/html/2512.14982v1
	Now          string
	Language     string
	VisualizeDir string
	LogOnStart   bool
	LogInputs    bool
}

func NewGenericOps() *GenericOps {
	return &GenericOps{
		Language:   "English",
		LogOnStart: true,
		LogInputs:  false,
	}
}
