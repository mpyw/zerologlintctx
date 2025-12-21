// Package zerologlintctx provides a go/analysis based analyzer for detecting
// missing context propagation in zerolog logging chains.
package zerologlintctx

import (
	"errors"
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"

	"github.com/mpyw/zerologlintctx/internal"
)

// Analyzer is the main analyzer for zerologlintctx.
var Analyzer = &analysis.Analyzer{
	Name:     "zerologlintctx",
	Doc:      "checks that context.Context is properly propagated to zerolog logging chains via .Ctx(ctx)",
	Requires: []*analysis.Analyzer{buildssa.Analyzer},
	Run:      run,
}

var ErrNoSSA = errors.New("SSA analyzer result not found")

func run(pass *analysis.Pass) (any, error) {
	ssaInfo, ok := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	if !ok {
		return nil, ErrNoSSA
	}

	// Build set of files to skip
	skipFiles := buildSkipFiles(pass)

	// Build ignore maps for each file (excluding skipped files)
	ignoreMaps := buildIgnoreMaps(pass, skipFiles)

	// Run SSA-based zerolog analysis
	internal.RunSSA(pass, ssaInfo, ignoreMaps, skipFiles, internal.IsContextType)

	return nil, nil
}

// buildSkipFiles creates a set of filenames to skip.
// Generated files are always skipped.
// Test files can be skipped via the driver's built-in -test flag.
func buildSkipFiles(pass *analysis.Pass) map[string]bool {
	skipFiles := make(map[string]bool)

	for _, file := range pass.Files {
		filename := pass.Fset.Position(file.Pos()).Filename

		// Always skip generated files
		if ast.IsGenerated(file) {
			skipFiles[filename] = true
		}
	}

	return skipFiles
}

// buildIgnoreMaps creates ignore maps for each file in the pass.
func buildIgnoreMaps(pass *analysis.Pass, skipFiles map[string]bool) map[string]internal.IgnoreMap {
	ignoreMaps := make(map[string]internal.IgnoreMap)
	for _, file := range pass.Files {
		filename := pass.Fset.Position(file.Pos()).Filename
		if skipFiles[filename] {
			continue
		}
		ignoreMaps[filename] = internal.BuildIgnoreMap(pass.Fset, file)
	}
	return ignoreMaps
}
