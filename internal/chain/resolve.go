// resolve.go finds the values a read can see, as sources one at a time: what
// store.go found at an address, with the variables a closure captures traced
// to where the closure is made, and the parameters of a func literal traced to
// what its calls pass, until only values stored, or unknown, are left.
// tracing.go judges each source.

package chain

import (
	"go/types"
	"iter"
	"slices"

	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/zerologapi"
)

// resolved is one source of what a read can see.
//
//declscope:shared // tracing.go judges each one
type resolved struct {
	// v is the value, or nil when it is not known: the value the caller
	// passed through a pointer parameter, or an aggregate copied whole from
	// where no store can be seen.
	v ssa.Value
	// pointer reports that v is a pointer, and the source is what it points
	// to, which resolve could not follow, such as a pointer that is not in a
	// variable. A nil pointer is not a nil value.
	pointer bool
}

// resolver finds the sources of what a read can see. It resolves a
// parameter of a func literal through the calls of the func literal, and its
// caller decides what to do when it gets to the same parameter again.
//
//declscope:shared // tracing.go and context.go make one each
type resolver struct {
	positions instrPositions
	closures  *closures
	// enter runs expand, which resolves param through the calls of its func
	// literal, unless param is already being resolved, as a recursive call
	// leads back to it. Then it does nothing: the other calls give the value.
	enter func(param *ssa.Parameter, expand func())
	// unseenUnknown adds an unknown source for a variable that may be
	// written where the walk of a function for its stores does not see it,
	// such as in another closure (see closures.writtenUnseen). Without it,
	// such a write is left out, as the tracer leaves it out, whose known
	// limitations those are.
	unseenUnknown bool
}

// resolve finds what can be at addr when read runs (see storesReaching). ok
// is false when storesReaching knows nothing of addr.
//
//declscope:shared // tracing.go traces what reaches a read
func (r resolver) resolve(addr ssa.Value, read ssa.Instruction) (sources iter.Seq[resolved], ok bool) {
	stored, ok := storesReaching(addr, read, r.positions)
	if !ok {
		return nil, false
	}
	at, _ := placeOf(addr)
	walked := parentFunc(addr)
	if read != nil {
		walked = read.Parent()
	}
	return r.resolveWalked(at, walked, stored), true
}

// resolveAt finds what can be at the address at when read runs, as
// storesReachingAt does. ok is false when storesReachingAt knows nothing of
// it.
//
//declscope:shared // tracing.go traces what reaches a selection of a value loaded
func (r resolver) resolveAt(at place, read ssa.Instruction) (sources iter.Seq[resolved], ok bool) {
	stored, ok := storesReachingAt(at, read, r.positions)
	if !ok {
		return nil, false
	}
	return r.resolveWalked(at, read.Parent(), stored), true
}

// resolveStored yields the sources of what storesReaching found: unknown,
// when some path carries a value the function cannot tell, what each
// captured variable holds as the closure runs, what the calls of a func
// literal pass for its parameters, and the values stored.
//
//declscope:shared // tracing.go traces the receiver at the embedded fields of a struct
func (r resolver) resolveStored(stored storeReaching) iter.Seq[resolved] {
	return func(yield func(resolved) bool) {
		r.yieldStored(stored, yield)
	}
}

// resolveFreeVar yields the sources of what is at path, the selections from
// fv, nil for fv itself, as its closure reads them: what can be there in the
// variable that each MakeClosure of the closure binds, when the closure runs.
//
//	l := base
//	l.Debug().Ctx(ctx).Msg("start") // reads base, before the closure is made
//	l = l.With().Ctx(ctx).Logger()
//	defer func() { l.Info().Msg("end") }()
//
// SSA makes a func literal once, where it is written, so there is at most one
// MakeClosure (see closures.makersOf). With none, the source is unknown.
//
//declscope:shared // tracing.go traces a free variable
func (r resolver) resolveFreeVar(fv *ssa.FreeVar, path []sel) iter.Seq[resolved] {
	return func(yield func(resolved) bool) {
		r.yieldFreeVar(fv, path, yield)
	}
}

// resolveWalked yields the sources of what a walk of walked found at at, as
// resolveStored does, after an unknown one when unseenUnknown is set and at
// may be written where that walk does not see.
func (r resolver) resolveWalked(at place, walked *ssa.Function, stored storeReaching) iter.Seq[resolved] {
	return func(yield func(resolved) bool) {
		r.yieldWalked(at, walked, nil, stored, yield)
	}
}

// yieldWalked yields what resolveWalked does, and reports whether yield
// asked for more. via is the call of a func literal that at is passed to, as
// writtenUnseen takes it.
func (r resolver) yieldWalked(
	at place,
	walked *ssa.Function,
	via ssa.Instruction,
	stored storeReaching,
	yield func(resolved) bool,
) bool {
	if r.unseenUnknown && r.closures.writtenUnseen(at, walked, via) && !yield(resolved{}) {
		return false
	}
	return r.yieldStored(stored, yield)
}

// yieldStored yields what resolveStored does, and reports whether yield asked
// for more.
func (r resolver) yieldStored(stored storeReaching, yield func(resolved) bool) bool {
	if stored.unknown && !yield(resolved{}) {
		return false
	}
	for _, c := range stored.captured {
		if !r.yieldFreeVar(c.root.(*ssa.FreeVar), c.path, yield) {
			return false
		}
	}
	for _, pp := range stored.params {
		if !r.yieldParam(pp, yield) {
			return false
		}
	}
	for _, v := range stored.values {
		if !yield(resolved{v: v}) {
			return false
		}
	}
	return true
}

// yieldFreeVar yields what resolveFreeVar does, and reports whether yield
// asked for more.
func (r resolver) yieldFreeVar(fv *ssa.FreeVar, path []sel, yield func(resolved) bool) bool {
	fn := fv.Parent()
	idx := slices.Index(fn.FreeVars, fv)
	makers := r.closures.makersOf(fn)
	if len(makers) == 0 {
		return yield(resolved{}) // A wrapper, such as a method value, made where it cannot be seen
	}
	for _, mc := range makers {
		stored := storesReachingClosure(mc.Bindings[idx], path, mc, r.positions)
		if !r.yieldWalked(place{root: mc.Bindings[idx], path: path}, mc.Parent(), nil, stored, yield) {
			return false
		}
	}
	return true
}

// yieldParam yields the sources of the value at pp, in a parameter of a func
// literal, as each call of the func literal passes it, and reports whether
// yield asked for more. The value is what is at the same selections from the
// argument when the call runs:
//
//	func(p *zerolog.Logger) {
//	    p.Info().Msg("x")       // reads *p, which is l below
//	}(&l)
//
// A call that passes the parameter on, as a recursive call does, reaches the
// same parameter again, which enter leaves out.
func (r resolver) yieldParam(pp storeParamPath, yield func(resolved) bool) bool {
	more := true
	r.enter(pp.param, func() {
		fn := pp.param.Parent()
		calls, ok := r.closures.callsOf(fn)
		if !ok || len(calls) == 0 {
			more = yield(resolved{})
			return
		}
		idx := slices.Index(fn.Params, pp.param)
		for _, call := range calls {
			if more = r.yieldArgument(call.Common().Args[idx], call, pp, yield); !more {
				return
			}
		}
	})
	return more
}

// yieldArgument yields the sources of the value at the selections of pp from
// arg, which call passes for the parameter of pp, and reports whether yield
// asked for more.
func (r resolver) yieldArgument(arg ssa.Value, call ssa.CallInstruction, pp storeParamPath, yield func(resolved) bool) bool {
	addr, read := arg, ssa.Instruction(call)
	switch {
	case !pp.pointer:
		// A struct or array passed by value is loaded from where it is
		load, ok := asLoad(arg)
		if !ok {
			return yield(resolved{})
		}
		addr, read = load.X, load
	case len(pp.path) == 0:
		// What the pointer passed points to, as for the receiver of a call
		if resolvesThrough(arg) {
			if stored, ok := storesReaching(arg, call, r.positions); ok {
				at, _ := placeOf(arg)
				return r.yieldWalked(at, call.Parent(), call, stored, yield)
			}
		}
		return yield(resolved{v: arg, pointer: true})
	}
	at, _ := placeOf(addr)
	stored, ok := storesReachingAt(at.with(pp.path...), read, r.positions)
	if !ok {
		return yield(resolved{})
	}
	return r.yieldWalked(at.with(pp.path...), call.Parent(), call, stored, yield)
}

// resolvesThrough reports whether addr is an address that a read goes
// through to what is stored there, such as the receiver of Logger.Info, which
// has a pointer receiver: a local variable, a field or element of an
// address, a variable a closure captures, or a pointer parameter. A
// *zerolog.Event is the value itself, not a variable it is read from.
//
//declscope:shared // tracing.go traces the stores behind such an address
func resolvesThrough(addr ssa.Value) bool {
	if zerologapi.TypeOf(addr.Type()) == zerologapi.TypeEvent {
		return false
	}
	switch a := addr.(type) {
	case *ssa.Alloc, *ssa.FieldAddr, *ssa.IndexAddr, *ssa.FreeVar:
		return true
	case *ssa.Parameter:
		_, isPtr := a.Type().Underlying().(*types.Pointer)
		return isPtr
	}
	return false
}
