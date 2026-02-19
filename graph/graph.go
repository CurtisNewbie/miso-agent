package graph

import (
	"context"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/curtisnewbie/miso/flow"
)

func CompileGraph[T, V any](o *GenericOps, g *compose.Graph[T, V], opts ...compose.GraphCompileOption) (compose.Runnable[T, V], error) {
	if o != nil {
		if o.VisualizeDir != "" {
			opts = append(opts, compose.WithGraphCompileCallbacks(NewMermaidGenerator(o.VisualizeDir)))
		}
		if o.MaxRunSteps > 0 {
			opts = append(opts, compose.WithMaxRunSteps(o.MaxRunSteps))
		}
	}
	return g.Compile(context.Background(), opts...)
}

func WithTraceCallback(name string, genops *GenericOps) compose.Option {
	b := callbacks.NewHandlerBuilder()
	if genops.LogOnStart {
		b = b.OnStartFn(func(ctx context.Context, ri *callbacks.RunInfo, in callbacks.CallbackInput) context.Context {
			if genops.LogInputs {
				flow.NewRail(ctx).Infof("Graph exec %v start, name: %v, type: %v, component: %v, input: %v", name, ri.Name, ri.Type, ri.Component, in)
			} else {
				flow.NewRail(ctx).Infof("Graph exec %v start, name: %v, type: %v, component: %v", name, ri.Name, ri.Type, ri.Component)
			}
			return ctx
		})
	}
	if genops.LogOnEnd {
		b = b.OnEndFn(func(ctx context.Context, ri *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			inToken, outToken, ok := tokenUsage(output)
			if ok {
				if genops.LogOutputs {
					flow.NewRail(ctx).Infof("Graph exec %v end, name: %v, type: %v, component: %v, output: %v, usage: %v (input), %v (output)", name, ri.Name,
						ri.Type, ri.Component, output, inToken, outToken)
				} else {
					flow.NewRail(ctx).Infof("Graph exec %v end, name: %v, type: %v, component: %v, usage: %v (input), %v (output)", name, ri.Name,
						ri.Type, ri.Component, inToken, outToken)
				}
			}
			return ctx
		})
	}
	return compose.WithCallbacks(b.Build())
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

// InvokeGraph invokes a compiled graph with trace callbacks enabled.
// This is a convenience function that automatically adds WithTraceCallback
// to enable token usage tracking and execution logging based on GenericOps settings.
//
// Parameters:
//   - rail: Execution context for logging
//   - genops: Generic operations config (controls trace callback behavior)
//   - graph: Compiled graph runnable
//   - graphName: Name of the graph for logging purposes
//   - input: Input to the graph
//   - opts: Additional compose options (e.g., callbacks, config)
//
// Returns:
//   - Output from the graph
//   - Error if invocation fails
func InvokeGraph[T, V any](rail flow.Rail, genops *GenericOps, graph compose.Runnable[T, V], graphName string, input T, opts ...compose.Option) (V, error) {
	// Add trace callback if LogOnStart is enabled
	if genops != nil && (genops.LogOnStart || genops.LogOnEnd) {
		opts = append(opts, WithTraceCallback(graphName, genops))
	}

	return graph.Invoke(rail, input, opts...)
}
