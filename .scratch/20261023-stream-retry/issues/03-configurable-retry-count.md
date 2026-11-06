# 03: Configurable retry count at agent level

**What to build:** The model retry count must be settable from the agent configuration, so a caller can choose its own budget (for example exactly one retry) instead of inheriting the fixed library default. Callers that do not set it keep today's behaviour.

**Blocked by:** 02

**Status:** done

- [x] The agent configuration exposes a retry count that is passed through to the model option.
- [x] An agent configured with a retry count of 1 performs exactly one retry (two attempts) end to end.
- [x] Leaving the field unset preserves the library default behaviour; existing callers that never set it are unaffected.

## Notes

- The field is `AgentConfig.ModelRetry`; it is passed to `agents.WithRetry` only when greater than zero, matching the existing conditional style used for `ModelTimeout`. Its doc comment states the semantics: a value of 1 means two attempts in total, and 0 means the library default.
- Covered by `TestAgentRetryCount` against a stub model endpoint, asserting observable request counts rather than internal wiring.