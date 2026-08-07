package agentloop

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordChatModelMetrics(t *testing.T) {
	const model = "test-model"

	// Use a distinct label per test to avoid cross-test counter contamination.
	metrics := []struct {
		name  string
		value func() float64
	}{
		{"requests", func() float64 {
			m, err := chatModelRequests.GetMetricWithLabelValues(model)
			if err != nil {
				t.Fatalf("GetMetricWithLabelValues: %v", err)
			}
			return testutil.ToFloat64(m)
		}},
		{"input cached", func() float64 {
			m, err := chatModelInputCached.GetMetricWithLabelValues(model)
			if err != nil {
				t.Fatalf("GetMetricWithLabelValues: %v", err)
			}
			return testutil.ToFloat64(m)
		}},
		{"input uncached", func() float64 {
			m, err := chatModelInputUncached.GetMetricWithLabelValues(model)
			if err != nil {
				t.Fatalf("GetMetricWithLabelValues: %v", err)
			}
			return testutil.ToFloat64(m)
		}},
		{"output", func() float64 {
			m, err := chatModelOutput.GetMetricWithLabelValues(model)
			if err != nil {
				t.Fatalf("GetMetricWithLabelValues: %v", err)
			}
			return testutil.ToFloat64(m)
		}},
	}
	read := func(name string) float64 {
		for _, mm := range metrics {
			if mm.name == name {
				return mm.value()
			}
		}
		t.Fatalf("unknown metric %q", name)
		return 0
	}

	tests := []struct {
		name            string
		modelName       string
		promptTokens    int
		completionToken int
		cachedTokens    int
		wantRequests    float64
		wantCached      float64
		wantUncached    float64
		wantOutput      float64
		description     string
	}{
		{name: "record first call", modelName: model, promptTokens: 5000, completionToken: 200, cachedTokens: 0, wantRequests: 1, wantCached: 0, wantUncached: 5000, wantOutput: 200, description: "first call counted in total, contributes 0 cached tokens"},
		{name: "record cache hit", modelName: model, promptTokens: 4000, completionToken: 300, cachedTokens: 3000, wantRequests: 1, wantCached: 3000, wantUncached: 1000, wantOutput: 300, description: "input split into hit/miss portions"},
		{name: "clamp negative uncached", modelName: model, promptTokens: 1000, completionToken: 50, cachedTokens: 2000, wantRequests: 1, wantCached: 2000, wantUncached: 0, wantOutput: 50, description: "provider misreport must not panic counters"},
		{name: "skip empty model name", modelName: "", promptTokens: 1000, completionToken: 100, cachedTokens: 500, wantRequests: 0, wantCached: 0, wantUncached: 0, wantOutput: 0, description: "no label, must not record"},
		{name: "skip zero prompt tokens", modelName: model, promptTokens: 0, completionToken: 0, cachedTokens: 0, wantRequests: 0, wantCached: 0, wantUncached: 0, wantOutput: 0, description: "no usage reported, must not record"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := map[string]float64{
				"requests":       read("requests"),
				"input cached":   read("input cached"),
				"input uncached": read("input uncached"),
				"output":         read("output"),
			}
			recordChatModelMetrics(tt.modelName, tt.promptTokens, tt.completionToken, tt.cachedTokens)
			want := map[string]float64{
				"requests":       tt.wantRequests,
				"input cached":   tt.wantCached,
				"input uncached": tt.wantUncached,
				"output":         tt.wantOutput,
			}
			for name, w := range want {
				if got := read(name) - before[name]; got != w {
					t.Errorf("%s = %v, want %v", name, got, w)
				}
			}
		})
	}
}
