package testapi

import "testing"

func TestCompileGraph(t *testing.T) {
	if err := compileGraph(); err != nil {
		t.Fatal(err)
	}
}
