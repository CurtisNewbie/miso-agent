package agents

import (
	"github.com/cloudwego/eino/compose"
	"github.com/curtisnewbie/miso/flow"
)

func CompileGraph[T, V any](rail flow.Rail, o *genericOps, g *compose.Graph[T, V], opts ...compose.GraphCompileOption) (compose.Runnable[T, V], error) {
	if o.VisualizeDir != "" {
		opts = append(opts, compose.WithGraphCompileCallbacks(NewMermaidGenerator(o.VisualizeDir)))
	}
	return g.Compile(rail, opts...)
}
