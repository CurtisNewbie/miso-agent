package agentloop

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

// ChatModel metrics, labeled by model name:
//
//   - llm_requests_total                — number of ChatModel calls
//   - llm_input_cached_tokens_total     — input tokens served from the provider cache (cache hit)
//   - llm_input_uncached_tokens_total   — input tokens not served from the cache (cache miss)
//   - llm_output_tokens_total           — output tokens
//
// PromQL examples:
//
//   - cache hit rate:    rate(llm_input_cached_tokens_total[5m]) / (rate(llm_input_cached_tokens_total[5m]) + rate(llm_input_uncached_tokens_total[5m]))
//   - tokens burned/sec: rate(llm_input_cached_tokens_total[5m]) + rate(llm_input_uncached_tokens_total[5m]) + rate(llm_output_tokens_total[5m])
var (
	chatModelRequests      = newCounterVec("llm_requests_total", "model")
	chatModelInputCached   = newCounterVec("llm_input_cached_tokens_total", "model")
	chatModelInputUncached = newCounterVec("llm_input_uncached_tokens_total", "model")
	chatModelOutput        = newCounterVec("llm_output_tokens_total", "model")
)

// newCounterVec creates a CounterVec labeled by label and registers it to the
// prometheus.DefaultRegisterer. Panics on duplicate registration, mirroring
// miso.NewPromCounter.
func newCounterVec(name, label string) *prometheus.CounterVec {
	vec := prometheus.NewCounterVec(prometheus.CounterOpts{Name: name}, []string{label})
	if err := prometheus.DefaultRegisterer.Register(vec); err != nil {
		panic(fmt.Errorf("failed to register counter vec %v, %w", name, err))
	}
	return vec
}

// recordChatModelMetrics records request/token counters for one ChatModel call.
// All calls are counted, including the first. Input tokens are split into cache
// hit (served from the provider cache) and cache miss (freshly billed) portions.
func recordChatModelMetrics(modelName string, promptTokens, completionTokens, cachedTokens int) {
	if modelName == "" || promptTokens <= 0 {
		return
	}
	uncached := promptTokens - cachedTokens
	if uncached < 0 {
		// Guard against provider misreporting; a negative Add would panic the counter.
		uncached = 0
	}
	chatModelRequests.WithLabelValues(modelName).Inc()
	chatModelInputCached.WithLabelValues(modelName).Add(float64(cachedTokens))
	chatModelInputUncached.WithLabelValues(modelName).Add(float64(uncached))
	chatModelOutput.WithLabelValues(modelName).Add(float64(completionTokens))
}
