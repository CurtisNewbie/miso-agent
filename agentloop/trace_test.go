package agentloop

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	einotool "github.com/cloudwego/eino/components/tool"
)

func TestBuildTraceHandler_ToolEventCallback(t *testing.T) {
	tests := []struct {
		name           string
		runInfo        *callbacks.RunInfo
		input          callbacks.CallbackInput
		wantEventCount int
		wantToolName   string
		wantArgs       string
	}{
		{
			name: "emits call event for Tool component",
			runInfo: &callbacks.RunInfo{
				Name:      "read_file",
				Component: components.Component("Tool"),
			},
			input:          &einotool.CallbackInput{ArgumentsInJSON: `{"path":"/foo.txt"}`},
			wantEventCount: 1,
			wantToolName:   "read_file",
			wantArgs:       `{"path":"/foo.txt"}`,
		},
		{
			name: "no event when component is not Tool",
			runInfo: &callbacks.RunInfo{
				Name:      "some_node",
				Component: components.Component("ChatModel"),
			},
			input:          &einotool.CallbackInput{ArgumentsInJSON: `{}`},
			wantEventCount: 0,
		},
		{
			name: "handles nil CallbackInput gracefully",
			runInfo: &callbacks.RunInfo{
				Name:      "write_file",
				Component: components.Component("Tool"),
			},
			input:          nil,
			wantEventCount: 1,
			wantToolName:   "write_file",
			wantArgs:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var received []ToolEvent
			ops := agentOps{
				toolEventCallback: func(event ToolEvent) {
					received = append(received, event)
				},
			}

			handler := buildTraceHandler("test-agent", ops)
			handler.OnStart(context.Background(), tt.runInfo, tt.input)

			if len(received) != tt.wantEventCount {
				t.Errorf("got %d events, want %d", len(received), tt.wantEventCount)
				return
			}
			if tt.wantEventCount == 0 {
				return
			}
			ev := received[0]
			if ev.Kind != ToolEventKindCall {
				t.Errorf("event Kind = %q, want %q", ev.Kind, ToolEventKindCall)
			}
			if ev.Name != tt.wantToolName {
				t.Errorf("event Name = %q, want %q", ev.Name, tt.wantToolName)
			}
			if ev.Args != tt.wantArgs {
				t.Errorf("event Args = %q, want %q", ev.Args, tt.wantArgs)
			}
		})
	}
}

func TestBuildTraceHandler_ToolResultEvent(t *testing.T) {
	toolRunInfo := &callbacks.RunInfo{
		Name:      "read_file",
		Component: components.Component("Tool"),
	}

	t.Run("emits result event on OnEnd for Tool component", func(t *testing.T) {
		var received []ToolEvent
		ops := agentOps{
			toolEventCallback: func(event ToolEvent) {
				received = append(received, event)
			},
		}
		handler := buildTraceHandler("test-agent", ops)
		handler.OnEnd(context.Background(), toolRunInfo, nil)

		if len(received) != 1 {
			t.Fatalf("got %d events, want 1", len(received))
		}
		if received[0].Kind != ToolEventKindResult {
			t.Errorf("Kind = %q, want %q", received[0].Kind, ToolEventKindResult)
		}
		if received[0].Name != "read_file" {
			t.Errorf("Name = %q, want %q", received[0].Name, "read_file")
		}
	})

	t.Run("result event fires even when logOnEnd is also enabled", func(t *testing.T) {
		var received []ToolEvent
		ops := agentOps{
			logOnEnd: true,
			toolEventCallback: func(event ToolEvent) {
				received = append(received, event)
			},
		}
		handler := buildTraceHandler("test-agent", ops)
		handler.OnEnd(context.Background(), toolRunInfo, nil)

		if len(received) != 1 {
			t.Fatalf("got %d events, want 1 (logOnEnd must not overwrite toolEventCallback)", len(received))
		}
		if received[0].Kind != ToolEventKindResult {
			t.Errorf("Kind = %q, want %q", received[0].Kind, ToolEventKindResult)
		}
	})
}
