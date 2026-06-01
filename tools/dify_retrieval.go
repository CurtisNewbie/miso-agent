package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso-dify/dify"
	"github.com/curtisnewbie/miso/flow"

	"github.com/curtisnewbie/miso-agent/agentloop"
)

// DifyRetrievalArgs are the typed arguments for the dify_retrieval tool.
type DifyRetrievalArgs struct {
	// Query is the search query used to retrieve relevant segments. Required.
	Query string `json:"query"`
}

// DifyRetrievalConfig holds the caller-controlled parameters for NewDifyRetrievalTool.
// These are fixed at construction time and never exposed to the LLM.
type DifyRetrievalConfig struct {
	// Host is the base URL of the Dify API (e.g. "https://api.dify.ai").
	Host string

	// ApiKey is the Dify dataset API key.
	ApiKey string

	// DatasetId is the ID of the Dify knowledge base (dataset) to query.
	DatasetId string

	// RetrievalModel controls retrieval behaviour (TopK, search method, reranking, etc.).
	// If nil, Dify will use the dataset's default retrieval settings.
	RetrievalModel *dify.RetrieveModelParam
}

// DifyRetrievalOption is a functional option that customises the tool after
// DifyRetrievalConfig has been applied.
type DifyRetrievalOption func(o *difyRetrievalOpts)

// difyRetrievalOpts holds the mutable settings controlled by DifyRetrievalOption.
type difyRetrievalOpts struct {
	name          string
	description   string
	metadataKey   string // non-empty → append RetrievedRecords to AgentContext.Metadata
	docNameFilter *dify.MetadataFilteringCondition
}

// WithDifyToolName overrides the tool name that is registered with the LLM.
// Useful when an agent uses multiple Dify knowledge bases simultaneously and
// each tool needs a distinct, descriptive name.
func WithDifyToolName(name string) DifyRetrievalOption {
	return func(o *difyRetrievalOpts) { o.name = name }
}

// WithDifyToolDescription overrides the tool description shown to the LLM.
func WithDifyToolDescription(desc string) DifyRetrievalOption {
	return func(o *difyRetrievalOpts) { o.description = desc }
}

// WithDifyMetadataAppendKey enables post-retrieval record collection.
// After each successful Dify call the raw []dify.RetrievedRecord slice is
// appended to AgentContext.Metadata under metaKey. The caller can read it
// back from TaskOutput.Metadata after Execute returns:
//
//	const refsKey = "rag_refs"
//	out, _ := agent.Execute(rail, req)
//	refs, _ := agentloop.GetMeta[[]dify.RetrievedRecord](out.Metadata, refsKey)
func WithDifyMetadataAppendKey(metaKey string) DifyRetrievalOption {
	return func(o *difyRetrievalOpts) { o.metadataKey = metaKey }
}

// WithDifyDocNameFilter adds a document_name metadata filtering condition to the
// retrieval request. operator is one of the Dify comparison operators:
// "contains", "not contains", "start with", "end with", "is", "is not", etc.
// value is the keyword to match against document names.
//
// This is a convenience wrapper for the common FAQ / knowledge-base split pattern
// where one tool queries only documents whose name contains a keyword and another
// queries everything else:
//
//	tools.NewDifyRetrievalTool(cfg,
//	    tools.WithDifyToolName("query_faq"),
//	    tools.WithDifyDocNameFilter("FAQ", "contains"),
//	)
//	tools.NewDifyRetrievalTool(cfg,
//	    tools.WithDifyToolName("query_knowledge_base"),
//	    tools.WithDifyDocNameFilter("FAQ", "not contains"),
//	)
//
// If the config already has a RetrievalModel with MetadataFilteringConditions,
// the new condition is appended (logical operator defaults to "and").
func WithDifyDocNameFilter(value, operator string) DifyRetrievalOption {
	return func(o *difyRetrievalOpts) {
		o.docNameFilter = &dify.MetadataFilteringCondition{
			Name:               "document_name",
			ComparisonOperator: operator,
			Value:              value,
		}
	}
}

// NewDifyRetrievalTool creates an agentloop Tool that retrieves relevant document
// segments from a Dify knowledge base. Host, ApiKey, DatasetId and RetrievalModel
// are fixed at construction time and never exposed to the LLM.
//
// Available options:
//   - [WithDifyToolName] — override the name shown to the LLM (default: "dify_retrieval")
//   - [WithDifyToolDescription] — override the description shown to the LLM
//   - [WithDifyMetadataAppendKey] — append raw RetrievedRecords to AgentContext.Metadata
//   - [WithDifyDocNameFilter] — add a document_name filter condition (FAQ/KB split pattern)
//
// Example — two tools splitting FAQ vs knowledge base, both collecting references:
//
//	const refsKey = "rag_refs"
//	cfg := tools.DifyRetrievalConfig{
//	    Host:      "https://api.dify.ai",
//	    ApiKey:    os.Getenv("DIFY_API_KEY"),
//	    DatasetId: os.Getenv("DIFY_DATASET_ID"),
//	    RetrievalModel: &dify.RetrieveModelParam{
//	        TopK:                  5,
//	        SearchMethod:          dify.SearchMethodHybrid,
//	        RerankingEnable:       true,
//	        RerankingMode:         dify.RerankModeWeightedScore,
//	        Weights:               &dify.WeightModel{
//	            VectorSetting:  &dify.WeightVectorSetting{VectorWeight: 0.7},
//	            KeywordSetting: &dify.WeightKeywordSetting{KeywordWeight: 0.3},
//	        },
//	        ScoreThresholdEnabled: true,
//	        ScoreThreshold:        0.4,
//	    },
//	}
//	agent, _ := agentloop.NewAgent(agentloop.AgentConfig{
//	    Tools: []agentloop.Tool{
//	        tools.NewDifyRetrievalTool(cfg,
//	            tools.WithDifyToolName("query_faq"),
//	            tools.WithDifyMetadataAppendKey(refsKey),
//	            tools.WithDifyDocNameFilter("FAQ", "contains"),
//	        ),
//	        tools.NewDifyRetrievalTool(cfg,
//	            tools.WithDifyToolName("query_knowledge_base"),
//	            tools.WithDifyMetadataAppendKey(refsKey),
//	            tools.WithDifyDocNameFilter("FAQ", "not contains"),
//	        ),
//	    },
//	})
//	out, _ := agent.Execute(rail, req)
//	refs := tools.GetDifyRetrievedRecords(out.Metadata, refsKey)
func NewDifyRetrievalTool(cfg DifyRetrievalConfig, opts ...DifyRetrievalOption) agentloop.Tool {
	o := difyRetrievalOpts{
		name:        "dify_retrieval",
		description: "Retrieve relevant document segments from the knowledge base using semantic search. Returns the most relevant passages for the given query.",
	}
	for _, opt := range opts {
		opt(&o)
	}

	retrievalModel := cloneRetrievalModel(cfg.RetrievalModel)
	if o.docNameFilter != nil {
		retrievalModel = applyDocNameFilter(retrievalModel, *o.docNameFilter)
	}

	params := map[string]*schema.ParameterInfo{
		"query": agentloop.StringParam(
			"The search query used to find relevant document segments.",
			true,
		),
	}

	metaKey := o.metadataKey
	return agentloop.NewTypedCtxAwareToolFunc(
		o.name,
		o.description,
		params,
		func(ctx context.Context, agentCtx agentloop.AgentContext, args DifyRetrievalArgs) (string, error) {
			rail := flow.NewRail(ctx)
			resp, err := dify.Retrieve(rail, cfg.Host, cfg.ApiKey, cfg.DatasetId, dify.RetrieveReq{
				Query:          args.Query,
				RetrievalModel: retrievalModel,
			})
			if err != nil {
				return "", err
			}
			if metaKey != "" && agentCtx.Metadata != nil && len(resp.Records) > 0 {
				agentloop.Append[dify.RetrievedRecord](agentCtx.Metadata, metaKey, resp.Records...)
			}
			retrieved := formatRetrieveResp(resp)
			rail.Infof("Retrieved: %v", retrieved)
			return retrieved, nil
		},
	)
}

func cloneRetrievalModel(p *dify.RetrieveModelParam) *dify.RetrieveModelParam {
	if p == nil {
		return nil
	}
	cp := *p
	cp.MetadataFilteringConditions = dify.MetadataFilteringConditions{
		LogicalOperator: p.MetadataFilteringConditions.LogicalOperator,
		Conditions:      append([]dify.MetadataFilteringCondition(nil), p.MetadataFilteringConditions.Conditions...),
	}
	return &cp
}

func applyDocNameFilter(p *dify.RetrieveModelParam, cond dify.MetadataFilteringCondition) *dify.RetrieveModelParam {
	if p == nil {
		p = &dify.RetrieveModelParam{}
	}
	if p.MetadataFilteringConditions.LogicalOperator == "" {
		p.MetadataFilteringConditions.LogicalOperator = "and"
	}
	p.MetadataFilteringConditions.Conditions = append(p.MetadataFilteringConditions.Conditions, cond)
	return p
}

// GetDifyRetrievedRecords retrieves the []dify.RetrievedRecord slice accumulated under key
// from a MetadataStore. Returns nil if the key is absent or the store is nil.
func GetDifyRetrievedRecords(m *agentloop.MetadataStore, key string) []dify.RetrievedRecord {
	records, _ := agentloop.GetMeta[[]dify.RetrievedRecord](m, key)
	return records
}

// formatRetrieveResp formats a Dify RetrieveRes into plain text for LLM consumption.
func formatRetrieveResp(resp dify.RetrieveRes) string {
	if len(resp.Records) == 0 {
		return "No relevant documents found."
	}

	var sb strings.Builder
	for i, rec := range resp.Records {
		seg := rec.Segment
		fmt.Fprintf(&sb, "[%d] %s (position: %d, score: %.4f)\n", i+1, seg.Document.Name, seg.Position, rec.Score)
		if seg.Content != "" {
			sb.WriteString(strings.TrimSpace(seg.Content))
			sb.WriteString("\n")
		}
		if seg.Answer != "" {
			sb.WriteString("Answer: ")
			sb.WriteString(strings.TrimSpace(seg.Answer))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String())
}
