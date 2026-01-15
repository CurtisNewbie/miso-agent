package agents

import (
	"context"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/compose"
	"github.com/curtisnewbie/miso/flow"
)

func CompileGraph[T, V any](rail flow.Rail, o *genericOps, g *compose.Graph[T, V], opts ...compose.GraphCompileOption) (compose.Runnable[T, V], error) {
	if o.VisualizeDir != "" {
		opts = append(opts, compose.WithGraphCompileCallbacks(NewMermaidGenerator(o.VisualizeDir)))
	}
	return g.Compile(rail, opts...)
}

func WithTraceCallback(name string) compose.Option {
	return compose.WithCallbacks(
		callbacks.NewHandlerBuilder().
			OnStartFn(func(ctx context.Context, ri *callbacks.RunInfo, in callbacks.CallbackInput) context.Context {
				flow.NewRail(ctx).Infof("[%v] name: %v, type: %v, component: %v, input: %v", name, ri.Name, ri.Type, ri.Component, in)
				return ctx
			}).
			Build(),
	)
}
