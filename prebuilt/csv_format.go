package prebuilt

import (
	"context"
	"os"

	"github.com/cloudwego/eino/components/model"
	"github.com/curtisnewbie/miso-agent/agentloop"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
	"github.com/curtisnewbie/miso/util/strutil"
)

// CsvFormatOption configures a CsvFormatAgent.
type CsvFormatOption func(o *csvFormatConfig)

type csvFormatConfig struct {
	Name        string
	Language    string
	MaxRunSteps int
}

// WithCsvFormatLanguage sets the language for agent responses and selects the matching task prompt.
// Supported values: "Chinese" (default), any other value selects the English prompt.
func WithCsvFormatLanguage(lang string) CsvFormatOption {
	return func(o *csvFormatConfig) {
		o.Language = lang
	}
}

// WithCsvFormatMaxRunSteps sets the maximum number of graph steps before the agent terminates.
// If 0, no limit is applied.
func WithCsvFormatMaxRunSteps(n int) CsvFormatOption {
	return func(o *csvFormatConfig) {
		o.MaxRunSteps = n
	}
}

// CsvFormatAgent formats a CSV file into a RAG-optimised plain-text representation.
// Each row (or logical group of rows) is rewritten as an independent, self-contained
// paragraph suitable for semantic chunking and vector retrieval.
//
// Use [NewCsvFormatAgent] to create an instance, then call [CsvFormatAgent.Format]
// to process a file.
type CsvFormatAgent struct {
	agent *agentloop.Agent
}

// NewCsvFormatAgent compiles and returns a new CsvFormatAgent.
//
// Example:
//
//	agent, err := prebuilt.NewCsvFormatAgent(chatModel,
//	    prebuilt.WithCsvFormatMaxRunSteps(30),
//	    prebuilt.WithCsvFormatLanguage("Chinese"),
//	)
func NewCsvFormatAgent(chatModel model.ToolCallingChatModel, opts ...CsvFormatOption) (*CsvFormatAgent, error) {
	cfg := &csvFormatConfig{
		Name:        "CsvFormatAgent",
		Language:    "English",
		MaxRunSteps: 100,
	}
	for _, o := range opts {
		o(cfg)
	}

	taskPrompt := csvFormatTaskPromptEn
	if strutil.EqualAnyFold(cfg.Language, "Chinese") {
		taskPrompt = csvFormatTaskPromptCn
	}

	agent, err := agentloop.NewAgent(agentloop.AgentConfig{
		Name:           cfg.Name,
		MaxRunSteps:    cfg.MaxRunSteps,
		Language:       cfg.Language,
		Model:          chatModel,
		EnableFileTool: true,
		TaskPrompt:     taskPrompt,
	})
	if err != nil {
		return nil, errs.Wrapf(err, "failed to compile CsvFormatAgent")
	}
	return &CsvFormatAgent{agent: agent}, nil
}

// Format reads the CSV file at srcPath, runs the formatting agent, and writes the
// resulting plain-text output to dstPath.
//
// The agent reads /input/data.csv from the virtual backend, transforms the content
// into RAG-optimised paragraphs, and writes the result to /output/context.txt.
// The output file is registered as an artifact and then written to dstPath.
//
// Returns an error if the file cannot be read, the agent fails, or no artifact is produced.
func (a *CsvFormatAgent) Format(rail flow.Rail, srcPath string, dstPath string) error {
	csvContent, err := os.ReadFile(srcPath)
	if err != nil {
		return errs.Wrapf(err, "failed to read csv file: %v", srcPath)
	}

	artifactFound := false
	_, err = a.agent.Execute(rail, agentloop.AgentRequest{
		UserInput: "Analyze and format /input/data.csv file",
		PreloadBackendFiles: func(store agentloop.FileStore) error {
			return store.WriteFile(context.Background(), "/input/data.csv", csvContent)
		},
		ArtifactCallback: func(store agentloop.FileStore, artifacts []agentloop.Artifact) error {
			for _, art := range artifacts {
				content, readErr := store.ReadFile(context.Background(), art.Path)
				if readErr != nil {
					return errs.Wrapf(readErr, "failed to read artifact: %v", art.Path)
				}
				if writeErr := os.WriteFile(dstPath, content, 0o644); writeErr != nil {
					return errs.Wrapf(writeErr, "failed to write artifact to dst: %v", dstPath)
				}
				artifactFound = true
				return nil
			}
			return nil
		},
	})
	if err != nil {
		return errs.Wrapf(err, "CsvFormatAgent execution failed")
	}
	if !artifactFound {
		return errs.NewErrf("CsvFormatAgent did not produce any artifact")
	}
	return nil
}

const csvFormatTaskPromptCn = `你是一个专精于 RAG（检索增强生成）知识库构建的数据处理专家，深度理解向量检索原理和语义分片策略。

## 目标

将 /input/data.csv 重写为 /output/context.txt，使每个文本段落成为独立、完整、可被 LLM 直接理解的知识单元。输出将用于语义切片和向量检索，因此每个段落在脱离上下文时也必须能被准确理解和检索。

## 执行流程

### 第一步：探测文件结构

使用 read_file 读取前 10 行（offset=0, limit=10），识别：
- 列的含义、数据类型和主要作用
- 是否存在标题行、合并行、说明行或注脚
- 数据的主体实体是什么（例如：产品、费率、规则、用户）

若前 10 行不足以理解结构，继续读取更多行。

### 第二步：制定转换策略（先思考，再行动）

在动手转换前，明确回答以下问题：
- 数据的最小完整语义单元是什么？
- 哪些列对检索最重要？哪些列是空白或冗余的？
- 如何分组才能让每个 chunk 携带足够的上下文？

### 第三步：读取完整数据

基于对结构的理解，使用 read_file 读取 /input/data.csv 的完整内容。

### 第四步：转换并写入

使用 write_file 将重写结果写入 /output/context.txt，遵循以下规范：

**段落结构：**
- 每个语义单元独立成段，段落之间用一个空行分隔
- 段首为该单元的标识/主题（实体名、类别名等）
- 段内每个属性单独一行，格式：「属性名: 属性值」
- 属性值为空、N/A 或无意义时跳过该行
- 跨行的说明、注意事项、脚注单独成段，加前缀「说明:」或「注意:」

**格式示例（仅用于说明格式，与实际 CSV 内容无关）：**

员工A
部门: 工程部
职级: L5
入职日期: 2021-03-15
所在城市: 上海

员工B
部门: 产品部
职级: L4
入职日期: 2022-07-01
所在城市: 北京

说明:
1. 职级体系从 L1 至 L8，L5 及以上为高级职位
2. 以上数据截至 2024 年 Q4

**关键原则：**
- 宁可多写一些上下文，不要让 chunk 因缺少背景而无法被正确检索
- 保留所有对语义理解有价值的信息，包括条件、限制、说明
- 不要机械地逐行转换 —— 根据语义合并或拆分以形成最优的知识单元

### 第五步：注册 artifact

使用 add_artifact 注册 /output/context.txt。`

const csvFormatTaskPromptEn = `You are a data processing expert specializing in RAG (Retrieval-Augmented Generation) knowledge base construction, with deep understanding of vector retrieval principles and semantic chunking strategies.

## Goal

Rewrite /input/data.csv into /output/context.txt so that each text paragraph becomes an independent, complete knowledge unit that an LLM can directly understand. The output will be used for semantic chunking and vector retrieval — every paragraph must be accurately understood and retrieved even when read in isolation, without surrounding context.

## Workflow

### Step 1: Probe the File Structure

Use read_file to read the first 10 rows (offset=0, limit=10) and identify:
- The meaning, data type, and role of each column
- Whether there are header rows, merged rows, annotation rows, or footnotes
- What the primary entity of the data is (e.g. product, fee schedule, rule, user)

If 10 rows are insufficient to understand the structure, continue reading more rows.

### Step 2: Plan the Transformation Strategy (Think Before Acting)

Before starting the transformation, explicitly answer:
- What is the smallest complete semantic unit in this data?
- Which columns are most important for retrieval? Which are blank or redundant?
- How should rows be grouped so each chunk carries enough context?

### Step 3: Read the Full Data

Based on your understanding of the structure, use read_file to read the complete contents of /input/data.csv.

### Step 4: Transform and Write

Use write_file to write the rewritten content to /output/context.txt, following these rules:

**Paragraph structure:**
- Each semantic unit is a separate paragraph, with one blank line between paragraphs
- The first line of each paragraph is the unit's identifier or topic (entity name, category name, etc.)
- Each attribute occupies its own line, in the format: "Attribute: Value"
- Skip any attribute whose value is empty, N/A, or meaningless
- Cross-row notes, caveats, or footnotes form their own paragraph, prefixed with "Note:" or "Remark:"

**Format example (for illustration only — unrelated to the actual CSV content):**

Employee A
Department: Engineering
Level: L5
Start Date: 2021-03-15
City: Shanghai

Employee B
Department: Product
Level: L4
Start Date: 2022-07-01
City: Beijing

Note:
1. Levels range from L1 to L8; L5 and above are senior positions
2. Data as of Q4 2024

**Core principles:**
- Err on the side of including more context rather than less — a chunk missing background cannot be retrieved correctly
- Preserve all information with semantic value, including conditions, constraints, and explanations
- Do not mechanically convert row by row — merge or split based on semantics to form the optimal knowledge units

### Step 5: Register the Artifact

Use add_artifact to register /output/context.txt.`
