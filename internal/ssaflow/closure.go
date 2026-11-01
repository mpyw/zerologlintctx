// closure.go indexes the closures and func literals of a package: where each
// is made, what each free variable is bound to, the variables that alias a
// local variable through the closures that capture it, and how each func
// literal is run. internal/chain asks it through its tracer. store.go asks only
// how a closure is run where it is made, which needs no index.

package ssaflow

import (
	"go/token"
	"go/types"
	"iter"
	"slices"

	"golang.org/x/tools/go/ssa"
)

// Closures indexes the Closures and func literals of a package. It indexes
// the tree of an outermost function, the function and the func literals
// declared in it at any depth, the first time a query reaches it, and keeps
// the answer to each query. SSA does not change while a package is analyzed,
// so an answer never goes stale.
type Closures struct {
	// indexed holds the outermost functions whose trees are indexed.
	indexed map[*ssa.Function]bool
	// makers holds, for each func literal, the MakeClosure instructions in
	// the function it is declared in, in block order.
	makers map[*ssa.Function][]*ssa.MakeClosure
	// captures holds, for each value a MakeClosure binds, the free variables
	// it is bound to.
	captures map[ssa.Value][]*ssa.FreeVar
	// aliases, calls and held keep the answers of aliasesOf, CallsOf and
	// funcsHeldBy.
	aliases map[ssa.Value]map[ssa.Value]bool
	calls   map[*ssa.Function]closureCalls
	held    map[*ssa.Alloc]closureHeld
}

// NewClosures creates an index with nothing indexed yet.
func NewClosures() *Closures {
	return &Closures{
		indexed:  make(map[*ssa.Function]bool),
		makers:   make(map[*ssa.Function][]*ssa.MakeClosure),
		captures: make(map[ssa.Value][]*ssa.FreeVar),
		aliases:  make(map[ssa.Value]map[ssa.Value]bool),
		calls:    make(map[*ssa.Function]closureCalls),
		held:     make(map[*ssa.Alloc]closureHeld),
	}
}

// makersOf returns the MakeClosure instructions that make fn, a func literal
// that captures variables, in the function it is declared in, in block order.
// It is nil for a declared function, or a wrapper such as a method value,
// which has no function it is declared in.
//
// SSA makes a func literal once, where it is written, so there is at most one.
// bindingOf takes the last, and the resolver of a free variable takes each,
// every one of which must give a value with context. The two rules agree as
// long as that holds.
//
//declscope:shared // resolve.go resolves a free variable from where it is bound
func (c *Closures) makersOf(fn *ssa.Function) []*ssa.MakeClosure {
	if fn.Parent() == nil {
		return nil
	}
	c.index(fn)
	return c.makers[fn]
}

// CallsOf finds every call, go and defer statement that may run fn, a func
// literal. ok is false when fn is a declared function, or may be called where
// it cannot be seen.
//
// A func literal can only be used inside the function it is written in, and
// the closures declared there. It may be kept in a variable on the way, and a
// closure that calls it captures that variable:
//
//	t2 = new func(e *Event) (logIt)  // a variable that may hold fn
//	*t2 = f$1
//	t3 = *t2                         // may be fn
//	t4 = t3(t6)                      // a call that may run fn
//	t8 = make closure f$2 [t2]       // logIt is a free variable of f$2
//
// A value that may be fn is fn, a closure of it, a load of such a variable,
// or a Phi with such an edge. It may only be called, stored to a local
// variable, or merged in a Phi. Such a variable may only be stored to,
// loaded, or captured. Any other use passes fn on, to be called with anything.
// A call that may run another func too still counts: every call that may
// run fn must pass an argument with context.
func (c *Closures) CallsOf(fn *ssa.Function) (calls []ssa.CallInstruction, ok bool) {
	if found, done := c.calls[fn]; done {
		return found.calls, found.ok
	}
	calls, ok = c.findCalls(fn)
	c.calls[fn] = closureCalls{calls: calls, ok: ok}
	return calls, ok
}

// findCalls does the work of CallsOf.
func (c *Closures) findCalls(fn *ssa.Function) (calls []ssa.CallInstruction, ok bool) {
	parent := fn.Parent()
	if parent == nil {
		return nil, false // A declared function can be called from anywhere
	}
	vars := make(map[ssa.Value]bool) // The variables that may hold fn, and their aliases
	phis := make(map[ssa.Value]bool) // The Phis that may be fn
	mayBeFn := func(v ssa.Value) bool {
		if load, ok := AsLoad(v); ok {
			return vars[load.X]
		}
		return phis[v] || isClosureOf(v, fn)
	}
	for changed := true; changed; {
		changed = false
		for instr := range instrsWithClosures(parent) {
			switch in := instr.(type) {
			case *ssa.Store:
				if alloc, isLocal := in.Addr.(*ssa.Alloc); isLocal && mayBeFn(in.Val) && !vars[alloc] {
					for alias := range c.aliasesOf(alloc) {
						vars[alias] = true
					}
					changed = true
				}
			case *ssa.Phi:
				if !phis[in] && slices.ContainsFunc(LiveEdgeValues(in), mayBeFn) {
					phis[in], changed = true, true
				}
			}
		}
	}
	for instr := range instrsWithClosures(parent) {
		if mc, ok := instr.(*ssa.MakeClosure); ok && mc.Fn == fn {
			continue // Where the closure is made
		}
		for _, op := range instr.Operands(nil) {
			switch {
			case mayBeFn(*op):
				if !keepsClosureValue(instr, op, vars) {
					return nil, false // The func is passed on, and may be called where it cannot be seen
				}
				if call, isCall := instr.(ssa.CallInstruction); isCall {
					calls = append(calls, call)
				}
			case vars[*op]:
				if !keepsClosureVar(instr) {
					return nil, false
				}
			}
		}
	}
	return calls, true
}

// FuncLiteralsOf returns the func literals that v can be, when it is a func
// literal or a closure of one, a Phi of such values, or a load of a variable
// that only such values are stored to (see funcsHeldBy):
//
//	ev := func() *zerolog.Event { return logger.Info().Ctx(ctx) }
//	if debug {
//	    ev = func() *zerolog.Event { return logger.Debug().Ctx(ctx) }
//	}
//	ev().Msg("x")           // ev is a Phi of the two
func (c *Closures) FuncLiteralsOf(v ssa.Value, seen map[ssa.Value]bool) ([]*ssa.Function, bool) {
	if mc, ok := v.(*ssa.MakeClosure); ok {
		v = mc.Fn
	}
	if fn, ok := v.(*ssa.Function); ok {
		return []*ssa.Function{fn}, fn.Parent() != nil
	}
	if seen[v] {
		return nil, true // A loop, which adds nothing new
	}
	seen[v] = true
	var values []ssa.Value
	switch val := v.(type) {
	case *ssa.Phi:
		values = LiveEdgeValues(val)
	case *ssa.UnOp:
		if val.Op != token.MUL {
			return nil, false
		}
		stored, ok := c.funcsHeldBy(val.X)
		if !ok {
			return nil, false
		}
		values = stored
	default:
		return nil, false
	}
	var fns []*ssa.Function
	for _, value := range values {
		more, ok := c.FuncLiteralsOf(value, seen)
		if !ok {
			return nil, false
		}
		fns = append(fns, more...)
	}
	return fns, true
}

// funcsHeldBy returns the values stored to v, when it is a local variable, or
// one a closure captures, whose address is only stored to, loaded and
// captured, in the function that declares it and the closures declared there:
//
//	f := func() *zerolog.Event { return logger.Warn() }
//	func() { _ = f }()      // f is kept in memory, and loaded to be called
//	f().Msg("x")
func (c *Closures) funcsHeldBy(v ssa.Value) ([]ssa.Value, bool) {
	for fv, ok := v.(*ssa.FreeVar); ok; fv, ok = v.(*ssa.FreeVar) {
		v = c.bindingOf(fv)
	}
	alloc, ok := v.(*ssa.Alloc)
	if !ok {
		return nil, false // Not a local variable, or its binding is not found
	}
	if found, done := c.held[alloc]; done {
		return found.values, found.ok
	}
	values, ok := c.findHeld(alloc)
	c.held[alloc] = closureHeld{values: values, ok: ok}
	return values, ok
}

// findHeld does the work of funcsHeldBy for alloc.
func (c *Closures) findHeld(alloc *ssa.Alloc) ([]ssa.Value, bool) {
	aliases := c.aliasesOf(alloc)
	var stored []ssa.Value
	for instr := range instrsWithClosures(alloc.Parent()) {
		for _, op := range instr.Operands(nil) {
			if !aliases[*op] {
				continue
			}
			switch in := instr.(type) {
			case *ssa.Store:
				if op != &in.Addr {
					return nil, false // The address itself is stored
				}
				stored = append(stored, in.Val)
			case *ssa.UnOp, *ssa.MakeClosure:
				// A load of the variable, or a capture of it
			default:
				return nil, false // The address is passed on
			}
		}
	}
	return stored, true
}

// boundTo returns v, or the value that v, a free variable, stands for: the
// value bound where its closure is made, at any depth. It is nil when that is
// not found.
func (c *Closures) boundTo(v ssa.Value) ssa.Value {
	for fv, ok := v.(*ssa.FreeVar); ok; fv, ok = v.(*ssa.FreeVar) {
		v = c.bindingOf(fv)
	}
	return v
}

// bindingOf returns the value bound to fv where its closure is made, or nil
// when that is not found. With more than one MakeClosure, it takes the last
// (see makersOf).
func (c *Closures) bindingOf(fv *ssa.FreeVar) ssa.Value {
	fn := fv.Parent()
	makers := c.makersOf(fn)
	if len(makers) == 0 {
		return nil
	}
	return makers[len(makers)-1].Bindings[slices.Index(fn.FreeVars, fv)]
}

// CalleeOf resolves the callee of a call as CalleeOf does, and also one
// that a closure loads from a variable it captures, which holds one value
// only. The callee is that value:
//
//	info := l.Info               // *t4 = (*Logger).Info$bound [t0]
//	func() { info().Msg("x") }() // t0 = *info ; t1 = t0()
//	msg := (*Event).Msg
//	func() { msg(e, "x") }()     // msg holds (*Event).Msg$thunk
//
// The receiver of a method value is then what it is bound to, in the
// function that declares the variable, and is read where the closure runs,
// which Read is. So is the struct passed to a method expression of a promoted
// method, when the closure loads it from a variable it captures, or the one
// pointer to it that the variable holds:
//
//	h := outer{&inner{e}}
//	func() { outer.Msg(h, "x") }() // reads h, then h.inner.Event, here
//
// The closure runs at its only call, go or defer statement where it is made,
// or else at any point after the MakeClosure, which stands for them. A method
// expression takes its receiver from the call, which reads it there.
func (c *Closures) CalleeOf(cc *ssa.CallCommon) (Callee, bool) {
	if callee, ok := CalleeOf(cc); ok {
		if callee.Embedded == nil {
			return callee, true
		}
		addr, read := c.capturedRead(callee.Recv)
		if addr == nil {
			return callee, true
		}
		if _, isPtr := callee.Recv.Type().Underlying().(*types.Pointer); !isPtr {
			callee.Recv, callee.Read = addr, read
			return callee, true
		}
		// A pointer: the one value the variable holds, if it is made there
		if held, ok := c.funcsHeldBy(addr); ok && len(held) == 1 && held[0].Parent() == read.Parent() {
			callee.Recv, callee.Read = held[0], read
		}
		return callee, true
	}
	addr, read := c.capturedRead(cc.Value)
	if addr == nil {
		return Callee{}, false
	}
	held, ok := c.funcsHeldBy(addr)
	if !ok || len(held) != 1 {
		return Callee{}, false
	}
	resolved := *cc
	resolved.Value = held[0]
	callee, ok := CalleeOf(&resolved)
	if mc, bound := held[0].(*ssa.MakeClosure); ok && bound {
		if mc.Parent() != read.Parent() {
			return Callee{}, false // Bound in another closure than the one read at
		}
		callee.Read = read
	}
	return callee, ok
}

// capturedRead returns the local variable that v, a load in a closure of a
// variable it captures, reads, at any depth of closures, and where the
// outermost of them runs, which stands for the read: its only call, go or
// defer statement where it is made, or else the MakeClosure. addr is nil
// when v is not such a load, or what the variable is bound to is not found.
func (c *Closures) capturedRead(v ssa.Value) (addr ssa.Value, read ssa.Instruction) {
	load, ok := AsLoad(v)
	if !ok {
		return nil, nil
	}
	v = load.X
	var mc *ssa.MakeClosure
	for fv, ok := v.(*ssa.FreeVar); ok; fv, ok = v.(*ssa.FreeVar) {
		fn := fv.Parent()
		makers := c.makersOf(fn)
		if len(makers) == 0 {
			return nil, nil
		}
		mc = makers[len(makers)-1] // See makersOf
		v = mc.Bindings[slices.Index(fn.FreeVars, fv)]
	}
	if _, isAlloc := v.(*ssa.Alloc); !isAlloc || mc == nil {
		return nil, nil
	}
	if runs, ok := closureRunsOf(mc); ok && len(runs) == 1 {
		return v, runs[0]
	}
	return v, mc
}

// writtenUnseen reports whether what is at at, in a local variable, a pointer
// parameter or a variable a closure captures, may be written where a walk of
// the control flow of walked does not see it: by a closure that is neither
// walked nor one walked is declared in, or through an address of it, or of a
// field or element of it, that is passed on in any function, such as to a
// call, other than via. A free variable stands for the variable bound where
// its closure is made, at any depth. One bound to anything else, such as the
// address of a field of another variable, may be written anywhere.
//
// When followed, the walk followed the pointers read from memory (see
// storePointers), and the root of at may also be a pointer loaded from a
// local variable that holds only pointers to such variables, as a pointer
// parameter that a closure captures is (see closureHolders). Each of them is asked of then, and
// so is each pointer loaded from the variable, which is the same address:
//
//	*pc = context.Background()       // t1 = new *Context ; *t1 = pc ; t2 = *t1 ; *t2 = ...
//	func() { e.Ctx(*pc).Msg("x") }() // t0 = *pc ; Ctx(*t0): t0 is pc
//
// The stores of a function that walked is declared in reach it as the
// closure starts, which a walk of that function finds (see
// storesReachingClosure). via, when not nil, is the call of a func literal
// through whose parameter the address is read, whose stores the walk of the
// func literal sees.
//
//declscope:shared // resolve.go finds the variables whose stores a walk cannot all see
func (c *Closures) writtenUnseen(at Place, walked *ssa.Function, via ssa.Instruction, followed bool) bool {
	h := closureHolders{c: c, follow: followed, holders: make(map[ssa.Value]bool), stores: make(map[ssa.Instruction]bool)}
	if !h.add(at.root) {
		return true
	}
	for store := range h.stores {
		if !closureEncloses(store.Parent(), walked) {
			return true // Another pointer put in a holder unseen
		}
	}
	writes := func(p Place, fn *ssa.Function) bool {
		other := !closureEncloses(fn, walked)
		for instr := range InstrsIn(fn) {
			if instr != via && !h.stores[instr] && closureWritesUnseen(instr, p, other) {
				return true
			}
		}
		return false
	}
	for _, root := range h.roots {
		for alias := range c.aliasesOf(root.root) {
			fn := parentFunc(alias)
			if fv, isFreeVar := alias.(*ssa.FreeVar); isFreeVar {
				fn = fv.Parent()
			}
			if writes(Place{root: alias, path: append(slices.Clip(root.path), at.path...)}, fn) {
				return true
			}
		}
	}
	for _, load := range h.loads {
		if writes(Place{root: load, path: at.path}, load.Parent()) {
			return true
		}
	}
	return false
}

// closureHolders gathers the variables that the root of a place for
// writtenUnseen can be: roots, each a local variable or a parameter, or a
// field or element of one. A root that is a pointer loaded from a local
// variable is each pointer the variable holds, as funcsHeldBy finds them. The variable is then a holder,
// stores holds its stores, and loads holds each load of it, in its function
// and in the closures that capture it. A store to a holder in a closure that
// a walk does not see may put another pointer there. A nil pointer has no variable to
// write, and is left out.
type closureHolders struct {
	c *Closures
	// follow reports that the walk followed the pointers read from memory to
	// what they point to. Without it, a pointer loaded is not followed.
	follow  bool
	roots   []Place
	holders map[ssa.Value]bool
	stores  map[ssa.Instruction]bool
	loads   []*ssa.UnOp
}

// add adds the variables that v can be, and reports whether each of them is
// one that writtenUnseen can tell of.
func (h *closureHolders) add(v ssa.Value) bool {
	at, _ := PlaceOf(v)
	if _, isFreeVar := v.(*ssa.FreeVar); isFreeVar {
		at = Place{root: v} // What it is bound to, even the address of a field
	}
	at.root = h.c.boundTo(at.root)
	switch at.root.(type) {
	case *ssa.Alloc, *ssa.Parameter:
		h.roots = append(h.roots, at)
		return true
	}
	load, isLoad := AsLoad(at.root)
	if !isLoad || len(at.path) > 0 || !h.follow {
		return false // Bound to an address in another variable, or not found
	}
	held, ok := h.c.funcsHeldBy(load.X)
	if !ok {
		return false
	}
	holder := h.c.boundTo(load.X)
	if h.holders[holder] {
		return true // Its pointers are added already
	}
	h.holders[holder] = true
	for alias := range h.c.aliasesOf(holder) {
		for _, ref := range *alias.Referrers() {
			switch in := ref.(type) {
			case *ssa.Store:
				h.stores[in] = true
			case *ssa.UnOp:
				h.loads = append(h.loads, in)
			}
		}
	}
	for _, v := range held {
		if !IsNilConst(v) && !h.add(v) {
			return false
		}
	}
	return true
}

// aliasesOf returns v, a local variable or a parameter, and the free
// variables that stand for it in the closures that capture it, at any depth.
func (c *Closures) aliasesOf(v ssa.Value) map[ssa.Value]bool {
	if aliases, ok := c.aliases[v]; ok {
		return aliases
	}
	c.index(parentFunc(v))
	aliases := map[ssa.Value]bool{v: true}
	queue := []ssa.Value{v}
	for len(queue) > 0 {
		w := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, fv := range c.captures[w] {
			if !aliases[fv] {
				aliases[fv] = true
				queue = append(queue, fv)
			}
		}
	}
	c.aliases[v] = aliases
	return aliases
}

// index indexes the tree of the outermost function fn is in, unless it is
// indexed already.
func (c *Closures) index(fn *ssa.Function) {
	root := fn
	for root.Parent() != nil {
		root = root.Parent()
	}
	if c.indexed[root] {
		return
	}
	c.indexed[root] = true
	for instr := range instrsWithClosures(root) {
		mc, ok := instr.(*ssa.MakeClosure)
		if !ok {
			continue
		}
		closure := mc.Fn.(*ssa.Function) // Always a function, a func literal or a wrapper
		if closure.Parent() != nil && mc.Parent() == closure.Parent() {
			c.makers[closure] = append(c.makers[closure], mc)
		}
		for i, b := range mc.Bindings {
			c.captures[b] = append(c.captures[b], closure.FreeVars[i])
		}
	}
}

// closureEncloses reports whether fn is inner, or a function that inner is
// declared in, at any depth.
func closureEncloses(fn, inner *ssa.Function) bool {
	for f := inner; f != nil; f = f.Parent() {
		if f == fn {
			return true
		}
	}
	return false
}

// closureWritesUnseen reports whether instr uses an address that overlaps
// at in a way that may write to it where a walk does not see: a store to it
// when other is true, as in another closure, or any use but a load, a store
// to it, a selection of a field or element, or a capture by a closure.
func closureWritesUnseen(instr ssa.Instruction, at Place, other bool) bool {
	for _, op := range instr.Operands(nil) {
		if *op == nil {
			continue
		}
		if p, _ := PlaceOf(*op); !p.overlaps(at) {
			continue
		}
		switch in := instr.(type) {
		case *ssa.Store:
			if op != &in.Addr || other {
				return true // A store to it unseen, or of the address itself
			}
		case *ssa.UnOp, *ssa.FieldAddr, *ssa.IndexAddr, *ssa.MakeClosure, *ssa.DebugRef:
			// A load, which an address is the operand of only as such, or a
			// use followed on its own, or by the aliases of the variable
		default:
			return true
		}
	}
	return false
}

// closureRuns are the calls, go and defer statements that run a closure.
type closureRuns []ssa.CallInstruction

// closureRunsOf returns the uses of mc, a closure, where it is made: the
// calls, go and defer statements that run it, in the order of its
// referrers. ok is false when it has another use, which passes it on. Unlike
// CallsOf, it does not follow the closure through a variable or a Phi.
//
//declscope:shared // store.go reads what a closure that is only called or deferred reads
func closureRunsOf(mc *ssa.MakeClosure) (runs closureRuns, ok bool) {
	for _, ref := range *mc.Referrers() {
		call, isCall := ref.(ssa.CallInstruction)
		if !isCall || call.Common().Value != mc {
			return nil, false
		}
		runs = append(runs, call)
	}
	return runs, true
}

// CallsOfClosure returns the calls of mc, a closure, where it is made: the
// calls, not go or defer statements, whose callee is mc itself. Each runs the
// func literal of mc there, and no other func, whatever else mc is used for.
func CallsOfClosure(mc *ssa.MakeClosure) []*ssa.Call {
	var calls []*ssa.Call
	for _, ref := range *mc.Referrers() {
		if call, isCall := ref.(*ssa.Call); isCall && call.Call.Value == mc {
			calls = append(calls, call)
		}
	}
	return calls
}

// callsOnly reports whether every one of runs is a call, not a go or defer
// statement, and returns them. Such a closure runs at each call, and reads
// what reaches it there, as an IIFE does:
//
//	func() { l.Info().Msg("x") }() // reads l here, not after
//	l = other
//
//declscope:shared // store.go walks to each call
func (runs closureRuns) callsOnly() ([]ssa.Instruction, bool) {
	calls := make([]ssa.Instruction, 0, len(runs))
	for _, run := range runs {
		call, isCall := run.(*ssa.Call)
		if !isCall {
			return nil, false
		}
		calls = append(calls, call)
	}
	return calls, true
}

// defersOnly reports whether every one of runs is a defer statement, and
// returns them. The closure runs at the returns after each:
//
//	defer func() { l.Info().Msg("x") }() // reads l at each return after it
//
//declscope:shared // store.go walks to the returns after each
func (runs closureRuns) defersOnly() ([]*ssa.Defer, bool) {
	defers := make([]*ssa.Defer, 0, len(runs))
	for _, run := range runs {
		d, isDefer := run.(*ssa.Defer)
		if !isDefer {
			return nil, false
		}
		defers = append(defers, d)
	}
	return defers, true
}

// closureCalls is what CallsOf found for a func literal.
type closureCalls struct {
	calls []ssa.CallInstruction
	ok    bool
}

// closureHeld is what funcsHeldBy found for a local variable.
type closureHeld struct {
	values []ssa.Value
	ok     bool
}

// isClosureOf reports whether v is fn, or a closure of it. A func literal that
// captures nothing is used as the function itself.
func isClosureOf(v ssa.Value, fn *ssa.Function) bool {
	if mc, ok := v.(*ssa.MakeClosure); ok {
		return mc.Fn == fn
	}
	return v == fn
}

// keepsClosureValue reports whether instr uses op, a value that may be a func
// literal, in a way that keeps it from being called where it cannot be seen:
// a call of it, a store of it to a local variable in vars, or a Phi.
func keepsClosureValue(instr ssa.Instruction, op *ssa.Value, vars map[ssa.Value]bool) bool {
	switch in := instr.(type) {
	case ssa.CallInstruction:
		return op == &in.Common().Value
	case *ssa.Store:
		return op == &in.Val && vars[in.Addr]
	case *ssa.Phi:
		return true
	}
	return false
}

// keepsClosureVar reports whether instr, a use of a variable that may hold a
// func literal, keeps it from being called where it cannot be seen: a store
// to it, a load, or a capture by a closure.
func keepsClosureVar(instr ssa.Instruction) bool {
	switch in := instr.(type) {
	case *ssa.Store, *ssa.MakeClosure:
		return true
	case *ssa.UnOp:
		return in.Op == token.MUL
	}
	return false
}

// instrsWithClosures yields every instruction of fn, and of the func literals
// declared in it, at any depth.
func instrsWithClosures(fn *ssa.Function) iter.Seq[ssa.Instruction] {
	return func(yield func(ssa.Instruction) bool) {
		for instr := range InstrsIn(fn) {
			if !yield(instr) {
				return
			}
		}
		for _, anon := range fn.AnonFuncs {
			for instr := range instrsWithClosures(anon) {
				if !yield(instr) {
					return
				}
			}
		}
	}
}
