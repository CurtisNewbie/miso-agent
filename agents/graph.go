package agents

import (
	"github.com/cloudwego/eino/compose"
	"github.com/curtisnewbie/miso/flow"
)

func CompileGraph[T, V any](rail flow.Rail, o *genericOps, g *compose.Graph[T, V]) (compose.Runnable[T, V], error) {
	if o.VisualizeDir != "" {
		return g.Compile(rail, compose.WithGraphCompileCallbacks(NewMermaidGenerator(o.VisualizeDir)))
	} else {
		return g.Compile(rail)
	}
}
