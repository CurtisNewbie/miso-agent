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
// DifyRetrievalConfig has been applied. Use [WithDifyToolName] to override
// the name exposed to the LLM.
type DifyRetrievalOption func(o *difyRetrievalOpts)

// difyRetrievalOpts holds the mutable settings controlled by DifyRetrievalOption.
type difyRetrievalOpts struct {
	name string
}

// WithDifyToolName overrides the tool name that is registered with the LLM.
// Useful when an agent uses multiple Dify knowledge bases simultaneously and
// each tool needs a distinct, descriptive name.
func WithDifyToolName(name string) DifyRetrievalOption {
	return func(o *difyRetrievalOpts) { o.name = name }
}

// NewDifyRetrievalTool creates an agentloop Tool that retrieves relevant document
// segments from a Dify knowledge base. Host, ApiKey, DatasetId and RetrievalModel
// are fixed at construction time and never exposed to the LLM.
// opts are applied after the defaults, allowing callers to customise tool behaviour
// (e.g. override the tool name with [WithDifyToolName]).
//
// Example:
//
//	agent, _ := agentloop.NewAgent(agentloop.AgentConfig{
//	    Tools: []agentloop.Tool{
//	        tools.NewDifyRetrievalTool(tools.DifyRetrievalConfig{
//	            Host:      "https://api.dify.ai",
//	            ApiKey:    os.Getenv("DIFY_API_KEY"),
//	            DatasetId: os.Getenv("DIFY_DATASET_ID"),
//	            RetrievalModel: &dify.RetrieveModelParam{TopK: 5},
//	        }, tools.WithDifyToolName("product_docs_retrieval")),
//	    },
//	})
func NewDifyRetrievalTool(cfg DifyRetrievalConfig, opts ...DifyRetrievalOption) agentloop.Tool {
	o := difyRetrievalOpts{name: "dify_retrieval"}
	for _, opt := range opts {
		opt(&o)
	}
	return agentloop.NewTypedToolFunc(
		o.name,
		"Retrieve relevant document segments from the knowledge base using semantic search. Returns the most relevant passages for the given query.",
		map[string]*schema.ParameterInfo{
			"query": agentloop.StringParam(
				"The search query used to find relevant document segments.",
				true,
			),
		},
		func(ctx context.Context, args DifyRetrievalArgs) (string, error) {
			rail := flow.NewRail(ctx)
			resp, err := dify.Retrieve(rail, cfg.Host, cfg.ApiKey, cfg.DatasetId, dify.RetrieveReq{
				Query:          args.Query,
				RetrievalModel: cfg.RetrievalModel,
			})
			if err != nil {
				return "", err
			}

			return formatRetrieveResp(resp), nil
		},
	)
}

// formatRetrieveResp formats a RetrieveRes into a plain-text string suitable for
// consumption by an LLM.
func formatRetrieveResp(resp dify.RetrieveRes) string {
	if len(resp.Records) == 0 {
		return "No relevant documents found."
	}

	var sb strings.Builder
	for i, rec := range resp.Records {
		seg := rec.Segment
		fmt.Fprintf(&sb, "[%d] %s\n", i+1, seg.Document.Name)
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
