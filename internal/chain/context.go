// context.go finds which functions have a context.Context in scope, and the
// name it goes by there.

package chain

import (
	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/typeutil"
)

// ContextNames maps each function of funcs that has a context.Context in
// scope to the name of that context. A closure without a context parameter of
// its own inherits the name from the function it is declared in.
//
// It works in two passes:
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
func ContextNames(funcs []*ssa.Function) map[*ssa.Function]string {
	names := make(map[*ssa.Function]string)

	// First pass: find direct context parameters
	for _, fn := range funcs {
		if name := contextParamName(fn); name != "" {
			names[fn] = name
		}
	}

	// Second pass: propagate context to closures (iterate until stable)
	for {
		changed := false
		for _, fn := range funcs {
			if _, hasCtx := names[fn]; hasCtx {
				continue
			}
			if fn.Parent() != nil {
				if parentName, ok := names[fn.Parent()]; ok {
					names[fn] = parentName
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}

	return names
}

// contextParamName finds the name of the context.Context parameter in fn's
// signature, or "" when it has none.
func contextParamName(fn *ssa.Function) string {
	if fn.Signature == nil {
		return ""
	}
	params := fn.Signature.Params()
	if params == nil {
		return ""
	}
	for param := range params.Variables() {
		if typeutil.IsContextType(param.Type()) {
			return param.Name()
		}
	}
	return ""
}
