# Spec: Retry model calls across stream consumption

Status: ready-for-agent

## Problem Statement

When the library streams a completion and that stream fails *after* the call returned — most commonly an HTTP client timeout while reading the response body — the retry layer never sees it, because retry only wraps the moment the stream is created. A single slow or stalled completion therefore fails the entire agent run.

A caller has no lever for this. The only adjustable knob is the per-attempt timeout, and raising it does not produce a retry — it just moves where the single attempt is cut. The retry count is a fixed library default that callers cannot override.

## Solution

Make the model retry unit cover the **stream's whole lifetime**, not just its creation. A streamed completion is retried as a unit: issue the call, then drain the response. If draining fails before any content has reached the consumer, the call is retried transparently within the same retry budget already used for creation failures.

Semantics are deliberately conservative: once the consumer has observed any chunk, the stream cannot be silently retried, because a re-issued completion starts from zero and replaying it would duplicate or replace output the consumer already saw. In that case the error propagates as it does today.

The retry count must also become configurable through agent configuration, so a caller can ask for exactly one retry instead of the fixed default.

## User Stories

1. As a library consumer, I want a model call that fails before producing any output to be retried automatically, so that a transient latency spike does not fail the agent run.
2. As a library consumer, I want the retry count to be configurable per agent, so that I can choose a budget that fits my own request deadline.
3. As a library consumer, I want retries to stop when the request context is already done, so that a dead context does not burn the retry budget and delay the failure.
4. As a library consumer, I want a stream that fails after partial output to fail fast rather than silently replay, so that consumers never receive duplicated or replaced content.
5. As a library consumer, I want to be able to bound total latency as (per-attempt timeout x (1 + retries)) plus backoff, so that an agent never outlives the caller's own timeout.
6. As a library consumer, I want the existing retry option to keep working for non-streaming calls, so that no current behaviour regresses.
7. As a library consumer, I want the retry option to be reachable from the agent configuration, so that I do not have to construct the model myself to set it.
8. As a library consumer, I want a panic inside the streaming pipeline to surface as a stream error rather than a silent truncation, so that I cannot mistake a crash for a complete answer.
9. As a library consumer, I want the retried attempt to release the abandoned response body, so that retries do not leak connections or goroutines.
10. As a library consumer, I want a consumer that stops reading early to release the pipeline, so that abandoned streams do not leak goroutines.
11. As a library consumer, I want the attempt counter shared across creation failures and consumption failures, so that the configured count is a total budget, not a per-phase one.
12. As a library consumer, I want the retry backoff to remain exponential and capped, so that behaviour is unchanged for the cases that already retried.
13. As a library consumer, I want streaming backpressure preserved, so that a slow consumer still throttles the upstream provider.
14. As a library consumer, I want the failure still surfaced as an error when the retry budget is exhausted, so that existing error handling on my side continues to work.
15. As a library consumer, I want retries logged with which attempt failed and the wait before the next, so that I can tell a retried success apart from a first-try success.
16. As a library maintainer, I want the streaming change covered by tests that drive a real streaming client against a local server, so that the tests exercise the same code path as production.
17. As a library maintainer, I want a test that proves a mid-stream failure before the first chunk is retried, so that the new behaviour cannot silently regress.
18. As a library maintainer, I want a test that proves a mid-stream failure after a chunk is **not** retried, so that the deliberate no-replay rule is locked in.
19. As a library maintainer, I want a test that proves a creation-time failure is still retried, so that pre-existing behaviour is protected.
20. As a library maintainer, I want the timeout case expressible with a small, deterministic timeout, so that the test suite stays fast.

## Implementation Decisions

- **Retry unit.** The retry boundary moves from "the call that returns a stream" to "issue the call, then drain the stream". The wrapper decorates the model interface it already decorates; no new abstraction is introduced, and no eino internals are touched.
- **No-replay rule.** Retry is only permitted while zero chunks have been delivered to the consumer. Delivery is defined as the consumer having successfully received a chunk (with an unbuffered pipe, after the send completes), not merely the wrapper having read one. This is the single most important behavioural decision and must be preserved by any refactor.
- **Shared attempt counter.** One counter spans creation failures and consumption failures, so the configured retry count is a total budget.
- **Timeout interaction.** The model timeout bounds a single attempt, so worst-case wall clock is `timeout x (1 + retries) + backoff`. Callers are responsible for keeping that inside their own deadline; this is documented, not enforced.
- **Context awareness.** The gap function must refuse to retry when the context is already done. The current implementation retries unconditionally, which wastes time and logs misleading retries.
- **Panic handling.** A panic inside the driving task must be converted into an error pushed onto the stream, because the concurrency helper only logs recovered panics; without this a crash would look like a clean end of stream.
- **Error delivery.** Errors are sent on the stream and the driving task returns success, so the failure is reported once, not both on the stream and in the log.
- **Concurrency helper.** The driving task is started with the project's async helper (`async.Fire`) rather than a bare goroutine, so panics are recovered and a rail is available for logging.
- **Resource release.** The abandoned reader is closed before a retry; the driving task closes the writer on every exit path; a consumer that stops reading causes the task to stop and release.
- **Backpressure.** The pipe is unbuffered, preserving end-to-end backpressure.
- **Agent-level configuration.** The agent configuration gains a retry count field, plumbed to the existing model option. Defaults remain the library default so existing agents are unaffected.

The core decision, as a state machine (trimmed; this shape was worked out during design, not lifted from a prototype):

```
loop:
  reader = call()                       # retried on error via the shared gap function
  delivered = false
  loop:
    chunk, err = reader.recv()
    err == EOF        -> done            # success
    err != nil && delivered -> emit err, done        # cannot replay
    err != nil            -> close reader; backoff; retry whole call (shared counter)
    emit chunk; if consumer gone -> close reader, done
    delivered = true
```

## Testing Decisions

- **What makes a good test here.** Test observable behaviour at the public boundary: what the consumer receives, and how many times the server was asked for a completion. Do not assert on internal state, the attempt counter, or the concrete wrapper type.
- **Highest available seam, exactly one.** Drive the public chat-model constructor (with a small timeout and a retry count of one) against a local HTTP server that speaks SSE. The base URL is already configurable, so the real streaming client and the real stream-consumption path are exercised, including the exact error site observed in production. No internal-interface fakes.
- **Cases.**
  - *A — fail before the first chunk, then succeed:* server stalls past the small model timeout on the first request and serves a normal stream on the second. Assert two server requests and that the consumer receives exactly the successful output with no duplicated prefix.
  - *B — fail after a chunk:* server emits one chunk then fails. Assert exactly one server request and that the error reaches the consumer.
  - *C — fail at creation:* server refuses the first request and serves the second. Assert the pre-existing retry behaviour still holds.
- **Determinism and speed.** A small explicit model timeout (sub-second) makes the timeout cases fast and stable; no reliance on real provider latency.
- **Prior art.** There is no existing test suite in the model-factory package, so this establishes it. Neighbouring agent-loop tests are plain table-driven Go tests, and the same style applies.

## Out of Scope

- Changing eino or its OpenAI client; the fix lives in the wrapper this library owns.
- Retrying after partial output has been delivered (explicitly rejected, not deferred).
- Any change to non-streaming call behaviour beyond the context-aware gap function.
- Choosing concrete retry counts and timeouts for a particular agent — that is a caller decision; this library only makes both settable.

## Further Notes

- Origin: a streamed model completion aborted with an HTTP client timeout while reading the response body, after exactly one attempt, with no retry — because the failure occurred after the stream was created.
- Retry count today is a fixed library default of 5, and it is not reachable from the agent configuration; the backoff is 1s/2s/4s/8s capped at 10s.
- The retry budget only helps when paired with a per-attempt timeout: worst case is `timeout x (1 + retries)` plus backoff, and the caller must keep that inside its own deadline.