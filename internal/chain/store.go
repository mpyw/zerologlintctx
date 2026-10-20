// store.go finds what can be at an address when it is read. storesReaching is
// the entry; it leans on aggregate.go to follow copies and on match.go to
// decide whether two addresses are the same.

package chain

import (
	"fmt"
	"go/token"
	"slices"
	"strings"

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
// closure runs, which is in captured.
//
// For any other address, such as a global, ok is true when the function
// stores to it, and holds every value it stores there, wherever it runs.
//
// positions finds where each instruction is in its block, and may be kept
// across calls.
//
//declscope:shared // tracing.go traces what reaches each read
func storesReaching(
	addr ssa.Value,
	read ssa.Instruction,
	positions instrPositions,
) (r storeReaching, ok bool) {
	root, path := aggregatePath(addr)
	switch root.(type) {
	case *ssa.Alloc, *ssa.Parameter, *ssa.FreeVar:
	default:
		plain := findStores(addr, make(map[ssa.Value]bool))
		for _, store := range plain {
			r.values = append(r.values, store.Val)
		}
		return r, len(plain) > 0
	}

	w := &storeWalk{root: root, path: path, positions: positions, out: &r}
	if read != nil {
		w.walkBefore(read)
		return r, true
	}
	reads, escapes := storeReadsOf(addr, root, path)
	for _, rd := range reads {
		w.walkBefore(rd)
	}
	for _, esc := range escapes {
		w.walkEscape(esc)
	}
	return r, true
}

// storesReachingClosure finds the values that can be at path, selections from
// v, when the closure mc makes reads it, as storesReaching does for an escape.
// v is a variable that mc captures: a local variable, or a variable that the
// function making mc captures in turn.
//
//declscope:shared // tracing.go traces what a closure reads of what it captures
func storesReachingClosure(
	v ssa.Value,
	path []ssa.Value,
	mc *ssa.MakeClosure,
	positions instrPositions,
) storeReaching {
	var r storeReaching
	w := &storeWalk{root: v, path: path, positions: positions, out: &r}
	w.walkEscape(mc)
	return r
}

// walkEscape walks to esc, an instruction that passes the address on, such as
// a closure that captures it. What it passes the address on to may read it at
// any point after, so what reaches esc, and every store after it, may be what
// it reads. A closure that is only deferred reads it at the returns after it,
// unless the variable is declared in a loop.
func (w *storeWalk) walkEscape(esc ssa.Instruction) {
	if rets, deferred := storeDeferredReturns(esc, w.positions); deferred && !w.declaredInLoop() {
		for _, ret := range rets {
			w.walkBefore(ret)
		}
		return
	}
	w.walkBefore(esc)
	w.addStoresAfter(esc)
}

// declaredInLoop reports whether the root of the walk is a local variable
// declared in a loop. Each iteration has a variable of its own, and a store
// on a later one, which a walk back from a return meets first, is to another
// variable than the one a closure deferred earlier captures.
func (w *storeWalk) declaredInLoop() bool {
	alloc, ok := w.root.(*ssa.Alloc)
	return ok && w.positions.reaches(alloc, alloc)
}

// storeDeferredReturns reports whether esc is a closure whose only uses are
// defer statements that call it, and returns the returns that can run after
// one of them, where the closure runs. The recover block, reached after a
// panic, is not among them.
//
//	defer func() { l.Info().Msg("x") }() // reads l at each return after it
func storeDeferredReturns(esc ssa.Instruction, positions instrPositions) (rets []ssa.Instruction, ok bool) {
	mc, ok := esc.(*ssa.MakeClosure)
	if !ok {
		return nil, false
	}
	var defers []*ssa.Defer
	for _, ref := range *mc.Referrers() {
		d, isDefer := ref.(*ssa.Defer)
		if !isDefer || d.Call.Value != mc {
			return nil, false
		}
		defers = append(defers, d)
	}
	fn := mc.Parent()
	for _, b := range fn.Blocks {
		ret, isReturn := b.Instrs[len(b.Instrs)-1].(*ssa.Return)
		if !isReturn || b == fn.Recover {
			continue
		}
		if slices.ContainsFunc(defers, func(d *ssa.Defer) bool { return positions.reaches(d, ret) }) {
			rets = append(rets, ret)
		}
	}
	return rets, true
}

// storeReaching is what storesReaching found can be at an address when it is
// read.
//
//declscope:shared // tracing.go traces each part
type storeReaching struct {
	// values are the values stored. Every one must have context.
	values []ssa.Value
	// unknown reports that some path carries a value the function cannot
	// tell: one the caller passed through a pointer parameter, or one an
	// aggregate was copied whole from where no store can be seen.
	unknown bool
	// captured is the variable a closure captures, when some path carries
	// the value at capturedPath in it as the closure starts. Tracing it finds
	// what is at the same selections of the variable bound where the closure
	// is made.
	captured     *ssa.FreeVar
	capturedPath []ssa.Value
	// params are the parameters of a func literal, with the selections from
	// them, whose values some path carries from the entry of the func literal.
	// Tracing them finds what the calls of the func literal pass.
	params []storeParamPath
}

// storeParamPath is the value at path, selections from a parameter of a func
// literal: from what the parameter points to when pointer is true, or from
// the parameter itself, a struct or array, otherwise.
//
//declscope:shared // tracing.go traces it in each call of the func literal
type storeParamPath struct {
	param   *ssa.Parameter
	path    []ssa.Value
	pointer bool
}

// storeIsFuncLiteralParam reports whether v is a parameter of a func literal.
func storeIsFuncLiteralParam(v ssa.Value) (*ssa.Parameter, bool) {
	p, ok := v.(*ssa.Parameter)
	return p, ok && p.Parent().Parent() != nil
}

// storesReachingAt finds the values that can be at the address that path
// selects from root when read runs, as storesReaching does for a read it is
// given. ok is false when root is not a local variable, a pointer parameter
// or a captured variable.
//
//declscope:shared // tracing.go walks from a call of a func literal
func storesReachingAt(
	root ssa.Value,
	path []ssa.Value,
	read ssa.Instruction,
	positions instrPositions,
) (r storeReaching, ok bool) {
	switch root.(type) {
	case *ssa.Alloc, *ssa.Parameter, *ssa.FreeVar:
	default:
		return r, false
	}
	w := &storeWalk{root: root, path: path, positions: positions, out: &r}
	w.walkBefore(read)
	return r, true
}

// storeWalk walks the control flow backwards from a read, and stops each path
// at the first store to the address it looks for.
type storeWalk struct {
	root      ssa.Value      // The variable or pointer parameter the address is in
	path      []ssa.Value    // The selections from root to the address, outermost first
	positions instrPositions // Where each instruction is in its block
	out       *storeReaching
	// copies holds the loads that copied walks have started from, with the
	// path they look for, shared by every walk that copied starts.
	copies map[storeCopy]bool
}

// storeCopy is a load that a copied walk starts from, and the selections it
// looks for from the aggregate loaded.
type storeCopy struct {
	load *ssa.UnOp
	path string
}

// walkBefore walks every path that leads to instr, from the instruction
// before it.
func (w *storeWalk) walkBefore(instr ssa.Instruction) {
	type step struct {
		block *ssa.BasicBlock
		from  int // Index of the last instruction of block to look at
	}
	b := instr.Block()
	queue := []step{{b, w.positions.of(instr) - 1}}
	seen := make(map[*ssa.BasicBlock]bool)
	for len(queue) > 0 {
		s := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if w.stopsInBlock(s.block, s.from) {
			continue
		}
		if len(s.block.Preds) == 0 {
			w.reachEntry()
			continue
		}
		for _, pred := range s.block.Preds {
			if !seen[pred] {
				seen[pred] = true
				queue = append(queue, step{pred, len(pred.Instrs) - 1})
			}
		}
	}
}

// reachEntry records a path that leads back to the entry of the function, or
// to its recover block. What is at a pointer parameter there is the caller's,
// which is known only for a func literal, from its calls. What is at a
// captured variable is the one the closure was made with.
//
// A local variable is declared before any read. A walk from a return, where a
// deferred closure runs, can get here past a block that declares it:
//
//	if cond {
//	    l := logger.With().Ctx(ctx).Logger()
//	    defer func() { l.Info().Msg("x") }()
//	}
//	return                  // the path that skips the if has no l
//
// On such a path the closure was never deferred, and it carries nothing.
func (w *storeWalk) reachEntry() {
	if _, ok := w.root.(*ssa.Alloc); ok {
		return
	}
	if fv, ok := w.root.(*ssa.FreeVar); ok {
		w.out.captured, w.out.capturedPath = fv, slices.Clone(w.path)
		return
	}
	if p, ok := storeIsFuncLiteralParam(w.root); ok {
		w.out.params = append(w.out.params, storeParamPath{param: p, path: slices.Clone(w.path), pointer: true})
		return
	}
	w.out.unknown = true
}

// stopsInBlock looks at the instructions of b from index from back to its
// first, and reports whether one of them ends the path.
func (w *storeWalk) stopsInBlock(b *ssa.BasicBlock, from int) bool {
	for i := from; i >= 0; i-- {
		if w.stopsAt(b.Instrs[i]) {
			return true
		}
	}
	return false
}

// stopsAt reports whether instr ends a path: a store to the address, or to an
// aggregate that holds it, or the declaration of the local variable.
func (w *storeWalk) stopsAt(instr ssa.Instruction) bool {
	switch in := instr.(type) {
	case *ssa.Alloc:
		return in == w.root // Its zero value has nothing to trace
	case *ssa.Store:
		n, ok := w.prefixLen(in.Addr)
		if !ok {
			return false
		}
		if n == len(w.path) {
			w.out.values = append(w.out.values, in.Val)
		} else {
			w.copied(in, w.path[n:])
		}
		return true
	}
	return false
}

// prefixLen reports whether addr is the root or a selection from it that the
// path of the walk starts with, and how many selections it takes.
func (w *storeWalk) prefixLen(addr ssa.Value) (int, bool) {
	root, path := aggregatePath(addr)
	if root != w.root || len(path) > len(w.path) {
		return 0, false
	}
	for i, step := range path {
		if !storeSelectsSame(step, w.path[i]) {
			return 0, false
		}
	}
	return len(path), true
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
func (w *storeWalk) copied(store *ssa.Store, rest []ssa.Value) {
	if _, isConst := store.Val.(*ssa.Const); isConst {
		return // The zero value of the aggregate has nothing to trace
	}
	if p, ok := storeIsFuncLiteralParam(store.Val); ok {
		w.out.params = append(w.out.params, storeParamPath{param: p, path: slices.Clone(rest)})
		return
	}
	load, ok := store.Val.(*ssa.UnOp)
	if !ok || load.Op != token.MUL {
		w.out.unknown = true
		return
	}
	root, path := aggregatePath(load.X)
	switch root.(type) {
	case *ssa.Alloc, *ssa.Parameter:
	default:
		w.out.unknown = true
		return
	}
	src := &storeWalk{
		root:      root,
		path:      append(slices.Clone(path), rest...),
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
	key := storeCopy{load: load, path: storePathKey(src.path)}
	if src.copies[key] {
		return
	}
	src.copies[key] = true
	src.walkBefore(load)
}

// storePathKey spells the selections of path, which storeSelectsSame
// compares, so that two paths that select the same have the same key.
func storePathKey(path []ssa.Value) string {
	var b strings.Builder
	for _, step := range path {
		field, index := storeSelection(step)
		if index == nil {
			fmt.Fprintf(&b, ".%d", field)
			continue
		}
		// A constant is named by its value, and any other index by the value
		// it is, which only matches itself
		fmt.Fprintf(&b, "[%s]", index.Name())
	}
	return b.String()
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
			if n == len(w.path) {
				w.out.values = append(w.out.values, store.Val)
			} else {
				w.out.unknown = true
			}
		}
	}
}

// storeReadsOf returns the instructions that read addr, whose root and path
// are given, or an address that matches it: the loads of it, apart from those
// that only feed a store to it, and the calls it is passed to, apart from the
// Logger.UpdateContext calls on it. A store whose value loads from addr is an
// update, and such a load is how it takes the value before it.
//
// escapes are the other instructions that use addr, apart from the stores to
// it. Each passes addr on, as a closure that captures it or a variable it is
// stored to does, to be read later.
func storeReadsOf(addr, root ssa.Value, path []ssa.Value) (reads, escapes []ssa.Instruction) {
	matches := func(v ssa.Value) bool {
		r, p := aggregatePath(v)
		return r == root && len(p) == len(path) && storePathsMatch(p, path)
	}
	var refs []ssa.Instruction
	if addr == root {
		refs = append(refs, *addr.Referrers()...) // A local variable or a parameter always has them
	}
	feeding := make(map[ssa.Instruction]bool)
	for instr := range instrsIn(parentFunc(addr)) {
		if v, isValue := instr.(ssa.Value); isValue && v != root && matches(v) {
			refs = append(refs, *v.Referrers()...)
		}
		if store, ok := instr.(*ssa.Store); ok && matches(store.Addr) {
			recordLoad := func(v ssa.Value) bool {
				if unop, ok := v.(*ssa.UnOp); ok && unop.Op == token.MUL && matches(unop.X) {
					feeding[unop] = true
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
			if c, ok := calleeOf(&r.Call); !ok || !c.updatesLoggerContext() || !matches(c.recv) {
				reads = append(reads, r)
			}
		default:
			escapes = append(escapes, ref)
		}
	}
	return reads, escapes
}

// storePathsMatch reports whether two paths of the same length select the same
// fields and constant elements.
func storePathsMatch(a, b []ssa.Value) bool {
	for i := range a {
		if !storeSelectsSame(a[i], b[i]) {
			return false
		}
	}
	return true
}

// storeSelectsSame reports whether two selections pick the same field, or the
// same constant element, each from its own aggregate.
func storeSelectsSame(a, b ssa.Value) bool {
	fa, ia := storeSelection(a)
	fb, ib := storeSelection(b)
	if ia == nil || ib == nil {
		return ia == nil && ib == nil && fa == fb
	}
	return constIndexesMatch(ia, ib)
}

// storeSelection returns what a selection in a path picks: the field, for a
// field of an address or of a struct value, or the index, nil for a field, for
// an element of an address or of an array value. A path holds no other value.
func storeSelection(step ssa.Value) (field int, index ssa.Value) {
	switch s := step.(type) {
	case *ssa.FieldAddr:
		return s.Field, nil
	case *ssa.Field:
		return s.Field, nil
	case *ssa.IndexAddr:
		return 0, s.Index
	}
	return 0, step.(*ssa.Index).Index
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
	root, path := aggregatePath(addr)
	if len(path) == 0 {
		return nil
	}

	var plain []*ssa.Store
	for src := range copySourcesOfAggregate(fn, root) {
		for _, equivalent := range resolveAggregatePath(fn, src, path) {
			plain = append(plain, findStores(equivalent, visited)...)
		}
	}
	return plain
}
