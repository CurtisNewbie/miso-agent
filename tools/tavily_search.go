package tools

// @author yongj.zhuang

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso-tavily/search"
	"github.com/curtisnewbie/miso/flow"

	"github.com/curtisnewbie/miso-agent/agentloop"
)

// TavilySearchArgs are the typed arguments for the tavily_search tool.
type TavilySearchArgs struct {
	// Query is the search query string. Required.
	Query string `json:"query"`

	// Topic narrows the search category. One of "general", "news", "finance".
	Topic string `json:"topic,omitempty"`

	// TimeRange restricts results to a time window. One of "day", "week", "month", "year".
	TimeRange string `json:"time_range,omitempty"`
}

// NewTavilySearchTool creates an agentloop Tool that performs a Tavily web search.
// apiKey is captured in a closure and never exposed to the LLM.
// maxResults controls how many results are returned (1-20); pass 0 to use the default of 5.
//
// Example:
//
//	agent, _ := agentloop.NewAgent(agentloop.AgentConfig{
//	    Tools: []agentloop.Tool{tools.NewTavilySearchTool(os.Getenv("TAVILY_API_KEY"), 5)},
//	})
func NewTavilySearchTool(apiKey string, maxResults int) agentloop.Tool {
	if maxResults < 1 {
		maxResults = 5
	}
	return agentloop.NewTypedToolFunc(
		"tavily_search",
		"Search the web using Tavily. Returns a direct answer (when available) followed by relevant result snippets with URLs. Use this tool to find up-to-date information, news, or factual details.",
		map[string]*schema.ParameterInfo{
			"query": agentloop.StringParam(
				"The search query string.",
				true,
			),
			"topic": agentloop.StringParamEnum(
				"Search category.",
				[]string{"general", "news", "finance"},
				false,
			),
			"time_range": agentloop.StringParamEnum(
				"Restrict results to a recency window.",
				[]string{"day", "week", "month", "year"},
				false,
			),
		},
		func(ctx context.Context, args TavilySearchArgs) (string, error) {
			rail := flow.NewRail(ctx)
			resp, err := search.Search(rail, apiKey, search.SearchReq{
				Query:         args.Query,
				MaxResults:    maxResults,
				Topic:         args.Topic,
				TimeRange:     args.TimeRange,
				IncludeAnswer: true,
			})
			if err != nil {
				return "", err
			}

			return formatSearchResp(resp), nil
		},
	)
}

// formatSearchResp formats a SearchResp into a plain-text string suitable for
// consumption by an LLM.
func formatSearchResp(resp search.SearchResp) string {
	var sb strings.Builder

	if resp.Answer != "" {
		sb.WriteString("Answer: ")
		sb.WriteString(strings.TrimSpace(resp.Answer))
		sb.WriteString("\n\n")
	}

	for i, r := range resp.Results {
		fmt.Fprintf(&sb, "[%d] %s\n", i+1, r.Title)
		fmt.Fprintf(&sb, "URL: %s\n", r.URL)
		if r.Content != "" {
			sb.WriteString(strings.TrimSpace(r.Content))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	result := strings.TrimSpace(sb.String())
	if result == "" {
		return "No results found."
	}
	return result
}
