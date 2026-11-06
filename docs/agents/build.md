# Build, format, test

Go modules (`go.mod`) — no other package manager.

```sh
go build ./...   # must pass with zero errors after any Go change
go vet ./...     # static analysis
go fmt ./...     # always before finishing any Go change
```

## Tests

Whole suite (~19s, no external services needed):

```sh
go test ./...
```

When iterating on one behavior, run a single test by name:

```sh
go test -run TestFunctionName -count=1 -timeout 30s ./package/...

go test -run TestAgent_shouldEvictToolResult -count=1 -timeout 30s ./agentloop/...
go test -run TestBuiltinTools_ReadFile -count=1 -timeout 30s ./agentloop/...
go test -run TestTokenizer_PruneMessagesToTokenLimit -count=1 -timeout 30s ./agentloop/...
```

One package in full:

```sh
go test -count=1 -timeout 60s ./agentloop/...
```