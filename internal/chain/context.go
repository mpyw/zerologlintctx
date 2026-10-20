// context.go finds which functions have a context.Context in scope, and the
// name it goes by there.

package chain

import (
	"go/types"

	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/typeutil"
)

// ContextNames maps each source function of the package that has a
// context.Context in scope to the name of that context. A closure without a
// usable context parameter of its own inherits the name from the function it
// is declared in. A parameter named _ cannot be used, so it counts as none.
//
// The source functions are those of buildssa, plus the closures in
// package-level variable initializers, which belong to the package's
// synthetic init function and are not among them.
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
func ContextNames(info *buildssa.SSA) map[*ssa.Function]string {
	funcs := append(contextInitializerClosures(info.Pkg), info.SrcFuncs...)
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

// contextInitializerClosures returns the closures, at any depth, of the
// package's synthetic init function, which runs the package-level variable
// initializers.
func contextInitializerClosures(pkg *ssa.Package) []*ssa.Function {
	if pkg == nil {
		return nil
	}
	init := pkg.Func("init")
	if init == nil {
		return nil
	}
	var closures []*ssa.Function
	var addAnons func(fn *ssa.Function)
	addAnons = func(fn *ssa.Function) {
		for _, anon := range fn.AnonFuncs {
			closures = append(closures, anon)
			addAnons(anon)
		}
	}
	addAnons(init)
	return closures
}

// contextParamName finds the name of the first usable context.Context
// parameter in fn's signature, or "" when it has none. A parameter that is
// unnamed or named _ is not usable. The name of a *context.Context parameter
// is given as "*name", the expression that yields the context.
func contextParamName(fn *ssa.Function) string {
	if fn.Signature == nil {
		return ""
	}
	params := fn.Signature.Params()
	if params == nil {
		return ""
	}
	for param := range params.Variables() {
		name := param.Name()
		if name == "" || name == "_" || !typeutil.IsContextType(param.Type()) {
			continue
		}
		if _, isPtr := param.Type().Underlying().(*types.Pointer); isPtr {
			return "*" + name
		}
		return name
	}
	return ""
}
