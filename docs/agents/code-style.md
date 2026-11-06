# Code style

## Import organization

Three blocks separated by blank lines, in this order:

```go
import (
    // 1. Standard library
    "context"
    "fmt"
    "strings"
    "sync"

    // 2. Third-party dependencies
    "github.com/cloudwego/eino/compose"
    "github.com/cloudwego/eino/schema"
    "gopkg.in/yaml.v2"

    // 3. Internal packages + miso framework (treated as one group)
    "github.com/curtisnewbie/miso-agent/graph"
    "github.com/curtisnewbie/miso/errs"
    "github.com/curtisnewbie/miso/flow"
    "github.com/curtisnewbie/miso/util/strutil"
)
```

## Naming

| Category | Convention | Examples |
|----------|-----------|---------|
| Structs (exported) | `PascalCase`, noun-based | `Agent`, `AgentConfig`, `ToolRegistry`, `FileStore` |
| Structs (unexported) | `camelCase` | `ctxKey`, `agentLoopState`, `taskInput`, `openAiModelConfig` |
| Interfaces | `PascalCase`, behavior-named, no `I` prefix | `FileStore`, `Tool`, `SelfInvokeTool` |
| Constructors | `NewXxx(...)` | `NewAgent`, `NewToolRegistry`, `NewTmpFileStore` |
| Functional options | `WithXxx(...)` returning `func(o *Config)` | `WithTemperature`, `WithMaxToken`, `WithRetry` |
| Methods | verb-first | `Execute`, `Build`, `Load`, `Register`, `AddTodo` |
| Exported variables | `PascalCase` | `DeepseekBaseURL`, `BasePrompt` |
| Unexported variables | `camelCase` | `agentCtxKey`, `finishToolName`, `modelMaxToken` |
| Constants | `PascalCase` (exported), `camelCase` (unexported) | `BasePrompt`, `maxToken32k` |
| Files | `snake_case` | `skill_loader.go`, `tools_builtin.go`, `tools_artifact.go` |
| Input/Output structs | `XxxInput` / `XxxOutput` | `RuleMatcherInput`, `MaterialExtractOutput` |
| Args structs for tools | `XxxArgs` | `ReadFileArgs`, `WriteFileArgs` |

## Doc comments

- Every **exported** type, function, and method has a Go doc comment beginning with its name.
- Comments are concise (typically one sentence); multi-line lists use `//   -` bullets.
- Cross-references use bracket notation: `// See [NewToolFunc], [StringParam]`.
- Include an `// Example:` code block in doc comments for non-obvious factory functions.
- Struct fields that represent configuration are individually documented.