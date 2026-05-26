package agentloop

import (
	"context"
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso/errs"
	glua "github.com/yuin/gopher-lua"
)

type transformCsvLuaArgs struct {
	InputPath  string `json:"input_path"`
	Script     string `json:"script"`
	OutputPath string `json:"output_path,omitempty"`
}

func validateLuaInputPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return errs.NewErrf("input_path must not be empty")
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == ".." {
			return errs.NewErrf("input_path must not contain traversal segments (..): %s", path)
		}
	}
	return nil
}

func validateLuaOutputPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return errs.NewErrf("output_path must not be empty")
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == ".." {
			return errs.NewErrf("output_path must not contain traversal segments (..): %s", path)
		}
	}
	return nil
}

const transformCsvLuaDescription = `Transform a CSV file using a Lua script.

Globals injected into the script:
  input       string   Raw file content of input_path.
  rows        table    CSV parsed into a 2-D array-of-arrays (1-indexed).
                       rows[i]    → the i-th row (table)
                       rows[i][j] → the j-th field of the i-th row (string)
                       rows[1]    → header row (if the CSV has one)

Available Lua standard libraries: string, table, math.
Filesystem access (os, io, require, dofile, loadfile) is not available.

The script must return a string, which becomes the transformation output.

Column lookup by name (common pattern):
  local header = rows[1]
  local col = {}
  for j = 1, #header do col[header[j]] = j end
  -- then access fields as: rows[i][col["column_name"]]

Example — reformat each data row as "Key: Value" paragraphs:
  local header = rows[1]
  local col = {}
  for j = 1, #header do col[header[j]] = j end
  local out = {}
  for i = 2, #rows do
    local parts = {}
    for j = 1, #header do
      if rows[i][j] ~= "" then
        parts[#parts + 1] = header[j] .. ": " .. rows[i][j]
      end
    end
    out[#out + 1] = table.concat(parts, "\n")
  end
  return table.concat(out, "\n\n")

If output_path is provided, the result is written to that path in the virtual filesystem
and the tool returns a confirmation message. Otherwise the result string is returned directly.`

func NewTransformCsvLuaTool() Tool {
	return NewTypedCtxAwareToolFunc(
		"transform_csv_lua",
		transformCsvLuaDescription,
		map[string]*schema.ParameterInfo{
			"input_path":  StringParam("Absolute virtual path to the input CSV file (e.g. /input/data.csv)", true),
			"script":      StringParam("Lua script to execute. Must return a string.", true),
			"output_path": StringParam("Optional: if provided, write the result to this virtual path instead of returning it inline", false),
		},
		func(ctx context.Context, agentCtx AgentContext, args transformCsvLuaArgs) (string, error) {
			if err := validateLuaInputPath(args.InputPath); err != nil {
				return "", err
			}
			if strings.TrimSpace(args.Script) == "" {
				return "", errs.NewErrf("script must not be empty")
			}
			if args.OutputPath != "" {
				if err := validateLuaOutputPath(args.OutputPath); err != nil {
					return "", err
				}
			}

			content, err := agentCtx.Store.ReadFile(ctx, args.InputPath)
			if err != nil {
				return "", errs.Wrapf(err, "failed to read input_path: %s", args.InputPath)
			}

			result, err := runLuaScript(args.Script, string(content))
			if err != nil {
				return "", errs.Wrapf(err, "lua script execution failed")
			}

			if args.OutputPath != "" {
				if err := agentCtx.Store.WriteFile(ctx, args.OutputPath, []byte(result)); err != nil {
					return "", errs.Wrapf(err, "failed to write output_path: %s", args.OutputPath)
				}
				return fmt.Sprintf("transformation complete, result written to %s (%d bytes)", args.OutputPath, len(result)), nil
			}
			return result, nil
		},
	)
}

func parseCSVToLuaTable(st *glua.LState, raw string) *glua.LTable {
	records, err := csv.NewReader(strings.NewReader(raw)).ReadAll()
	outer := st.NewTable()
	if err != nil {
		return outer
	}
	for i, record := range records {
		row := st.NewTable()
		for j, field := range record {
			row.RawSetInt(j+1, glua.LString(field))
		}
		outer.RawSetInt(i+1, row)
	}
	return outer
}

func runLuaScript(script, input string) (string, error) {
	st := glua.NewState()
	defer st.Close()

	st.SetGlobal("dofile", glua.LNil)
	st.SetGlobal("loadfile", glua.LNil)
	st.SetGlobal("require", glua.LNil)
	st.SetGlobal("io", glua.LNil)
	st.SetGlobal("os", glua.LNil)

	st.SetGlobal("input", glua.LString(input))
	st.SetGlobal("rows", parseCSVToLuaTable(st, input))

	if err := st.DoString(script); err != nil {
		return "", errs.Wrap(err)
	}

	if st.GetTop() < 1 {
		return "", errs.NewErrf("lua script did not return a value")
	}
	ret := st.Get(1)
	if ret.Type() != glua.LTString {
		return "", errs.NewErrf("lua script must return a string, got %s", ret.Type().String())
	}
	return glua.LVAsString(ret), nil
}
