package zerologlintctx_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// TestManyCallsOnOneLogger checks a function with thousands of log calls on
// one local logger, whose address every Info call takes. Finding the stores
// of that logger again for each call took time cubic in their number (#99):
// over a minute for this function, against well under a second now.
func TestManyCallsOnOneLogger(t *testing.T) {
	const calls = 3000
	dir := t.TempDir()
	if err := os.CopyFS(filepath.Join(dir, "src"), os.DirFS(filepath.Join(analysistest.TestData(), "src"))); err != nil {
		t.Fatal(err)
	}
	var src strings.Builder
	src.WriteString("package manycalls\n\nimport (\n\t\"context\"\n\n\t\"github.com/rs/zerolog\"\n)\n\n")
	src.WriteString("func f(ctx context.Context, l zerolog.Logger) {\n\tl = l.With().Str(\"k\", \"v\").Logger()\n")
	for range calls {
		src.WriteString("\tl.Info().Msg(\"x\") // want `zerolog call chain missing .Ctx\\(ctx\\)`\n")
	}
	src.WriteString("}\n")
	pkgDir := filepath.Join(dir, "src", "manycalls")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "a.go"), []byte(src.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		analysistest.Run(t, dir, zerologlintctx.Analyzer, "manycalls")
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatalf("checking %d log calls on one logger took over 30s", calls)
	}
}
