# 01: Context-aware retry gating

**What to build:** The retry decision that gates model-call retries must stop retrying as soon as the request context is already done, so a dead context surfaces its error immediately instead of sleeping through backoff first. This gating is shared by streaming and non-streaming model calls, so the fix applies to both.

**Blocked by:** None (can start immediately)

**Status:** done

- [x] When the context is already done (cancelled or deadline exceeded) and a model call fails, the call is attempted exactly once and the error is returned; no backoff sleep is performed.
- [x] When the context is live, retry behaviour is unchanged: the same number of attempts and the same exponential backoff (1s, 2s, 4s, 8s, capped at 10s).
- [x] Covered by a test that drives a failing call with an already-done context and asserts a single attempt with no further delay.

## Notes

- Covered by `TestContextAwareRetry` (both entry points, `Generate` and `Stream`); the dead-context cases complete in ~0.00s, the live-context cases sleep the expected 1s.
- The gap function is also covered directly by `TestRetryGapFunc_BackoffCap`, including the 10s cap and the done-context outcome.