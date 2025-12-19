package analyzer_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	analyzer "github.com/mpyw/zerologlintctx"
)

func TestZerolog(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, analyzer.Analyzer, "zerolog")
}

func TestFileFilter(t *testing.T) {
	testdata := analysistest.TestData()
	// Tests that generated files are skipped
	analysistest.Run(t, testdata, analyzer.Analyzer, "filefilter")
}
