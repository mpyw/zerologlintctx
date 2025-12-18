package analyzer_test

import (
	"testing"

	"github.com/mpyw/zerologlintctx"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestZerolog(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, analyzer.Analyzer, "zerolog")
}
