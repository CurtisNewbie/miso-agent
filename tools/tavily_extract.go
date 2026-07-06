package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
	"github.com/curtisnewbie/miso/miso"

	"github.com/curtisnewbie/miso-agent/agentloop"
)

// TavilyExtractArgs are the typed arguments for the tavily_extract tool.
type TavilyExtractArgs struct {
	// URLs is the list of URLs to extract content from. Required.
	URLs []string `json:"urls" desc:"List of URLs to extract content from."`

	Query string `json:"query,omitempty" desc:"Optional query to rerank extracted content chunks by relevance."`
}

// TavilyExtractOption is a functional option that customizes the tavilyExtractReq before it is sent to Tavily.
// It is applied after all TavilyExtractArgs fields have been set, so it can override any field.
type TavilyExtractOption func(o *TavilyExtractReq)

type TavilyExtractReq struct {
	// URLs is the list of URLs to extract content from. Required.
	URLs []string `json:"urls"`

	// Query is an optional query used to rerank extracted content chunks by relevance.
	Query string `json:"query,omitempty"`

	// ExtractDepth controls extraction depth. One of "basic" or "advanced".
	ExtractDepth string `json:"extract_depth,omitempty"`

	// Format is the output format of extracted content. One of "markdown" or "text".
	Format string `json:"format,omitempty"`
}

type tavilyExtractResult struct {
	URL        string   `json:"url"`
	RawContent string   `json:"raw_content"`
	Images     []string `json:"images"`
	Favicon    string   `json:"favicon"`
}

type tavilyFailedResult struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

type tavilyExtractResp struct {
	Results       []tavilyExtractResult `json:"results"`
	FailedResults []tavilyFailedResult  `json:"failed_results"`
	ResponseTime  float64               `json:"response_time"`
	RequestID     string                `json:"request_id"`
}

var tavilyExtractURL = "https://api.tavily.com/extract"

// NewTavilyExtractTool creates an agentloop Tool that extracts web page content from URLs using Tavily Extract.
// apiKey is captured in a closure and never exposed to the LLM.
// opts are applied to the request after args, allowing callers to set any additional fields.
//
// Example:
//
//	agent, _ := agentloop.NewAgent(agentloop.AgentConfig{
//	    Tools: []agentloop.Tool{tools.NewTavilyExtractTool(os.Getenv("TAVILY_API_KEY"))},
//	})
func NewTavilyExtractTool(apiKey string, opts ...TavilyExtractOption) agentloop.Tool {
	return agentloop.NewAutoTypedCtxAwareToolFunc(
		"tavily_extract",
		"Extract the full content of one or more web pages from their URLs using Tavily. Returns the raw text or markdown content of each page. Use this tool when you need to read or analyze the content of a specific URL.",
		func(ctx context.Context, agentCtx agentloop.AgentContext, args TavilyExtractArgs) (string, error) {
			if len(args.URLs) == 0 {
				return "", errs.NewErrf("urls required")
			}
			rail := flow.NewRail(ctx)
			rail.Infof("tavily_extract args: %+v", args)
			req := TavilyExtractReq{
				URLs:  args.URLs,
				Query: args.Query,
			}
			for _, opt := range opts {
				opt(&req)
			}
			var resp tavilyExtractResp
			err := miso.NewClient(rail, tavilyExtractURL).
				AddAuthBearer(apiKey).
				Require2xx().
				PostJson(req).
				Json(&resp)
			if err != nil {
				return "", errs.Wrapf(err, "tavily extract failed")
			}
			return formatExtractResp(resp), nil
		},
	)
}

// formatExtractResp formats a tavilyExtractResp into a plain-text string suitable for
// consumption by an LLM.
func formatExtractResp(resp tavilyExtractResp) string {
	var sb strings.Builder

	for i, r := range resp.Results {
		fmt.Fprintf(&sb, "[%d] %s\n", i+1, r.URL)
		if r.RawContent != "" {
			sb.WriteString(strings.TrimSpace(r.RawContent))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	for _, f := range resp.FailedResults {
		fmt.Fprintf(&sb, "[FAILED] %s: %s\n", f.URL, f.Error)
	}

	result := strings.TrimSpace(sb.String())
	if result == "" {
		return "No content extracted."
	}
	return result
}
