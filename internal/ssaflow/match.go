// match.go decides whether two SSA values name the same address. It is the
// bottom of this package and calls nothing above it.

package ssaflow

import (
	"go/token"
	"maps"
	"slices"

	"golang.org/x/tools/go/ssa"
)

// addressesMatch checks if two addresses refer to the same memory location.
//
// Selections are compared structurally rather than by identity, because SSA
// emits a fresh FieldAddr/IndexAddr for every access. Without that, the two
// halves of a nested access would never line up:
//
//	t1 = &t0.inner ; t2 = &t1.event ; *t2 = v   // write
//	t3 = &t0.inner ; t4 = &t3.event ; ... = *t4 // read, t3 != t1
//
// Two selections match as sel.same says, so an element whose index is not a
// constant matches only through the same instruction, which a == b catches.
//
//declscope:shared // store.go tells whether two roots are the same pointer
func addressesMatch(a, b ssa.Value) bool {
	if a == b {
		return true
	}

	if xa, sa, ok := addrSel(a); ok {
		xb, sb, ok := addrSel(b)
		return ok && sa.same(sb) && addressesMatch(xa, xb)
	}

	// A pointer is loaded again for each use of it, as a captured receiver
	// is, or read again from a struct value, as h.m is for a value h. Two
	// loads of the same address that the same stores to it reach, or two
	// fields of the same value, are taken for the same pointer. A store
	// through any of them may then be what a read through another sees (see
	// storesReachingPointer).
	//
	//	t1 = *t0 ; t2 = &t1.log ; *t2 = v   // write
	//	t8 = *t0 ; t9 = &t8.log ; ... = *t9 // read, t8 != t1
	//
	// A store of another pointer between the two loads makes the second one
	// another pointer:
	//
	//	*t0 = a ; t1 = *t0 ; t2 = &t1.log ; *t2 = v
	//	*t0 = b ; t8 = *t0 ; t9 = &t8.log ; ... = *t9 // reads b.log
	if l1, ok := a.(*ssa.UnOp); ok {
		l2, ok := b.(*ssa.UnOp)
		return ok && l1.Op == token.MUL && l2.Op == token.MUL && addressesMatch(l1.X, l2.X) &&
			maps.Equal(matchReachingStores(l1), matchReachingStores(l2))
	}

	// An element of an array value is not matched, only a field of a struct
	// value
	f1, ok1 := a.(*ssa.Field)
	f2, ok2 := b.(*ssa.Field)
	return ok1 && ok2 && f1.Field == f2.Field && addressesMatch(f1.X, f2.X)
}

// matchClosureRun is a run of a closure, a call of mc in the function that
// makes it.
//
//declscope:shared // store.go walks out of the closures a pointer is read in
type matchClosureRun struct {
	mc  *ssa.MakeClosure
	run ssa.Instruction
}

// matchAcrossClosure reports whether inner, a pointer that a closure reads
// from the variables it captures, is outer, a pointer in a function the
// closure is nested in. runs are the runs that lead out to that function,
// innermost first: runs[0] runs the closure, and the last one runs a closure
// in the function of outer. Selections from an address match as for
// addressesMatch, and a field of a struct value does not. A captured variable
// is the one the closure is made with. A load in the closure must see what is
// at its address as the closure starts, with no store before it there, and
// the load in the function of outer must see the same stores as reach the
// last run:
//
//	*t0 = pp ; t1 = *t0 ; t2 = *t1 ; *&t2.log = v   // outer: t2 is **pp
//	t0 = *pp ; t1 = *t0 ; Info(&t1.log)             // in the closure: t1 is **pp too
//
// A closure in between binds a variable it captures in turn. The caller makes
// sure that it stores nothing through it, so that what is there is what the
// outer function left (see storeWalk.walkPointerOut).
//
//declscope:shared // store.go tells whether a pointer a closure reads is one stored through before it runs
func matchAcrossClosure(inner, outer ssa.Value, runs []matchClosureRun) bool {
	switch in := inner.(type) {
	case *ssa.FreeVar:
		mc := runs[0].mc
		var bound ssa.Value
		if idx := slices.Index(in.Parent().FreeVars, in); in.Parent() == mc.Fn && idx >= 0 {
			bound = mc.Bindings[idx]
		}
		if len(runs) == 1 {
			return bound == outer
		}
		fv, isFreeVar := bound.(*ssa.FreeVar)
		return isFreeVar && matchAcrossClosure(fv, outer, runs[1:])
	case *ssa.UnOp:
		out, ok := outer.(*ssa.UnOp)
		if !ok || in.Op != token.MUL || out.Op != token.MUL || !matchAcrossClosure(in.X, out.X, runs) {
			return false
		}
		return maps.Equal(matchReachingStores(in), map[ssa.Instruction]bool{nil: true}) &&
			maps.Equal(matchReachingStores(out), matchStoresBefore(out.X, runs[len(runs)-1].run))
	}
	xa, sa, okA := addrSel(inner)
	xb, sb, okB := addrSel(outer)
	return okA && okB && sa.same(sb) && matchAcrossClosure(xa, xb, runs)
}

// matchReachingStores returns the instructions after which the value that load
// reads is at its address, on some path to load: the stores to the address,
// or to an aggregate that holds it, and the declaration of the local variable
// it is in. A path that leads back to the entry of the function, or to its
// recover block, is the nil key. Stores through another reference are not
// seen, as storesReaching does not see them.
func matchReachingStores(load *ssa.UnOp) map[ssa.Instruction]bool {
	return matchStoresBefore(load.X, load)
}

// matchStoresBefore returns what matchReachingStores does for a load of addr
// right before at.
func matchStoresBefore(addr ssa.Value, at ssa.Instruction) map[ssa.Instruction]bool {
	found := make(map[ssa.Instruction]bool)
	b := at.Block()
	type step struct {
		block *ssa.BasicBlock
		from  int // Index of the last instruction of block to look at
	}
	queue := []step{{b, slices.Index(b.Instrs, at) - 1}}
	seen := make(map[*ssa.BasicBlock]bool)
	for len(queue) > 0 {
		s := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		stopped := false
		for i := s.from; i >= 0 && !stopped; i-- {
			if instr := s.block.Instrs[i]; matchOverwrites(instr, addr) {
				found[instr] = true
				stopped = true
			}
		}
		if stopped {
			continue
		}
		if len(s.block.Preds) == 0 {
			found[nil] = true
			continue
		}
		for _, pred := range s.block.Preds {
			if !seen[pred] {
				seen[pred] = true
				queue = append(queue, step{pred, len(pred.Instrs) - 1})
			}
		}
	}
	return found
}

// matchOverwrites reports whether instr puts another value at addr: a store
// to addr or to an aggregate that holds it, or the declaration of the local
// variable addr is in.
//
//declscope:shared // store.go stops a walk for a pointer where another one is stored
func matchOverwrites(instr ssa.Instruction, addr ssa.Value) bool {
	store, isStore := instr.(*ssa.Store)
	for x, ok := addr, true; ok; x, _, ok = addrSel(x) {
		if isStore && addressesMatch(store.Addr, x) {
			return true
		}
		if alloc, isAlloc := x.(*ssa.Alloc); isAlloc && instr == alloc {
			return true
		}
	}
	return false
}

// MatchedPlaces keys the places that a function reads through a pointer that
// is not in a variable, such as one a call returns, so that two places
// through the same pointer, as addressesMatch takes them, have the same key:
//
//	t0 = *pp ; t1 = &t0.e ; t2 = *t1   // (*pp).e
//	t5 = *pp ; t6 = &t5.e ; t7 = *t6   // (*pp).e again: t5 is t0, the same key
//
// It keeps the roots it has keyed in each function.
type MatchedPlaces struct {
	roots map[*ssa.Function][]ssa.Value
}

// NewMatchedPlaces creates a MatchedPlaces that has keyed nothing yet.
func NewMatchedPlaces() *MatchedPlaces {
	return &MatchedPlaces{roots: make(map[*ssa.Function][]ssa.Value)}
}

// KeyOf returns the key of at: that of a place keyed before whose root is the
// same pointer, with the same selections, or its own. ok is false when the
// root of at is a global, or is read from one, and key is then its own.
// Another goroutine may change what is there between two reads, so two reads
// of it are not the same.
func (m *MatchedPlaces) KeyOf(at Place) (key PlaceKey, ok bool) {
	if matchFromGlobal(at.root) {
		return at.Key(), false
	}
	fn := parentFunc(at.root)
	for _, root := range m.roots[fn] {
		if addressesMatch(root, at.root) {
			return Place{root: root, path: at.path}.Key(), true
		}
	}
	m.roots[fn] = append(m.roots[fn], at.root)
	return at.Key(), true
}

// KeyAtRun returns the key that the function making mc has for key, the key
// of a place the func literal of mc, or one nested in it, reads through a
// pointer it loads from a variable it captures, through any number of loads,
// as run, a call of mc, runs it. That is the key of the same place through
// the same loads of the variable bound there, which the same stores reach as
// reach run (see matchAcrossClosure and matchRunsOut):
//
//	t2 = make closure f$1 [t1] ; t3 = t2()   // f$1: t0 = *h ; &t0.e
//	t4 = *t1 ; t5 = &t4.e                     // the same place: t4 is *h at t3
//
// Any such load is the same pointer as the others, as KeyOf takes them, so
// the key is the same as that of the load the function reads the place by.
//
// ok is false when key is not such a place, the closure stores to the
// variable before the load, or the function has no such load.
func (m *MatchedPlaces) KeyAtRun(key PlaceKey, mc *ssa.MakeClosure, run ssa.Instruction) (_ PlaceKey, ok bool) {
	if _, isLoad := AsLoad(key.base()); !isLoad {
		return key, false
	}
	fv, isFreeVar := matchReadFrom(key.base()).(*ssa.FreeVar)
	if !isFreeVar {
		return key, false
	}
	_, runs, ok := matchRunsOut(fv, mc, run)
	if !ok {
		return key, false
	}
	for instr := range InstrsIn(mc.Parent()) {
		v, isValue := instr.(ssa.Value)
		if !isValue {
			continue
		}
		if outer, isLoad := AsLoad(v); isLoad && matchAcrossClosure(key.base(), outer, runs) {
			at, ok := m.KeyOf(PlaceAt(outer))
			return key.rooted(at.base()), ok
		}
	}
	return key, false
}

// MatchPlaceAtRun returns the place that the function making mc has for the
// place load reads, in the func literal of mc or one nested in it, from a
// variable the func literal captures, as a call of mc runs it: the same
// selections from the variable bound there (see matchRunsOut). The func
// literal must not store there before load, so that load sees what is there
// as the call runs it:
//
//	t2 = make closure f$1 [t1] ; t3 = t2()   // f$1: t0 = *last ; Ctx(t0, ctx)
//	                                          // *last is *t1 at t3
//
// ok is false when load is not such a read, or selects an element at an index
// that is not a constant, which is a value of the func literal.
func MatchPlaceAtRun(load *ssa.UnOp, mc *ssa.MakeClosure) (_ Place, ok bool) {
	at, _ := PlaceOf(load.X)
	fv, isFreeVar := at.root.(*ssa.FreeVar)
	if !isFreeVar || at.Variable() || !maps.Equal(matchReachingStores(load), map[ssa.Instruction]bool{nil: true}) {
		return Place{}, false
	}
	bound, _, ok := matchRunsOut(fv, mc, nil)
	if !ok {
		return Place{}, false
	}
	return Place{root: bound, path: at.path}, true
}

// matchRunsOut returns the value bound to fv, a variable that the func literal
// of mc or one nested in it captures, in the function that makes mc, and the
// runs out to there, innermost first, as matchAcrossClosure takes them. run is
// the run of mc, and the runs of the closures in between have no call, which
// matchAcrossClosure does not look at.
//
// A closure in between must capture a variable of the function it is nested
// in, and store nothing to it or through it, so that what is there as it runs
// the inner one is what that function left:
//
//	func() {                 // captures h, and stores nothing through it
//	    func() { h.e.Ctx(ctx) }()
//	}()
//
// ok is false when a closure in between does, or when fv is not captured
// from the function that makes mc.
func matchRunsOut(fv *ssa.FreeVar, mc *ssa.MakeClosure, run ssa.Instruction) (bound ssa.Value, runs []matchClosureRun, ok bool) {
	for fn := fv.Parent(); fn != mc.Fn; fn = fn.Parent() {
		if fn == nil {
			return nil, nil, false // Not in the func literal of mc
		}
	}
	for {
		fn := fv.Parent()
		idx := slices.Index(fn.FreeVars, fv)
		if fn == mc.Fn {
			return mc.Bindings[idx], append(runs, matchClosureRun{mc: mc, run: run}), true
		}
		maker := matchMakerOf(fn)
		runs = append(runs, matchClosureRun{mc: maker})
		if fv, ok = maker.Bindings[idx].(*ssa.FreeVar); !ok || matchStoredThrough(fv) {
			return nil, nil, false
		}
	}
}

// matchMakerOf returns the MakeClosure that makes fn, a func literal that
// captures variables, in the function it is declared in. SSA makes a func
// literal once, where it is written.
func matchMakerOf(fn *ssa.Function) *ssa.MakeClosure {
	for instr := range InstrsIn(fn.Parent()) {
		if mc, isMaker := instr.(*ssa.MakeClosure); isMaker && mc.Fn == fn {
			return mc
		}
	}
	return nil // Not reached: a func literal that captures variables is made where it is written
}

// matchStoredThrough reports whether the function of fv, a free variable,
// stores to it, or to what is read through it: a field or element of it, or
// what a pointer loaded from it points to, at any depth. A store through a
// copy of it, or through a pointer passed to a function, is not seen.
func matchStoredThrough(fv *ssa.FreeVar) bool {
	for instr := range InstrsIn(fv.Parent()) {
		if store, isStore := instr.(*ssa.Store); isStore && matchReadFrom(store.Addr) == fv {
			return true
		}
	}
	return false
}

// matchReadFrom returns the value that v, an address or a pointer, is read
// from through loads, fields and elements: v itself when it is none of those.
func matchReadFrom(v ssa.Value) ssa.Value {
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

// matchFromGlobal reports whether v is a global, or is read from one: a load
// of it, or a field or element of one, at any depth.
func matchFromGlobal(v ssa.Value) bool {
	for {
		switch x := v.(type) {
		case *ssa.Global:
			return true
		case *ssa.UnOp:
			if x.Op != token.MUL {
				return false
			}
			v = x.X
		case *ssa.FieldAddr:
			v = x.X
		case *ssa.IndexAddr:
			v = x.X
		case *ssa.Field:
			v = x.X
		case *ssa.Index:
			v = x.X
		default:
			return false
		}
	}
}
