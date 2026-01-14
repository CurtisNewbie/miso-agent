package agents

type genericOps struct {
	Language string
}

func NewGenericOps() *genericOps {
	return &genericOps{
		Language: "English",
	}
}
