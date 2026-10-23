# 04: Close test-coverage gaps in the retry path

**What to build:** The retry behaviour added in tickets 01–03 must be covered by tests that exercise every branch of the changed code, with each new test proven to fail when the guarding behaviour is deliberately removed.

**Blocked by:** 02, 03 (both complete)

**Status:** done

- [x] The backoff cap in the gap function is covered directly: an attempt index whose exponential wait exceeds the cap yields the capped wait, and one below it yields the uncapped wait. Asserted without sleeping.
- [x] A drain failure that exhausts the retry budget is covered: the retried attempt also fails before delivering any chunk; the error reaches the consumer and the number of attempts equals the budget.
- [x] A panic occurring while draining an open reader is covered, and surfaces to the consumer as a stream error rather than a clean end of stream.
- [x] The attempt counter is proven shared across phases: a creation-time retry followed by a pre-delivery drain retry, with total attempts equal to the budget. Proven by a scenario in which a per-phase counter would succeed, so the test discriminates.
- [x] Line coverage of the new drain loop and the gap function reaches 100% (from 93.8% and 85.7%).
- [x] Each new test is demonstrated to go red when its corresponding guard is temporarily removed, and green again once restored.

## Notes

- Guards neutralised for the mutation evidence: the backoff cap, the exhausted-budget drain branch, the panic recover, and the shared attempt counter. Each produced a red run; `agents/model.go` was restored byte-identically (`sha256 b0f0c745…404124`) after every mutation.
- The shared-counter test asserts the **error outcome**, not just the request count: under a per-phase counter the same scenario succeeds, which is what makes the test discriminating.