package tools

// @author yongj.zhuang

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

// NewDifyRetrievalTool creates an agentloop Tool that retrieves relevant document
// segments from a Dify knowledge base. Host, ApiKey, DatasetId and RetrievalModel
// are fixed at construction time and never exposed to the LLM.
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
//	        }),
//	    },
//	})
func NewDifyRetrievalTool(cfg DifyRetrievalConfig) agentloop.Tool {
	return agentloop.NewTypedToolFunc(
		"dify_retrieval",
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
