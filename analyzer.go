// Package zerologlintctx provides a go/analysis based analyzer for detecting
// missing context propagation in zerolog logging chains.
package zerologlintctx

import (
	"errors"
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"

	"github.com/mpyw/zerologlintctx/internal/chain"
	"github.com/mpyw/zerologlintctx/internal/directive"
)

// Analyzer is the main analyzer for zerologlintctx.
var Analyzer = &analysis.Analyzer{
	Name:     "zerologlintctx",
	Doc:      "checks that context.Context is properly propagated to zerolog logging chains via .Ctx(ctx)",
	Requires: []*analysis.Analyzer{buildssa.Analyzer},
	Run:      run,
}

// ErrNoSSA is returned when the pass has no buildssa result, which the
// analyzer requires.
var ErrNoSSA = errors.New("SSA analyzer result not found")

// run checks every function that has a context in scope, then reports the
// ignore directives that silenced nothing and the directives that do nothing.
//
// The analysis tracks context through three zerolog types:
//   - Event: The log event being built (e.g., log.Info().Str("k","v"))
//   - Context: The builder for derived loggers (e.g., log.With().Str("k","v"))
//   - Logger: The logger instance
//
// Context can be set via:
//   - Event.Ctx(ctx): Sets context on the current event
//   - Context.Ctx(ctx): Sets default context for the derived logger
//   - zerolog.Ctx(ctx): Returns the logger stored in ctx. It does not attach
//     ctx to its events, but taking the logger from ctx counts as using it
func run(pass *analysis.Pass) (any, error) {
	ssaInfo, ok := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	if !ok {
		return nil, ErrNoSSA
	}

	ignoresByFile, problems := scanFiles(pass)

	checker := chain.NewChecker()
	for fn, ctxName := range chain.ContextNames(ssaInfo) {
		ignores, ok := ignoresByFile[unadjustedFilename(pass.Fset, fn.Pos())]
		if !ok {
			continue // Not in a checked file, or no position at all
		}
		checker.CheckFunction(fn, ctxName, func(pos token.Pos, msg string) {
			if ignores.ShouldIgnore(pos) {
				return
			}
			pass.Report(analysis.Diagnostic{Pos: pos, Message: msg})
		})
	}

	for _, ignores := range ignoresByFile {
		for _, pos := range ignores.Unused() {
			pass.Reportf(pos, "unused zerologlintctx:ignore directive")
		}
	}

	for _, p := range problems {
		pass.Reportf(p.Pos, "%s", p.Message)
	}

	return nil, nil
}

// scanFiles reads the directives of each file to check. It returns the
// ignores of each such file, keyed by its unadjusted filename, and the
// directives that do nothing, in file order.
//
// Generated files are not checked, and get no entry. Test files can be
// skipped via the driver's built-in -test flag.
func scanFiles(pass *analysis.Pass) (map[string]*directive.Ignores, []directive.Problem) {
	ignoresByFile := make(map[string]*directive.Ignores)
	var problems []directive.Problem
	for _, file := range pass.Files {
		if ast.IsGenerated(file) {
			continue
		}
		ignores, fileProblems := directive.Scan(pass.Fset, file)
		ignoresByFile[unadjustedFilename(pass.Fset, file.Pos())] = ignores
		problems = append(problems, fileProblems...)
	}
	return ignoresByFile, problems
}

// unadjustedFilename names the file pos is in, ignoring //line directives so
// that one cannot change the key. It is "" for an invalid pos.
func unadjustedFilename(fset *token.FileSet, pos token.Pos) string {
	return fset.PositionFor(pos, false).Filename
}
