// closure.go indexes the closures and func literals of a package: where each
// is made, what each free variable is bound to, the variables that alias a
// local variable through the closures that capture it, and how each func
// literal is run. tracing.go asks it through the tracer. store.go asks only
// how a closure is run where it is made, which needs no index.

package chain

import (
	"go/token"
	"iter"
	"slices"

	"golang.org/x/tools/go/ssa"
)

// closures indexes the closures and func literals of a package. It indexes
// the tree of an outermost function, the function and the func literals
// declared in it at any depth, the first time a query reaches it, and keeps
// the answer to each query. SSA does not change while a package is analyzed,
// so an answer never goes stale.
//
//declscope:shared // tracing.go keeps one in its tracer
type closures struct {
	// indexed holds the outermost functions whose trees are indexed.
	//
	//declscope:private
	indexed map[*ssa.Function]bool
	// makers holds, for each func literal, the MakeClosure instructions in
	// the function it is declared in, in block order.
	//
	//declscope:private
	makers map[*ssa.Function][]*ssa.MakeClosure
	// captures holds, for each value a MakeClosure binds, the free variables
	// it is bound to.
	//
	//declscope:private
	captures map[ssa.Value][]*ssa.FreeVar
	// aliases, calls and held keep the answers of aliasesOf, callsOf and
	// funcsHeldBy.
	//
	//declscope:private
	aliases map[*ssa.Alloc]map[ssa.Value]bool
	//declscope:private
	calls map[*ssa.Function]closureCalls
	//declscope:private
	held map[*ssa.Alloc]closureHeld
}

// newClosures creates an index with nothing indexed yet.
//
//declscope:shared // tracing.go creates one per tracer
func newClosures() *closures {
	return &closures{
		indexed:  make(map[*ssa.Function]bool),
		makers:   make(map[*ssa.Function][]*ssa.MakeClosure),
		captures: make(map[ssa.Value][]*ssa.FreeVar),
		aliases:  make(map[*ssa.Alloc]map[ssa.Value]bool),
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
// bindingOf takes the last, and the tracer of a free variable tries each. The
// two rules agree as long as that holds.
//
//declscope:shared // tracing.go traces a free variable from where it is bound
func (c *closures) makersOf(fn *ssa.Function) []*ssa.MakeClosure {
	if fn.Parent() == nil {
		return nil
	}
	c.index(fn)
	return c.makers[fn]
}

// callsOf finds every call, go and defer statement that may run fn, a func
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
//
//declscope:shared // tracing.go traces the arguments of each call of a func literal
func (c *closures) callsOf(fn *ssa.Function) (calls []ssa.CallInstruction, ok bool) {
	if found, done := c.calls[fn]; done {
		return found.calls, found.ok
	}
	calls, ok = c.findCalls(fn)
	c.calls[fn] = closureCalls{calls: calls, ok: ok}
	return calls, ok
}

// findCalls does the work of callsOf.
func (c *closures) findCalls(fn *ssa.Function) (calls []ssa.CallInstruction, ok bool) {
	parent := fn.Parent()
	if parent == nil {
		return nil, false // A declared function can be called from anywhere
	}
	vars := make(map[ssa.Value]bool) // The variables that may hold fn, and their aliases
	phis := make(map[ssa.Value]bool) // The Phis that may be fn
	mayBeFn := func(v ssa.Value) bool {
		if load, ok := asLoad(v); ok {
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
				if !phis[in] && slices.ContainsFunc(in.Edges, mayBeFn) {
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

// funcLiteralsOf returns the func literals that v can be, when it is a func
// literal or a closure of one, a Phi of such values, or a load of a variable
// that only such values are stored to (see funcsHeldBy):
//
//	ev := func() *zerolog.Event { return logger.Info().Ctx(ctx) }
//	if debug {
//	    ev = func() *zerolog.Event { return logger.Debug().Ctx(ctx) }
//	}
//	ev().Msg("x")           // ev is a Phi of the two
//
//declscope:shared // tracing.go traces the results of the func literals a call runs
func (c *closures) funcLiteralsOf(v ssa.Value, seen map[ssa.Value]bool) ([]*ssa.Function, bool) {
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
		values = val.Edges
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
		more, ok := c.funcLiteralsOf(value, seen)
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
func (c *closures) funcsHeldBy(v ssa.Value) ([]ssa.Value, bool) {
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
func (c *closures) findHeld(alloc *ssa.Alloc) ([]ssa.Value, bool) {
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

// bindingOf returns the value bound to fv where its closure is made, or nil
// when that is not found. With more than one MakeClosure, it takes the last
// (see makersOf).
func (c *closures) bindingOf(fv *ssa.FreeVar) ssa.Value {
	fn := fv.Parent()
	makers := c.makersOf(fn)
	if len(makers) == 0 {
		return nil
	}
	return makers[len(makers)-1].Bindings[slices.Index(fn.FreeVars, fv)]
}

// aliasesOf returns alloc, a local variable, and the free variables that
// stand for it in the closures that capture it, at any depth.
func (c *closures) aliasesOf(alloc *ssa.Alloc) map[ssa.Value]bool {
	if aliases, ok := c.aliases[alloc]; ok {
		return aliases
	}
	c.index(alloc.Parent())
	aliases := map[ssa.Value]bool{alloc: true}
	queue := []ssa.Value{alloc}
	for len(queue) > 0 {
		v := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, fv := range c.captures[v] {
			if !aliases[fv] {
				aliases[fv] = true
				queue = append(queue, fv)
			}
		}
	}
	c.aliases[alloc] = aliases
	return aliases
}

// index indexes the tree of the outermost function fn is in, unless it is
// indexed already.
func (c *closures) index(fn *ssa.Function) {
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

// closureRuns are the calls, go and defer statements that run a closure.
type closureRuns []ssa.CallInstruction

// closureRunsOf returns the uses of mc, a closure, where it is made: the
// calls, go and defer statements that run it, in the order of its
// referrers. ok is false when it has another use, which passes it on. Unlike
// callsOf, it does not follow the closure through a variable or a Phi.
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

// closureCalls is what callsOf found for a func literal.
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
		for instr := range instrsIn(fn) {
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
