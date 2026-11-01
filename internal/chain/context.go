// context.go finds which functions have a context.Context in scope, and the
// name it goes by there, and which context values are fresh ones that carry
// nothing of the caller's.

package chain

import (
	"go/types"

	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/ssaflow"
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
// parameter, a variable a closure captures, or a field of one, where only
// such calls reach it, through the calls of a func literal and where a
// closure is made (see resolver):
//
//	bg := context.Background()
//	if debug {
//	    bg = context.TODO()
//	}
//	l.Info().Ctx(bg).Msg("x")   // bg is fresh on both paths
//
// A variable that a closure stores to, other than the function read in, is
// not fresh: a walk of that function does not see the store. A value of any
// other kind, such as a parameter, a global, or a call of another function,
// is not fresh.
//
//declscope:shared // tracing.go does not count a Ctx call given one
func isFreshContext(v ssa.Value, positions ssaflow.Positions, closures *ssaflow.Closures) bool {
	f := &contextFreshness{
		done:   make(map[ssa.Value]verdict),
		active: make(map[*ssa.Parameter]bool),
	}
	f.r = ssaflow.Resolver{Positions: positions, Closures: closures, Model: zerologapi.Model{}, Enter: f.enter, UnseenUnknown: true}
	return f.isFresh(v) == yes
}

// contextFreshness works out whether values are fresh contexts, for one
// isFreshContext.
type contextFreshness struct {
	r ssaflow.Resolver
	// done holds the verdict of each value already asked about. A value
	// still being worked out is neutral: it is reached again only through an
	// edge of a Phi that leads back to it, or a store in a loop that reaches
	// itself, which adds no other value.
	done map[ssa.Value]verdict
	// active holds the parameters of func literals being resolved.
	active map[*ssa.Parameter]bool
}

// enter resolves param through expand, unless it is being resolved already,
// as a recursive call of its func literal leads back to it. The other calls
// give the value.
func (f *contextFreshness) enter(param *ssa.Parameter, expand func()) {
	if f.active[param] {
		return
	}
	f.active[param] = true
	expand()
	delete(f.active, param)
}

// isFresh is yes for a fresh context, and no for any other.
func (f *contextFreshness) isFresh(v ssa.Value) verdict {
	if found, ok := f.done[v]; ok {
		return found
	}
	f.done[v] = neutral
	found := f.trace(v)
	f.done[v] = found
	return found
}

// trace does the work of isFresh for a value not asked about yet.
func (f *contextFreshness) trace(v ssa.Value) verdict {
	switch val := v.(type) {
	case *ssa.Call:
		sfn := val.Call.StaticCallee()
		if sfn == nil {
			return no
		}
		fn, ok := sfn.Object().(*types.Func)
		return verdictOf(ok && zerologapi.MakesFreshContext(fn))
	case *ssa.Phi:
		return all(verdictsOf(val.Edges, f.isFresh))
	case *ssa.ChangeType:
		return f.isFresh(val.X)
	case *ssa.UnOp:
		if _, isLoad := ssaflow.AsLoad(val); !isLoad {
			return no
		}
		// The stores that reach the load must be known from the function
		// alone. A global can be changed anywhere, so ResolveAt knows nothing
		// of it
		at, _ := ssaflow.PlaceOf(val.X)
		sources, ok := f.r.ResolveAt(at, val)
		if !ok {
			return no
		}
		return all(func(yield func(verdict) bool) {
			for src := range sources {
				found := no
				if src.V != nil && !src.Pointer {
					found = f.isFresh(src.V)
				}
				if !yield(found) {
					return
				}
			}
		})
	}
	return no
}
