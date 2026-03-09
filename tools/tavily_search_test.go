package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/curtisnewbie/miso-tavily/search"
)

func TestNewTavilySearchTool_Metadata(t *testing.T) {
	tool := NewTavilySearchTool("dummy-key", 5)

	if tool.Name() != "tavily_search" {
		t.Errorf("expected name %q, got %q", "tavily_search", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}

	params := tool.Parameters()
	required := []string{"query"}
	for _, name := range required {
		if _, ok := params[name]; !ok {
			t.Errorf("expected parameter %q to be defined", name)
		}
	}

	optional := []string{"topic", "time_range"}
	for _, name := range optional {
		if _, ok := params[name]; !ok {
			t.Errorf("expected parameter %q to be defined", name)
		}
	}

	if _, ok := params["max_results"]; ok {
		t.Error("max_results should not be exposed as a tool parameter")
	}
}

func TestFormatSearchResp_WithAnswer(t *testing.T) {
	resp := search.SearchResp{
		Answer: "Go was created at Google.",
		Results: []search.SearchResult{
			{Title: "Go (programming language)", URL: "https://go.dev", Content: "Go is an open source programming language."},
			{Title: "Wikipedia: Go", URL: "https://en.wikipedia.org/wiki/Go_(programming_language)", Content: "Go is statically typed and compiled."},
		},
	}

	out := formatSearchResp(resp)

	if !strings.Contains(out, "Answer: Go was created at Google.") {
		t.Errorf("expected answer in output, got:\n%s", out)
	}
	if !strings.Contains(out, "[1] Go (programming language)") {
		t.Errorf("expected first result title, got:\n%s", out)
	}
	if !strings.Contains(out, "URL: https://go.dev") {
		t.Errorf("expected first result URL, got:\n%s", out)
	}
	if !strings.Contains(out, "[2] Wikipedia: Go") {
		t.Errorf("expected second result title, got:\n%s", out)
	}
}

func TestFormatSearchResp_NoResults(t *testing.T) {
	out := formatSearchResp(search.SearchResp{})
	if out != "No results found." {
		t.Errorf("expected 'No results found.', got %q", out)
	}
}

// TestTavilySearchTool_Live exercises the tool against the real Tavily Search API.
//
// Required environment variables:
//
//	TAVILY_API_KEY  – your Tavily API key
//
// The test is skipped automatically when TAVILY_API_KEY is not set.
func TestTavilySearchTool_Live(t *testing.T) {
	apiKey, ok := os.LookupEnv("TAVILY_API_KEY")
	if !ok || apiKey == "" {
		t.Skip("TAVILY_API_KEY not set, skipping live Tavily search test")
	}

	tool := NewTavilySearchTool(apiKey, 3)

	// The tool implements SelfInvokeTool; invoke via ExecuteJson so we exercise
	// the same path the agent would use at runtime.
	type selfInvoker interface {
		ExecuteJson(ctx context.Context, jsonArg string) (string, error)
	}
	si, ok := tool.(selfInvoker)
	if !ok {
		t.Fatal("expected tool to implement ExecuteJson (SelfInvokeTool)")
	}

	result, err := si.ExecuteJson(context.Background(), `{"query":"latest Go programming language release"}`)
	if err != nil {
		t.Fatalf("ExecuteJson failed: %v", err)
	}
	if result == "" || result == "No results found." {
		t.Fatalf("expected non-empty search results, got: %q", result)
	}

	fmt.Println("=== Tavily Search Result ===")
	fmt.Println(result)
}
