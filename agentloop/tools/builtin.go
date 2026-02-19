package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/curtisnewbie/miso-agent/agentloop/backend"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/util/strutil"
)

// BuiltinTools returns the built-in tools.
func BuiltinTools(backend backend.FileBackend, todoManager *TodoManager) *Registry {
	registry := NewRegistry()

	registry.Register(NewToolFunc(
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
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			path, ok := args["path"].(string)
			if !ok {
				return "", errs.NewErrf("path is required")
			}
			// Check if offset/limit are provided for pagination
			offset := 0
			if o, ok := args["offset"].(float64); ok {
				offset = int(o)
			}
			limit := 0
			if l, ok := args["limit"].(float64); ok {
				limit = int(l)
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

	registry.Register(NewToolFunc(
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
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			path, ok := args["path"].(string)
			if !ok {
				return "", errs.NewErrf("path is required")
			}
			content, ok := args["content"].(string)
			if !ok {
				return "", errs.NewErrf("content is required")
			}

			if err := backend.WriteFile(ctx, path, []byte(content)); err != nil {
				return "", errs.Wrapf(err, "failed to write file")
			}

			return fmt.Sprintf("Successfully wrote to %s", path), nil
		},
	))

	registry.Register(NewToolFunc(
		"list_directory",
		"List the names of files and subdirectories in a directory.",
		map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The absolute path to the directory to list",
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			path, ok := args["path"].(string)
			if !ok {
				return "", errs.NewErrf("path is required")
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

	registry.Register(NewToolFunc(
		"glob",
		"Find files matching a pattern (e.g., '*.go', 'src/**/*.ts', '**/*.md'). Supports * (any characters in path component), ** (zero or more directories), and ? (single character).",
		map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "The glob pattern to match",
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			pattern, ok := args["pattern"].(string)
			if !ok {
				return "", errs.NewErrf("pattern is required")
			}

			matches, err := globRecursive(ctx, backend, pattern, ".")
			if err != nil {
				return "", errs.Wrapf(err, "failed to glob")
			}

			return strings.Join(matches, "\n"), nil
		},
	))

	// Add todo tools
	registry.Register(NewToolFunc(
		"add_todo",
		"Add a new todo item to the list.",
		map[string]interface{}{
			"task": map[string]interface{}{
				"type":        "string",
				"description": "The task description",
			},
			"priority": map[string]interface{}{
				"type":        "string",
				"description": "Priority level: high, medium, or low (default: medium)",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Additional details about the task",
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			task, _ := args["task"].(string)
			priority, _ := args["priority"].(string)
			description, _ := args["description"].(string)

			id, err := todoManager.AddTodo(task, priority, description)
			if err != nil {
				return "", err
			}

			return fmt.Sprintf("Added todo %s", id), nil
		},
	))

	registry.Register(NewToolFunc(
		"update_todo",
		"Update the status of a todo item.",
		map[string]interface{}{
			"id": map[string]interface{}{
				"type":        "string",
				"description": "The todo item ID",
			},
			"status": map[string]interface{}{
				"type":        "string",
				"description": "New status: pending, in_progress, completed, or failed",
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			id, _ := args["id"].(string)
			status, _ := args["status"].(string)

			if err := todoManager.UpdateTodoStatus(id, status); err != nil {
				return "", err
			}

			return fmt.Sprintf("Updated todo %s to %s", id, status), nil
		},
	))

	registry.Register(NewToolFunc(
		"list_todos",
		"List all todo items.",
		map[string]interface{}{},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			return todoManager.Format(), nil
		},
	))

	registry.Register(NewToolFunc(
		"delete_todo",
		"Delete a todo item.",
		map[string]interface{}{
			"id": map[string]interface{}{
				"type":        "string",
				"description": "The todo item ID",
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			id, _ := args["id"].(string)

			if err := todoManager.DeleteTodo(id); err != nil {
				return "", err
			}

			return fmt.Sprintf("Deleted todo %s", id), nil
		},
	))

	return registry
}

// globRecursive performs recursive glob matching with ** support.
// pattern: glob pattern (e.g., "**/*.go", "src/**/*.ts")
// basePath: current directory to search from (normalized, no leading/trailing slashes)
func globRecursive(ctx context.Context, be backend.FileBackend, pattern, basePath string) ([]string, error) {
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
func globWalk(ctx context.Context, be backend.FileBackend, patternSegments []string, currentPath, accumulatedPath string, matches *[]string) error {
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
func globWalkMatchAll(ctx context.Context, be backend.FileBackend, currentPath, accumulatedPath string, matches *[]string) error {
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
func matchSegment(ctx context.Context, be backend.FileBackend, segment string, remainingSegments []string, currentPath, accumulatedPath string, matches *[]string) error {
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
