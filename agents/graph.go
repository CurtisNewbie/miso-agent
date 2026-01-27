package agents

import (
	"context"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/curtisnewbie/miso/flow"
)

func CompileGraph[T, V any](rail flow.Rail, o *GenericOps, g *compose.Graph[T, V], opts ...compose.GraphCompileOption) (compose.Runnable[T, V], error) {
	if o.VisualizeDir != "" {
		opts = append(opts, compose.WithGraphCompileCallbacks(NewMermaidGenerator(o.VisualizeDir)))
	}
	if o.MaxRunSteps > 0 {
		opts = append(opts, compose.WithMaxRunSteps(o.MaxRunSteps))
	}
	return g.Compile(rail, opts...)
}

func WithTraceCallback(name string, logInputs bool) compose.Option {
	return compose.WithCallbacks(
		callbacks.NewHandlerBuilder().
			OnStartFn(func(ctx context.Context, ri *callbacks.RunInfo, in callbacks.CallbackInput) context.Context {
				if logInputs {
					flow.NewRail(ctx).Infof("Graph exec %v start, name: %v, type: %v, component: %v, input: %v", name, ri.Name, ri.Type, ri.Component, in)
				} else {
					flow.NewRail(ctx).Infof("Graph exec %v start, name: %v, type: %v, component: %v", name, ri.Name, ri.Type, ri.Component)
				}
				return ctx
			}).
			OnEndFn(func(ctx context.Context, ri *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
				inToken, outToken, ok := tokenUsage(output)
				if ok {
					flow.NewRail(ctx).Infof("Graph exec %v end, name: %v, type: %v, component: %v, usage: %v (input), %v (output)", name, ri.Name,
						ri.Type, ri.Component, inToken, outToken)
				}
				return ctx
			}).
			Build(),
	)
}

func tokenUsage(in callbacks.CallbackOutput) (_in int, _out int, ok bool) {
	switch m := in.(type) {
	case *model.CallbackOutput:
		if m.TokenUsage != nil {
			return m.TokenUsage.PromptTokens, m.TokenUsage.CompletionTokens, true
		}
	}
	return 0, 0, false
}
