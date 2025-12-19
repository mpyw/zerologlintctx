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

func TestFileFilterDefault(t *testing.T) {
	testdata := analysistest.TestData()
	// Default: -test=true, so test files are analyzed
	analysistest.Run(t, testdata, analyzer.Analyzer, "filefilter")
}

func TestFileFilterSkipTests(t *testing.T) {
	testdata := analysistest.TestData()

	// Set -test=false to skip test files
	if err := analyzer.Analyzer.Flags.Set("test", "false"); err != nil {
		t.Fatal(err)
	}

	defer func() {
		_ = analyzer.Analyzer.Flags.Set("test", "true")
	}()

	analysistest.Run(t, testdata, analyzer.Analyzer, "filefilterskip")
}
