// context.go finds which functions have a context.Context in scope, and the
// name it goes by there, and which context values are fresh ones that carry
// nothing of the caller's.

package chain

import (
	"go/types"

	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/zerologapi"
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
		if name == "" || name == "_" || !zerologapi.IsContextType(param.Type()) {
			continue
		}
		if _, isPtr := param.Type().Underlying().(*types.Pointer); isPtr {
			return "*" + name
		}
		return name
	}
	return ""
}

// isFreshContext reports whether every value v can be is a call of
// context.Background or context.TODO: a context that carries nothing of the
// caller's. A Ctx call given one does not pass on the context in scope. It
// sees through a Phi, a conversion, and a load of a local variable, a pointer
// parameter or a field of one, where only such calls reach it (see
// storesReaching):
//
//	bg := context.Background()
//	if debug {
//	    bg = context.TODO()
//	}
//	l.Info().Ctx(bg).Msg("x")   // bg is fresh on both paths
//
// A value of any other kind, such as a parameter, a global, a variable a
// closure captures, or a call of another function, is not fresh.
//
//declscope:shared // tracing.go does not count a Ctx call given one
func isFreshContext(v ssa.Value, positions instrPositions) bool {
	return contextIsFresh(v, positions, make(map[ssa.Value]verdict)) == yes
}

// contextIsFresh does the work of isFreshContext: yes for a fresh context,
// and no for any other. done holds the verdict of each value already asked
// about. A value still being worked out is neutral: it is reached again only
// through an edge of a Phi that leads back to it, or a store in a loop that
// reaches itself, which adds no other value.
func contextIsFresh(v ssa.Value, positions instrPositions, done map[ssa.Value]verdict) verdict {
	if found, ok := done[v]; ok {
		return found
	}
	done[v] = neutral
	found := contextTraceFresh(v, positions, done)
	done[v] = found
	return found
}

// contextTraceFresh does the work of contextIsFresh for a value not asked
// about yet.
func contextTraceFresh(v ssa.Value, positions instrPositions, done map[ssa.Value]verdict) verdict {
	switch val := v.(type) {
	case *ssa.Call:
		sfn := val.Call.StaticCallee()
		if sfn == nil {
			return no
		}
		fn, ok := sfn.Object().(*types.Func)
		return verdictOf(ok && zerologapi.MakesFreshContext(fn))
	case *ssa.Phi:
		return contextAllFresh(val.Edges, positions, done)
	case *ssa.ChangeType:
		return contextIsFresh(val.X, positions, done)
	case *ssa.UnOp:
		if _, isLoad := asLoad(val); !isLoad {
			return no
		}
		// The stores that reach the load must be known from the function
		// alone. A global can be changed anywhere
		if _, tracked := placeOf(val.X); !tracked {
			return no
		}
		stored, ok := storesReaching(val.X, val, positions)
		if !ok || stored.unknown || stored.captured != nil || len(stored.params) > 0 {
			return no
		}
		return contextAllFresh(stored.values, positions, done)
	}
	return no
}

// contextAllFresh combines the verdicts of values, as contextIsFresh judges
// each: every one must be fresh.
func contextAllFresh(values []ssa.Value, positions instrPositions, done map[ssa.Value]verdict) verdict {
	return all(verdictsOf(values, func(v ssa.Value) verdict {
		return contextIsFresh(v, positions, done)
	}))
}
