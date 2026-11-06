# miso-agent

Pure Go library (`github.com/curtisnewbie/miso-agent`) for building LLM-powered ReAct agents on
[Eino](https://github.com/cloudwego/eino) and [miso](https://github.com/curtisnewbie/miso). There is
no `main.go`; consumed as a dependency through Go modules (`go.mod`).

```sh
go build ./...   # must pass after any Go change
go vet ./...
go fmt ./...     # always before finishing
go test ./...
```

## Hard rules

- Never edit `agents/models_gen.go`; regenerate with `go run agents/script/gen_models.go`.
- Use a Python script for numeric calculations, never mental arithmetic.
- New conventions and discoveries go into the matching doc below, not into this file.
- After presenting a plan, do not switch to an alternative solution without explicit confirmation.

## Docs

| Read when | File |
|-----------|------|
| building, vetting, formatting, testing | `docs/agents/build.md` |
| writing Go — imports, naming, doc comments | `docs/agents/code-style.md` |
| handling errors, logging, `flow.Rail` | `docs/agents/errors-and-logging.md` |
| touching packages, graphs, tools, options | `docs/agents/architecture.md` |
| writing tests | `docs/agents/testing.md` |
| using a pre-built agent | `docs/prebuilt/` |
| editing prompt assets (local only) | `docs/prompts/` |
| design docs, or checking prior notes before a non-trivial task | `docs/design/` (not created yet) |
| repo workflow config | `docs/agents/` — issue tracker, triage labels, domain, code review |
| architecture decisions | `docs/adr/` (not created yet) |

## Agent skills

### Issue tracker

Local markdown under `.scratch/<YYYYMMDD>-<feature-slug>/`: spec at `spec.md`, tickets at `issues/NN-<slug>.md`. See `docs/agents/issue-tracker.md`.

### Triage labels

The five default triage roles (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `GLOSSARY.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.

### Code review

Read before writing and when reviewing code. See `docs/agents/code-review.md`.