package agents

type GenericOps struct {
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
