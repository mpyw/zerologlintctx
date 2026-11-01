// resolve.go finds the values a read can see, as sources one at a time: what
// store.go found at an address, with the variables a closure captures traced
// to where the closure is made, and the parameters of a func literal traced to
// what its calls pass, until only values stored, or unknown, are left.
// The caller judges each source, as the tracer of internal/chain does.

package ssaflow

import (
	"go/token"
	"go/types"
	"iter"
	"slices"

	"golang.org/x/tools/go/ssa"
)

// Resolved is one source of what a read can see.
type Resolved struct {
	// V is the value, or nil when it is not known: the value the caller
	// passed through a pointer parameter, or an aggregate copied whole from
	// where no store can be seen.
	V ssa.Value
	// Pointer reports that v is a Pointer, and the source is what it points
	// to, which resolve could not follow, such as a Pointer that is not in a
	// variable. A nil Pointer is not a nil value.
	Pointer bool
}

// Resolver finds the sources of what a read can see. It resolves a
// parameter of a func literal through the calls of the func literal, and its
// caller decides what to do when it gets to the same parameter again.
type Resolver struct {
	// Positions finds where each instruction is in its block. It may be
	// shared by the resolvers of a package.
	Positions Positions
	// Closures indexes the closures and func literals of the package.
	Closures *Closures
	// Model tells what the library does with memory that its signatures do
	// not tell.
	Model Model
	// Enter runs expand, which resolves param through the calls of its func
	// literal, unless param is already being resolved, as a recursive call
	// leads back to it. Then it does nothing: the other calls give the value.
	Enter func(param *ssa.Parameter, expand func())
	// UnseenUnknown adds an unknown source for a variable that may be
	// written where the walk of a function for its stores does not see it,
	// such as in another closure (see Closures.writtenUnseen). Without it,
	// such a write is left out, as the tracer of internal/chain leaves it
	// out, whose known limitations those are.
	UnseenUnknown bool
	// Bound, when not nil, is the call of the func literal BoundFunc whose
	// result is resolved: a parameter of BoundFunc is what Bound passes,
	// not what each call of it does.
	//
	//	id := func(e *zerolog.Event) *zerolog.Event { h := holder{e}; return h.e }
	//	a := id(logger.Info().Ctx(ctx)) // with Bound this call, h.e is this argument
	Bound     ssa.CallInstruction
	BoundFunc *ssa.Function
}

// Resolve finds what can be at addr when read runs (see storesReaching). ok
// is false when storesReaching knows nothing of addr.
func (r Resolver) Resolve(addr ssa.Value, read ssa.Instruction) (sources iter.Seq[Resolved], ok bool) {
	stored, ok := storesReaching(addr, read, r.Positions, r.Model)
	if !ok {
		return nil, false
	}
	at, _ := PlaceOf(addr)
	walked := parentFunc(addr)
	if read != nil {
		walked = read.Parent()
	}
	return r.resolveWalked(at, walked, stored), true
}

// ResolveAt finds what can be at the address at when read runs, as
// storesReachingAt does. ok is false when storesReachingAt knows nothing of
// it.
func (r Resolver) ResolveAt(at Place, read ssa.Instruction) (sources iter.Seq[Resolved], ok bool) {
	stored, ok := storesReachingAt(at, read, r.Positions)
	if !ok {
		return nil, false
	}
	return r.resolveWalked(at, read.Parent(), stored), true
}

// ResolveEmbedded yields the sources of the receiver of a promoted method,
// at the embedded fields of the struct of type typ at the address at, as read
// reads it. call is the call of the method expression.
//
// An embedded field but the last may be a pointer. The receiver is then at
// the rest of the fields of what each pointer that can be stored there points
// to, as call reads it. A nil pointer has no event to log, and adds nothing:
//
//	type outer struct{ *inner }
//	o := outer{&inner{e}}  // t3 = new inner ; *&t3.Event = e ; *&t1.inner = t3
//	outer.Msg(o, "x")      // reads t1.inner, then t3.Event
//
// A pointer that is not known is an unknown source.
func (r Resolver) ResolveEmbedded(at Place, typ types.Type, embedded []int, read, call ssa.Instruction) iter.Seq[Resolved] {
	return func(yield func(Resolved) bool) {
		r.yieldEmbedded(at, typ, embedded, read, call, yield)
	}
}

// yieldEmbedded yields what ResolveEmbedded does, and reports whether yield
// asked for more.
func (r Resolver) yieldEmbedded(
	at Place,
	typ types.Type,
	embedded []int,
	read, call ssa.Instruction,
	yield func(Resolved) bool,
) bool {
	at, n, field := at.withEmbedded(typ, embedded)
	stored, ok := storesReachingAt(at, read, r.Positions)
	switch {
	case n == len(embedded) && ok:
		return r.yieldStored(stored, yield)
	case !ok || stored.unknown || len(stored.captured) > 0 || len(stored.params) > 0:
		return yield(Resolved{})
	}
	elem := field.Underlying().(*types.Pointer).Elem()
	for _, v := range stored.values {
		if IsNilConst(v) {
			continue
		}
		next, _ := PlaceOf(v)
		if !r.yieldEmbedded(next, elem, embedded[n:], call, call, yield) {
			return false
		}
	}
	return true
}

// ResolveFreeVar yields the sources of what is at path, the selections from
// fv, nil for fv itself, as its closure reads them: what can be there in the
// variable that each MakeClosure of the closure binds, when the closure runs.
//
//	l := base
//	l.Debug().Ctx(ctx).Msg("start") // reads base, before the closure is made
//	l = l.With().Ctx(ctx).Logger()
//	defer func() { l.Info().Msg("end") }()
//
// SSA makes a func literal once, where it is written, so there is at most one
// MakeClosure (see Closures.makersOf). With none, the source is unknown.
func (r Resolver) ResolveFreeVar(fv *ssa.FreeVar, path []sel) iter.Seq[Resolved] {
	return func(yield func(Resolved) bool) {
		r.yieldFreeVar(fv, path, yield)
	}
}

// resolveWalked yields the sources of what a walk of walked found at at, as
// yieldStored does, after an unknown one when unseenUnknown is set and at
// may be written where that walk does not see.
func (r Resolver) resolveWalked(at Place, walked *ssa.Function, stored storeReaching) iter.Seq[Resolved] {
	return func(yield func(Resolved) bool) {
		r.yieldWalked(at, walked, nil, stored, yield)
	}
}

// yieldWalked yields what resolveWalked does, and reports whether yield
// asked for more. via is the call of a func literal that at is passed to, as
// writtenUnseen takes it.
func (r Resolver) yieldWalked(
	at Place,
	walked *ssa.Function,
	via ssa.Instruction,
	stored storeReaching,
	yield func(Resolved) bool,
) bool {
	if r.UnseenUnknown && r.Closures.writtenUnseen(at, walked, via, stored.followed) && !yield(Resolved{}) {
		return false
	}
	return r.yieldStored(stored, yield)
}

// yieldStored yields the sources of what storesReaching found: unknown, when
// some path carries a value the function cannot tell, what each captured
// variable holds as the closure runs, what the calls of a func literal pass
// for its parameters, and the values stored. It reports whether yield asked
// for more.
func (r Resolver) yieldStored(stored storeReaching, yield func(Resolved) bool) bool {
	if stored.unknown && !yield(Resolved{}) {
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
		if !yield(Resolved{V: v}) {
			return false
		}
	}
	return true
}

// yieldFreeVar yields what ResolveFreeVar does, and reports whether yield
// asked for more.
func (r Resolver) yieldFreeVar(fv *ssa.FreeVar, path []sel, yield func(Resolved) bool) bool {
	fn := fv.Parent()
	idx := slices.Index(fn.FreeVars, fv)
	makers := r.Closures.makersOf(fn)
	if len(makers) == 0 {
		return yield(Resolved{}) // A wrapper, such as a method value, made where it cannot be seen
	}
	for _, mc := range makers {
		stored := storesReachingClosure(mc.Bindings[idx], path, mc, r.Positions)
		if !r.yieldWalked(Place{root: mc.Bindings[idx], path: path}, mc.Parent(), nil, stored, yield) {
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
// same parameter again, which enter leaves out. With Bound, the call of
// BoundFunc is Bound alone.
func (r Resolver) yieldParam(pp storeParamPath, yield func(Resolved) bool) bool {
	more := true
	r.Enter(pp.param, func() {
		fn := pp.param.Parent()
		calls, ok := r.Closures.CallsOf(fn)
		if r.Bound != nil && fn == r.BoundFunc {
			calls, ok = []ssa.CallInstruction{r.Bound}, true
		}
		if !ok || len(calls) == 0 {
			more = yield(Resolved{})
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
func (r Resolver) yieldArgument(arg ssa.Value, call ssa.CallInstruction, pp storeParamPath, yield func(Resolved) bool) bool {
	addr, read := arg, ssa.Instruction(call)
	switch {
	case !pp.pointer:
		// A struct or array passed by value is loaded from where it is
		load, ok := AsLoad(arg)
		if !ok {
			return yield(Resolved{})
		}
		addr, read = load.X, load
	case len(pp.path) == 0:
		// What the pointer passed points to, as for the receiver of a call
		if r.ResolvesThrough(arg) {
			if stored, ok := storesReaching(arg, call, r.Positions, r.Model); ok {
				at, _ := PlaceOf(arg)
				return r.yieldWalked(at, call.Parent(), call, stored, yield)
			}
		}
		return yield(Resolved{V: arg, Pointer: true})
	}
	at, _ := PlaceOf(addr)
	stored, ok := storesReachingAt(at.with(pp.path...), read, r.Positions)
	if !ok {
		return yield(Resolved{})
	}
	return r.yieldWalked(at.with(pp.path...), call.Parent(), call, stored, yield)
}

// ResolvesThrough reports whether addr is an address that a read goes
// through to what is stored there, such as the receiver of Logger.Info, which
// has a pointer receiver: a local variable, a field or element of an
// address, a variable a closure captures, a pointer parameter, a pointer a
// call returns, or that is received from a channel or looked up in a map,
// whose stores storesReaching walks for (see storesReachingPointer), or a
// pointer read from memory, which storesReaching follows to where it points
// (see storePointers):
//
//	b := box{l}                // *&t0.p = l
//	*b.p = ctxLogger           // t1 = *&t0.p ; *t1 = ...
//	b.p.Info()                 // t2 = *&t0.p ; Info(t2), and t2 is t1
//
// A pointer that the model takes for a handle, such as a *zerolog.Event, is
// the value itself, not a variable it is read from.
func (r Resolver) ResolvesThrough(addr ssa.Value) bool {
	if r.Model.IsHandle(addr.Type()) {
		return false
	}
	switch addr.(type) {
	case *ssa.Alloc, *ssa.FieldAddr, *ssa.IndexAddr, *ssa.FreeVar:
		return true
	}
	if _, isPtr := addr.Type().Underlying().(*types.Pointer); !isPtr {
		return false
	}
	switch a := addr.(type) {
	case *ssa.Parameter, *ssa.Call, *ssa.Lookup, *ssa.Extract:
		return true
	case *ssa.UnOp:
		if a.Op == token.ARROW {
			return true
		}
	}
	_, isRead := storePointerRead(addr)
	return isRead
}
