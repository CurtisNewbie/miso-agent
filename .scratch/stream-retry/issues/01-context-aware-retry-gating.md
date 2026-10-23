# 01: Context-aware retry gating

**What to build:** The retry decision that gates model-call retries must stop retrying as soon as the request context is already done, so a dead context surfaces its error immediately instead of sleeping through backoff first. This gating is shared by streaming and non-streaming model calls, so the fix applies to both.

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] When the context is already done (cancelled or deadline exceeded) and a model call fails, the call is attempted exactly once and the error is returned; no backoff sleep is performed.
- [ ] When the context is live, retry behaviour is unchanged: the same number of attempts and the same exponential backoff (1s, 2s, 4s, 8s, capped at 10s).
- [ ] Covered by a test that drives a failing call with an already-done context and asserts a single attempt with no further delay.