# 02: Retry a streamed completion that fails before any chunk

**What to build:** An agent run whose first model completion stalls and fails before producing any output must be retried transparently, so the run recovers and the customer gets an answer instead of the caller receiving an error. The retry unit becomes "issue the call, then drain the stream" rather than just "issue the call".

**Blocked by:** 01

**Status:** done

- [x] A streaming completion that fails before any chunk reaches the consumer is retried within the same retry budget; on success the consumer receives exactly the successful output, with no duplicated prefix.
- [x] The number of completions requested from the provider equals the number of attempts made (two, in the fail-then-succeed case).
- [x] A failure that occurs after a chunk has been delivered to the consumer is **not** retried; the error propagates to the consumer unchanged.
- [x] A failure at stream creation is still retried (pre-existing behaviour preserved).
- [x] The abandoned response stream is released before a retry is issued.
- [x] A consumer that stops reading early causes the driving task to stop and release; no goroutine or connection leak.
- [x] A panic in the driving task is delivered to the consumer as a stream error, not silently converted into a clean end of stream.
- [x] The attempt counter is shared across creation failures and consumption failures, so the configured retry count is a total budget rather than a per-phase one.
- [x] End-to-end backpressure is preserved (unbuffered pipe).
- [x] Verified at the public chat-model constructor against a local SSE server using a sub-second per-attempt timeout, so the timeout cases are fast and deterministic.

## Notes

- Covered by `TestStreamRetry` (fail-before-chunk, fail-after-chunk, fail-at-creation, and `Generate` routing through the same path), plus `TestStreamRetry_PanicWhileDraining`, `TestStreamRetry_ConsumerStopsReading`, `TestStreamRetry_DrainBudgetExhausted` and `TestStreamRetry_AttemptCounterSharedAcrossPhases_Exhausts`.
- Two deliberate test choices worth knowing: the post-chunk failure is injected as an in-band SSE error rather than a TCP reset (a clean FIN is reported as `io.EOF`, a *clean* end, which would not represent a failure; the reset variant is racy); and the shared-counter test asserts the **error outcome**, because a per-phase counter would succeed in the same scenario.
- Line coverage of the drain loop and the gap function is 100%, with mutation evidence recorded on ticket 04.