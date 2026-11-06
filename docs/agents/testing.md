# Testing

- Test files: standard `*_test.go` placed alongside source files (same package, internal testing).
- Test function naming: `TestTypeName_MethodName` or `TestTypeName_MethodName_Scenario`
  (e.g. `TestAgent_shouldEvictToolResult`, `TestBuiltinTools_ReadFile_NotFound`).
- Table-driven tests are the primary pattern:

  ```go
  tests := []struct {
      name        string
      input       SomeType
      wantErr     bool
      description string
  }{
      {name: "valid case", ...},
      {name: "error case", wantErr: true, ...},
  }
  for _, tt := range tests {
      t.Run(tt.name, func(t *testing.T) { ... })
  }
  ```

- Mock types are defined within the test file itself.
- Helper functions are unexported (e.g. `generateLargeContent`, `containsString`).