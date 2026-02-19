package graph

type GenericOps struct {
	MaxRunSteps  int
	RepeatPrompt bool // Propmt Repeation: https://arxiv.org/html/2512.14982v1
	Now          string
	Language     string
	VisualizeDir string
	LogOnStart   bool
	LogOnEnd     bool
	LogInputs    bool
	LogOutputs   bool
}

func NewGenericOps() *GenericOps {
	return &GenericOps{
		Language:   "English",
		LogOnStart: true,
		LogOnEnd:   true,
		LogInputs:  false,
		LogOutputs: false,
	}
}
