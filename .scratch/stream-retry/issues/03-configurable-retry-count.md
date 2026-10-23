# 03: Configurable retry count at agent level

**What to build:** The model retry count must be settable from the agent configuration, so a caller can choose its own budget (for example exactly one retry) instead of inheriting the fixed library default. Callers that do not set it keep today's behaviour.

**Blocked by:** 02

**Status:** ready-for-agent

- [ ] The agent configuration exposes a retry count that is passed through to the model option.
- [ ] An agent configured with a retry count of 1 performs exactly one retry (two attempts) end to end.
- [ ] Leaving the field unset preserves the library default behaviour; existing callers that never set it are unaffected.