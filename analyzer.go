// Package analyzer provides a go/analysis based analyzer for detecting
// missing context propagation in zerolog logging chains.
package analyzer

import (
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

func run(pass *analysis.Pass) (any, error) {
	ssaInfo, _ := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)

	// Build ignore maps for each file
	ignoreMaps := buildIgnoreMaps(pass)

	// Run SSA-based zerolog analysis
	internal.RunSSA(pass, ssaInfo, ignoreMaps, internal.IsContextType)

	return nil, nil
}

// buildIgnoreMaps creates ignore maps for each file in the pass.
func buildIgnoreMaps(pass *analysis.Pass) map[string]internal.IgnoreMap {
	ignoreMaps := make(map[string]internal.IgnoreMap)
	for _, file := range pass.Files {
		filename := pass.Fset.Position(file.Pos()).Filename
		ignoreMaps[filename] = internal.BuildIgnoreMap(pass.Fset, file)
	}
	return ignoreMaps
}
