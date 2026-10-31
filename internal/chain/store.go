// store.go finds what can be at an address when it is read. storesReaching is
// the entry; it leans on walk.go to walk the control flow, on aggregate.go to
// follow copies and on match.go to decide whether two addresses are the same.

package chain

import (
	"go/token"
	"slices"

	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/zerologapi"
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
// A call to Logger.UpdateContext changes the fields of the Logger in place,
// but not its context: zerolog keeps only the fields the func writes, and
// drops what Context.Ctx sets. So it neither stops a path nor counts as a
// read.
//
// This holds for an address whose root is a local variable, a pointer
// parameter or a variable a closure captures: its field, its constant
// element, or itself. The stores that reach a read are known there, from the
// function alone. A path that leads back to the declaration of a local
// variable carries its zero value, which has no value to trace. One that
// leads back to the entry of the function carries the value the caller
// passed, which is unknown, or the value of the captured variable when the
// closure runs, which is among captured.
//
// For any other address, such as a global, ok is true when the function
// stores to it, and holds every value it stores there, wherever it runs.
//
// positions finds where each instruction is in its block, and may be kept
// across calls.
//
//declscope:shared // resolve.go and context.go find what reaches each read
func storesReaching(
	addr ssa.Value,
	read ssa.Instruction,
	positions instrPositions,
) (r storeReaching, ok bool) {
	at, tracked := placeOf(addr)
	if !tracked {
		plain := findStores(addr, make(map[ssa.Value]bool))
		for _, store := range plain {
			r.values = append(r.values, store.Val)
		}
		if phi, isPhi := at.root.(*ssa.Phi); isPhi && len(plain) == 0 {
			return storesReachingPicked(phi, at.path, read, positions)
		}
		return r, len(plain) > 0
	}

	w := &storeWalk{at: at, positions: positions, out: &r}
	if read != nil {
		w.walkToRead(read)
		return r, true
	}
	reads, escapes := storeReadsOf(addr, at)
	for _, rd := range reads {
		w.walkBefore(rd)
	}
	for _, esc := range escapes {
		w.walkEscape(esc)
	}
	return r, true
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
	positions instrPositions,
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
		leafAt, tracked := placeOf(leaf)
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
			if seen[edge] || isNilConst(edge) {
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
func (r *storeReaching) addCaptured(c place) {
	key := c.key()
	if !slices.ContainsFunc(r.captured, func(o place) bool { return o.key() == key }) {
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
	positions instrPositions,
) storeReaching {
	var r storeReaching
	w := &storeWalk{at: place{root: v, path: path}, positions: positions, out: &r}
	w.walkEscape(mc)
	return r
}

// walkEscape walks to esc, an instruction that passes the address on, such as
// a closure that captures it. What it passes the address on to may read it at
// any point after, so what reaches esc, and every store after it, may be what
// it reads. A closure that is only deferred reads it at the returns after it,
// unless the variable is declared in a loop, and one that is only called reads
// it at each call (see closureRuns).
func (w *storeWalk) walkEscape(esc ssa.Instruction) {
	if mc, ok := esc.(*ssa.MakeClosure); ok {
		if w.walkToRuns(mc) {
			return
		}
	}
	w.walkBefore(esc)
	w.addStoresAfter(esc)
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
	var rets []walkPoint
	for _, ret := range storeReturnsAfter(d, w.positions) {
		rets = append(rets, walkPointBefore(ret, w.positions))
	}
	reachesD := false
	visit := func(instr ssa.Instruction) walkStep {
		if instr == d {
			reachesD = true // Before it, the path goes on from d
			return walkStop
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
func storeReturnsAfter(d *ssa.Defer, positions instrPositions) []ssa.Instruction {
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
func (w *storeWalk) walkToRead(read ssa.Instruction) {
	if d, ok := read.(*ssa.Defer); ok && !w.declaredInLoop() {
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
//declscope:shared // resolve.go resolves each part, and tracing.go reads the pointers at an embedded field
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
	captured []place
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
// root of at is not one isTrackedRoot accepts.
//
//declscope:shared // resolve.go walks from a call of a func literal, tracing.go from an embedded field
func storesReachingAt(
	at place,
	read ssa.Instruction,
	positions instrPositions,
) (r storeReaching, ok bool) {
	if !isTrackedRoot(at.root) {
		return r, false
	}
	w := &storeWalk{at: at, positions: positions, out: &r}
	w.walkToRead(read)
	return r, true
}

// storeLoadsOf returns, for addr, a local variable, a pointer parameter or a
// captured variable, or a field or constant element of one, the loads of the
// same address in the function, and the instructions after which another
// value is there: the stores to it, or to an aggregate that holds it, and the
// declaration of a local variable. ok is false for any other address.
//
//declscope:shared // tracing.go finds the Ctx calls on an event in memory
func storeLoadsOf(addr ssa.Value) (loads []*ssa.UnOp, overwrites map[ssa.Instruction]bool, ok bool) {
	at, _ := placeOf(addr)
	return storeLoadsAt(at)
}

// storeLoadsAt returns what storeLoadsOf does for the address at.
//
//declscope:shared // tracing.go finds the Ctx calls on an embedded event
func storeLoadsAt(at place) (loads []*ssa.UnOp, overwrites map[ssa.Instruction]bool, ok bool) {
	if !isTrackedRoot(at.root) {
		return nil, nil, false
	}
	w := &storeWalk{at: at}
	overwrites = make(map[ssa.Instruction]bool)
	fn := parentFunc(at.root)
	if fv, isFreeVar := at.root.(*ssa.FreeVar); isFreeVar {
		fn = fv.Parent()
	}
	for instr := range instrsIn(fn) {
		switch in := instr.(type) {
		case *ssa.UnOp:
			if n, ok := w.prefixLen(in.X); ok && in.Op == token.MUL && n == len(at.path) {
				loads = append(loads, in)
			}
		case *ssa.Store:
			if _, ok := w.prefixLen(in.Addr); ok {
				overwrites[in] = true
			}
		case *ssa.Alloc:
			if in == at.root {
				overwrites[in] = true
			}
		}
	}
	return loads, overwrites, true
}

// storeWalk walks the control flow backwards from a read, and stops each path
// at the first store to the address it looks for.
type storeWalk struct {
	at        place          // The address, in a variable or pointer parameter
	positions instrPositions // Where each instruction is in its block
	out       *storeReaching
	// copies holds the loads that copied walks have started from, with the
	// path they look for, shared by every walk that copied starts.
	copies map[storeCopy]bool
	// picked, when not nil, is the Phi the address is read through, and the
	// variable of root it points to. The walk takes only the paths on which
	// the Phi points to it.
	picked *storePick
}

// storeCopy is a load that a copied walk starts from, and the selections it
// looks for from the aggregate loaded.
type storeCopy struct {
	load *ssa.UnOp
	at   placeKey
}

// walkBefore walks every path that leads to instr, from the instruction
// before it.
//
// When the walk has a picked variable, each path also holds the Phi that must
// point to it there, until the path has passed the edge that takes the
// variable itself. At the top of the block of that Phi, the walk
// goes on through the edges that may hold the variable only.
func (w *storeWalk) walkBefore(instr ssa.Instruction) {
	atEntry := func() bool {
		w.reachEntry()
		return true
	}
	if w.picked == nil {
		walkBack([]walkPoint{walkPointBefore(instr, w.positions)}, w.stepAt, atEntry, nil)
		return
	}
	from := []walkFrom[*ssa.Phi]{{walkPointBefore(instr, w.positions), w.picked.phi}}
	edge := func(b *ssa.BasicBlock, i int, need *ssa.Phi) (*ssa.Phi, bool) {
		if need == nil || need.Block() != b {
			return need, true
		}
		return w.pickedThrough(need.Edges[i]) // The Phi may not point to the variable on this path
	}
	walkBackWith(from, w.stepAt, atEntry, edge)
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
func (w *storeWalk) reachEntry() {
	if fv, ok := w.at.root.(*ssa.FreeVar); ok {
		w.out.addCaptured(place{root: fv, path: slices.Clone(w.at.path)})
		return
	}
	if p, ok := storeIsFuncLiteralParam(w.at.root); ok {
		w.out.params = append(w.out.params, storeParamPath{param: p, path: slices.Clone(w.at.path), pointer: true})
		return
	}
	w.out.unknown = true
}

// stepAt stops a path at a store to the address, or to an aggregate that
// holds it, and at the declaration of the local variable. It records what
// the store carries.
func (w *storeWalk) stepAt(instr ssa.Instruction) walkStep {
	switch in := instr.(type) {
	case *ssa.Alloc:
		if in == w.at.root {
			return walkStop // Its zero value has nothing to trace
		}
	case *ssa.Store:
		n, ok := w.prefixLen(in.Addr)
		if !ok {
			return walkOn
		}
		if n == len(w.at.path) {
			w.out.values = append(w.out.values, in.Val)
		} else {
			w.copied(in, w.at.path[n:])
		}
		return walkStop
	}
	return walkOn
}

// prefixLen reports whether addr is the root or a selection from it that the
// path of the walk starts with, and how many selections it takes.
func (w *storeWalk) prefixLen(addr ssa.Value) (int, bool) {
	at, _ := placeOf(addr)
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
	load, ok := asLoad(store.Val)
	if !ok {
		w.out.unknown = true
		return
	}
	from, tracked := placeOf(load.X)
	if !tracked {
		w.out.unknown = true
		return
	}
	src := &storeWalk{
		at:        from.with(rest...),
		positions: w.positions,
		out:       w.out,
		copies:    w.copies,
	}
	// An aggregate copied back and forth in a loop leads back to a copy
	// already followed, which adds nothing new.
	if src.copies == nil {
		src.copies = make(map[storeCopy]bool)
		w.copies = src.copies
	}
	key := storeCopy{load: load, at: src.at.key()}
	if src.copies[key] {
		return
	}
	src.copies[key] = true
	src.walkBefore(load)
}

// addStoresAfter adds the values stored to the address by the stores that
// can run after instr.
func (w *storeWalk) addStoresAfter(instr ssa.Instruction) {
	after := w.positions.after(instr)
	for other := range instrsIn(instr.Parent()) {
		store, ok := other.(*ssa.Store)
		if !ok || !after(store) {
			continue
		}
		if n, ok := w.prefixLen(store.Addr); ok {
			if n == len(w.at.path) {
				w.out.values = append(w.out.values, store.Val)
			} else {
				w.out.unknown = true
			}
		}
	}
}

// storeReadsOf returns the instructions that read addr, whose place is at,
// or an address with the same place: the loads of it, apart from those
// that only feed a store to it, and the calls it is passed to, apart from the
// Logger.UpdateContext calls on it. A store whose value loads from addr is an
// update, and such a load is how it takes the value before it.
//
// escapes are the other instructions that use addr, apart from the stores to
// it. Each passes addr on, as a closure that captures it or a variable it is
// stored to does, to be read later.
func storeReadsOf(addr ssa.Value, at place) (reads, escapes []ssa.Instruction) {
	matches := func(v ssa.Value) bool {
		p, _ := placeOf(v)
		return p.same(at)
	}
	var refs []ssa.Instruction
	if addr == at.root {
		refs = append(refs, *addr.Referrers()...) // A local variable or a parameter always has them
	}
	feeding := make(map[ssa.Instruction]bool)
	for instr := range instrsIn(parentFunc(addr)) {
		if v, isValue := instr.(ssa.Value); isValue && v != at.root && matches(v) {
			refs = append(refs, *v.Referrers()...)
		}
		if store, ok := instr.(*ssa.Store); ok && matches(store.Addr) {
			recordLoad := func(v ssa.Value) bool {
				if load, ok := asLoad(v); ok && matches(load.X) {
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
			if c, ok := calleeOf(&r.Call); !ok || c.api.Kind != zerologapi.KindInPlaceUpdate || !matches(c.recv) {
				reads = append(reads, r)
			}
		default:
			escapes = append(escapes, ref)
		}
	}
	return reads, escapes
}

// findStores finds the plain stores to addr, carrying the set of addresses
// already resolved so that following aggregate copies terminates. A store
// whose value loads from addr is an update, which is built from the plain
// stores, and is left out.
func findStores(addr ssa.Value, visited map[ssa.Value]bool) []*ssa.Store {
	if visited[addr] {
		return nil
	}
	visited[addr] = true

	fn := parentFunc(addr)
	if fn == nil {
		return nil
	}

	var plain []*ssa.Store
	for instr := range instrsIn(fn) {
		store, ok := instr.(*ssa.Store)
		if ok && addressesMatch(store.Addr, addr) && !valueLoadsFromMatchingAddress(store.Val, addr) {
			plain = append(plain, store)
		}
	}
	if len(plain) > 0 {
		return plain
	}

	return findStoresThroughAggregateCopy(fn, addr, visited)
}

// findStoresThroughAggregateCopy resolves stores hidden behind a whole-aggregate
// copy, as storeWalk.copied does for a local variable.
func findStoresThroughAggregateCopy(fn *ssa.Function, addr ssa.Value, visited map[ssa.Value]bool) []*ssa.Store {
	at, _ := placeOf(addr)
	if len(at.path) == 0 {
		return nil
	}

	var plain []*ssa.Store
	for src := range copySourcesOfAggregate(fn, at.root) {
		for _, equivalent := range resolveAggregatePath(fn, src, at.path) {
			plain = append(plain, findStores(equivalent, visited)...)
		}
	}
	return plain
}
