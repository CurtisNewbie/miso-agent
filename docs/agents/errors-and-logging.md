# Errors and logging

## Errors

Use `github.com/curtisnewbie/miso/errs`. Do not use `fmt.Errorf` (except `%w` in stdlib-only
contexts).

```go
return errs.NewErrf("file not found: %s", path)
return errs.NewErrf("task cannot be empty")

return nil, errs.Wrapf(err, "failed to load skills from %s", source)
return nil, errs.Wrapf(err, "failed to initialize tokenizer for model %s", config.Model)

return nil, errs.Wrap(err)
```

- Return errors up the call stack; never log-and-swallow.
- Functions that may fail return `(value, error)` tuples.
- Tool execution functions return `(string, error)` — the string is the LLM-visible result.
- Never `panic` for recoverable errors.

## Logging and context

Every function that touches the agent loop receives `flow.Rail` as its **first parameter**. `Rail`
is the miso context carrier for logging, tracing, and span management.

```go
rail.Infof("loaded %d skills from %s", len(skills), source)
rail.Debugf("pruning messages to fit token limit %d", limit)
rail.Warnf("skill file %s has no frontmatter", path)
rail.Errorf("tool execution failed: %v", err)

start := time.Now()
defer rail.TimeOp(start, "execute_tool")

childRail := rail.NextSpan()
```