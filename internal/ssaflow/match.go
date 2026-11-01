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
	// copied, when not nil, is the one store that reaches run of the whole
	// variable that mc binds, in one run only. What the closure reads from
	// the variable is what that store copied there (see matchCopiedLoad).
	copied *ssa.Store
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
		return matchBoundOut(in, runs) == outer
	case *ssa.UnOp:
		if matchCopiedLoad(in, outer, runs) {
			return true
		}
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

// matchCopiedLoad reports whether inner, a load in a closure of the variable
// it captures, or of a selection from it, is outer, when the last of runs has
// a store copied to that variable, through what was copied. A load of the
// variable itself in the outer function is matched as for any run, by
// matchAcrossClosure. The load must see what is at its address as the closure
// starts. A load of the whole variable is the value copied:
//
//	*t0 = other                       // copied: t0 is b, other a parameter
//	t1 = *b ; Info(&t1.log)           // in the closure: t1 is other
//
// A load of a selection from it is the same load from where the value copied
// was loaded, as that load saw it, when outer loads the same selection from
// there and the same stores reach both:
//
//	t2 = *&t0.p ; *t2 = v             // outer: t0 is a local struct
//	t8 = *t0 ; *t7 = t8               // copied: t7 is b
//	t1 = *&b.p ; Info(t1)             // in the closure: t1 is t2
//
// When the run with the copy is not the last one, as for a copy made in a
// closure from a variable it captures, what was copied is matched across the
// runs after it as matchAcrossClosure matches a load: the load copied must
// see what is at its address as that closure starts.
func matchCopiedLoad(inner *ssa.UnOp, outer ssa.Value, runs []matchClosureRun) bool {
	k := slices.IndexFunc(runs, func(r matchClosureRun) bool { return r.copied != nil })
	if k < 0 {
		return false
	}
	store, rest := runs[k].copied, runs[k+1:]
	fv, sels := matchSelsOf(inner.X)
	bound, isFreeVar := fv.(*ssa.FreeVar)
	if inner.Op != token.MUL || !isFreeVar || matchBoundOut(bound, runs[:k+1]) != store.Addr ||
		!maps.Equal(matchReachingStores(inner), map[ssa.Instruction]bool{nil: true}) {
		return false
	}
	if len(sels) == 0 {
		return len(rest) == 0 && addressesMatch(store.Val, outer) || len(rest) > 0 && matchAcrossClosure(store.Val, outer, rest)
	}
	from, isLoad := AsLoad(store.Val)
	return isLoad && matchLoadedSels(from, sels, outer, rest)
}

// matchLoadedSels reports whether outer, a load of a pointer, reads the
// pointer at sels in what from, a load of an aggregate, loaded: outer loads
// the same selections from where from loads, and the same stores reach both.
//
//	t2 = *&t0.p ; *t2 = v             // outer
//	t8 = *t0                          // from, with sels .p: t2 is t8.p
//
// With runs, outer is in the function of the last of them, and from is in the
// closure runs[0] runs, where it must see what is at its address as the
// closure starts. Where from loads is matched across the runs as
// matchAcrossClosure matches it.
//
//declscope:shared // store.go matches a store through the pointer in a copy made where it is read
func matchLoadedSels(from *ssa.UnOp, sels []sel, outer ssa.Value, runs []matchClosureRun) bool {
	out, isOut := AsLoad(outer)
	if !isOut {
		return false
	}
	x, outSels := matchSelsOf(out.X)
	fromX, fromSels := matchSelsOf(from.X)
	if !slices.EqualFunc(append(fromSels, sels...), outSels, sel.same) {
		return false
	}
	if len(runs) == 0 {
		return addressesMatch(x, fromX) && maps.Equal(matchStoresBefore(out.X, out), matchStoresBefore(out.X, from))
	}
	return matchAcrossClosure(fromX, x, runs) &&
		maps.Equal(matchStoresBefore(from.X, from), map[ssa.Instruction]bool{nil: true}) &&
		maps.Equal(matchStoresBefore(out.X, out), matchStoresBefore(out.X, runs[len(runs)-1].run))
}

// matchSameOrigin reports whether a and b, two loads of a pointer in one
// function, read the same pointer: each is followed back past the copies of
// a whole aggregate that put the pointer where it is loaded (see
// matchOriginOf), to the same selections from the same address, and the
// same stores reach that address there:
//
//	t2 = *t0 ; *t1 = t2               // b := h, for h in t0 and b in t1
//	t4 = *&t0.p ; *t4 = v             // a: h.p, after the copy
//	t9 = *&t1.p ; Info(t9)            // b: b.p, which is t2.p, so h.p too
//
// Two loads of the same address are matched as addressesMatch matches them,
// with no copy to follow.
//
//declscope:shared // store.go tells whether a store is through the pointer of a walk
func matchSameOrigin(a, b ssa.Value) bool {
	oa, ok := matchOriginOf(a)
	return ok && matchOriginOfIs(oa, b)
}

// matchOriginOfIs reports whether v, a load of a pointer, reads it from o in
// the end (see matchOriginOf): the same selections from the same address, in
// the same function, with the same stores reaching both.
//
//declscope:shared // store.go matches a store through the pointer a copy holds
func matchOriginOfIs(o matchOrigin, v ssa.Value) bool {
	ov, ok := matchOriginOf(v)
	return ok && o.at.Parent() == ov.at.Parent() &&
		addressesMatch(o.root, ov.root) && slices.EqualFunc(o.sels, ov.sels, sel.same) &&
		maps.Equal(matchStoresBeforeSels(o.root, o.sels, o.at), matchStoresBeforeSels(o.root, o.sels, ov.at))
}

// matchOrigin is where a pointer is read from: the address at sels from
// root, as it is when at runs.
//
//declscope:shared // store.go builds the origin of the pointer a copy holds
type matchOrigin struct {
	root ssa.Value
	sels []sel
	at   ssa.Instruction
}

// matchOriginOf returns where v, a load of a pointer, reads it from in the
// end, as matchOriginAt does from where v loads. ok is false when v is not a
// load.
func matchOriginOf(v ssa.Value) (_ matchOrigin, ok bool) {
	load, isLoad := AsLoad(v)
	if !isLoad {
		return matchOrigin{}, false
	}
	root, sels := matchSelsOf(load.X)
	return matchOriginAt(matchOrigin{root: root, sels: sels, at: load}), true
}

// matchOriginAt follows o back past the copies of a whole aggregate that put
// what is there. When one store reaches o, and it copies a whole aggregate
// that holds the address, loaded from memory, what is at o is at the same
// selections in what that load loaded, as it loaded it:
//
//	t2 = *t0 ; *t1 = t2               // the one store that reaches t1.p
//	t9 = *&t1.p                       // o: t1.p at t9, then t0.p at t2
//
// It stops at any other store, at more than one, or when a store may write
// the address, or not, as one at a variable index may. Each copy followed
// runs before the load it is found from on every path, so the walk never
// comes back to a load, even in a loop.
//
//declscope:shared // store.go takes the origin of the pointer a copy holds
func matchOriginAt(o matchOrigin) matchOrigin {
	for {
		stores := matchStoresBeforeSels(o.root, o.sels, o.at)
		if len(stores) != 1 {
			return o
		}
		var store *ssa.Store
		for instr := range stores {
			store, _ = instr.(*ssa.Store)
		}
		if store == nil {
			return o // The entry of the function, or the declaration
		}
		load, isLoad := AsLoad(store.Val)
		to, toSels := matchSelsOf(store.Addr)
		cover := Place{root: o.root, path: toSels}.covers(Place{root: o.root, path: o.sels}, matchSameIndex)
		if !isLoad || len(toSels) == len(o.sels) || !addressesMatch(to, o.root) || cover != coverMust {
			return o // Not a copy of an aggregate that holds the address
		}
		root, sels := matchSelsOf(load.X)
		o = matchOrigin{root: root, sels: append(sels, o.sels[len(toSels):]...), at: load}
	}
}

// matchSameIndex takes two elements for the same element when their indexes
// are the same value.
func matchSameIndex(a, b ssa.Value) bool { return a == b }

// matchStoresBeforeSels returns what matchStoresBefore does for an address
// at sels from root, which need not be a value of the function: the stores
// that may write it, or an aggregate that holds it, and the declaration of
// the local variable it is in.
func matchStoresBeforeSels(root ssa.Value, sels []sel, at ssa.Instruction) map[ssa.Instruction]bool {
	return matchStoresBeforeBy(at, func(instr ssa.Instruction) bool {
		if alloc, isAlloc := root.(*ssa.Alloc); isAlloc && instr == alloc {
			return true // The declaration of the variable
		}
		store, isStore := instr.(*ssa.Store)
		if !isStore {
			return false
		}
		to, toSels := matchSelsOf(store.Addr)
		cover := Place{root: root, path: toSels}.covers(Place{root: root, path: sels}, matchSameIndex)
		return cover != coverNone && addressesMatch(to, root)
	})
}

// matchSelsOf returns the address that v, an address, selects from through
// fields and elements, and those selections, outermost first: v itself and
// none when it is neither.
//
//declscope:shared // store.go finds the selections a copy of an aggregate holds the pointer at
func matchSelsOf(v ssa.Value) (ssa.Value, []sel) {
	var sels []sel
	for x, s, ok := addrSel(v); ok; x, s, ok = addrSel(v) {
		sels = append(sels, s)
		v = x
	}
	slices.Reverse(sels)
	return v, sels
}

// matchBoundOut returns the value bound to fv, a free variable of the closure
// runs[0] runs, in the function of the last of runs, through the closures in
// between that capture it in turn. It is nil when fv is not of that closure,
// and a value of the function in between when one binds something else.
func matchBoundOut(fv *ssa.FreeVar, runs []matchClosureRun) ssa.Value {
	var bound ssa.Value
	if idx := slices.Index(fv.Parent().FreeVars, fv); fv.Parent() == runs[0].mc.Fn {
		bound = runs[0].mc.Bindings[idx]
	}
	if next, isFreeVar := bound.(*ssa.FreeVar); isFreeVar && len(runs) > 1 {
		return matchBoundOut(next, runs[1:])
	}
	return bound
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
//
//declscope:shared // store.go finds the copy of a captured variable that reaches a call of the closure
func matchStoresBefore(addr ssa.Value, at ssa.Instruction) map[ssa.Instruction]bool {
	return matchStoresBeforeBy(at, func(instr ssa.Instruction) bool { return matchOverwrites(instr, addr) })
}

// matchStoresBeforeBy returns the instructions that overwrites accepts, at
// which the paths back from at first stop, with nil for a path that leads
// back to the entry of the function or to its recover block.
func matchStoresBeforeBy(at ssa.Instruction, overwrites func(ssa.Instruction) bool) map[ssa.Instruction]bool {
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
			if instr := s.block.Instrs[i]; overwrites(instr) {
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
	if _, isLoad := AsLoad(key.Root()); !isLoad {
		return key, false
	}
	fv, isFreeVar := matchReadFrom(key.Root()).(*ssa.FreeVar)
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
		if outer, isLoad := AsLoad(v); isLoad && matchAcrossClosure(key.Root(), outer, runs) {
			at, ok := m.KeyOf(PlaceAt(outer))
			return key.rooted(at.Root()), ok
		}
	}
	return key, false
}

// SameAtRuns reports whether a and b, keys of places that two func literals
// nested in fn read through pointers they load from variables they capture,
// are the same place as fn runs them: the same selections through the same
// loads of the same variable of fn, which fn captures in turn. KeyAtRun maps
// such a key to one of a load in the function, and finds none in fn when fn
// reads the place only through the func literals:
//
//	func() {                          // fn: captures p, and reads p.e only below
//	    if cond {
//	        func() { p.e.Ctx(ctx) }() // t0 = *p ; &t0.e
//	    } else {
//	        func() { p.e.Ctx(ctx) }() // t0 = *p ; &t0.e: the same place
//	    }
//	}()
//
// Each load must see what is at its address as its func literal starts, and
// fn and each func literal in between must store nothing to the variable or
// through it, so that both see what the function that makes fn left there.
func (m *MatchedPlaces) SameAtRuns(a, b PlaceKey, fn *ssa.Function) bool {
	if a == b {
		return true
	}
	_, aLoad := AsLoad(a.Root())
	_, bLoad := AsLoad(b.Root())
	sameSelections := a.rooted(b.Root()) == b
	return sameSelections && aLoad && bLoad && matchSiblingReads(a.Root(), b.Root(), fn)
}

// matchSiblingReads reports whether a and b, read in func literals nested in
// fn, are read the same way from the same variable of fn, as
// MatchedPlaces.SameAtRuns has it.
func matchSiblingReads(a, b ssa.Value, fn *ssa.Function) bool {
	entry := map[ssa.Instruction]bool{nil: true}
	switch x := a.(type) {
	case *ssa.FreeVar:
		y, ok := b.(*ssa.FreeVar)
		if !ok {
			return false
		}
		bx, okX := matchCapturedIn(x, fn)
		by, okY := matchCapturedIn(y, fn)
		return okX && okY && bx == by
	case *ssa.UnOp:
		y, ok := b.(*ssa.UnOp)
		return ok && x.Op == token.MUL && y.Op == token.MUL && matchSiblingReads(x.X, y.X, fn) &&
			maps.Equal(matchReachingStores(x), entry) && maps.Equal(matchReachingStores(y), entry)
	}
	xa, sa, okA := addrSel(a)
	xb, sb, okB := addrSel(b)
	return okA && okB && sa.same(sb) && matchSiblingReads(xa, xb, fn)
}

// matchCapturedIn returns the variable of fn that fv, a variable that fn or
// a func literal nested in it captures, is bound to, through the func
// literals in between. ok is false when fv is not captured from a variable
// that fn captures, or a func literal in between stores to it or through it,
// or fn does. A func literal that is not nested in fn is bound, at the end,
// to a variable of a function that is not a func literal.
func matchCapturedIn(fv *ssa.FreeVar, fn *ssa.Function) (_ *ssa.FreeVar, ok bool) {
	for lit := fv.Parent(); lit != fn; lit = fv.Parent() {
		idx := slices.Index(lit.FreeVars, fv)
		for instr := range InstrsIn(lit.Parent()) {
			if mc, isMaker := instr.(*ssa.MakeClosure); isMaker && mc.Fn == lit {
				if fv, ok = mc.Bindings[idx].(*ssa.FreeVar); !ok || matchStoredThrough(fv) {
					return nil, false
				}
			}
		}
	}
	return fv, true
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
		// SSA makes a func literal that captures variables once, where it is
		// written
		var maker *ssa.MakeClosure
		for instr := range InstrsIn(fn.Parent()) {
			if mc, isMaker := instr.(*ssa.MakeClosure); isMaker && mc.Fn == fn {
				maker = mc
			}
		}
		runs = append(runs, matchClosureRun{mc: maker})
		if fv, ok = maker.Bindings[idx].(*ssa.FreeVar); !ok || matchStoredThrough(fv) {
			return nil, nil, false
		}
	}
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
