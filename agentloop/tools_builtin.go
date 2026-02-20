package agentloop

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/util/slutil"
	"github.com/curtisnewbie/miso/util/strutil"
	"github.com/spf13/cast"
)

const finishToolName = "finish_tool"

// BuiltinTools returns the built-in tools.
func BuiltinTools(enableFinishTool bool) *ToolRegistry {
	registry := NewToolRegistry()

	registry.Register(NewStoreAwareToolFunc(
		"read_file",
		"Read file content. Supports chunked reading with offset/limit for large files. Use offset and limit to read specific sections.",
		map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The absolute path to the file to read",
			},
			"offset": map[string]interface{}{
				"type":        "number",
				"description": "Optional: Line number to start reading from (0-based). Default: 0",
			},
			"limit": map[string]interface{}{
				"type":        "number",
				"description": "Optional: Maximum number of lines to read. Default: read entire file",
			},
		},
		func(ctx context.Context, backend FileStore, args map[string]interface{}) (string, error) {
			path, err := cast.ToStringE(args["path"])
			if err != nil {
				return "", errs.NewErrf("path is required and must be a string")
			}
			// Check if offset/limit are provided for pagination
			offset := 0
			if o, ok := args["offset"]; ok {
				offset = cast.ToInt(o)
			}
			limit := 0
			if l, ok := args["limit"]; ok {
				limit = cast.ToInt(l)
			}

			content, err := backend.ReadFile(ctx, path)
			if err != nil {
				return "", errs.Wrapf(err, "failed to read file")
			}

			// Apply pagination if requested
			if offset > 0 || limit > 0 {
				lines := strings.Split(string(content), "\n")
				start := offset
				if start < 0 {
					start = 0
				}
				if start >= len(lines) {
					return "", nil
				}
				end := start + limit
				if limit <= 0 || end > len(lines) {
					end = len(lines)
				}
				content = []byte(strings.Join(lines[start:end], "\n"))
			}

			return string(content), nil
		},
	))

	registry.Register(NewStoreAwareToolFunc(
		"write_file",
		"Write content to a file. Creates the file if it doesn't exist, overwrites if it does.",
		map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The absolute path to the file to write",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The content to write to the file",
			},
		},
		func(ctx context.Context, backend FileStore, args map[string]interface{}) (string, error) {
			path, err := cast.ToStringE(args["path"])
			if err != nil {
				return "", errs.NewErrf("path is required and must be a string")
			}
			content, err := cast.ToStringE(args["content"])
			if err != nil {
				return "", errs.NewErrf("content is required and must be a string")
			}

			if err := backend.WriteFile(ctx, path, []byte(content)); err != nil {
				return "", errs.Wrapf(err, "failed to write file")
			}

			return fmt.Sprintf("Successfully wrote to %s", path), nil
		},
	))

	registry.Register(NewStoreAwareToolFunc(
		"edit_file",
		"Performs exact string replacements in files. You must read the file before editing. Preserve exact indentation from the read output. Prefer editing existing files over creating new ones.",
		map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The absolute path to the file to edit",
			},
			"old_string": map[string]interface{}{
				"type":        "string",
				"description": "The exact text to find and replace. Must be unique in the file unless replace_all is True",
			},
			"new_string": map[string]interface{}{
				"type":        "string",
				"description": "The text to replace old_string with. Must be different from old_string",
			},
			"replace_all": map[string]interface{}{
				"type":        "boolean",
				"description": "If True, replace all occurrences of old_string. If False (default), old_string must be unique",
			},
		},
		func(ctx context.Context, backend FileStore, args map[string]interface{}) (string, error) {
			path, err := cast.ToStringE(args["path"])
			if err != nil {
				return "", errs.NewErrf("path is required and must be a string")
			}
			oldString, err := cast.ToStringE(args["old_string"])
			if err != nil {
				return "", errs.NewErrf("old_string is required and must be a string")
			}
			newString, err := cast.ToStringE(args["new_string"])
			if err != nil {
				return "", errs.NewErrf("new_string is required and must be a string")
			}

			// Check if old_string and new_string are different
			if oldString == newString {
				return "", errs.NewErrf("old_string and new_string must be different")
			}

			// Get replace_all flag (default: false)
			replaceAll := cast.ToBool(args["replace_all"])

			// Read the file
			content, err := backend.ReadFile(ctx, path)
			if err != nil {
				return "", errs.Wrapf(err, "failed to read file for editing")
			}

			contentStr := string(content)

			// Count occurrences of old_string
			occurrences := strings.Count(contentStr, oldString)

			if occurrences == 0 {
				return "", errs.NewErrf("String not found in file: '%s'", oldString)
			}

			if occurrences > 1 && !replaceAll {
				return "", errs.NewErrf("String '%s' appears %d times in file. Use replace_all=true to replace all instances, or provide a more specific string with surrounding context", oldString, occurrences)
			}

			// Perform replacement
			newContent := strings.ReplaceAll(contentStr, oldString, newString)

			// Write the modified content back
			if err := backend.WriteFile(ctx, path, []byte(newContent)); err != nil {
				return "", errs.Wrapf(err, "failed to write edited file")
			}

			return fmt.Sprintf("Successfully replaced %d instance(s) of the string in '%s'", occurrences, path), nil
		},
	))

	registry.Register(NewStoreAwareToolFunc(
		"list_directory",
		"List the names of files and subdirectories in a directory.",
		map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The absolute path to the directory to list",
			},
		},
		func(ctx context.Context, backend FileStore, args map[string]interface{}) (string, error) {
			path, err := cast.ToStringE(args["path"])
			if err != nil {
				return "", errs.NewErrf("path is required and must be a string")
			}

			files, err := backend.ListDirectory(ctx, path)
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

	registry.Register(NewStoreAwareToolFunc(
		"glob",
		"Find files matching a pattern (e.g., '*.go', 'src/**/*.ts', '**/*.md'). Supports * (any characters in path component), ** (zero or more directories), and ? (single character).",
		map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "The glob pattern to match",
			},
		},
		func(ctx context.Context, backend FileStore, args map[string]interface{}) (string, error) {
			pattern, err := cast.ToStringE(args["pattern"])
			if err != nil {
				return "", errs.NewErrf("pattern is required and must be a string")
			}

			matches, err := globRecursive(ctx, backend, pattern, ".")
			if err != nil {
				return "", errs.Wrapf(err, "failed to glob")
			}

			return strings.Join(matches, "\n"), nil
		},
	))

	// Add todo tools
	registry.Register(NewTodoAwareToolFunc(
		"add_todo",
		"Add multiple todo items to the list.",
		map[string]interface{}{
			"todos": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"task": map[string]interface{}{
							"type":        "string",
							"description": "The task description",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Additional details about the task",
						},
					},
					"required": []string{"task"},
				},
				"description": "Array of todo items to add",
			},
		},
		func(ctx context.Context, tm *TodoManager, args map[string]interface{}) (string, error) {
			todosData, ok := args["todos"].([]interface{})
			if !ok {
				return "", errs.NewErrf("todos is required and must be an array")
			}

			if len(todosData) == 0 {
				return "", errs.NewErrf("todos list cannot be empty")
			}

			todos := make([]TodoItem, 0, len(todosData))
			for _, td := range todosData {
				todoMap, ok := td.(map[string]interface{})
				if !ok {
					return "", errs.NewErrf("each todo must be an object")
				}

				task := cast.ToString(todoMap["task"])
				description := cast.ToString(todoMap["description"])

				todos = append(todos, TodoItem{
					Task:        task,
					Description: description,
				})
			}

			ids, err := tm.AddTodos(todos)
			if err != nil {
				return "", err
			}

			return fmt.Sprintf("Added %d todos: %s", len(ids), strings.Join(ids, ", ")), nil
		},
	))

	registry.Register(NewTodoAwareToolFunc(
		"update_todo",
		"Update the status of a todo item.",
		map[string]interface{}{
			"id": map[string]interface{}{
				"type":        "string",
				"description": "The todo item ID",
			},
			"status": map[string]interface{}{
				"type":        "string",
				"description": "New status: pending or completed",
			},
		},
		func(ctx context.Context, tm *TodoManager, args map[string]interface{}) (string, error) {
			id := cast.ToString(args["id"])
			status := cast.ToString(args["status"])

			if err := tm.UpdateTodoStatus(id, status); err != nil {
				return "", err
			}

			return fmt.Sprintf("Updated todo %s to %s", id, status), nil
		},
	))

	registry.Register(NewTodoAwareToolFunc(
		"list_todos",
		"List all todo items.",
		map[string]interface{}{},
		func(ctx context.Context, tm *TodoManager, args map[string]interface{}) (string, error) {
			return tm.Format(), nil
		},
	))

	registry.Register(NewTodoAwareToolFunc(
		"delete_todo",
		"Delete multiple todo items.",
		map[string]interface{}{
			"ids": map[string]interface{}{
				"type":        "array",
				"description": "Array of todo item IDs to delete",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
		},
		func(ctx context.Context, tm *TodoManager, args map[string]interface{}) (string, error) {
			ids := cast.ToStringSlice(args["ids"])

			if len(ids) == 0 {
				return "", errs.NewErrf("ids is required and must be an array")
			}

			if err := tm.DeleteTodos(ids); err != nil {
				return "", err
			}

			return fmt.Sprintf("Deleted %d todos: %s", len(ids), strings.Join(ids, ", ")), nil
		},
	))

	// Add finish_tool if enabled
	if enableFinishTool {
		registry.Register(NewToolFunc(
			finishToolName,
			"Call this tool when you have completed the task and have a final answer. This signals the end of the ReAct loop and your final response will be provided to the user.",
			map[string]interface{}{
				"response": map[string]interface{}{
					"type":        "string",
					"description": "Your final answer to the task. This will be returned to the user as the final response.",
				},
			},
			func(ctx context.Context, args map[string]interface{}) (string, error) {
				response := cast.ToString(args["response"])
				if response == "" {
					return "Task completed", nil
				}
				return response, nil
			}))
	}

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
func NewThinkTool(toolName ...string) Tool {
	return NewToolFunc(
		slutil.VarArgAny(toolName, func() string { return "think_tool" }),
		"Tool for strategic reflection on research progress and decision-making. Use this tool after each search to analyze results and plan next steps systematically. This creates a deliberate pause in the research workflow for quality decision-making.",
		map[string]interface{}{
			"reflection": map[string]interface{}{
				"type":        "string",
				"description": "Your detailed reflection on research progress, findings, gaps, and next steps. Reflection should address: 1) Analysis of current findings, 2) Gap assessment, 3) Quality evaluation, 4) Strategic decision",
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if args["reflection"] == nil {
				return "", errs.NewErrf("reflection is required")
			}

			reflection := cast.ToString(args["reflection"])

			return fmt.Sprintf("Reflection recorded: %s", reflection), nil
		},
	)
}
