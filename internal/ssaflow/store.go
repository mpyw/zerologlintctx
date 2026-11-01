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
		w.walkBefore(rd)
	}
	for _, esc := range escapes {
		w.walkEscape(esc)
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
// storeWalk.walkPointerBefore). A path that gets there, or to the entry of
// the function, carries nothing: what is there was stored where the function
// cannot see, by the callee that returned the pointer, by the caller, or
// before another pointer was put where it is read from. Only the stores of
// the function itself are traced, and ok is false when no path carries one,
// so that the caller traces the pointer itself.
//
// For a global, an update is left out, and the path goes on past it (see
// storePointer.global).
func storesReachingPointer(at Place, read ssa.Instruction, positions Positions) (r storeReaching, ok bool) {
	if _, isConst := at.root.(*ssa.Const); isConst || read == nil {
		return r, false
	}
	w := &storeWalk{at: at, positions: positions, out: &r, pointer: storePointerOf(at.root)}
	w.walkToRead(read)
	return r, len(r.values) > 0 || r.unknown || len(r.captured) > 0 || len(r.params) > 0
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

// walkPointerBefore walks every path that leads to instr, for a walk whose
// root is a pointer that is not in a variable. Each path goes back to def,
// where the pointer is made, and on from there only when def loads it from
// an address: up to a store of another pointer there, or the declaration of
// the variable the address is in.
//
//	*t0 = a                   // stops the path: before it, t0 holds another pointer
//	t1 = *t0 ; *&t1.log = v   // reaches the read, past def
//	t8 = *t0                  // def
//	Info(&t8.log)
//
// A store of another pointer at from between def and instr does not stop a
// path: the pointer read is the one def loaded.
func (w *storeWalk) walkPointerBefore(instr ssa.Instruction) {
	def, reached := w.pointer.def, false
	toDef := func(in ssa.Instruction) WalkStep {
		if def != nil && in == def {
			reached = true
			return WalkStop
		}
		return w.stepAt(in)
	}
	walkBack([]WalkPoint{WalkPointBefore(instr, w.positions)}, toDef, nil, nil)
	from := w.pointer.from
	if !reached || from == nil {
		return
	}
	pastDef := func(in ssa.Instruction) WalkStep {
		if matchOverwrites(in, from) {
			return WalkStop
		}
		return w.stepAt(in)
	}
	walkBack([]WalkPoint{WalkPointBefore(def, w.positions)}, pastDef, nil, nil)
}

// samePointer reports whether root, the root of a store, is the pointer the
// walk looks for the stores through.
func (p *storePointer) samePointer(root, pointer ssa.Value) bool {
	same, done := p.same[root]
	if !done {
		same = addressesMatch(root, pointer)
		p.same[root] = same
	}
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
// Each variable is walked from read, as storesReaching walks one, along the
// paths on which phi points to it: where the walk first reaches the top of
// the block of phi, it goes on through the edges that hold the variable only.
// An edge that is a Phi itself holds it when one of its own edges does. Every
// value that reaches read must have context, as for the edges of a Phi.
//
// ok is false when an edge is not a pointer to a variable, a pointer
// parameter or a captured variable, or a field or constant element of one,
// when phi may point to more than storePickedMax of them, or when read is a
// go or defer statement, which reads the address later.
func storesReachingPicked(
	phi *ssa.Phi,
	path []sel,
	read ssa.Instruction,
	positions Positions,
) (r storeReaching, ok bool) {
	switch read.(type) {
	case nil, *ssa.Defer, *ssa.Go:
		return r, false
	}
	leaves, ok := storePickedLeaves(phi)
	if !ok {
		return r, false
	}
	for _, leaf := range leaves {
		leafAt, tracked := PlaceOf(leaf)
		if !tracked {
			return storeReaching{}, false
		}
		var lr storeReaching
		w := &storeWalk{
			at:        leafAt.with(path...),
			positions: positions,
			out:       &lr,
			picked:    &storePick{phi: phi, leaf: leaf},
		}
		w.walkBefore(read)
		r.merge(lr)
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

// storePick is a variable that a Phi of pointers points to, which a walk
// looks for the stores to: the walk takes only the paths on which the Phi
// points to it.
type storePick struct {
	phi  *ssa.Phi
	leaf ssa.Value
}

// holds reports whether p, phi or a Phi that an edge of phi leads to, may be
// the leaf, through edges that are Phis themselves.
func (pk *storePick) holds(p *ssa.Phi) bool {
	seen := make(map[*ssa.Phi]bool)
	var holds func(*ssa.Phi) bool
	holds = func(p *ssa.Phi) bool {
		if seen[p] {
			return false
		}
		seen[p] = true
		return slices.ContainsFunc(p.Edges, func(edge ssa.Value) bool {
			inner, isPhi := edge.(*ssa.Phi)
			return edge == pk.leaf || isPhi && holds(inner)
		})
	}
	return holds(p)
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

// storeTarget is something a pointer can point to: v, or one of the values
// that the Phi of picked can be, when v is that one.
//
// via, when not empty, are the closures the pointer is read in, innermost
// first, out to the function v is in: the pointer is read from a variable a
// closure captures, and v is what that variable holds when the closure runs.
type storeTarget struct {
	v      ssa.Value
	picked *storePick
	via    []*ssa.MakeClosure
}

// newStorePointers returns a storePointers that has followed no pointer yet.
func newStorePointers(positions Positions) *storePointers {
	return &storePointers{positions: positions, pointed: make(map[ssa.Value]storePointed)}
}

// reachingThrough finds the values that can be at at, whose root is a
// pointer read from memory, when read runs: what can be at the same
// selections from each variable the pointer can point to. ok is false when
// the pointer cannot be followed, or points to nothing but nil.
func (p *storePointers) reachingThrough(at Place, read ssa.Instruction) (r storeReaching, ok bool) {
	targets, ok := p.targetsOf(at.root)
	if !ok || len(targets) == 0 {
		return r, false
	}
	for _, t := range targets {
		tat, tracked := PlaceOf(t.v)
		tat = tat.with(at.path...)
		var tr storeReaching
		if !tracked && len(t.via) == 0 {
			// A pointer copied from another one read from memory
			if tr, ok = p.reachingThrough(tat, read); !ok {
				return storeReaching{}, false
			}
			r.merge(tr)
			continue
		}
		w := &storeWalk{at: tat, positions: p.positions, out: &tr, picked: t.picked, pointers: p, outer: t.via}
		w.walkToRead(read)
		if !tracked && tr.isEmpty() {
			// What a callee stored there is not seen, as for storesReachingPointer
			return storeReaching{}, false
		}
		r.merge(tr)
	}
	return r, true
}

// isEmpty reports whether r found nothing at all.
func (r storeReaching) isEmpty() bool {
	return len(r.values) == 0 && !r.unknown && len(r.captured) == 0 && len(r.params) == 0
}

// targetsOf returns what v, a pointer read from memory, can point to. ok is
// false when v is not one, or what it points to cannot be told.
func (p *storePointers) targetsOf(v ssa.Value) ([]storeTarget, bool) {
	if got, done := p.pointed[v]; done {
		return got.targets, got.ok
	}
	at, load, isRead := storePointerRead(v)
	if !isRead {
		return nil, false
	}
	p.pointed[v] = storePointed{} // Reaching v again while it is followed fails
	var got storePointed
	if stored, ok := p.reaching(at, load); ok {
		got.targets, got.ok = storeTargetsReaching(stored, nil, p.positions)
	}
	p.pointed[v] = got
	return got.targets, got.ok
}

// storeTargetsReaching returns what a pointer can point to when it can be
// any of what stored found where it is read from. via are the closures the
// read is in, innermost first, out to the function stored was found in.
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
// a func literal, when a captured variable has no MakeClosure that binds it,
// or when a target through a closure is one of the values of a Phi.
func storeTargetsReaching(stored storeReaching, via []*ssa.MakeClosure, positions Positions) ([]storeTarget, bool) {
	if stored.unknown || len(stored.params) > 0 {
		return nil, false
	}
	targets, ok := storeTargetsOf(stored.values)
	if !ok {
		return nil, false
	}
	for i := range targets {
		if len(via) > 0 && targets[i].picked != nil {
			return nil, false // walkOut does not keep to the paths of a Phi
		}
		targets[i].via = via
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
			more, ok := storeTargetsReaching(bound, append(slices.Clip(via), mc), positions)
			if !ok {
				return nil, false
			}
			targets = append(targets, more...)
		}
	}
	return targets, true
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
func (p *storePointers) through(at Place, root ssa.Value) (Place, placeCover) {
	if _, _, isRead := storePointerRead(at.root); !isRead {
		return at, coverNone
	}
	targets, ok := p.targetsOf(at.root)
	if !ok {
		return at, coverNone
	}
	for _, t := range targets {
		tat, _ := PlaceOf(t.v)
		if tat.root != root {
			continue
		}
		if len(targets) == 1 && t.picked == nil {
			return tat.with(at.path...), coverMust
		}
		return tat.with(at.path...), coverMay
	}
	return at, coverNone
}

// storePointerRead returns where the pointer v is read from, when it is read
// from memory: the address a load reads, with the fields and elements of a
// struct or array value selected from what it loads, and the load.
//
//	t3 = *&t0.p             // read from t0.p
//	t5 = *t0 ; t6 = t5.p    // read from t0.p too, as t5 is loaded
func storePointerRead(v ssa.Value) (at Place, load *ssa.UnOp, ok bool) {
	var selections []sel
	for {
		from, s, isSel := valueSel(v)
		if !isSel {
			break
		}
		selections = append(selections, s)
		v = from
	}
	load, ok = AsLoad(v)
	if !ok {
		return Place{}, nil, false
	}
	slices.Reverse(selections)
	base, _ := PlaceOf(load.X)
	return base.with(selections...), load, true
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
			targets = append(targets, storeTarget{v: leaf, picked: &storePick{phi: phi, leaf: leaf}})
		}
	}
	return targets, true
}

// merge adds what o found to r.
func (r *storeReaching) merge(o storeReaching) {
	r.values = append(r.values, o.values...)
	r.params = append(r.params, o.params...)
	r.unknown = r.unknown || o.unknown
	for _, c := range o.captured {
		r.addCaptured(c)
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
	w.walkEscape(mc)
	return r
}

// walkEscape walks to esc, an instruction that passes the address on, such as
// a closure that captures it. What it passes the address on to may read it at
// any point after, so what reaches esc, and every store after it, may be what
// it reads. A closure that is only deferred reads it at the returns after it,
// unless the variable is declared in a loop, and one that is only called reads
// it at each call (see closureRuns). A Phi passes the address on only from
// the edges that are the address (see walkToPhiEdges).
func (w *storeWalk) walkEscape(esc ssa.Instruction) {
	switch e := esc.(type) {
	case *ssa.MakeClosure:
		if w.walkToRuns(e) {
			return
		}
	case *ssa.Phi:
		w.walkToPhiEdges(e)
		w.addStoresAfter(esc)
		return
	}
	w.walkBefore(esc)
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

// walkToRuns walks to where mc runs, when it is a closure that is only
// deferred, outside a loop the variable is declared in, or only called. It
// reports whether it did.
func (w *storeWalk) walkToRuns(mc *ssa.MakeClosure) bool {
	runs, ok := closureRunsOf(mc)
	if !ok {
		return false
	}
	if defers, deferred := runs.defersOnly(); deferred && !w.declaredInLoop() {
		for _, d := range defers {
			w.walkDeferred(d)
		}
		return true
	}
	if calls, called := runs.callsOnly(); called {
		for _, call := range calls {
			w.walkBefore(call)
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
func (w *storeWalk) walkDeferred(d *ssa.Defer) {
	var rets []WalkPoint
	for _, ret := range storeReturnsAfter(d, w.positions) {
		rets = append(rets, WalkPointBefore(ret, w.positions))
	}
	reachesD := false
	visit := func(instr ssa.Instruction) WalkStep {
		if instr == d {
			reachesD = true // Before it, the path goes on from d
			return WalkStop
		}
		return w.stepAt(instr)
	}
	walkBack(rets, visit, nil, walkThrough(d, w.positions))
	if reachesD {
		w.walkBefore(d)
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

// walkToRead walks every path to read. A deferred call reads at the returns
// after it, and a go call at any point after it, as an escape does. A
// deferred call that reads a variable declared in a loop is read as an
// escape too, since a return may meet a later iteration's variable first.
// So is one for a walk with a picked variable, whose paths walkDeferred does
// not keep to.
func (w *storeWalk) walkToRead(read ssa.Instruction) {
	if d, ok := read.(*ssa.Defer); ok && !w.declaredInLoop() && w.picked == nil && w.pointer == nil {
		w.walkDeferred(d)
		return
	}
	switch read.(type) {
	case *ssa.Defer, *ssa.Go:
		w.walkBefore(read)
		w.addStoresAfter(read)
	default:
		w.walkBefore(read)
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
		return newStorePointers(positions).reachingThrough(at, read)
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
	// picked, when not nil, is the Phi the address is read through, and the
	// variable of root it points to. The walk takes only the paths on which
	// the Phi points to it.
	picked *storePick
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
// before it.
//
// When the walk has a picked variable, each path also holds the Phi that must
// point to it there, until the path has passed the edge that takes the
// variable itself. At the top of the block of that Phi, the walk
// goes on through the edges that may hold the variable only.
func (w *storeWalk) walkBefore(instr ssa.Instruction) {
	if w.pointer != nil {
		w.walkPointerBefore(instr)
		return
	}
	if w.picked == nil {
		w.walkBackFrom([]WalkPoint{WalkPointBefore(instr, w.positions)})
		return
	}
	atEntry := func() bool {
		w.reachEntry()
		return true
	}
	from := []WalkFrom[*ssa.Phi]{{WalkPointBefore(instr, w.positions), w.picked.phi}}
	edge := func(b *ssa.BasicBlock, i int, need *ssa.Phi) (*ssa.Phi, bool) {
		if need == nil || need.Block() != b {
			return need, true
		}
		return w.pickedThrough(need.Edges[i]) // The Phi may not point to the variable on this path
	}
	WalkBackWith(from, w.stepAt, atEntry, edge)
}

// walkBackFrom walks every path that leads to a point of from, and stops each
// at the first store to the address.
func (w *storeWalk) walkBackFrom(from []WalkPoint) {
	atEntry := func() bool {
		w.reachEntry()
		return true
	}
	walkBack(from, w.stepAt, atEntry, nil)
}

// pickedThrough reports whether edge, of a Phi that points to the picked
// variable, may be the variable, and returns the Phi that must point to it
// before the edge: edge itself when it is a Phi, or nil when it is the
// variable.
func (w *storeWalk) pickedThrough(edge ssa.Value) (*ssa.Phi, bool) {
	if edge == w.picked.leaf {
		return nil, true
	}
	if inner, isPhi := edge.(*ssa.Phi); isPhi && w.picked.holds(inner) {
		return inner, true
	}
	return nil, false
}

// reachEntry records a path that leads back to the entry of the function, or
// to its recover block. What is at a pointer parameter there is the caller's,
// which is known only for a func literal, from its calls. What is at a
// captured variable is the one the closure was made with. A local variable is
// declared before any read, and a walk from a return where a deferred call
// runs only takes the paths through its defer, so its walk never gets here.
// A walk for a variable that a pointer read from memory points to may get
// here, on a path where the pointer is another one, such as nil, and the
// variable is not made: that path carries nothing.
func (w *storeWalk) reachEntry() {
	if len(w.outer) > 0 {
		w.walkOut()
		return
	}
	if _, isAlloc := w.at.root.(*ssa.Alloc); isAlloc && w.pointers != nil {
		return
	}
	if fv, ok := w.at.root.(*ssa.FreeVar); ok {
		w.out.addCaptured(Place{root: fv, path: slices.Clone(w.at.path)})
		return
	}
	if p, ok := storeIsFuncLiteralParam(w.at.root); ok {
		w.out.params = append(w.out.params, storeParamPath{param: p, path: slices.Clone(w.at.path), pointer: true})
		return
	}
	w.out.unknown = true
}

// walkOut goes on from the entry of outer[0], the closure the walk is in,
// in the function that makes it, as a walk for what the closure reads of a
// variable it captures does (see walkEscape): a store before the closure
// runs, or after it is made when it may run later, may be what is at the
// address. In the function root is in, a root that is not in a variable is
// walked for as storesReachingPointer walks for one.
func (w *storeWalk) walkOut() {
	out := &storeWalk{at: w.at, positions: w.positions, out: w.out, pointers: w.pointers, outer: w.outer[1:]}
	if len(out.outer) == 0 && !isTrackedRoot(w.at.root) {
		out.pointer = storePointerOf(w.at.root)
	}
	out.walkEscape(w.outer[0])
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
// l = l.With().Logger() does.
func (w *storeWalk) updates(store *ssa.Store) bool {
	isLoadOfAddr := func(v ssa.Value) bool {
		load, isLoad := AsLoad(v)
		if !isLoad {
			return false
		}
		n, cover := w.storeCover(load.X)
		return n == len(w.at.path) && cover == coverMust
	}
	return derivesFrom(store.Val, isLoadOfAddr, unOpOperand, make(map[ssa.Value]bool))
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

// storeIndexAlways is an index that is always taken to have one value, so
// that an element at the same index is the same element, for a test of places
// that runs nowhere in particular.
func storeIndexAlways(ssa.Value) bool { return true }

// indexOnce reports whether index, the index of an element of the address of
// the walk, has one value wherever the walk meets it: it is not computed by
// an instruction that runs again, in a loop. A store at the same index is
// then to the same element.
//
//	for i := range arr {   // i is a Phi, which runs again on each iteration
//	    arr[i].Info()      // the store below is to arr[i-1] here
//	    arr[i] = base
//	}
func (w *storeWalk) indexOnce(index ssa.Value) bool {
	def, ok := index.(ssa.Instruction)
	return !ok || !w.positions.reaches(def, def)
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
	src.walkBefore(load)
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
