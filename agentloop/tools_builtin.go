package agentloop

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/util/strutil"
)

// Typed argument structs for builtin tools

type ReadFileArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type WriteFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type EditFileArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type ListDirectoryArgs struct {
	Path string `json:"path"`
}

type GlobArgs struct {
	Pattern string `json:"pattern"`
}

type TodoItemInput struct {
	Task        string `json:"task"`
	Description string `json:"description,omitempty"`
}

type AddTodoArgs struct {
	Todos []TodoItemInput `json:"todos"`
}

type UpdateTodoArgs struct {
	ID     string `json:"id,omitempty"`
	Status string `json:"status,omitempty"`
}

type DeleteTodoArgs struct {
	IDs []string `json:"ids"`
}

type AddArtifactArgs struct {
	Path     string            `json:"path"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// BuiltinToolsOption configures which built-in tools are registered.
type BuiltinToolsOption struct {
	// EnableFileTool enables the file-related tools: read_file, write_file, edit_file,
	// list_directory, glob, and add_artifact. Default: false.
	EnableFileTool bool
}

// WithEnableFileTool enables or disables the built-in file tools (read_file, write_file,
// edit_file, list_directory, glob, add_artifact).
func WithEnableFileTool(v bool) func(o *BuiltinToolsOption) {
	return func(o *BuiltinToolsOption) {
		o.EnableFileTool = v
	}
}

// BuiltinTools returns the built-in tools configured by the provided options.
// By default (no options), no tools are registered; use WithEnableFileTool to opt in.
func BuiltinTools(ops ...func(o *BuiltinToolsOption)) *ToolRegistry {
	o := &BuiltinToolsOption{}
	for _, op := range ops {
		op(o)
	}

	registry := NewToolRegistry()

	if o.EnableFileTool {
		registry.Register(NewTypedCtxAwareToolFunc(
			"read_file",
			"Read file content. Supports chunked reading with offset/limit for large files. Use offset and limit to read specific sections.",
			map[string]*schema.ParameterInfo{
				"path":   StringParam("The absolute path to the file to read", true),
				"offset": NumberParam("Optional: Line number to start reading from (0-based). Default: 0", false),
				"limit":  NumberParam("Optional: Maximum number of lines to read. Default: read entire file", false),
			},
			func(ctx context.Context, agentCtx AgentContext, args ReadFileArgs) (string, error) {
				content, err := agentCtx.Store.ReadFile(ctx, args.Path)
				if err != nil {
					return "", errs.Wrapf(err, "failed to read file")
				}

				// Apply pagination if requested
				if args.Offset > 0 || args.Limit > 0 {
					lines := strings.Split(string(content), "\n")
					start := args.Offset
					if start < 0 {
						start = 0
					}
					if start >= len(lines) {
						return "", nil
					}
					end := start + args.Limit
					if args.Limit <= 0 || end > len(lines) {
						end = len(lines)
					}
					content = []byte(strings.Join(lines[start:end], "\n"))
				}

				return string(content), nil
			},
		))

		registry.Register(NewTypedCtxAwareToolFunc(
			"write_file",
			"Write content to a file. Creates the file if it doesn't exist, overwrites if it does.",
			map[string]*schema.ParameterInfo{
				"path":    StringParam("The absolute path to the file to write", true),
				"content": StringParam("The content to write to the file", true),
			},
			func(ctx context.Context, agentCtx AgentContext, args WriteFileArgs) (string, error) {
				if err := agentCtx.Store.WriteFile(ctx, args.Path, strutil.UnsafeStr2Byt(args.Content)); err != nil {
					return "", errs.Wrapf(err, "failed to write file")
				}

				return fmt.Sprintf("Successfully wrote to %s", args.Path), nil
			},
		))

		registry.Register(NewTypedCtxAwareToolFunc(
			"edit_file",
			"Performs exact string replacements in files. You must read the file before editing. Preserve exact indentation from the read output. Prefer editing existing files over creating new ones.",
			map[string]*schema.ParameterInfo{
				"path":        StringParam("The absolute path to the file to edit", true),
				"old_string":  StringParam("The exact text to find and replace. Must be unique in the file unless replace_all is True", true),
				"new_string":  StringParam("The text to replace old_string with. Must be different from old_string", true),
				"replace_all": BoolParam("If True, replace all occurrences of old_string. If False (default), old_string must be unique", false),
			},
			func(ctx context.Context, agentCtx AgentContext, args EditFileArgs) (string, error) {
				// Check if old_string and new_string are different
				if args.OldString == args.NewString {
					return "", errs.NewErrf("old_string and new_string must be different")
				}

				// Read the file
				content, err := agentCtx.Store.ReadFile(ctx, args.Path)
				if err != nil {
					return "", errs.Wrapf(err, "failed to read file for editing")
				}

				contentStr := string(content)

				// Count occurrences of old_string
				occurrences := strings.Count(contentStr, args.OldString)

				if occurrences == 0 {
					return "", errs.NewErrf("String not found in file: '%s'", args.OldString)
				}

				if occurrences > 1 && !args.ReplaceAll {
					return "", errs.NewErrf("String '%s' appears %d times in file. Use replace_all=true to replace all instances, or provide a more specific string with surrounding context", args.OldString, occurrences)
				}

				// Perform replacement
				newContent := strings.ReplaceAll(contentStr, args.OldString, args.NewString)

				// Write the modified content back
				if err := agentCtx.Store.WriteFile(ctx, args.Path, []byte(newContent)); err != nil {
					return "", errs.Wrapf(err, "failed to write edited file")
				}

				return fmt.Sprintf("Successfully replaced %d instance(s) of the string in '%s'", occurrences, args.Path), nil
			},
		))

		registry.Register(NewTypedCtxAwareToolFunc(
			"list_directory",
			"List the names of files and subdirectories in a directory.",
			map[string]*schema.ParameterInfo{
				"path": StringParam("The absolute path to the directory to list", true),
			},
			func(ctx context.Context, agentCtx AgentContext, args ListDirectoryArgs) (string, error) {
				files, err := agentCtx.Store.ListDirectory(ctx, args.Path)
				if err != nil {
					return "", errs.Wrapf(err, "failed to list directory")
				}

				sb := strutil.NewBuilder()
				for _, file := range files {
					if file.IsDir {
						sb.Printf("[DIR]  %s\n", file.Path)
					} else {
						sb.Printf("[FILE] %s (%d bytes)\n", file.Path, file.Size)
					}
				}

				return sb.String(), nil
			},
		))

		registry.Register(NewTypedCtxAwareToolFunc(
			"glob",
			"Find files matching a pattern (e.g., '*.go', 'src/**/*.ts', '**/*.md'). Supports * (any characters in path component), ** (zero or more directories), and ? (single character).",
			map[string]*schema.ParameterInfo{
				"pattern": StringParam("The glob pattern to match", true),
			},
			func(ctx context.Context, agentCtx AgentContext, args GlobArgs) (string, error) {
				matches, err := globRecursive(ctx, agentCtx.Store, args.Pattern, ".")
				if err != nil {
					return "", errs.Wrapf(err, "failed to glob")
				}

				return strings.Join(matches, "\n"), nil
			},
		))

		registry.Register(NewTypedCtxAwareToolFunc(
			"add_artifact",
			"Register a file as an artifact collected during execution. The file size will be automatically read from the FileStore.",
			map[string]*schema.ParameterInfo{
				"path":     StringParam("The absolute path to the file to register as an artifact", true),
				"metadata": ObjectParam("Optional metadata about the artifact (e.g., title, url, description)", map[string]*schema.ParameterInfo{}, false),
			},
			func(ctx context.Context, agentCtx AgentContext, args AddArtifactArgs) (string, error) {
				// Read file from FileStore to get size
				content, err := agentCtx.Store.ReadFile(ctx, args.Path)
				if err != nil {
					return "", errs.Wrapf(err, "failed to read artifact file")
				}

				artifact := Artifact{
					Path:        args.Path,
					SizeInBytes: int64(len(content)),
					Meta:        args.Metadata,
				}

				if err := agentCtx.Artifacts.AddArtifact(artifact); err != nil {
					return "", errs.Wrapf(err, "failed to add artifact")
				}

				return fmt.Sprintf("Successfully registered artifact: %s (%d bytes)", args.Path, len(content)), nil
			},
		))
	} // end if o.EnableFileTool

	// Add todo tools
	registry.Register(NewTypedCtxAwareToolFunc(
		"add_todo",
		"Add multiple todo items to the list.",
		map[string]*schema.ParameterInfo{
			"todos": ArrayParam("Array of todo items to add",
				ObjectParam("", map[string]*schema.ParameterInfo{
					"task":        StringParam("The task description", true),
					"description": StringParam("Additional details about the task", false),
				}, false), true),
		},
		func(ctx context.Context, agentCtx AgentContext, args AddTodoArgs) (string, error) {
			if len(args.Todos) == 0 {
				return "", errs.NewErrf("todos list cannot be empty")
			}

			todos := make([]TodoItem, 0, len(args.Todos))
			for _, td := range args.Todos {
				todos = append(todos, TodoItem{
					Task:        td.Task,
					Description: td.Description,
				})
			}

			ids, err := agentCtx.Todos.AddTodos(todos)
			if err != nil {
				return "", err
			}

			return fmt.Sprintf("Added %d todos: %s", len(ids), strings.Join(ids, ", ")), nil
		},
	))

	registry.Register(NewTypedCtxAwareToolFunc(
		"update_todo",
		"Update the status of a todo item.",
		map[string]*schema.ParameterInfo{
			"id":     StringParam("The todo item ID", false),
			"status": StringParam("New status: pending or completed", false),
		},
		func(ctx context.Context, agentCtx AgentContext, args UpdateTodoArgs) (string, error) {
			if err := agentCtx.Todos.UpdateTodoStatus(args.ID, args.Status); err != nil {
				return "", err
			}

			return fmt.Sprintf("Updated todo %s to %s", args.ID, args.Status), nil
		},
	))

	registry.Register(NewTypedCtxAwareToolFunc(
		"list_todos",
		"List all todo items.",
		map[string]*schema.ParameterInfo{},
		func(ctx context.Context, agentCtx AgentContext, args struct{}) (string, error) {
			return agentCtx.Todos.Format(), nil
		},
	))

	registry.Register(NewTypedCtxAwareToolFunc(
		"delete_todo",
		"Delete multiple todo items.",
		map[string]*schema.ParameterInfo{
			"ids": ArrayParam("Array of todo item IDs to delete", StringParam("", false), true),
		},
		func(ctx context.Context, agentCtx AgentContext, args DeleteTodoArgs) (string, error) {
			if len(args.IDs) == 0 {
				return "", errs.NewErrf("ids is required and must be an array")
			}

			if err := agentCtx.Todos.DeleteTodos(args.IDs); err != nil {
				return "", err
			}

			return fmt.Sprintf("Deleted %d todos: %s", len(args.IDs), strings.Join(args.IDs, ", ")), nil
		},
	))

	return registry
}

// globRecursive performs recursive glob matching with ** support.
// pattern: glob pattern (e.g., "**/*.go", "src/**/*.ts")
// basePath: current directory to search from (normalized, no leading/trailing slashes)
func globRecursive(ctx context.Context, be FileStore, pattern, basePath string) ([]string, error) {
	// Normalize paths
	pattern = normalizeGlobPath(pattern)
	basePath = normalizeGlobPath(basePath)

	// Split pattern into segments by '/'
	patternSegments := splitGlobSegments(pattern)
	if len(patternSegments) == 0 {
		return []string{}, nil
	}

	// Collect all matching paths
	var matches []string
	err := globWalk(ctx, be, patternSegments, basePath, "", &matches)
	return matches, err
}

// globWalk recursively walks the directory tree and matches paths against pattern segments
func globWalk(ctx context.Context, be FileStore, patternSegments []string, currentPath, accumulatedPath string, matches *[]string) error {
	if len(patternSegments) == 0 {
		// All segments consumed - check if accumulatedPath exists and is a file
		if accumulatedPath == "" {
			return nil
		}
		exists, err := be.FileExists(ctx, accumulatedPath)
		if err == nil && exists {
			// Check if it's a file (not a directory)
			files, err := be.ListDirectory(ctx, accumulatedPath)
			if err != nil || len(files) > 0 {
				// Either error listing or has children, skip (it's a directory or we can't check)
				return nil
			}
			*matches = append(*matches, accumulatedPath)
		}
		return nil
	}

	currentSegment := patternSegments[0]
	remainingSegments := patternSegments[1:]

	// Handle ** (match zero or more directories)
	if currentSegment == "**" {
		// ** at the end - match everything recursively
		if len(remainingSegments) == 0 {
			return globWalkMatchAll(ctx, be, currentPath, accumulatedPath, matches)
		}

		// ** in the middle - try matching zero or more directories
		// First try zero directories (current path matches next segment)
		if err := matchSegment(ctx, be, remainingSegments[0], remainingSegments[1:], currentPath, accumulatedPath, matches); err != nil {
			return err
		}

		// Then try one or more directories by recursing into subdirectories
		files, err := be.ListDirectory(ctx, currentPath)
		if err != nil {
			return nil // Ignore errors in directory listing
		}

		for _, file := range files {
			if file.IsDir {
				nextPath := joinGlobPath(currentPath, file.Path)
				nextAccumulated := joinGlobPath(accumulatedPath, file.Path)
				if err := globWalk(ctx, be, patternSegments, nextPath, nextAccumulated, matches); err != nil {
					return err
				}
			}
		}
		return nil
	}

	// Handle normal segment with wildcards
	return matchSegment(ctx, be, currentSegment, remainingSegments, currentPath, accumulatedPath, matches)
}

// globWalkMatchAll matches all files recursively (used for ** at the end)
func globWalkMatchAll(ctx context.Context, be FileStore, currentPath, accumulatedPath string, matches *[]string) error {
	// List current directory
	files, err := be.ListDirectory(ctx, currentPath)
	if err != nil {
		return nil // Ignore errors
	}

	for _, file := range files {
		joinedPath := joinGlobPath(accumulatedPath, file.Path)
		if file.IsDir {
			// Recurse into subdirectories
			nextPath := joinGlobPath(currentPath, file.Path)
			if err := globWalkMatchAll(ctx, be, nextPath, joinedPath, matches); err != nil {
				return err
			}
		} else {
			// Add file to matches
			*matches = append(*matches, joinedPath)
		}
	}
	return nil
}

// matchSegment matches a single segment against directory entries
func matchSegment(ctx context.Context, be FileStore, segment string, remainingSegments []string, currentPath, accumulatedPath string, matches *[]string) error {
	files, err := be.ListDirectory(ctx, currentPath)
	if err != nil {
		return nil // Ignore errors
	}

	for _, file := range files {
		// Check if the file name matches the segment pattern
		matched, err := filepath.Match(segment, file.Path)
		if err != nil {
			continue // Invalid pattern, skip
		}
		if !matched {
			continue
		}

		joinedPath := joinGlobPath(accumulatedPath, file.Path)

		// If this is the last segment, add file to matches (if it's a file)
		if len(remainingSegments) == 0 {
			if !file.IsDir {
				*matches = append(*matches, joinedPath)
			}
		} else {
			// Continue to next segments if this is a directory
			if file.IsDir {
				nextPath := joinGlobPath(currentPath, file.Path)
				if err := globWalk(ctx, be, remainingSegments, nextPath, joinedPath, matches); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// splitGlobSegments splits a glob pattern into segments by '/'
func splitGlobSegments(pattern string) []string {
	if pattern == "" || pattern == "." {
		return []string{}
	}
	return strings.Split(pattern, "/")
}

// normalizeGlobPath normalizes a path for glob matching
func normalizeGlobPath(path string) string {
	path = filepath.ToSlash(path)
	path = strings.Trim(path, "/")
	if path == "" {
		return "."
	}
	return path
}

// joinGlobPath joins two path components with '/'
func joinGlobPath(base, component string) string {
	if base == "" || base == "." {
		return component
	}
	return base + "/" + component
}

// NewThinkTool creates a think tool for strategic reflection on research progress and decision-making.
// This tool is not included in the built-in tools by default, but can be added by users if needed.
// Use this tool after each search to analyze results and plan next steps systematically.
// This creates a deliberate pause in the research workflow for quality decision-making.
//
// When to use:
// - After receiving search results: What key information did I find?
// - Before deciding next steps: Do I have enough to answer comprehensively?
// - When assessing research gaps: What specific information am I still missing?
// - Before concluding research: Can I provide a complete answer now?
//
// Reflection should address:
// 1. Analysis of current findings - What concrete information have I gathered?
// 2. Gap assessment - What crucial information is still missing?
// 3. Quality evaluation - Do I have sufficient evidence/examples for a good answer?
// 4. Strategic decision - Should I continue searching or provide my answer?
//
// Example:
//
//	agent := agentloop.NewAgent(agentloop.AgentConfig{
//	    Model: chatModel,
//	    Tools: []agentloop.Tool{agentloop.NewThinkTool()},
//	})
func NewThinkTool() Tool {
	type ThinkToolArgs struct {
		Reflection *string `json:"reflection"`
	}

	return NewTypedToolFunc(
		"think_tool",
		"Tool for strategic reflection on research progress and decision-making. Use this tool after each search to analyze results and plan next steps systematically. This creates a deliberate pause in the research workflow for quality decision-making.",
		map[string]*schema.ParameterInfo{
			"reflection": StringParam("Your detailed reflection on research progress, findings, gaps, and next steps. Reflection should address: 1) Analysis of current findings, 2) Gap assessment, 3) Quality evaluation, 4) Strategic decision", true),
		},
		func(ctx context.Context, args ThinkToolArgs) (string, error) {
			if args.Reflection == nil {
				return "", errs.NewErrf("reflection is required")
			}

			return fmt.Sprintf("Reflection recorded: %s", *args.Reflection), nil
		},
	)
}
