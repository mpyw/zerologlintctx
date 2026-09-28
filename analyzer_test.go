package zerologlintctx_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/mpyw/zerologlintctx"
)

func TestZerolog(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, zerologlintctx.Analyzer, "zerolog")
}

func TestFileFilter(t *testing.T) {
	testdata := analysistest.TestData()
	// Tests that generated files are skipped
	analysistest.Run(t, testdata, zerologlintctx.Analyzer, "filefilter")
}

func TestLineDirective(t *testing.T) {
	testdata := analysistest.TestData()
	// Tests that //line directives do not break ignore directives or generated file skipping
	analysistest.Run(t, testdata, zerologlintctx.Analyzer, "linedirective")
}
