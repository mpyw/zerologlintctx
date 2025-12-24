// Package internal provides SSA-based analysis for zerolog context propagation.
//
// This package uses SSA (Static Single Assignment) form to track zerolog Event
// values through variable assignments, conditionals, and closures. It detects
// cases where a context.Context is available but not passed to the log chain
// via .Ctx(ctx).
//
// # Architecture
//
//	┌─────────────────────────────────────────────────────────────────────────┐
//	│                         Analysis Flow                                    │
//	│                                                                          │
//	│   analyzer.go (public)                                                   │
//	│        │                                                                 │
//	│        ▼                                                                 │
//	│   internal/analyzer.go   ◀── You are here                                │
//	│   ┌─────────────────────────────────────────────────────────────────┐   │
//	│   │  RunSSA()                                                       │   │
//	│   │    │                                                            │   │
//	│   │    ├── Build function context map                               │   │
//	│   │    ├── Skip excluded files                                      │   │
//	│   │    ├── Run SSA analysis via ssa.Checker                         │   │
//	│   │    └── Report unused ignore directives                          │   │
//	│   └─────────────────────────────────────────────────────────────────┘   │
//	│        │                                                                 │
//	│        ▼                                                                 │
//	│   internal/ssa/                                                          │
//	│   (Core SSA analysis)                                                    │
//	└─────────────────────────────────────────────────────────────────────────┘
package internal

import (
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/directive"
	ssautil "github.com/mpyw/zerologlintctx/internal/ssa"
)

// RunSSA performs SSA-based analysis for zerolog context propagation.
//
// The analysis tracks context through three zerolog types:
//   - Event: The log event being built (e.g., log.Info().Str("k","v"))
//   - Context: The builder for derived loggers (e.g., log.With().Str("k","v"))
//   - Logger: The logger instance
//
// Context can be set via:
//   - Event.Ctx(ctx): Sets context on the current event
//   - Context.Ctx(ctx): Sets default context for the derived logger
//   - zerolog.Ctx(ctx): Returns a logger from context (already has ctx)
func RunSSA(
	pass *analysis.Pass,
	ssaInfo *buildssa.SSA,
	ignoreMaps map[string]directive.IgnoreMap,
	skipFiles map[string]bool,
	isContextType func(types.Type) bool,
) {
	funcCtx := buildFunctionContextMap(ssaInfo, isContextType)

	for fn, info := range funcCtx {
		pos := fn.Pos()
		if !pos.IsValid() {
			continue
		}
		filename := pass.Fset.Position(pos).Filename
		if skipFiles[filename] {
			continue
		}
		ignoreMap := ignoreMaps[filename]

		chk := ssautil.NewChecker(pass, info.name, ignoreMap)
		chk.CheckFunction(fn)
	}

	// Report unused ignore directives
	for _, ignoreMap := range ignoreMaps {
		if ignoreMap == nil {
			continue
		}
		for _, pos := range ignoreMap.GetUnusedIgnores() {
			pass.Reportf(pos, "unused zerologlintctx:ignore directive")
		}
	}
}

// =============================================================================
// Function Context Discovery
// =============================================================================

// contextInfo holds context variable information for a function.
type contextInfo struct {
	name string // The context variable name (for error messages)
}

// buildFunctionContextMap builds a map of functions to their context info.
// It handles both direct context parameters and closures that inherit context.
//
// The algorithm works in two passes:
//
//	Pass 1: Find functions with direct context.Context parameters
//	Pass 2: Propagate context to nested closures (iterate until stable)
//
// Example: Context propagation to closures
//
//	func handler(ctx context.Context) {        // Pass 1: ctx found
//	    go func() {                            // Pass 2: inherits ctx
//	        log.Info().Msg("async")            // Should use .Ctx(ctx)
//	    }()
//	}
//
// Closure hierarchy:
//
//	handler(ctx)          ← Has context.Context param
//	    │
//	    └── anonymous     ← Inherits context from parent
//	            │
//	            └── nested ← Also inherits (multi-level)
func buildFunctionContextMap(
	ssaInfo *buildssa.SSA,
	isContextType func(types.Type) bool,
) map[*ssa.Function]contextInfo {
	funcCtx := make(map[*ssa.Function]contextInfo)

	// First pass: find direct context parameters
	for _, fn := range ssaInfo.SrcFuncs {
		if info := findContextInParams(fn, isContextType); info != nil {
			funcCtx[fn] = *info
		}
	}

	// Second pass: propagate context to closures (iterate until stable)
	for {
		changed := false
		for _, fn := range ssaInfo.SrcFuncs {
			if _, hasCtx := funcCtx[fn]; hasCtx {
				continue
			}
			if fn.Parent() != nil {
				if parentCtx, ok := funcCtx[fn.Parent()]; ok {
					funcCtx[fn] = parentCtx
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}

	return funcCtx
}

// findContextInParams finds the context.Context parameter in function signature.
func findContextInParams(fn *ssa.Function, isContextType func(types.Type) bool) *contextInfo {
	if fn.Signature == nil {
		return nil
	}
	params := fn.Signature.Params()
	if params == nil {
		return nil
	}
	for param := range params.Variables() {
		if isContextType(param.Type()) {
			return &contextInfo{name: param.Name()}
		}
	}
	return nil
}
