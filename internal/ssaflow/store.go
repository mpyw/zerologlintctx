// store.go finds what can be at an address when it is read. storesReaching is
// the entry; it leans on walk.go to walk the control flow, and on match.go to
// decide whether two pointers that are not in a variable are the same.

package ssaflow

import (
	"slices"

	"golang.org/x/tools/go/ssa"
)

// storesReaching finds the values that can be at addr when read runs: the
// stores to addr from which some path leads to read with no other store to
// addr between. A nil read stands for every read of addr. Multiple stores can
// reach a read on different control flow paths, and every one must be checked
// for context:
//
//	l := base                          // reaches the read on the else path
//	if cond {
//	    l = l.With().Ctx(ctx).Logger() // reaches it on the then path
//	}
//	l.Info().Msg("msg")                // should report: one path lacks ctx
//
// A store that an update overwrites does not reach the reads after it. Each
// update is a value of its own, which loads the value before it, so tracing it
// goes on from the stores that reach that load:
//
//	l := base
//	l = l.With().Ctx(ctx).Logger()     // the only store that reaches the read
//	l.Info().Msg("msg")                // Info takes &l with real zerolog
//
// A call that model says leaves its receiver neither stops a path nor counts
// as a read. Logger.UpdateContext is one: it changes the fields of the Logger
// in place, but not its context, since zerolog keeps only the fields the func
// writes, and drops what Context.Ctx sets.
//
// This holds for an address whose root is a local variable, a pointer
// parameter or a variable a closure captures: its field, its element, or
// itself. The stores that reach a read are known there, from the function
// alone. A store that may write an element, or not, such as one to arr[0]
// for a read of arr[i], is among them, but does not stop a path (see
// storeWalk.stepAt). A path that leads back to the declaration of a local
// variable carries its zero value, which has no value to trace. One that
// leads back to the entry of the function carries the value the caller
// passed, which is unknown, or the value of the captured variable when the
// closure runs, which is among captured.
//
// An address whose root is a pointer the function reads from such an address,
// such as h.p.l for a struct h with a pointer field p, is what is at the same
// selections from each pointer that can be there when the pointer is read.
// The read is then found as for an address in that pointer's variable (see
// storePointers).
//
// For any other address, whose root is a pointer that is not in a variable,
// such as one a call returns, or a global, the walk goes back from read to
// where the pointer is made (see storesReachingPointer). ok is false when no
// store of the function reaches read, and for a Phi of pointers with no such
// store, the variables it picks are walked instead (see storesReachingPicked).
//
// positions finds where each instruction is in its block, and may be kept
// across calls. model is asked of the calls addr is passed to, when read is
// nil.
//
//declscope:shared // resolve.go finds what reaches each read
func storesReaching(
	addr ssa.Value,
	read ssa.Instruction,
	positions Positions,
	model Model,
) (r storeReaching, ok bool) {
	at, tracked := PlaceOf(addr)
	if !tracked {
		if read != nil {
			if r, ok := newStorePointers(positions).reachingThrough(at, read); ok {
				r.followed = true
				return r, true
			}
		}
		if r, ok := storesReachingPointer(at, read, positions); ok {
			return r, true
		}
		if phi, isPhi := at.root.(*ssa.Phi); isPhi {
			return storesReachingPicked(phi, at.path, read, positions)
		}
		return r, false
	}

	w := &storeWalk{at: at, positions: positions, out: &r}
	if read != nil {
		w.walkToRead(read)
		return r, true
	}
	reads, escapes := storeReadsOf(addr, at, model)
	for _, rd := range reads {
		w.walkBefore(rd, storePath{})
	}
	for _, esc := range escapes {
		w.walkEscape(esc, storePath{})
	}
	return r, true
}

// storesReachingPointer finds the values that can be at at when read runs,
// for at whose root is a pointer that is not in a variable, and that
// storePointers cannot follow: a pointer a call returns, a Phi of pointers, a
// pointer read from a variable that another reference may change, such as a
// receiver a closure captures, a field of a struct value that holds one, or
// a global. A store through the same pointer is one through root, or through
// another read of it that addressesMatch takes for the same pointer:
//
//	t1 = *t0 ; *&t1.log = v   // reaches the read
//	t8 = *t0 ; Info(&t8.log)  // t8 and t1 are the same pointer
//
// The walk goes back from read, and stops each path at the first store, as
// for a variable, up to where the pointer is made (see
// storeWalk.walkPointerBefore). A path that gets there, for at the pointer
// itself, made other than by a load, carries what tracing the pointer finds
// (see storeReaching.made). A path that gets back to a pointer the caller
// passed, as b.p for a pointer parameter b is, carries what the caller
// passed, which is not known. Otherwise a path that gets there, or to the
// entry of the function, carries nothing: what is there was stored where the
// function cannot see, by the callee that returned the pointer, or before
// another pointer was put where it is read from. Only the stores of
// the function itself are traced, and ok is false when no path carries one,
// so that the caller traces the pointer itself.
//
// For a global, an update is left out, and the path goes on past it (see
// storePointer.global). A path that gets to the entry of the function, or
// out of the func literals it is in to the entry of one it cannot go on out
// of, reads the global as it was there, which is not known, when another
// path carries a store (see walkGlobalOut).
func storesReachingPointer(at Place, read ssa.Instruction, positions Positions) (r storeReaching, ok bool) {
	if _, isConst := at.root.(*ssa.Const); isConst || read == nil {
		return r, false
	}
	w := &storeWalk{at: at, positions: positions, out: &r, pointer: storePointerOf(at.root)}
	w.walkToRead(read)
	if r.isEmpty() {
		return r, false
	}
	r.unknown = r.unknown || w.pointer.unseen
	return r, true
}

// storePointer is where a pointer that is not in a variable is made, for a
// walk of the stores through it.
type storePointer struct {
	// def is the instruction that makes the pointer, or the value it is a
	// field of, and nil when the function does not make it, as for a
	// parameter or a global.
	def ssa.Instruction
	// from is the address def loads the pointer from, when def is a load.
	// Before def, the same pointer is there until a store puts another one
	// there.
	from ssa.Value
	// same holds whether each root of a store is the same pointer, as
	// addressesMatch tells.
	same map[ssa.Value]bool
	// global reports that the root is a global. It can be changed anywhere,
	// by another goroutine too, so what a load of it read is not known
	// either: an update of it, which builds on such a load, is left out.
	global bool
	// unseen reports that a path got to the entry of a function, for a
	// global, that the walk cannot go on out of (see walkGlobalOut). Such a
	// path reads what was stored where the function cannot see.
	unseen bool
	// closure, when not empty, are the runs of the closures the pointer is
	// read in, innermost first, out to the function of the walk: the root of
	// a store there is the same pointer as matchAcrossClosure tells (see
	// walkPointerOut).
	closure []matchClosureRun
	// source, when not nil, is the copy the pointer was taken from, in the
	// function it is read in: the root of a store is the same pointer when it
	// is read from where the copy loaded it from (see walkPastCopyIn).
	source *storeCopySource
}

// storePointerOf returns where root, a pointer that is not in a variable, is
// made: by root, or by the struct value it is a field of.
func storePointerOf(root ssa.Value) *storePointer {
	for {
		f, isField := root.(*ssa.Field)
		if !isField {
			break
		}
		root = f.X
	}
	_, global := root.(*ssa.Global)
	p := &storePointer{same: make(map[ssa.Value]bool), global: global}
	p.def, _ = root.(ssa.Instruction)
	if load, isLoad := AsLoad(root); isLoad {
		p.from = load.X
	}
	return p
}

// walkPointerBefore walks every path that leads to a point of starts, in its
// state, for a walk whose root is a pointer that is not in a variable. Each
// path goes back to def, where the pointer is made, and on from there only
// when def loads it from an address: up to a store of another pointer there,
// or the declaration of the variable the address is in.
//
//	*t0 = a                   // stops the path: before it, t0 holds another pointer
//	t1 = *t0 ; *&t1.log = v   // reaches the read, past def
//	t8 = *t0                  // def
//	Info(&t8.log)
//
// A store of another pointer at from between def and the read does not stop a
// path: the pointer read is the one def loaded. A path that gets to the entry
// of a closure goes on from each call of it (see walkPointerOut), and so does
// one for a global, which no def makes (see walkGlobalOut). One that
// gets to the entry of the function, when from is in what a parameter holds,
// or to a store of a parameter at from, carries the caller's value:
//
//	if cond { *b.p = v }      // b is a pointer parameter
//	b.p.Info()                // reads what the caller passed when cond is false
//
// With a guard, what a path does at def, or at the entry, it does once the
// guard is resolved (see storeGuard): before, the pointer is not yet the
// leaf there.
func (w *storeWalk) walkPointerBefore(starts []WalkFrom[storePath]) {
	def, reached := w.pointer.def, false
	toDef := func(in ssa.Instruction) WalkStep {
		if def != nil && in == def {
			reached = true
			return WalkStop
		}
		return w.stepAt(in)
	}
	entered := false
	toEntry := func(st storePath) {
		entered = entered || w.pointer.global && st.resolved()
	}
	w.walkStates(starts, storeStates{visit: toDef, atEntry: toEntry})
	if entered {
		w.walkGlobalOut(starts[0].Point.block.Parent())
	}
	from := w.pointer.from
	if reached && from == nil && len(w.at.path) == 0 {
		w.out.addMade(w.at.root)
	}
	if !reached || from == nil {
		return
	}
	var copies []storeCopySource
	pastDef := func(in ssa.Instruction) WalkStep {
		if matchOverwrites(in, from) {
			if store, isStore := in.(*ssa.Store); isStore && storeIsCallerParam(store.Val) {
				w.out.unknown = true // The pointer is in what the caller passed
			} else if c, isCopy := storeCopyOf(in, from, nil); isCopy {
				copies = append(copies, c)
			}
			return WalkStop
		}
		return w.stepAt(in)
	}
	entered = false
	atEntry := func() bool {
		entered = true
		if storeIsCallerParam(storeReadFrom(from)) {
			w.out.unknown = true // The pointer is read from what the caller passed
		}
		return true
	}
	walkBack([]WalkPoint{WalkPointBefore(def, w.positions)}, pastDef, atEntry, nil)
	for _, c := range copies {
		w.walkPastCopyIn(c)
	}
	if fv, isFreeVar := storeReadFrom(from).(*ssa.FreeVar); entered && isFreeVar {
		w.walkPointerOut(def.Parent(), fv, nil, storeLoadsIn(from))
	}
}

// storeCopySource is where a copy of a whole aggregate, made in the function
// the pointer of a walk is read in, took the pointer from: the pointer at
// sels in what load loaded, for store, the copy.
//
//	b := h                            // t2 = *h ; *t1 = t2: load is t2, sels .p
//	q := b.p                          // t4 = *&t1.p
type storeCopySource struct {
	store *ssa.Store
	load  *ssa.UnOp
	sels  []sel
}

// origin returns where the pointer c took is read from in the end: at c.sels
// in what c.load loads, past the copies that put it there (see
// matchOriginAt).
func (c *storeCopySource) origin() matchOrigin {
	root, sels := matchSelsOf(c.load.X)
	return matchOriginAt(matchOrigin{root: root, sels: append(sels, c.sels...), at: c.load})
}

// storeCopyOf returns where instr, which puts another value at the address
// sels from addr, takes the pointer there from, when it is a store of a whole
// aggregate that holds that address, loaded from memory. ok is false for any
// other instruction, such as a store of the pointer itself.
func storeCopyOf(instr ssa.Instruction, addr ssa.Value, sels []sel) (_ storeCopySource, ok bool) {
	store, isStore := instr.(*ssa.Store)
	if !isStore {
		return storeCopySource{}, false
	}
	load, isLoad := AsLoad(store.Val)
	to, toSels := matchSelsOf(store.Addr)
	x, xSels := matchSelsOf(addr)
	n := len(toSels)
	if !isLoad || n > len(xSels) || !addressesMatch(to, x) || !slices.EqualFunc(toSels, xSels[:n], sel.same) {
		return storeCopySource{}, false
	}
	rest := append(xSels[n:], sels...)
	return storeCopySource{store: store, load: load, sels: rest}, len(rest) > 0
}

// walkPastCopyIn goes on past c, a copy of a whole aggregate that put the
// root of the walk where it is read from, in the function it is read in. The
// pointer is then the one at c.sels in what c.load loads, and a store through
// a pointer read from the same place, as the load saw it, is a store through
// the root (see matchLoadedSels):
//
//	func() {
//	    *h.p = base                   // reaches the read, through h.p
//	    b := h                        // the copy: t2 = *h ; *t1 = t2
//	    q := b.p
//	    if other {
//	        *q = l.With().Ctx(ctx).Logger()
//	    }
//	    q.Info().Msg("x")             // base when other is false: reported
//	}()
//
// The walk goes back from the copy as walkPastCopy goes back from a call: a
// store over where c.load loads, before it loads, stops a path, with the
// caller's value for a parameter, and past another copy, it goes on past that
// one. A path that gets to the entry of the function carries the caller's
// value, when c.load loads from a parameter, and goes on out of the closure
// when it loads from a variable the closure captures (see walkPointerOut).
// Each copy gone past is before the one it is found from, and cannot run
// after the load of that one, so the walk does not come back to a copy.
func (w *storeWalk) walkPastCopyIn(c storeCopySource) {
	past := &storeWalk{
		at:        w.at,
		positions: w.positions,
		out:       w.out,
		pointer:   &storePointer{same: make(map[ssa.Value]bool), source: &c},
	}
	var copies []storeCopySource
	visit := func(in ssa.Instruction) WalkStep {
		store, isStore := in.(*ssa.Store)
		if !isStore || w.positions.reaches(c.load, store) ||
			!matchOverwrites(store, c.load.X) && !storeOverwritesWithin(store, c.load.X, c.sels) {
			return past.stepAt(in)
		}
		if storeIsCallerParam(store.Val) {
			w.out.unknown = true // The pointer is in what the caller passed
		} else if next, isCopy := storeCopyOf(store, c.load.X, c.sels); isCopy {
			copies = append(copies, next)
		}
		return WalkStop
	}
	entered := false
	atEntry := func() bool {
		entered = true
		if storeIsCallerParam(storeReadFrom(c.load.X)) {
			w.out.unknown = true // The pointer is read from what the caller passed
		}
		return true
	}
	walkBack([]WalkPoint{WalkPointBefore(c.store, w.positions)}, visit, atEntry, nil)
	for _, next := range copies {
		w.walkPastCopyIn(next)
	}
	if fv, isFreeVar := storeReadFrom(c.load.X).(*ssa.FreeVar); entered && isFreeVar {
		past.walkPointerOut(c.store.Parent(), fv, nil, storeLoadsIn(c.load.X))
	}
}

// walkGlobalOut goes on from the entry of fn, for a walk whose root is a
// global, from each call of fn when it is a func literal that is only called
// where it is made (see storeCallsOf). A store before the call is what the
// func literal reads there:
//
//	g.log = zerolog.Ctx(ctx).With().Logger() // reaches the read
//	func() { g.log.Info().Msg("x") }()
//
// A path that gets to the entry of that function goes on out of it in the
// same way. One that gets to the entry of a declared function, or of a func
// literal that is not only called, reads what was stored where the function
// cannot see. When another path carries a store, it carries what is not
// known (see storesReachingPointer).
func (w *storeWalk) walkGlobalOut(fn *ssa.Function) {
	calls, ok := storeCallsOf(fn)
	if !ok {
		w.pointer.unseen = true
		return
	}
	from := make([]WalkPoint, 0, len(calls))
	for _, call := range calls {
		from = append(from, WalkPointBefore(call, w.positions))
	}
	entered := false
	atEntry := func() bool {
		entered = true
		return true
	}
	walkBack(from, w.stepAt, atEntry, nil)
	if entered {
		w.walkGlobalOut(fn.Parent())
	}
}

// storeCallsOf returns the calls of fn, a func literal, when it is only
// called where it is made: each use of it is a call of it, or makes a closure
// of it that is only called (see closureRuns.callsOnly). A func literal that
// captures nothing is called as the function itself. ok is false when fn is
// a declared function, or it has another use, such as a go or defer
// statement, or one that passes it on.
func storeCallsOf(fn *ssa.Function) (calls []ssa.Instruction, ok bool) {
	if fn.Parent() == nil {
		return nil, false
	}
	for _, ref := range *fn.Referrers() {
		switch r := ref.(type) {
		case *ssa.MakeClosure:
			runs, ok := closureRunsOf(r)
			runCalls, called := runs.callsOnly()
			if !ok || !called {
				return nil, false
			}
			calls = append(calls, runCalls...)
		case *ssa.Call:
			if r.Call.Value != fn || slices.Contains(r.Call.Args, ssa.Value(fn)) {
				return nil, false
			}
			calls = append(calls, r)
		default:
			return nil, false
		}
	}
	return calls, true
}

// walkPointerOut goes on from the entry of fn, a closure that reads the root
// of the walk from fv, a variable it captures, in the function that makes it.
// The walk goes back from each call of the closure, and a store there through
// a pointer read from the same variable, with the same stores reaching the
// read as reach the call, is a store through the root:
//
//	*t0 = h                           // h is a struct parameter
//	t2 = *&t0.m ; *&t2.log = v        // reaches the read below
//	func() { h.m.log.Info() }()       // t1 = *&h.m ; Info(&t1.log), and t1 is t2
//
// A path that gets to the entry of that function, when it is a closure that
// captures the variable in turn, goes on out of it in the same way. When that
// closure stores to the variable, or through it where the pointer is read
// from, as *pp = other does, the pointer may be another one there. A path
// back from the call stops at such a store, and one that gets to the entry
// of that closure carries what is not known. A store through the pointer itself, as
// **pp = l does, leaves the pointer as it is. In the function the variable is
// declared in, a walk past a copy of what it holds goes on from the call
// instead, when one is found (see walkPastCopy).
// runs are the runs of the closures that the walk went out of to get to fn,
// innermost first, and loads is how many loads the pointer takes from fv (see
// storeLoadsIn). A closure that is not only called where it is made adds
// nothing. A walk that went past a copy already looks for no other one.
func (w *storeWalk) walkPointerOut(fn *ssa.Function, fv *ssa.FreeVar, runs []matchClosureRun, loads int) {
	idx := slices.Index(fn.FreeVars, fv)
	source := w.pointer.source
	pastCopy := source != nil || slices.ContainsFunc(runs, func(r matchClosureRun) bool { return r.copied != nil })
	for _, mc := range storeMakersOf(fn) {
		closureRuns, ok := closureRunsOf(mc)
		if !ok {
			continue
		}
		calls, called := closureRuns.callsOnly()
		if !called {
			continue
		}
		bound, boundFree := mc.Bindings[idx].(*ssa.FreeVar)
		for _, call := range calls {
			var copied *ssa.Store
			if !pastCopy {
				copied = storeCopiedTo(mc.Bindings[idx], call, loads)
			}
			outRuns := append(slices.Clip(runs), matchClosureRun{mc: mc, run: call, copied: copied})
			out := &storeWalk{
				at:        w.at,
				positions: w.positions,
				out:       w.out,
				pointer:   &storePointer{same: make(map[ssa.Value]bool), closure: outRuns, source: source},
			}
			if copied != nil {
				out.walkPastCopy(call, w.pointer.from)
				continue
			}
			atEntry := func() bool {
				switch {
				case !boundFree:
					// The variable is declared in the function that makes mc.
					// When it only holds a parameter, the pointer is read from
					// what the caller passed
					if alloc, isAlloc := mc.Bindings[idx].(*ssa.Alloc); isAlloc && w.storeHoldsCallerParam(alloc) {
						w.out.unknown = true
					}
				case storesThrough(bound, loads):
					w.out.unknown = true
				default:
					w.walkPointerOut(mc.Parent(), bound, outRuns, loads)
				}
				return true
			}
			visit := out.stepAt
			if boundFree {
				visit = func(in ssa.Instruction) WalkStep {
					if store, isStore := in.(*ssa.Store); isStore && storeGoesThrough(store, bound, loads) {
						return WalkStop // Before it, another pointer may be there
					}
					return out.stepAt(in)
				}
			}
			walkBack([]WalkPoint{WalkPointBefore(call, w.positions)}, visit, atEntry, nil)
		}
	}
}

// storeCopiedTo returns the store that copies what bound, the variable a
// closure binds, holds when call runs the closure, for a walk out of the
// closure (see walkPastCopy). It is the one store that reaches call of the
// whole variable, as SSA makes for a parameter the closure captures, or for
// b := other. It is nil when bound is not a local variable, when another
// store, or none, may reach call, and when the function stores to a
// selection from bound where the pointer may be read from, with at most
// loads loads (see storesThrough).
func storeCopiedTo(bound ssa.Value, call ssa.Instruction, loads int) *ssa.Store {
	alloc, isAlloc := bound.(*ssa.Alloc)
	if !isAlloc {
		return nil
	}
	for instr := range InstrsIn(alloc.Parent()) {
		store, isStore := instr.(*ssa.Store)
		if isStore && store.Addr != alloc && storeReadFrom(store.Addr) == alloc && storeLoadsIn(store.Addr) <= loads {
			return nil
		}
	}
	var copied *ssa.Store
	for instr := range matchStoresBefore(alloc, call) {
		store, isStore := instr.(*ssa.Store)
		if !isStore || store.Addr != alloc || copied != nil {
			return nil
		}
		copied = store
	}
	return copied
}

// walkPastCopy walks for the stores through the root, a pointer read in a
// closure from a variable it captures, from call, a call of the closure, when
// a store copies what that variable holds there (see storeCopiedTo). The
// copy is the last of the runs of the pointer, and from is where the root is
// read from in the closure. A store through a pointer read from the value
// copied, or from where it was loaded from to be copied, is a store through
// the root (see matchCopiedLoad):
//
//	*other.p = v                      // reaches the read below
//	b := other                        // the copy: *t0 = other
//	func() { b.p.Info() }()           // t1 = *b ; Info(*&t1.p), and t1 is other
//
// The walk goes back from call, past the copy, as for a pointer not in a
// variable. A store over where the value copied was loaded from, before it
// was loaded, stops a path. When it stores a parameter, as SSA makes for a
// struct parameter, the path carries the caller's value. So does one that
// gets to the entry of the function, when the value copied is read from a
// parameter:
//
//	if cond { *b.p = v }              // b is a struct parameter: *t0 = b is the copy
//	func() { b.p.Info() }()           // reads what the caller passed when cond is false
//
// A path that gets to the entry of a closure, when the value copied is loaded
// from a variable it captures, as b := *other is in a func literal, goes on
// out of it (see walkPointerOut). The pointer is read there as through the
// address the value copied is loaded from, at the selections from that is
// read from.
func (w *storeWalk) walkPastCopy(call ssa.Instruction, from ssa.Value) {
	runs := w.pointer.closure
	copied := runs[len(runs)-1].copied
	load, isLoad := AsLoad(copied.Val)
	sels := storeSelsOfRoot(from)
	overwrites := func(store *ssa.Store) bool {
		return isLoad && !w.positions.reaches(load, store) &&
			(matchOverwrites(store, load.X) || storeOverwritesWithin(store, load.X, sels)) ||
			storeOverwritesAcross(store, from, runs)
	}
	visit := func(in ssa.Instruction) WalkStep {
		if store, isStore := in.(*ssa.Store); isStore && overwrites(store) {
			if storeIsCallerParam(store.Val) {
				w.out.unknown = true // The pointer is in what the caller passed
			}
			return WalkStop
		}
		return w.stepAt(in)
	}
	entered := false
	atEntry := func() bool {
		entered = true
		if storeIsCallerParam(storeReadFrom(copied.Val)) {
			w.out.unknown = true // The pointer is read from what the caller passed
		}
		return true
	}
	walkBack([]WalkPoint{WalkPointBefore(call, w.positions)}, visit, atEntry, nil)
	if fv, isFreeVar := storeReadFrom(copied.Val).(*ssa.FreeVar); entered && isLoad && isFreeVar {
		// The value copied is read from a variable the closure of call
		// captures: the same pointer is there as that closure starts
		w.walkPointerOut(call.Parent(), fv, runs, storeLoadsIn(load.X)+storeLoadsIn(from))
	}
}

// storeOverwritesWithin reports whether store puts another value inside what
// addr points to, on the way to sels from it: at a selection from addr that
// overlaps sels (see Place.overlaps).
//
//	other.p = l                       // within *t1, at .p
//	b := *other                       // t1 = *other ; t2 = *t1: the copy
//	func() { b.p.Info() }()           // sels is .p
func storeOverwritesWithin(store *ssa.Store, addr ssa.Value, sels []sel) bool {
	at, _ := PlaceOf(store.Addr)
	if len(at.path) == 0 || !addressesMatch(at.root, addr) {
		return false
	}
	at.root = addr
	return at.overlaps(Place{root: addr, path: sels})
}

// storeSelsOfRoot returns the selections that v, an address, takes from the
// value storeReadFrom returns for it, up to the first load, outermost first.
func storeSelsOfRoot(v ssa.Value) []sel {
	var sels []sel
	for {
		if load, isLoad := AsLoad(v); isLoad {
			v, sels = load.X, nil
			continue
		}
		x, s, ok := addrSel(v)
		if !ok {
			break
		}
		sels = append(sels, s)
		v = x
	}
	slices.Reverse(sels)
	return sels
}

// storeOverwritesAcross reports whether store, in the function of the last of
// runs, puts another value at from, an address in the closure runs[0] runs,
// or at an aggregate that holds it, as matchAcrossClosure matches them:
//
//	other.p = l                       // overwrites b.p, as b := other copied it
//	func() { b.p.Info() }()           // from is &(*b).p
//
// A store to the variable the closure captures, or to a selection from it,
// is not one: before the copy, what it held is not what the closure reads.
func storeOverwritesAcross(store *ssa.Store, from ssa.Value, runs []matchClosureRun) bool {
	for x, ok := from, true; ok; x, _, ok = addrSel(x) {
		if _, isFreeVar := x.(*ssa.FreeVar); isFreeVar {
			return false
		}
		if matchAcrossClosure(x, store.Addr, runs) {
			return true
		}
	}
	return false
}

// storesThrough reports whether the function of fv, a free variable, stores
// to it, or to what is read through it with at most loads loads: a field or
// element of it, or what a pointer loaded from it points to. A pointer read
// from an address that loads loads take from fv may be replaced by such a
// store, and not by one through the pointer itself, which takes more:
//
//	b.p = other               // *&(*b).p = other, one load: replaces b.p
//	*b.p = l                  // **&(*b).p = l, two loads: writes the Logger only
//
// A store through a copy of fv, or through a pointer passed to a function, is
// not seen.
func storesThrough(fv *ssa.FreeVar, loads int) bool {
	for instr := range InstrsIn(fv.Parent()) {
		if store, isStore := instr.(*ssa.Store); isStore && storeGoesThrough(store, fv, loads) {
			return true
		}
	}
	return false
}

// storeGoesThrough reports whether store is to fv, a free variable, or to
// what is read through it with at most loads loads, as storesThrough looks
// for.
func storeGoesThrough(store *ssa.Store, fv *ssa.FreeVar, loads int) bool {
	return storeReadFrom(store.Addr) == fv && storeLoadsIn(store.Addr) <= loads
}

// storeLoadsIn returns how many loads v, an address or a pointer, takes from
// the value storeReadFrom returns for it.
func storeLoadsIn(v ssa.Value) int {
	loads := 0
	for {
		if load, isLoad := AsLoad(v); isLoad {
			v = load.X
			loads++
			continue
		}
		x, _, ok := addrSel(v)
		if !ok {
			return loads
		}
		v = x
	}
}

// storeReadFrom returns the value that v, an address or a pointer, is read
// from through loads, fields and elements: v itself when it is none of those.
func storeReadFrom(v ssa.Value) ssa.Value {
	for {
		if load, isLoad := AsLoad(v); isLoad {
			v = load.X
			continue
		}
		x, _, ok := addrSel(v)
		if !ok {
			return v
		}
		v = x
	}
}

// samePointer reports whether root, the root of a store, is the pointer the
// walk looks for the stores through.
func (p *storePointer) samePointer(root, pointer ssa.Value) bool {
	same, done := p.same[root]
	switch {
	case done:
	case p.source != nil:
		same = matchLoadedSels(p.source.load, p.source.sels, root, p.closure) ||
			len(p.closure) == 0 && matchOriginOfIs(p.source.origin(), root)
	case len(p.closure) > 0:
		same = matchAcrossClosure(pointer, root, p.closure)
	default:
		same = addressesMatch(root, pointer) || matchSameOrigin(root, pointer)
	}
	p.same[root] = same
	return same
}

// storePickedMax bounds the variables a Phi of pointers may point to that
// storesReachingPicked walks for. A Phi that may point to more is not walked.
const storePickedMax = 16

// storesReachingPicked finds the values that can be at path, selections from
// what phi points to, when read runs. phi picks one of several pointers to a
// variable, and no store goes through phi itself:
//
//	h := &holder{l: a}      // edge t1
//	if cond {
//	    h = &holder{l: b}   // edge t5
//	}
//	h.l.Info()              // reads t1.l or t5.l: both a and b reach it
//
// Each leaf, a value phi can be through the edges that are Phis themselves,
// is walked for from read, as storesReaching walks for a variable, along the
// paths on which phi is that leaf (see storeGuard). Every value that reaches
// read must have context, as for the edges of a Phi.
//
// A leaf that is not a pointer to a variable, a pointer parameter or a
// captured variable, or a field or constant element of one, such as a
// pointer read from a parameter, is walked for as storesReachingPointer
// walks for one, back to where it is made, and a path back to there, as for
// the logger of ctx, carries what tracing the leaf itself finds (see
// storeReaching.made).
//
// A go or defer statement takes the pointer when it runs, and reads what it
// points to later: the walk goes back from it, and every store after it
// counts too (see walkToRead).
//
// ok is false when no path carries a store through such a leaf or the leaf
// itself, when the walk for a leaf gives up (see storeGuard.giveUp), when phi
// may point to more than storePickedMax variables, or when read is nil.
func storesReachingPicked(
	phi *ssa.Phi,
	path []sel,
	read ssa.Instruction,
	positions Positions,
) (r storeReaching, ok bool) {
	if read == nil {
		return r, false
	}
	leaves, ok := storePickedLeaves(phi)
	if !ok {
		return r, false
	}
	pointers := newStorePointers(positions)
	for _, leaf := range leaves {
		lr, ok := storesReachingLeaf(phi, path, storeTarget{v: leaf, phi: true}, read, positions, nil, pointers)
		if !ok {
			return storeReaching{}, false
		}
		r.merge(lr)
	}
	return r, true
}

// storesReachingLeaf finds the values that can be at path, selections from
// what root points to, when read runs, along the paths on which root is
// leaf: what is at the same selections from leaf. pointers, when not nil,
// follows the pointers read from memory for the walk of leaf (see
// storeWalk.pointers), and guard follows those a guarded place is read
// through (see storeGuard).
//
// A leaf that is not in a variable is walked for as storesReachingPointer
// walks for one, in the function it is in. ok is false when no path carries a
// store through such a leaf or the leaf itself, as what is there was stored
// where the function cannot see, or when the walk gave up.
func storesReachingLeaf(
	root ssa.Value,
	path []sel,
	leaf storeTarget,
	read ssa.Instruction,
	positions Positions,
	pointers, guard *storePointers,
) (r storeReaching, ok bool) {
	at, tracked := PlaceOf(leaf.v)
	g := newStoreGuard(root, leaf.v, guard)
	w := &storeWalk{
		at:        at.with(path...),
		positions: positions,
		out:       &r,
		pointers:  pointers,
		outer:     leaf.via,
		guard:     g,
	}
	if !tracked && len(leaf.via) == 0 {
		w.pointer = storePointerOf(leaf.v)
	}
	w.walkToRead(read)
	if g.gaveUp || !tracked && r.isEmpty() && len(r.made) == 0 {
		return storeReaching{}, false
	}
	return r, true
}

// storePickedLeaves returns the values that phi may be, through the edges
// that are Phis themselves, apart from nil. ok is false when there are more
// than storePickedMax.
func storePickedLeaves(phi *ssa.Phi) (leaves []ssa.Value, ok bool) {
	seen := map[ssa.Value]bool{phi: true}
	queue := []*ssa.Phi{phi}
	for len(queue) > 0 {
		p := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, edge := range p.Edges {
			if seen[edge] || IsNilConst(edge) {
				continue
			}
			seen[edge] = true
			if inner, isPhi := edge.(*ssa.Phi); isPhi {
				queue = append(queue, inner)
				continue
			}
			if len(leaves) == storePickedMax {
				return nil, false
			}
			leaves = append(leaves, edge)
		}
	}
	return leaves, true
}

// storeGuard keeps a walk for one leaf of a pointer, a value the pointer can
// be in the end, to the paths on which the pointer is that leaf. Each path of
// the walk carries a storePath: what must still hold, going back from the
// read, for the pointer to be the leaf there.
//
//	p := zerolog.Ctx(ctx)             // *p = t0
//	if cond {
//	    l := base.With().Ctx(ctx).Logger()
//	    p = &l                        // *p = t3: the path holds the leaf t3
//	}
//	func() { p.Info().Msg("x") }()    // t1 = *p ; Info(t1): t1 must be t3
//
// The walk for t3 starts with the value t1 that must be the leaf. Past the
// load, the place p must hold it, and past the closure entry, the variable
// bound where the closure runs. At a store to that place, the value stored
// must be the leaf: t3 resolves the guard, and the path goes on as a walk for
// t3 does. t0 is another value, and the path does not hold the leaf. The
// same holds for a Phi, whose edges are taken at the top of its block, and
// for a field of a struct value, which is the same field where the value is
// loaded from.
//
// Before the guard is resolved, a path is between the read and where the
// pointer gets to be the leaf. A store to the leaf there is kept on the path,
// and counts only when the path resolves later (see storePath). A store
// through a pointer just loaded from the guarded place, as *b.p = v does for
// a place b.p, is a store through the leaf on that path. Where the leaf is
// made, met there, drops what is kept: in a loop, it made the pointer of a
// later iteration.
type storeGuard struct {
	// root is the pointer, the value that must be the leaf at the read.
	root ssa.Value
	// leaf is the value root is on the paths of the walk.
	leaf ssa.Value
	// made is where leaf is made: the instruction, or the declaration of the
	// local variable it points into. It is nil when the function does not
	// make it, as for a parameter or a global.
	made ssa.Instruction
	// pointers follows the pointers read from memory that a guarded place is
	// read through, to the variable they point to.
	pointers *storePointers
	// places are the values and places that the paths name, by their keys.
	places map[PlaceKey]Place
	// gaveUp reports that a path met what the guard cannot tell (see
	// giveUp).
	gaveUp bool
}

// newStoreGuard returns a guard for the paths on which root is leaf.
func newStoreGuard(root, leaf ssa.Value, pointers *storePointers) *storeGuard {
	g := &storeGuard{root: root, leaf: leaf, pointers: pointers, places: make(map[PlaceKey]Place)}
	at, tracked := PlaceOf(leaf)
	if alloc, isAlloc := at.root.(*ssa.Alloc); isAlloc {
		g.made = alloc
	} else if !tracked {
		g.made = storePointerOf(leaf).def
	}
	return g
}

// storeGuardKind is what a path of a guarded walk still needs.
type storeGuardKind uint8

const (
	// storeGuardResolved is a path on which the pointer is the leaf.
	storeGuardResolved storeGuardKind = iota
	// storeGuardValue is a path on which a value must be the leaf: a Phi, at
	// the top of its block, or a load, where it runs.
	storeGuardValue
	// storeGuardPlace is a path on which a place in memory must hold the leaf.
	storeGuardPlace
)

// storePath is the state of a path of a walk with a guard (see storeGuard),
// and the zero value for one without.
type storePath struct {
	kind storeGuardKind
	// at keys the value, with the selections from it, or the place, that
	// kind names.
	at PlaceKey
	// must and may are the stores to the leaf that the path met before the
	// guard is resolved: the one that writes it every time it runs, and one
	// that may write it, or not. The path stopped for the leaf at must, and
	// went on past may. mustSels is how many selections must takes: fewer
	// than the address of the walk for a store of a whole aggregate that
	// holds it (see storeWalk.copied).
	must, may *ssa.Store
	mustSels  int
}

// resolved reports whether the pointer is the leaf on the path, or the walk
// has no guard.
func (st storePath) resolved() bool {
	return st.kind == storeGuardResolved
}

// start returns the state of a path at the read: root must be the leaf. It
// is the zero value for a walk without a guard. root is a Phi, or a pointer
// read from memory, which need takes.
func (g *storeGuard) start() storePath {
	var st storePath
	if g != nil {
		g.need(&st, PlaceAt(g.root))
	}
	return st
}

// need sets st to a path on which v, a value and the selections from it,
// must be the leaf. A field or element selected from a value is the same
// selection from that value. It reports whether v may be the leaf: a Phi or
// a load may be, the leaf is, and any other value is not.
func (g *storeGuard) need(st *storePath, v Place) bool {
	for {
		if len(v.path) == 0 && v.root == g.leaf {
			st.kind = storeGuardResolved
			return true
		}
		from, s, isSel := valueSel(v.root)
		if !isSel {
			break
		}
		v = Place{root: from, path: append([]sel{s}, v.path...)}
	}
	_, isPhi := v.root.(*ssa.Phi)
	if _, isLoad := AsLoad(v.root); !isPhi && !isLoad {
		return false
	}
	st.kind, st.at = storeGuardValue, g.key(v)
	return true
}

// key returns the key of at, and keeps at by it.
func (g *storeGuard) key(at Place) PlaceKey {
	k := at.Key()
	g.places[k] = at
	return k
}

// giveUp ends the walk: it met what the guard cannot tell, such as a store
// that may write the guarded place, or not. The walk for the pointer is then
// not done.
func (g *storeGuard) giveUp() WalkStep {
	g.gaveUp = true
	return WalkAbort
}

// settle goes on from where v, a value and the selections from it, must be
// the leaf, on the path of st. When v is the leaf, the stores kept on the
// path are what is at the address, and the path stops at must, or goes on
// resolved. When v cannot be the leaf, the path stops with nothing.
func (g *storeGuard) settle(w *storeWalk, st *storePath, v Place) WalkStep {
	if !g.need(st, v) {
		return WalkStop // The pointer is another one on this path
	}
	if !st.resolved() {
		return WalkOn
	}
	return w.resolve(st)
}

// resolve takes the path of st as one on which the pointer is the leaf: the
// stores kept on it are what is at the address, and the path stops at must,
// or goes on resolved.
func (w *storeWalk) resolve(st *storePath) WalkStep {
	if st.may != nil {
		w.out.values = append(w.out.values, st.may.Val)
	}
	must, n := st.must, st.mustSels
	*st = storePath{}
	switch {
	case must == nil:
		return WalkOn
	case n == len(w.at.path):
		w.out.values = append(w.out.values, must.Val)
	default:
		w.copied(must, w.at.path[n:])
	}
	return WalkStop
}

// step visits instr on a path whose guard is not resolved. Past a load that
// must be the leaf, the place it loads from must hold it, and past a store to
// that place, the value stored. A store to the leaf is kept on the path (see
// storePath), and where the leaf is made drops what is kept. With held, the
// guard does not change, and a store is to the leaf only as storeCover takes
// it.
func (g *storeGuard) step(w *storeWalk, instr ssa.Instruction, st *storePath, held bool) WalkStep {
	if instr == g.made {
		st.must, st.may = nil, nil // Those were to the leaf of a later iteration
		return WalkOn
	}
	if st.kind == storeGuardValue && !held {
		v := g.places[st.at]
		if load, isLoad := AsLoad(v.root); isLoad && instr == ssa.Instruction(load) {
			from, _ := PlaceOf(load.X)
			return g.enter(st, from.with(v.path...))
		}
	}
	store, isStore := instr.(*ssa.Store)
	if !isStore {
		return WalkOn
	}
	if st.kind == storeGuardPlace && !held {
		at := g.places[st.at]
		switch n, cover := g.cover(w, store.Addr, at); cover {
		case coverMust:
			return g.settle(w, st, Place{root: store.Val, path: at.path[n:]})
		case coverMay:
			return g.giveUp()
		}
	}
	if st.must != nil {
		return WalkOn // The path stopped for the leaf already
	}
	n, cover := w.storeCover(store.Addr)
	if !held {
		n, cover = w.pathCover(store, *st)
	}
	switch {
	case cover == coverNone, w.pointer != nil && w.pointer.global && w.updates(store):
	case cover == coverMust:
		st.must, st.mustSels = store, n
	case n != len(w.at.path), st.may != nil:
		return g.giveUp() // A copy that may be made, or more than a path keeps
	default:
		st.may = store
	}
	return WalkOn
}

// enter sets st to a path on which at, a place in memory, must hold the leaf.
// A place in what a pointer read from memory points to is the same place in
// the variable the pointer points to, when that is the only one. ok is false
// for any other place whose stores cannot all be seen, and the walk gives up.
func (g *storeGuard) enter(st *storePath, at Place) WalkStep {
	at, ok := g.pointed(at)
	if !ok {
		return g.giveUp()
	}
	st.kind, st.at = storeGuardPlace, g.key(at)
	return WalkOn
}

// pointed returns at, as a place in a variable, through the pointers read
// from memory that it is read through, each of which points to one variable
// only. ok is false when one may point to several, or cannot be followed.
func (g *storeGuard) pointed(at Place) (Place, bool) {
	seen := make(map[ssa.Value]bool)
	for !isTrackedRoot(at.root) {
		targets, ok := g.pointers.targetsOf(at.root)
		if !ok || len(targets) != 1 || targets[0].phi || seen[at.root] {
			return at, false
		}
		seen[at.root] = true
		inner, _ := PlaceOf(targets[0].v)
		at = inner.with(at.path...)
	}
	return at, true
}

// edge takes a path whose guard is not resolved into the i-th predecessor of
// b. At the top of the block of a Phi that must be the leaf, the edge of that
// predecessor must be.
func (g *storeGuard) edge(w *storeWalk, b *ssa.BasicBlock, i int, st storePath) (storePath, bool) {
	if st.kind != storeGuardValue {
		return st, true
	}
	v := g.places[st.at]
	phi, isPhi := v.root.(*ssa.Phi)
	if !isPhi || phi.Block() != b {
		return st, true
	}
	return st, g.settle(w, &st, Place{root: phi.Edges[i], path: v.path}) == WalkOn
}

// cover tells how much of at, a guarded place in a variable, a store to addr
// writes, and how many selections addr takes from the variable. A store
// through a pointer read from memory writes the variable when the pointer
// points to it, as storeWalk.storeCover takes one.
func (g *storeGuard) cover(w *storeWalk, addr ssa.Value, at Place) (int, placeCover) {
	to, _ := PlaceOf(addr)
	limit := coverMust
	if to.root != at.root {
		to, limit = g.pointers.through(to, at.root)
	}
	return len(to.path), min(to.covers(at, w.indexOnce), limit)
}

// pathCover tells how much of the address of the walk store writes, on a path
// of st, and how many selections it takes, as storeCover does. A store
// through the pointer that st needs to be the leaf is a store through the
// leaf: through the value itself, or through a load of the place, in the same
// block, with no store to the place between.
func (w *storeWalk) pathCover(store *ssa.Store, st storePath) (int, placeCover) {
	to, _ := PlaceOf(store.Addr)
	if !w.guard.through(w, to.root, store, st) {
		return w.storeCover(store.Addr)
	}
	leaf, _ := PlaceOf(w.guard.leaf)
	to = leaf.with(to.path...)
	return len(to.path), to.covers(w.at, w.indexOnce)
}

// through reports whether pointer, the root of the address of store, is the
// pointer that st, a path whose guard is not resolved, needs to be the leaf,
// where store runs.
func (g *storeGuard) through(w *storeWalk, pointer ssa.Value, store *ssa.Store, st storePath) bool {
	at := g.places[st.at]
	if st.kind == storeGuardValue {
		return len(at.path) == 0 && at.root == pointer
	}
	load, isLoad := AsLoad(pointer)
	if !isLoad || load.Block() != store.Block() {
		return false
	}
	from, _ := PlaceOf(load.X)
	if from, ok := g.pointed(from); !ok || from.Key() != st.at {
		return false
	}
	instrs := store.Block().Instrs
	for _, instr := range instrs[w.positions.of(load)+1 : w.positions.of(store)] {
		if other, isStore := instr.(*ssa.Store); isStore {
			if _, cover := g.cover(w, other.Addr, at); cover != coverNone {
				return false // The place holds another pointer by the store
			}
		}
	}
	return true
}

// storedAfter reports whether a store that can run after esc may write the
// place that st, a path that gets out of a closure, needs to hold the leaf.
func (g *storeGuard) storedAfter(w *storeWalk, esc ssa.Instruction, st storePath) bool {
	at, after := g.places[st.at], w.positions.after(esc)
	for instr := range InstrsIn(esc.Parent()) {
		if store, isStore := instr.(*ssa.Store); isStore && after(store) {
			if _, cover := g.cover(w, store.Addr, at); cover != coverNone {
				return true
			}
		}
	}
	return false
}

// outOf returns st, a path that gets to the entry of the closure mc makes,
// as a path in the function that makes it. A value of the closure runs past
// the entry, so st needs a place there: one in a variable the closure
// captures is in the variable bound where it is made. A place in a variable
// of a function further out stays as it is.
func (g *storeGuard) outOf(st storePath, mc *ssa.MakeClosure) storePath {
	at := g.places[st.at]
	if fv, isFreeVar := at.root.(*ssa.FreeVar); isFreeVar && fv.Parent() == mc.Fn {
		at.root = mc.Bindings[slices.Index(mc.Fn.(*ssa.Function).FreeVars, fv)]
		st.at = g.key(at)
	}
	return st
}

// storePointers follows the pointers that a function reads from memory to
// the variables they point to, so that an address whose root is such a
// pointer is walked for as an address in each of those variables:
//
//	h := holder{p: &inner{l: base}}  // t1 = new inner ; *&t1.l = base ; *&t0.p = t1
//	h.p.l.Info()                     // t3 = *&t0.p ; Info(&t3.l), and t3 is t1
//
// A pointer is what the stores that reach its read left where it is read
// from: a field or element of a variable, or of a struct or array value
// loaded from one, or of what another pointer read from memory points to.
// Each one it can be must be a variable, a pointer parameter, a captured
// variable, a field or element of one, or such a pointer itself, or a Phi
// that picks one of them (see storesReachingPicked). A nil pointer has no
// variable to read, and is left out.
//
// The walk for each variable takes a store through another pointer that may
// point to it as a store to it: on every path when that pointer can be the
// variable only, or maybe otherwise (see storeWalk.storeCover). A write
// through a reference that is not read from memory, such as a pointer
// passed to a function, is not seen there either.
//
//	h.p.l = base.With().Ctx(ctx).Logger() // t5 = *&t0.p ; *&t5.l = ..., and t5 is t1 too
//	h.p.l.Info()                          // reads only that store
type storePointers struct {
	positions Positions
	// pointed holds what each pointer already followed can point to, by the
	// value read. One still being followed is there and not ok, so that
	// reaching it again, through a store that leads back to it, fails.
	pointed map[ssa.Value]storePointed
}

// storePointed is what a pointer read from memory can point to. ok is false
// when that cannot be told.
type storePointed struct {
	targets []storeTarget
	ok      bool
}

// storeTarget is something a pointer can point to: v. phi reports that v is
// one of the values a Phi that the pointer can be may be.
//
// via, when not empty, are the closures the pointer is read in, innermost
// first, out to the function v is in: the pointer is read from a variable a
// closure captures, and v is what that variable holds when the closure runs.
type storeTarget struct {
	v   ssa.Value
	phi bool
	via []*ssa.MakeClosure
}

// newStorePointers returns a storePointers that has followed no pointer yet.
func newStorePointers(positions Positions) *storePointers {
	return &storePointers{
		positions: positions,
		pointed:   make(map[ssa.Value]storePointed),
	}
}

// reachingThrough finds the values that can be at at, whose root is a
// pointer read from memory, when read runs: what can be at the same
// selections from each leaf the pointer can be, along the paths on which it
// is that leaf (see storesReachingLeaf). ok is false when the pointer cannot
// be followed, points to nothing but nil, or the walk for a leaf is not done.
//
// A pointer that the pointer can be may itself be read from memory, as one
// copied from another field is. It points to what that one does, as it is
// read where the copy is made, and the guard of each leaf goes through both
// (see storeGuard):
//
//	a := holder{&inner{l: base.With().Ctx(ctx).Logger()}}
//	h := holder{a.p}                  // t5 = *&a.p ; *&h.p = t5
//	func() { h.p.l.Info() }()         // t5, then a.p, as the copy is made
//
// When that one cannot be followed, as for one read from a pointer
// parameter, it is a leaf that is not in a variable, and the stores through
// the same pointer are walked for, as storesReachingPointer does.
func (p *storePointers) reachingThrough(at Place, read ssa.Instruction) (r storeReaching, ok bool) {
	leaves, ok := p.leavesOf(at.root, nil, map[ssa.Value]bool{at.root: true})
	if !ok || len(leaves) == 0 {
		return r, false
	}
	for _, leaf := range leaves {
		lr, ok := storesReachingLeaf(at.root, at.path, leaf, read, p.positions, p, p)
		if !ok {
			return storeReaching{}, false
		}
		r.merge(lr)
	}
	return r, true
}

// leavesOf returns the leaves of v, a pointer read from memory: what it can
// point to, through the pointers read from memory among them, which are not
// in seen, the pointers on the way to v. via are the closures that lead out
// from the read to the function of v. A pointer copied back to where it was
// read from, in a loop, adds nothing. ok is false when v cannot be followed.
func (p *storePointers) leavesOf(v ssa.Value, via []*ssa.MakeClosure, seen map[ssa.Value]bool) ([]storeTarget, bool) {
	targets, ok := p.targetsOf(v)
	if !ok {
		return nil, false
	}
	var leaves []storeTarget
	for _, t := range targets {
		t.via = append(slices.Clip(via), t.via...)
		if _, isRead := storePointerRead(t.v); isRead && !isTrackedRoot(t.v) {
			if seen[t.v] {
				continue
			}
			seen[t.v] = true
			inner, ok := p.leavesOf(t.v, t.via, seen)
			delete(seen, t.v)
			if ok && len(inner) > 0 {
				leaves = append(leaves, inner...)
				continue
			}
		}
		leaves = append(leaves, t)
	}
	return leaves, true
}

// isEmpty reports whether r found nothing at all, apart from made.
func (r storeReaching) isEmpty() bool {
	return len(r.values) == 0 && !r.unknown && len(r.captured) == 0 && len(r.params) == 0
}

// targetsOf returns what v, a pointer read from memory, can point to. ok is
// false when v is not one, or what it points to cannot be told.
func (p *storePointers) targetsOf(v ssa.Value) ([]storeTarget, bool) {
	if got, done := p.pointed[v]; done {
		return got.targets, got.ok
	}
	reads, isRead := storePointerRead(v)
	if !isRead {
		return nil, false
	}
	p.pointed[v] = storePointed{} // Reaching v again while it is followed fails
	var got storePointed
	if stored, ok := p.reachingReads(reads); ok {
		got.targets, got.ok = storeTargetsReaching(stored, parentFunc(v), nil, p.positions)
	}
	p.pointed[v] = got
	return got.targets, got.ok
}

// storeTargetsReaching returns what a pointer can point to when it can be
// any of what stored found where it is read from, in from. The via of each
// target are the closures from is in, innermost first, out to the function of
// the target: a walk with closures to go out of finds stores in each of them
// (see storeWalk.walkOut). via is what is taken for a value that is in no
// function, or one that cannot be told: the closures from is in out to the
// function stored was found in.
//
// A path that carries the value of a variable a closure captures, as the
// closure starts, carries what the variable bound where the closure is made
// holds when the closure runs (see storesReachingClosure). The walk for each
// of those targets goes from the read back to the entry of the closure, then
// on from where the closure runs (see storeWalk.walkOut):
//
//	p := &lh{}                               // t1 = new lh ; *t0 = t1
//	p.log = base.With().Ctx(ctx).Logger()    // t2 = *t0 ; *&t2.log = ...
//	func() { p.log.Info().Msg("x") }()       // t0 = *p ; Info(&t0.log), and t0 is t1
//
// ok is false when stored has a value that is not known, or a parameter of
// a func literal, or when a captured variable has no MakeClosure that binds
// it.
func storeTargetsReaching(
	stored storeReaching,
	from *ssa.Function,
	via []*ssa.MakeClosure,
	positions Positions,
) ([]storeTarget, bool) {
	if stored.unknown || len(stored.params) > 0 || len(stored.made) > 0 {
		return nil, false
	}
	targets, ok := storeTargetsOf(stored.values)
	if !ok {
		return nil, false
	}
	for i := range targets {
		targets[i].via = storeViaTo(from, targets[i].v, via)
	}
	for _, c := range stored.captured {
		fv := c.root.(*ssa.FreeVar)
		makers := storeMakersOf(fv.Parent())
		if len(makers) == 0 {
			return nil, false
		}
		idx := slices.Index(fv.Parent().FreeVars, fv)
		for _, mc := range makers {
			bound := storesReachingClosure(mc.Bindings[idx], c.path, mc, positions)
			more, ok := storeTargetsReaching(bound, from, append(slices.Clip(via), mc), positions)
			if !ok {
				return nil, false
			}
			targets = append(targets, more...)
		}
	}
	return targets, true
}

// storeViaTo returns the closures from is in, innermost first, out to the
// function v is in. It returns def when v is in no function, or when a
// closure on the way is not made once where it is declared.
func storeViaTo(from *ssa.Function, v ssa.Value, def []*ssa.MakeClosure) []*ssa.MakeClosure {
	to := parentFunc(v)
	if fv, isFreeVar := v.(*ssa.FreeVar); isFreeVar {
		to = fv.Parent()
	}
	if to == nil {
		return def
	}
	var via []*ssa.MakeClosure
	for fn := from; fn != to; fn = fn.Parent() {
		makers := storeMakersOf(fn)
		if len(makers) != 1 {
			return def
		}
		via = append(via, makers[0])
	}
	return via
}

// storeMakersOf returns the MakeClosure instructions that make fn, a func
// literal, in the function it is declared in, as Closures.makersOf does.
func storeMakersOf(fn *ssa.Function) []*ssa.MakeClosure {
	if fn.Parent() == nil {
		return nil
	}
	var makers []*ssa.MakeClosure
	for instr := range InstrsIn(fn.Parent()) {
		if mc, ok := instr.(*ssa.MakeClosure); ok && mc.Fn == fn {
			makers = append(makers, mc)
		}
	}
	return makers
}

// reachingReads finds the values that can be at each of reads, as reaching
// does for one, together.
func (p *storePointers) reachingReads(reads []LoadedRead) (storeReaching, bool) {
	var r storeReaching
	for _, rd := range reads {
		stored, ok := p.reaching(rd.At, rd.Load)
		if !ok {
			return storeReaching{}, false
		}
		r.merge(stored)
	}
	return r, true
}

// reaching finds the values that can be at at when read runs, for at in a
// variable, or in what a pointer read from memory points to.
func (p *storePointers) reaching(at Place, read ssa.Instruction) (storeReaching, bool) {
	if !isTrackedRoot(at.root) {
		return p.reachingThrough(at, read)
	}
	var r storeReaching
	w := &storeWalk{at: at, positions: p.positions, out: &r, pointers: p}
	w.walkToRead(read)
	return r, true
}

// through returns at, an address whose root is a pointer read from memory,
// as an address in root, when the pointer can point to root, and how much
// of it a store to at writes: all of it when root is all the pointer can
// point to, or maybe all of it otherwise. It is coverNone when the pointer
// cannot point to root, or cannot be followed.
//
// What the pointer points to may be another pointer read from memory, which
// points to root in turn, as a pointer copied from a captured parameter in a
// func literal does:
//
//	b := loggerPtrBox{l}      // t3 = *l ; *&t0.p = t3, and l is the parameter
//	*b.p = v                  // t6 = *&t0.p ; *t6 = v, and t6 is t3
func (p *storePointers) through(at Place, root ssa.Value) (Place, placeCover) {
	return p.throughSeen(at, root, make(map[ssa.Value]bool))
}

// throughSeen returns what through does, for a pointer that is not in seen,
// the pointers already gone through, as one copied back and forth in a loop
// leads back to.
func (p *storePointers) throughSeen(at Place, root ssa.Value, seen map[ssa.Value]bool) (Place, placeCover) {
	if _, isRead := storePointerRead(at.root); !isRead || seen[at.root] {
		return at, coverNone
	}
	seen[at.root] = true
	targets, ok := p.targetsOf(at.root)
	if !ok {
		return at, coverNone
	}
	for _, t := range targets {
		cover := coverMay
		if len(targets) == 1 && !t.phi {
			cover = coverMust
		}
		tat, _ := PlaceOf(t.v)
		tat = tat.with(at.path...)
		if tat.root == root {
			return tat, cover
		}
		if inner, c := p.throughSeen(tat, root, seen); c != coverNone {
			return inner, min(c, cover)
		}
	}
	return at, coverNone
}

// storePointerRead returns where the pointer v is read from, when it is read
// from memory: the address a load reads, with the fields and elements of a
// struct or array value selected from what it loads, and the load. A pointer
// selected from a Phi of such values is read from each load an edge leads to
// (see LoadedSelection).
//
//	t3 = *&t0.p             // read from t0.p
//	t5 = *t0 ; t6 = t5.p    // read from t0.p too, as t5 is loaded
//
//declscope:shared // resolve.go resolves a read through such a pointer
func storePointerRead(v ssa.Value) (reads []LoadedRead, ok bool) {
	if load, isLoad := AsLoad(v); isLoad {
		at, _ := PlaceOf(load.X)
		return []LoadedRead{{At: at, Load: load}}, true
	}
	return LoadedSelection(v)
}

// storeTargetsOf returns what a pointer can point to when it can be any of
// values: each value apart from nil, and each value a Phi among them can be.
// ok is false when a Phi can be more than storePickedMax of them.
func storeTargetsOf(values []ssa.Value) ([]storeTarget, bool) {
	var targets []storeTarget
	for _, v := range values {
		if IsNilConst(v) {
			continue // A nil pointer has no variable to read
		}
		phi, isPhi := v.(*ssa.Phi)
		if !isPhi {
			targets = append(targets, storeTarget{v: v})
			continue
		}
		leaves, ok := storePickedLeaves(phi)
		if !ok {
			return nil, false
		}
		for _, leaf := range leaves {
			targets = append(targets, storeTarget{v: leaf, phi: true})
		}
	}
	return targets, true
}

// merge adds what o found to r.
func (r *storeReaching) merge(o storeReaching) {
	r.values = append(r.values, o.values...)
	r.params = append(r.params, o.params...)
	r.unknown = r.unknown || o.unknown
	for _, v := range o.made {
		r.addMade(v)
	}
	for _, c := range o.captured {
		r.addCaptured(c)
	}
}

// addMade adds v, a pointer that is not in a variable, to the pointers that
// r found are made on a path, unless it is there already.
func (r *storeReaching) addMade(v ssa.Value) {
	if !slices.Contains(r.made, v) {
		r.made = append(r.made, v)
	}
}

// addCaptured adds c, a place in a variable a closure captures, to what r
// found, unless it is there already.
func (r *storeReaching) addCaptured(c Place) {
	key := c.Key()
	if !slices.ContainsFunc(r.captured, func(o Place) bool { return o.Key() == key }) {
		r.captured = append(r.captured, c)
	}
}

// storesReachingClosure finds the values that can be at path, selections from
// v, when the closure mc makes reads it, as storesReaching does for an escape.
// v is a variable that mc captures: a local variable, or a variable that the
// function making mc captures in turn.
//
//declscope:shared // resolve.go finds what a closure reads of what it captures
func storesReachingClosure(
	v ssa.Value,
	path []sel,
	mc *ssa.MakeClosure,
	positions Positions,
) storeReaching {
	var r storeReaching
	w := &storeWalk{at: Place{root: v, path: path}, positions: positions, out: &r}
	w.walkEscape(mc, storePath{})
	return r
}

// walkEscape walks to esc, an instruction that passes the address on, such as
// a closure that captures it. What it passes the address on to may read it at
// any point after, so what reaches esc, and every store after it, may be what
// it reads. A closure that is only deferred reads it at the returns after it,
// unless the variable is declared in a loop, and one that is only called reads
// it at each call (see closureRuns). A Phi passes the address on only from
// the edges that are the address (see walkToPhiEdges).
//
// The walk starts in st, and a path whose guard is not resolved keeps to it
// back from esc. Every store to the address after esc counts all the same. A
// store to the guarded place after esc may make the pointer the leaf before
// the closure runs, so the path is then taken as resolved.
func (w *storeWalk) walkEscape(esc ssa.Instruction, st storePath) {
	switch e := esc.(type) {
	case *ssa.MakeClosure:
		if w.walkToRuns(e, st) {
			return
		}
	case *ssa.Phi:
		w.walkToPhiEdges(e)
		w.addStoresAfter(esc)
		return
	}
	if !st.resolved() && w.guard.storedAfter(w, esc, st) && w.resolve(&st) == WalkStop {
		return
	}
	w.walkBefore(esc, st)
	w.addStoresAfter(esc)
}

// walkToPhiEdges walks to the ends of the predecessors of phi whose edge is
// the address, or the variable that holds it. On another edge, phi points to
// something else, and what the variable holds there is not what phi passes
// on. That edge may not even run the declaration of the variable:
//
//	p := zerolog.Ctx(ctx)
//	if cond {
//	    l := base.With().Ctx(ctx).Logger() // phi takes &l from this edge only
//	    p = &l
//	}
//	p.Info().Msg("msg")                    // p is phi
//
// phi is an escape since an edge is the root or the address, as storeReadsOf
// matches it, so some edge holds it.
func (w *storeWalk) walkToPhiEdges(phi *ssa.Phi) {
	var from []WalkPoint
	for i, edge := range phi.Edges {
		if w.phiEdgeHolds(edge) {
			pred := phi.Block().Preds[i]
			from = append(from, WalkPoint{block: pred, index: len(pred.Instrs)})
		}
	}
	w.walkBackFrom(from)
}

// phiEdgeHolds reports whether edge, an edge of a Phi, is the root of the
// walk or a selection from it that the path of the walk starts with, or the
// address of the walk, where an element at a variable index is the same
// element at the same index.
func (w *storeWalk) phiEdgeHolds(edge ssa.Value) bool {
	if _, ok := w.prefixLen(edge); ok {
		return true
	}
	at, _ := PlaceOf(edge)
	return len(at.path) == len(w.at.path) && at.covers(w.at, storeIndexAlways) == coverMust
}

// walkToRuns walks to where mc runs, in st, when it is a closure that is
// only deferred, outside a loop the variable is declared in, or only called.
// It reports whether it did.
func (w *storeWalk) walkToRuns(mc *ssa.MakeClosure, st storePath) bool {
	runs, ok := closureRunsOf(mc)
	if !ok {
		return false
	}
	if defers, deferred := runs.defersOnly(); deferred && !w.declaredInLoop() {
		for _, d := range defers {
			w.walkDeferred(d, st, false)
		}
		return true
	}
	if calls, called := runs.callsOnly(); called {
		for _, call := range calls {
			w.walkBefore(call, st)
		}
		return true
	}
	return false
}

// declaredInLoop reports whether the root of the walk is a local variable
// declared in a loop. Each iteration has a variable of its own, and a store
// on a later one, which a walk back from a return meets first, is to another
// variable than the one a closure deferred earlier captures.
func (w *storeWalk) declaredInLoop() bool {
	alloc, ok := w.at.root.(*ssa.Alloc)
	return ok && w.positions.reaches(alloc, alloc)
}

// pointerMadeAfter reports whether the root of the walk is a pointer that is
// not in a variable, which a walk without a guard looks for, made again
// after d, as in a loop: a store through it there may be through another
// pointer than the one d takes. With a guard, a store through the leaf made
// again is dropped where the leaf is made (see storeGuard.step).
func (w *storeWalk) pointerMadeAfter(d *ssa.Defer) bool {
	if w.pointer == nil || w.guard != nil || w.pointer.def == nil {
		return false
	}
	return w.positions.reaches(d, w.pointer.def)
}

// walkDeferred walks to the returns after d, where what d defers runs, along
// the paths that pass d only: on a path that skips d, nothing is deferred.
// Each path goes back to d, and from there on as walkBefore goes.
//
//	l := base
//	if cond {
//	    defer func() { l.Info().Msg("x") }()
//	    l = l.With().Ctx(ctx).Logger() // the only store on a path through d
//	}
//	return                              // the path that skips the if defers nothing
//
// The walk starts in st at each return. With held, the guard holds as it is
// up to d, where the pointer is taken, as for a deferred call of a method on
// it: only the stores to the leaf are kept past d. Otherwise the guard goes
// on from the returns, where a deferred closure reads what it captures.
func (w *storeWalk) walkDeferred(d *ssa.Defer, st storePath, held bool) {
	var rets []WalkFrom[storePath]
	for _, ret := range storeReturnsAfter(d, w.positions) {
		rets = append(rets, WalkFrom[storePath]{WalkPointBefore(ret, w.positions), st})
	}
	var atD []storePath
	stopped := func(st storePath) {
		if !slices.Contains(atD, st) {
			atD = append(atD, st) // Before it, the path goes on from d
		}
	}
	w.walkStates(rets, storeStates{
		visit:   w.stepAt,
		through: walkThrough(d, w.positions),
		held:    held,
		stop:    d,
		stopped: stopped,
	})
	for _, st := range atD {
		w.walkBefore(d, st)
	}
}

// storeReturnsAfter returns the returns that can run after d, where what it
// defers runs. The recover block, reached after a panic, is not among them.
func storeReturnsAfter(d *ssa.Defer, positions Positions) []ssa.Instruction {
	var rets []ssa.Instruction
	fn := d.Parent()
	for _, b := range fn.Blocks {
		ret, isReturn := b.Instrs[len(b.Instrs)-1].(*ssa.Return)
		if !isReturn || b == fn.Recover {
			continue
		}
		if positions.reaches(d, ret) {
			rets = append(rets, ret)
		}
	}
	return rets
}

// walkToRead walks every path to read, from where the guard starts (see
// storeGuard.start). A deferred call reads at the returns after it, and a go
// call at any point after it, as an escape does. A deferred call that reads
// a variable declared in a loop is read as an escape too, since a return may
// meet a later iteration's variable first. So is one for a pointer that is
// not in a variable and is made again after the defer statement, in a loop,
// since a store through it there may be through the pointer of a later
// iteration.
//
// A store through the pointer itself after a defer statement writes what the
// deferred call reads, as the call takes the pointer when the statement runs:
//
//	p := &a
//	if other {
//	    p = &b
//	}
//	defer p.Print("x")                 // reads *p at the return
//	*p = base.With().Ctx(ctx).Logger() // the only store that reaches it
func (w *storeWalk) walkToRead(read ssa.Instruction) {
	st := w.guard.start()
	if d, ok := read.(*ssa.Defer); ok && !w.declaredInLoop() && !w.pointerMadeAfter(d) {
		w.walkDeferred(d, st, true)
		return
	}
	switch read.(type) {
	case *ssa.Defer, *ssa.Go:
		w.walkBefore(read, st)
		w.addStoresAfter(read)
	default:
		w.walkBefore(read, st)
	}
}

// storeReaching is what storesReaching found can be at an address when it is
// read.
//
//declscope:shared // resolve.go resolves each part
type storeReaching struct {
	// values are the values stored. Every one must have context.
	values []ssa.Value
	// unknown reports that some path carries a value the function cannot
	// tell: one the caller passed through a pointer parameter, or one an
	// aggregate was copied whole from where no store can be seen.
	unknown bool
	// captured are the places in variables a closure captures, each a free
	// variable and the selections from it, whose values some path carries as
	// the closure starts. Each is what is at the same selections of the
	// variable bound where the closure is made, when the closure runs.
	captured []Place
	// params are the parameters of a func literal, with the selections from
	// them, whose values some path carries from the entry of the func literal.
	// Tracing them finds what the calls of the func literal pass.
	params []storeParamPath
	// made are the pointers that are not in a variable, such as one a call
	// returns, to where some path gets without a store through them, other
	// than by a load (see storeWalk.walkPointerBefore). What each points to
	// there is what tracing the pointer itself finds.
	made []ssa.Value
	// followed reports that the address is in what a pointer read from
	// memory points to, and the stores were found in each variable it can
	// point to (see storePointers).
	followed bool
}

// storeParamPath is the value at path, selections from a parameter of a func
// literal: from what the parameter points to when pointer is true, or from
// the parameter itself, a struct or array, otherwise.
//
//declscope:shared // resolve.go resolves it in each call of the func literal
type storeParamPath struct {
	param   *ssa.Parameter
	path    []sel
	pointer bool
}

// storeIsCallerParam reports whether v is a parameter of a function that is
// not a func literal, whose value only the caller knows.
func storeIsCallerParam(v ssa.Value) bool {
	p, ok := v.(*ssa.Parameter)
	return ok && p.Parent().Parent() == nil
}

// storeHoldsCallerParam reports whether alloc, a local variable, holds one
// value wherever it is loaded (see storeVariableOnce), a parameter that only
// the caller knows, as the variable SSA makes for a parameter that a closure
// captures does.
func (w *storeWalk) storeHoldsCallerParam(alloc *ssa.Alloc) bool {
	for _, ref := range *alloc.Referrers() {
		if store, isStore := ref.(*ssa.Store); isStore && !storeIsCallerParam(store.Val) {
			return false
		}
	}
	return w.storeVariableOnce(alloc)
}

// storeIsFuncLiteralParam reports whether v is a parameter of a func literal.
func storeIsFuncLiteralParam(v ssa.Value) (*ssa.Parameter, bool) {
	p, ok := v.(*ssa.Parameter)
	return p, ok && p.Parent().Parent() != nil
}

// storesReachingAt finds the values that can be at the address at when read
// runs, as storesReaching does for a read it is given. ok is false when the
// root of at is not one isTrackedRoot accepts, nor a pointer read from such
// an address that storePointers can follow.
//
//declscope:shared // resolve.go walks from a call of a func literal, and from an embedded field
func storesReachingAt(
	at Place,
	read ssa.Instruction,
	positions Positions,
) (r storeReaching, ok bool) {
	if !isTrackedRoot(at.root) {
		r, ok = newStorePointers(positions).reachingThrough(at, read)
		r.followed = true
		return r, ok
	}
	w := &storeWalk{at: at, positions: positions, out: &r}
	w.walkToRead(read)
	return r, true
}

// storeWalk walks the control flow backwards from a read, and stops each path
// at the first store to the address it looks for.
type storeWalk struct {
	at        Place     // The address, in a variable or pointer parameter
	positions Positions // Where each instruction is in its block
	out       *storeReaching
	// copies holds the loads that copied walks have started from, with the
	// path they look for, shared by every walk that copied starts.
	copies map[storeCopy]bool
	// guard, when not nil, keeps the walk to the paths on which the pointer
	// the address is read through is the leaf root is in (see storeGuard).
	guard *storeGuard
	// pointers, when not nil, follows the pointers read from memory, so that
	// a store through one that points to root is a store to root.
	pointers *storePointers
	// pointer, when not nil, is where root is made, when it is a pointer
	// that is not in a variable (see storesReachingPointer).
	pointer *storePointer
	// outer, when not empty, are the closures the walk starts in, innermost
	// first, out to the function root is in. A path that leads back to the
	// entry of the first goes on from where it runs (see walkOut).
	outer []*ssa.MakeClosure
}

// storeCopy is a load that a copied walk starts from, and the selections it
// looks for from the aggregate loaded.
type storeCopy struct {
	load *ssa.UnOp
	at   PlaceKey
}

// walkBefore walks every path that leads to instr, from the instruction
// before it, in st.
func (w *storeWalk) walkBefore(instr ssa.Instruction, st storePath) {
	from := []WalkFrom[storePath]{{WalkPointBefore(instr, w.positions), st}}
	if w.pointer != nil {
		w.walkPointerBefore(from)
		return
	}
	w.walkStates(from, storeStates{visit: w.stepAt, atEntry: w.reachEntry})
}

// walkBackFrom walks every path that leads to a point of from, and stops each
// at the first store to the address.
func (w *storeWalk) walkBackFrom(from []WalkPoint) {
	starts := make([]WalkFrom[storePath], len(from))
	for i, p := range from {
		starts[i].Point = p
	}
	w.walkStates(starts, storeStates{visit: w.stepAt, atEntry: w.reachEntry})
}

// storeStates is how walkStates walks the paths.
type storeStates struct {
	// visit visits an instruction on a path that is resolved, as walkBack
	// does: on every path of a walk without a guard.
	visit func(ssa.Instruction) WalkStep
	// atEntry, when not nil, is called with the state of a path that gets to
	// the entry of the function.
	atEntry func(storePath)
	// through, when not nil, keeps the walk to the predecessors it accepts.
	through func(*ssa.BasicBlock) bool
	// held keeps the guard of each path as it is (see walkDeferred).
	held bool
	// stop, when not nil, stops each path that gets to it, and stopped is
	// called with its state there.
	stop    ssa.Instruction
	stopped func(storePath)
}

// walkStates walks every path that leads to a point of from, in its state,
// as how says. A path whose guard is not resolved goes through step, and, at
// the top of a block, on only into the predecessors whose edge may be the
// leaf, unless held. The walk ends when the guard gives up.
func (w *storeWalk) walkStates(from []WalkFrom[storePath], how storeStates) {
	if w.guard == nil {
		w.walkResolved(from, how)
		return
	}
	visit := func(instr ssa.Instruction, st *storePath) WalkStep {
		switch {
		case instr == how.stop:
			how.stopped(*st)
			return WalkStop
		case st.resolved():
			return how.visit(instr)
		}
		return w.guard.step(w, instr, st, how.held)
	}
	edge := func(b *ssa.BasicBlock, i int, st storePath) (storePath, bool) {
		switch {
		case how.through != nil && !how.through(b.Preds[i]):
			return st, false
		case how.held || st.resolved():
			return st, true
		}
		return w.guard.edge(w, b, i, st)
	}
	entry := func(st storePath) bool {
		if how.atEntry != nil {
			how.atEntry(st)
		}
		return !w.guard.gaveUp
	}
	walkBackStates(from, visit, entry, edge)
}

// walkResolved walks every path that leads to a point of from, as
// walkStates does, for a walk without a guard: every path is resolved.
func (w *storeWalk) walkResolved(from []WalkFrom[storePath], how storeStates) {
	points := make([]WalkPoint, len(from))
	for i, f := range from {
		points[i] = f.Point
	}
	visit := how.visit
	if how.stop != nil {
		visit = func(instr ssa.Instruction) WalkStep {
			if instr == how.stop {
				how.stopped(storePath{})
				return WalkStop
			}
			return how.visit(instr)
		}
	}
	var atEntry func() bool
	if how.atEntry != nil {
		atEntry = func() bool {
			how.atEntry(storePath{})
			return true
		}
	}
	walkBack(points, visit, atEntry, how.through)
}

// reachEntry records a path that leads back to the entry of the function, or
// to its recover block. What is at a pointer parameter there is the caller's,
// which is known only for a func literal, from its calls. What is at a
// captured variable is the one the closure was made with. A local variable is
// declared before any read, and a walk from a return where a deferred call
// runs only takes the paths through its defer, so its walk never gets here.
// A walk for a variable that a pointer read from memory points to may get
// here, on a path where the pointer is another one, such as nil, and the
// variable is not made: that path carries nothing. So does a path whose
// guard is not resolved, in the function the leaf is in: the pointer is
// another one there.
func (w *storeWalk) reachEntry(st storePath) {
	_, isAlloc := w.at.root.(*ssa.Alloc)
	fv, isFreeVar := w.at.root.(*ssa.FreeVar)
	p, isParam := storeIsFuncLiteralParam(w.at.root)
	switch {
	case len(w.outer) > 0:
		w.walkOut(st)
	case !st.resolved() || isAlloc && w.pointers != nil:
	case isFreeVar:
		w.out.addCaptured(Place{root: fv, path: slices.Clone(w.at.path)})
	case isParam:
		w.out.params = append(w.out.params, storeParamPath{param: p, path: slices.Clone(w.at.path), pointer: true})
	default:
		w.out.unknown = true
	}
}

// walkOut goes on from the entry of outer[0], the closure the walk is in,
// in the function that makes it, as a walk for what the closure reads of a
// variable it captures does (see walkEscape): a store before the closure
// runs, or after it is made when it may run later, may be what is at the
// address. In the function root is in, a root that is not in a variable is
// walked for as storesReachingPointer walks for one. A path whose guard is
// not resolved goes on with the place the guard needs there (see
// storeGuard.outOf).
func (w *storeWalk) walkOut(st storePath) {
	mc := w.outer[0]
	if !st.resolved() {
		st = w.guard.outOf(st, mc)
	}
	out := &storeWalk{
		at:        w.at,
		positions: w.positions,
		out:       w.out,
		pointers:  w.pointers,
		outer:     w.outer[1:],
		guard:     w.guard,
	}
	if len(out.outer) == 0 && !isTrackedRoot(w.at.root) {
		out.pointer = storePointerOf(w.at.root)
	}
	out.walkEscape(mc, st)
}

// stepAt stops a path at a store to the address, or to an aggregate that
// holds it, and at the declaration of the local variable. It records what
// the store carries.
//
// A store that may write the address, or not, such as one to an element at
// another index than a variable index, records what it carries too, but does
// not stop the path: the value before it may still be the one read.
//
//	arr[i] = a             // reaches the read
//	arr[0] = b             // reaches it too, when i is 0
//	arr[i].Info()
func (w *storeWalk) stepAt(instr ssa.Instruction) WalkStep {
	switch in := instr.(type) {
	case *ssa.Alloc:
		if in == w.at.root {
			return WalkStop // Its zero value has nothing to trace
		}
	case *ssa.Store:
		n, cover := w.storeCover(in.Addr)
		if cover == coverNone || w.pointer != nil && w.pointer.global && w.updates(in) {
			return WalkOn
		}
		if n == len(w.at.path) {
			w.out.values = append(w.out.values, in.Val)
		} else {
			w.copied(in, w.at.path[n:])
		}
		if cover == coverMay {
			return WalkOn
		}
		return WalkStop
	}
	return WalkOn
}

// updates reports whether store is an update of the address of the walk: the
// value it stores derives from a load of the address, as
// l = l.With().Logger() does. A load from a local variable, such as the
// temporary of a composite literal, derives from what the stores that reach
// it left there:
//
//	G.log = (&BL{G.log}).v.With().Ctx(ctx).Logger() // BL.v is a copy of G.log
func (w *storeWalk) updates(store *ssa.Store) bool {
	seen := make(map[ssa.Value]bool)
	var isLoadOfAddr func(v ssa.Value) bool
	isLoadOfAddr = func(v ssa.Value) bool {
		load, isLoad := AsLoad(v)
		if !isLoad {
			return false
		}
		n, cover := w.storeCover(load.X)
		if n == len(w.at.path) && cover == coverMust {
			return true
		}
		at, tracked := PlaceOf(load.X)
		if !tracked {
			return false
		}
		stored, _ := storesReachingAt(at, load, w.positions)
		return slices.ContainsFunc(stored.values, func(val ssa.Value) bool {
			return derivesFrom(val, isLoadOfAddr, unOpOperand, seen)
		})
	}
	return derivesFrom(store.Val, isLoadOfAddr, unOpOperand, seen)
}

// storeCover tells how much of the address of the walk a store to addr
// writes, as Place.covers does, and how many selections addr takes.
//
// With pointers, a store through a pointer read from memory writes root when
// the pointer is root: on every path when that is the only pointer that can
// be there, or maybe otherwise.
func (w *storeWalk) storeCover(addr ssa.Value) (int, placeCover) {
	at, _ := PlaceOf(addr)
	limit := coverMust
	switch {
	case at.root == w.at.root:
	case w.pointer != nil && w.pointer.samePointer(at.root, w.at.root):
		at.root = w.at.root
	case w.pointers != nil:
		at, limit = w.pointers.through(at, w.at.root)
	}
	return len(at.path), min(at.covers(w.at, w.indexOnce), limit)
}

// storeIndexAlways takes an index to have one value, so that an element at
// the same index is the same element, for a test of places that runs nowhere
// in particular.
func storeIndexAlways(a, b ssa.Value) bool { return a == b }

// indexOnce reports whether a and b, the indexes of two elements of the
// address of the walk, have one value wherever the walk meets them. A store
// at the same index is then to the same element. Either a and b are the same
// value, not computed by an instruction that runs again, in a loop:
//
//	for i := range arr {   // i is a Phi, which runs again on each iteration
//	    arr[i].Info()      // the store below is to arr[i-1] here
//	    arr[i] = base
//	}
//
// Or each is a load of the same variable that holds one value (see
// storeVariableOnce), as one a closure captures does:
//
//	s[i] = l                  // t3 = *t1 ; *&t2[t3] = l
//	func() { s[i].Info() }()  // t1 = *i  ; Info(&t0[t1]) reads the same element
func (w *storeWalk) indexOnce(a, b ssa.Value) bool {
	if a != b {
		va, okA := storeLoadedVariable(a)
		vb, okB := storeLoadedVariable(b)
		return okA && okB && va == vb && w.storeVariableOnce(va)
	}
	def, ok := a.(ssa.Instruction)
	return !ok || !w.positions.reaches(def, def)
}

// storeLoadedVariable returns the local variable that v, a load, reads: its
// address, or the variable bound to the free variable it loads, through the
// closures that capture it at any depth. SSA makes a func literal once, where
// it is written, so a closure has one MakeClosure. ok is false when v is not
// such a load, or a closure on the way is made nowhere that can be seen.
func storeLoadedVariable(v ssa.Value) (_ *ssa.Alloc, ok bool) {
	load, isLoad := AsLoad(v)
	if !isLoad {
		return nil, false
	}
	addr := load.X
	for fv, isFreeVar := addr.(*ssa.FreeVar); isFreeVar; fv, isFreeVar = addr.(*ssa.FreeVar) {
		fn := fv.Parent()
		addr = nil
		for _, mc := range storeMakersOf(fn) {
			addr = mc.Bindings[slices.Index(fn.FreeVars, fv)]
		}
	}
	alloc, isAlloc := addr.(*ssa.Alloc)
	return alloc, isAlloc
}

// storeVariableOnce reports whether alloc, a local variable, holds one value
// wherever it is loaded: it is stored to once, outside a loop, before each
// load of it and each closure that captures it, and is otherwise only
// loaded, whole or by a field, in its function and in those closures. A
// parameter that a closure captures is such a variable.
func (w *storeWalk) storeVariableOnce(alloc *ssa.Alloc) bool {
	var store *ssa.Store
	var uses []ssa.Instruction // Loads and captures in the function of alloc
	var only func(addr ssa.Value, outer bool) bool
	only = func(addr ssa.Value, outer bool) bool {
		for _, ref := range *addr.Referrers() {
			switch in := ref.(type) {
			case *ssa.Store:
				if !outer || in.Addr != addr || store != nil {
					return false
				}
				store = in
			case *ssa.UnOp:
				// A load: the only unary operation on an address
			case *ssa.FieldAddr:
				// A field of a struct variable, such as a struct parameter,
				// that is only loaded
				if !only(in, false) {
					return false
				}
			case *ssa.MakeClosure:
				fn := in.Fn.(*ssa.Function) // Always a function, a func literal or a wrapper
				for i, bound := range in.Bindings {
					if bound == addr && !only(fn.FreeVars[i], false) {
						return false
					}
				}
			default:
				return false
			}
			if outer {
				uses = append(uses, ref)
			}
		}
		return true
	}
	if !only(alloc, true) || store == nil || w.positions.reaches(store, store) {
		return false
	}
	for _, use := range uses {
		if use != store && !storeDominates(store, use) {
			return false
		}
	}
	return true
}

// storeDominates reports whether a runs before b on every path to b, both
// in one function.
func storeDominates(a, b ssa.Instruction) bool {
	if a.Block() != b.Block() {
		return a.Block().Dominates(b.Block())
	}
	return slices.Index(a.Block().Instrs, a) < slices.Index(b.Block().Instrs, b)
}

// prefixLen reports whether addr is the root or a selection from it that the
// path of the walk starts with, and how many selections it takes.
func (w *storeWalk) prefixLen(addr ssa.Value) (int, bool) {
	at, _ := PlaceOf(addr)
	return len(at.path), at.prefixOf(w.at)
}

// copied follows a store of a whole aggregate that holds the address, at rest
// further along it. The value stored loads from another aggregate, such as
// the temporary a composite literal is built in, so what reaches the address
// is what reaches rest in that one when it is loaded:
//
//	t1 = local eventHolder (complit)
//	t2 = &t1.event
//	*t2 = v                // reaches the load of t1 below
//	t5 = *t1
//	*t0 = t5               // the store of a whole aggregate
//	t6 = &t0.event         // the address asked about
func (w *storeWalk) copied(store *ssa.Store, rest []sel) {
	if _, isConst := store.Val.(*ssa.Const); isConst {
		return // The zero value of the aggregate has nothing to trace
	}
	if p, ok := storeIsFuncLiteralParam(store.Val); ok {
		w.out.params = append(w.out.params, storeParamPath{param: p, path: slices.Clone(rest)})
		return
	}
	load, ok := AsLoad(store.Val)
	if !ok {
		w.out.unknown = true
		return
	}
	from, tracked := PlaceOf(load.X)
	src := &storeWalk{
		at:        from.with(rest...),
		positions: w.positions,
		out:       w.out,
		copies:    w.copies,
		pointers:  w.pointers,
	}
	if !tracked {
		if w.pointer == nil {
			w.out.unknown = true
			return
		}
		src.pointer = storePointerOf(from.root)
	}
	// An aggregate copied back and forth in a loop leads back to a copy
	// already followed, which adds nothing new.
	if src.copies == nil {
		src.copies = make(map[storeCopy]bool)
		w.copies = src.copies
	}
	key := storeCopy{load: load, at: src.at.Key()}
	if src.copies[key] {
		return
	}
	src.copies[key] = true
	src.walkBefore(load, storePath{})
}

// addStoresAfter adds the values stored to the address by the stores that
// can run after instr.
//
// For a variable declared in a loop, a path that goes round the loop again
// meets the declaration first, and the stores after it are to the variable of
// the next iteration. Those paths stop there:
//
//	for _, it := range items {
//	    l := base                          // a store to another l, after go
//	    l = base.With().Ctx(ctx).Logger()  // so is this, on the next iteration
//	    go func() { l.Info().Msg("x") }()
//	}
func (w *storeWalk) addStoresAfter(instr ssa.Instruction) {
	if !w.declaredInLoop() {
		after := w.positions.after(instr)
		for other := range InstrsIn(instr.Parent()) {
			if store, ok := other.(*ssa.Store); ok && after(store) {
				w.addStoreAfter(store)
			}
		}
		return
	}
	WalkFwd(WalkPointAfter(instr, w.positions), func(other ssa.Instruction) WalkStep {
		switch in := other.(type) {
		case *ssa.Alloc:
			if in == w.at.root {
				return WalkStop
			}
		case *ssa.Store:
			w.addStoreAfter(in)
		}
		return WalkOn
	})
}

// addStoreAfter adds what store writes to the address, when it may write it,
// for addStoresAfter.
func (w *storeWalk) addStoreAfter(store *ssa.Store) {
	n, cover := w.storeCover(store.Addr)
	if cover == coverNone {
		return
	}
	if n == len(w.at.path) {
		w.out.values = append(w.out.values, store.Val)
	} else {
		w.out.unknown = true
	}
}

// storeReadsOf returns the instructions that read addr, whose place is at,
// or an address with the same place: the loads of it, apart from those
// that only feed a store to it, and the calls it is passed to, apart from the
// calls on it of a method that model says leaves its receiver, such as
// Logger.UpdateContext. A store whose value loads from addr is an
// update, and such a load is how it takes the value before it.
//
// escapes are the other instructions that use addr, apart from the stores to
// it. Each passes addr on, as a closure that captures it or a variable it is
// stored to does, to be read later.
func storeReadsOf(addr ssa.Value, at Place, model Model) (reads, escapes []ssa.Instruction) {
	matches := func(v ssa.Value) bool {
		p, _ := PlaceOf(v)
		return len(p.path) == len(at.path) && p.covers(at, storeIndexAlways) == coverMust
	}
	var refs []ssa.Instruction
	if addr == at.root {
		refs = append(refs, *addr.Referrers()...) // A local variable or a parameter always has them
	}
	feeding := make(map[ssa.Instruction]bool)
	for instr := range InstrsIn(parentFunc(addr)) {
		if v, isValue := instr.(ssa.Value); isValue && v != at.root && matches(v) {
			refs = append(refs, *v.Referrers()...)
		}
		if store, ok := instr.(*ssa.Store); ok && matches(store.Addr) {
			recordLoad := func(v ssa.Value) bool {
				if load, ok := AsLoad(v); ok && matches(load.X) {
					feeding[load] = true
				}
				return false
			}
			derivesFrom(store.Val, recordLoad, unOpOperand, make(map[ssa.Value]bool))
		}
	}
	for _, ref := range refs {
		switch r := ref.(type) {
		case *ssa.Store:
			if !matches(r.Addr) {
				escapes = append(escapes, r) // addr itself is stored
			}
		case *ssa.UnOp:
			if !feeding[r] {
				reads = append(reads, r)
			}
		case *ssa.Call:
			if c, ok := CalleeOf(&r.Call); !ok || !model.LeavesReceiver(c.Func) || !matches(c.Recv) {
				reads = append(reads, r)
			}
		default:
			escapes = append(escapes, ref)
		}
	}
	return reads, escapes
}
