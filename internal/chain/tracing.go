package chain

import (
	"go/token"
	"go/types"
	"iter"
	"slices"

	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/zerologapi"
)

// =============================================================================
// Trace State
// =============================================================================

// traceState is what a value is traced as: the zerolog type it is, and the
// sources of context that still count for it. Logger.Output drops the
// context that Context.Ctx sets, so past it only a logger stored in a
// context, from zerolog.Ctx(ctx), counts.
//
//declscope:shared // memo.go keys each trace by it
type traceState struct {
	// typ is the zerolog type of the value traced.
	//
	//declscope:private
	typ zerologapi.Type
	// accepts are the sources of context that count for it.
	//
	//declscope:private
	accepts zerologapi.Sources
}

var (
	// traceEvent traces the event of a terminator.
	traceEvent = traceState{typ: zerologapi.TypeEvent, accepts: zerologapi.AllSources}
	// traceLogger traces the Logger of a direct logging call.
	traceLogger = traceState{typ: zerologapi.TypeLogger, accepts: zerologapi.AllSources}
)

// setsCtx reports whether a call of m gives the value st traces its
// context. A source that st does not accept is a method like any other.
func (st traceState) setsCtx(m zerologapi.Method) bool {
	return m.Kind == zerologapi.KindSource && st.accepts&m.Source != 0
}

// receiverState returns the state to trace the receiver of a call of m in,
// when the value st traces is made from that receiver:
//
//   - A method of the type st traces, in the same state. A chain method, such
//     as Event.Str or Logger.Output, keeps only the sources its result keeps.
//   - A handoff whose result is that type, such as Logger.Info for an event,
//     in the state of the receiver's type.
//   - For an event, a handoff whose result is a Logger, which is what an
//     event is made from: Context.Logger, in the state of a Context.
//
// The last one is for a value of another type. The first argument of an
// unknown function is traced in the state of its result, as for a method
// expression read from a map:
//
//	levels[level](&l).Msg("x") // l is traced as an event
//
// A Logger and a Context need no such case: the handoff that makes either
// one is a method of the other, which the first case follows.
func (st traceState) receiverState(m zerologapi.Method) (traceState, bool) {
	switch {
	case m.Recv == st.typ:
		if m.Kind == zerologapi.KindChain {
			st.accepts &= m.Keeps
		}
		return st, true
	case m.Kind == zerologapi.KindHandoff &&
		(m.Result == st.typ || st.typ == zerologapi.TypeEvent && m.Result == zerologapi.TypeLogger):
		return traceState{typ: m.Recv, accepts: st.accepts}, true
	}
	return traceState{}, false
}

// =============================================================================
// Tracer
// =============================================================================

// tracer follows SSA values backwards to find whether context was set.
//
//declscope:shared // checker.go holds one per checked function
type tracer struct {
	// memo keeps the verdict of each value traced, per trace state, so that
	// it is not worked out again for the next call that reaches it. Without
	// it, a chain of Phis whose edges all lead to the previous one is traced
	// once per path, 2^n times in all.
	//
	//declscope:private
	memo traceMemo
	// positions finds where each instruction is in its block, for
	// storesReaching, which walks back from a read once per trace of it.
	//
	//declscope:private
	positions instrPositions
	// closures indexes the closures and func literals of the package.
	//
	//declscope:private
	closures *closures
}

// newTracer creates a tracer with nothing found yet.
//
//declscope:shared // checker.go creates one per package
func newTracer() *tracer {
	return &tracer{memo: newTraceMemo(), positions: make(instrPositions), closures: newClosures()}
}

// eventHasCtx traces an Event value, the receiver of the terminator at, to
// check if .Ctx() was called.
func (tr *tracer) eventHasCtx(v ssa.Value, at ssa.Instruction) bool {
	if tr.traceValue(v, traceEvent) == yes {
		return true
	}
	done := make(map[tracePointValue]verdict)
	if tr.traceCtxSetInPlace(v, walkPointBefore(at, tr.positions), done) == yes {
		return true
	}
	if d, deferred := at.(*ssa.Defer); deferred {
		return tr.traceCtxSetAtReturns(v, d)
	}
	return false
}

// loggerHasCtx traces a Logger value, the receiver of the direct logging
// call at, such as Logger.Print, to check if the Logger carries a context.
// Logger.Print builds its event from the Logger, as Logger.Info does, so
// the event takes the Logger's context.
//
//declscope:shared // checker.go asks this of each direct logging call's receiver
func (tr *tracer) loggerHasCtx(v ssa.Value, at ssa.Instruction) bool {
	return tr.traceReceiver(v, at, traceLogger) == yes
}

// traceCtxSetAtReturns reports whether, on every path from d, a deferred
// terminator on the event v, to a return, Event.Ctx is called on the event.
// The deferred call runs at the return:
//
//	e := logger.Info()
//	defer e.Msg("x")        // runs at the return, after Ctx
//	e.Ctx(ctx)
//
// A Ctx call on a value that the chain of v is built from counts too, since
// it is the same event. A path that makes the event again, in a loop, has a
// new one after. The recover block, reached after a panic, is not looked at,
// as it has no path from d.
func (tr *tracer) traceCtxSetAtReturns(v ssa.Value, d *ssa.Defer) bool {
	calls := make(map[ssa.Instruction]bool)
	defs := make(map[ssa.Instruction]bool) // Where the event is made, which starts another one in a loop
	for w := v; w != nil; w = sameEventCalleeRecv(w) {
		tr.traceCtxCalls(w, d.Parent(), calls, make(map[ssa.Value]bool))
		if def, ok := w.(ssa.Instruction); ok {
			defs[def] = true
		}
	}
	if len(calls) == 0 {
		return false
	}
	return walkFwd(walkPointAfter(d, tr.positions), func(instr ssa.Instruction) walkStep {
		switch {
		case defs[instr]:
			return walkAbort // The event is made again, so a Ctx call after is on another one
		case calls[instr]:
			return walkStop // Ctx runs on this path
		}
		if _, ok := instr.(*ssa.Return); ok {
			return walkAbort
		}
		return walkOn
	})
}

// tracePointValue is an event asked about at a point.
type tracePointValue struct {
	v     ssa.Value
	point walkPoint
}

// traceCtxSetInPlace reports whether Event.Ctx is called on the event v
// before point, on every path, with its result unused or not. Event.Ctx sets
// the context on the event it runs on, and returns that same event:
//
//	e := logger.Info()
//	e.Ctx(ctx)              // sets the context of e
//	e.Str("k", "v").Msg("x")
//
// Each method of the event but CreateDict returns the event it runs on, so a
// Ctx call on any value of the chain counts. For a Phi, each edge must have one before the
// end of the block it comes from.
//
// done holds the verdict for each value already asked about. A value still
// being worked out is neutral: it is reached again only through an edge of a
// Phi that leads back to it, which carries the same event. done is worked out
// for each terminator, and not kept, since a call before one terminator may
// come after another.
func (tr *tracer) traceCtxSetInPlace(v ssa.Value, before walkPoint, done map[tracePointValue]verdict) verdict {
	key := tracePointValue{v: v, point: before}
	if found, ok := done[key]; ok {
		return found
	}
	done[key] = neutral
	found := verdictOf(tr.traceCtxCalledBefore(v, before))
	if found == no {
		if recv := sameEventCalleeRecv(v); recv != nil {
			found = tr.traceCtxSetInPlace(recv, before, done)
		} else if phi, ok := v.(*ssa.Phi); ok {
			found = tr.traceCtxSetOnEdges(phi, done)
		}
	}
	done[key] = found
	return found
}

// traceCtxSetOnEdges reports whether each edge of phi, apart from nil, has
// Event.Ctx called on it before the end of the block it comes from, or has
// context of its own. An edge that leads back to phi is neutral.
func (tr *tracer) traceCtxSetOnEdges(phi *ssa.Phi, done map[tracePointValue]verdict) verdict {
	return all(func(yield func(verdict) bool) {
		for i, edge := range phi.Edges {
			if isNilConst(edge) {
				continue
			}
			pred := phi.Block().Preds[i]
			found := tr.traceCtxSetInPlace(edge, walkPoint{block: pred, index: len(pred.Instrs)}, done)
			if found == no {
				found = tr.traceValue(edge, traceEvent) // An edge with context of its own needs no Ctx call in place
			}
			if !yield(found) {
				return
			}
		}
	})
}

// traceCtxCalledBefore reports whether, on every path from v to before,
// Event.Ctx is called on the event v, directly or through a method value. A
// path that leads back to the entry of the function without passing v does
// not have v, and needs no call, unless v is a parameter or a free variable,
// which the entry gives.
func (tr *tracer) traceCtxCalledBefore(v ssa.Value, before walkPoint) bool {
	fn := before.block.Parent()
	calls := make(map[ssa.Instruction]bool)
	makes := make(map[ssa.Instruction]bool) // Where another event is in v on the path
	def, made := v.(ssa.Instruction)        // A parameter or a free variable is not made, but given at the entry
	if load, ok := asLoad(v); ok && parentFunc(load.X) == fn {
		// An event in memory, read by a load of its own at each use. A Ctx call
		// on any load of the same address counts, unless another event is
		// stored there in between.
		if loads, overwrites, ok := storeLoadsOf(load.X); ok {
			for _, other := range loads {
				tr.traceCtxCalls(other, fn, calls, make(map[ssa.Value]bool))
			}
			makes = overwrites
			at, _ := placeOf(load.X)
			_, local := at.root.(*ssa.Alloc)
			made = local
		}
	}
	if len(makes) == 0 && made {
		makes[def] = true
	}
	tr.traceCtxCalls(v, fn, calls, make(map[ssa.Value]bool))
	return tr.traceCtxCalledOnEveryPath(calls, makes, made, before)
}

// traceCtxCalledOnEveryPath reports whether every path that leads to before
// passes one of calls, the Event.Ctx calls on an event, after the last of
// makes on it, where another event is in its place. Such an instruction that
// stores an event with context of its own ends a path too. A path that leads
// back to the entry of the function passes none of makes, and has the event
// only when made is false: the event is given at the entry.
func (tr *tracer) traceCtxCalledOnEveryPath(
	calls, makes map[ssa.Instruction]bool,
	made bool,
	before walkPoint,
) bool {
	if len(calls) == 0 {
		return false
	}
	visit := func(instr ssa.Instruction) walkStep {
		switch {
		case calls[instr]:
			return walkStop // Ctx runs on this path
		case !makes[instr]:
			return walkOn
		case !tr.traceStoredEventHasCtx(instr):
			return walkAbort // v is made on this path, and Ctx does not run after
		}
		return walkStop // An event with context of its own is stored on this path
	}
	atEntry := func() bool { return made }
	return walkBack([]walkPoint{before}, visit, atEntry, nil)
}

// traceStoredEventHasCtx reports whether instr, where another event is in a
// variable on the path, stores an event that has context of its own:
//
//	e := logger.Info()
//	e.Ctx(ctx)                      // in place, on the first iteration
//	for range n {
//	    e.Msg("x")
//	    e = logger.Warn().Ctx(ctx)  // of its own, on the later ones
//	}
func (tr *tracer) traceStoredEventHasCtx(instr ssa.Instruction) bool {
	store, ok := instr.(*ssa.Store)
	return ok && zerologapi.TypeOf(store.Val.Type()) == zerologapi.TypeEvent && tr.traceValue(store.Val, traceEvent) == yes
}

// traceCtxCalls adds to calls the Event.Ctx calls in fn on the event v,
// directly or through a method value, or on a value that a method of v
// returns, which is the same event. A Ctx call given a fresh
// context.Background() or TODO() does not count, nor does one on the event
// Event.CreateDict returns, which is another one.
func (tr *tracer) traceCtxCalls(v ssa.Value, fn *ssa.Function, calls map[ssa.Instruction]bool, seen map[ssa.Value]bool) {
	refs := v.Referrers()
	if seen[v] || refs == nil {
		return
	}
	seen[v] = true
	for _, ref := range *refs {
		uses := []ssa.Instruction{ref}
		if mc, ok := ref.(*ssa.MakeClosure); ok {
			uses = *mc.Referrers() // A method value bound to v, called later
		}
		for _, use := range uses {
			call, ok := use.(*ssa.Call)
			if !ok || call.Parent() != fn {
				continue
			}
			c, ok := calleeOf(&call.Call)
			if !ok || c.recv != v || c.embedded != nil || c.api.Recv != zerologapi.TypeEvent {
				continue
			}
			if c.api.Kind == zerologapi.KindSource {
				if !tr.givesFreshContext(&call.Call) {
					calls[call] = true
				}
			} else if c.returnsSameEvent() {
				tr.traceCtxCalls(call, fn, calls, seen)
			}
		}
	}
}

// traceValue traces an SSA value backwards to find if context was set.
//
// Tracing flow:
//
//	┌─────────────────────────────────────────────────────────────────┐
//	│                    traceValue Decision Tree                      │
//	│                                                                  │
//	│  Input: ssa.Value                                                │
//	│     │                                                            │
//	│     ├─ In progress? → neutral (a cycle adds no source)          │
//	│     ├─ Traced before? → the verdict kept (see traceMemo)        │
//	│     │                                                            │
//	│     ├─ Is *ssa.Call?                                             │
//	│     │     │                                                      │
//	│     │     ├─ No static callee? → trace receiver                 │
//	│     │     │                                                      │
//	│     │     ├─ Func literal? → trace return values                │
//	│     │     │                                                      │
//	│     │     ├─ Sets context? → yes                                │
//	│     │     ├─ Handoff? → trace receiver with its own tracer      │
//	│     │     └─ Receiver of the traced type? → trace receiver      │
//	│     │                                                            │
//	│     └─ Not a Call → traceCommon (Phi, UnOp, Alloc, etc.)        │
//	└─────────────────────────────────────────────────────────────────┘
func (tr *tracer) traceValue(v ssa.Value, st traceState) verdict {
	return tr.memo.trace(memoKey{v: v, st: st}, true, func() verdict {
		return tr.traceUncached(v, st)
	})
}

// traceUncached does the work of traceValue for a value not traced yet.
func (tr *tracer) traceUncached(v ssa.Value, st traceState) verdict {
	call, ok := v.(*ssa.Call)
	if !ok {
		return tr.traceCommon(v, st)
	}

	c, declared := calleeOf(&call.Call)
	if !declared {
		if found, ok := tr.traceFuncLiteralResult(call, 0, st); ok {
			return found
		}
		if call.Call.StaticCallee() != nil || len(call.Call.Args) == 0 {
			return no
		}
		// Dynamic dispatch: the method is not known
		return tr.traceReceiver(call.Call.Args[0], call, st)
	}

	if st.setsCtx(c.api) {
		// A fresh context.Background() or TODO() given in place of the one in
		// scope replaces what the chain had
		return verdictOf(!tr.givesFreshContext(&call.Call))
	}
	if next, ok := st.receiverState(c.api); ok {
		return tr.traceCalleeReceiver(c, call, next)
	}
	return no
}

// givesFreshContext reports whether cc, a call of a Ctx of zerolog, gives it
// a fresh context.Background() or TODO() (see isFreshContext). The context is
// the last argument in each form of the call.
func (tr *tracer) givesFreshContext(cc *ssa.CallCommon) bool {
	return len(cc.Args) > 0 && isFreshContext(cc.Args[len(cc.Args)-1], tr.positions)
}

// traceSource traces v, one of the values something can be, such as an edge
// of a Phi or a value stored. A nil event logs nothing, so it is neutral.
func (tr *tracer) traceSource(v ssa.Value, st traceState) verdict {
	if isNilConst(v) {
		return neutral
	}
	return tr.traceValue(v, st)
}

// =============================================================================
// Common SSA Value Handling
// =============================================================================

// traceCommon handles common SSA value types (Phi, UnOp, FreeVar, etc.).
func (tr *tracer) traceCommon(v ssa.Value, st traceState) verdict {
	switch val := v.(type) {
	case *ssa.Field, *ssa.Index:
		if found, ok := tr.traceSelectedValue(v, st); ok {
			return found
		}
	case *ssa.Extract:
		if call, ok := val.Tuple.(*ssa.Call); ok {
			if found, ok := tr.traceFuncLiteralResult(call, val.Index, st); ok {
				return found
			}
		}
		return tr.traceValue(val.Tuple, st)
	case *ssa.Phi:
		// Every edge must have context. One that leads back to the Phi, as an
		// update in a loop does, is neutral there
		return all(verdictsOf(val.Edges, func(edge ssa.Value) verdict {
			return tr.traceSource(edge, st)
		}))
	case *ssa.UnOp:
		return tr.traceUnOp(val, st)
	case *ssa.Alloc:
		return tr.traceAlloc(val, st)
	case *ssa.FreeVar:
		return tr.traceResolved(tr.resolver(st).resolveFreeVar(val, nil), st)
	case *ssa.Parameter:
		// A pointer parameter can be updated in place; see storesReaching.
		// Otherwise, or failing that, the arguments give it
		found, ok := tr.traceAddressAt(val, nil, st)
		if !ok {
			return tr.traceArguments(val, st)
		}
		return found.or(func() verdict { return tr.traceArguments(val, st) })
	case *ssa.FieldAddr, *ssa.IndexAddr:
		// The address of a field or element is itself the receiver of a
		// pointer method, such as Logger.Info on s.log. Failing that, the
		// value it is the address in may have context
		if found, ok := tr.traceStoredValues(val, nil, st); ok {
			return found.or(func() verdict { return tr.traceValue(unwrapInner(v), st) })
		}
	}

	// Handle simple wrapper types that just need inner value tracing
	if inner := unwrapInner(v); inner != nil {
		return tr.traceValue(inner, st)
	}

	return no
}

// traceSelectedValue traces v, a field of a struct value or an element of an
// array value, selected from one loaded whole from an address. What is at v is
// what the stores that reach the load left at the same selection there:
//
//	t8 = *t4                // the whole struct, loaded
//	t9 = t8.Logger          // v, read by value
//
// ok is false when the value it selects from is not loaded from an address
// whose stores storesReachingAt can find.
func (tr *tracer) traceSelectedValue(v ssa.Value, st traceState) (found verdict, ok bool) {
	var selections []sel
	for {
		from, s, ok := valueSel(v)
		if !ok {
			break
		}
		selections = append(selections, s)
		v = from
	}
	load, isLoad := asLoad(v)
	if !isLoad {
		return no, false
	}
	slices.Reverse(selections)
	at, _ := placeOf(load.X)
	sources, ok := tr.resolver(st).resolveAt(at.with(selections...), load)
	if !ok {
		return no, false
	}
	return tr.traceResolved(sources, st), true
}

// =============================================================================
// Special Value Handling
// =============================================================================

// traceUnOp handles SSA unary operations, especially pointer dereferences.
// A load reads what the stores that reach it left at its address.
func (tr *tracer) traceUnOp(unop *ssa.UnOp, st traceState) verdict {
	if unop.Op == token.MUL {
		if found, ok := tr.traceStoredValues(unop.X, unop, st); ok {
			return found
		}
	}
	return tr.traceValue(unop.X, st)
}

// traceAlloc handles SSA Alloc nodes (local variable allocation), reached
// where the read that uses it is not known, such as an edge of a Phi.
func (tr *tracer) traceAlloc(alloc *ssa.Alloc, st traceState) verdict {
	found, _ := tr.traceStoredValues(alloc, nil, st) // no when it knows nothing of alloc
	return found
}

// traceStoredValues traces the values that can be at addr when read runs,
// or at any read of addr when read is nil. ok is false when storesReaching
// knows nothing of addr.
//
// Like the edges of a Phi, every value must have context, and a value that
// is already being traced is neutral: it is an update in a loop, which keeps
// the context of the value before it.
func (tr *tracer) traceStoredValues(addr ssa.Value, read ssa.Instruction, st traceState) (found verdict, ok bool) {
	sources, ok := tr.resolver(st).resolve(addr, read)
	if !ok {
		return no, false
	}
	return tr.traceResolved(sources, st), true
}

// traceResolved combines the verdicts of the sources a resolver found, as
// traceSource judges each: every one must have context. An unknown source
// has none, and a pointer that the resolver could not follow is traced as a
// value, through what it points to.
func (tr *tracer) traceResolved(sources iter.Seq[resolved], st traceState) verdict {
	return all(func(yield func(verdict) bool) {
		for src := range sources {
			found := no
			switch {
			case src.v == nil:
			case src.pointer:
				found = tr.traceValue(src.v, st)
			default:
				found = tr.traceSource(src.v, st)
			}
			if !yield(found) {
				return
			}
		}
	})
}

// resolver returns a resolver for a trace in st. A parameter of a func
// literal that it resolves is in progress in the memo, as a value is, so
// that reaching it again, through a recursive call, is neutral, and a
// verdict that depends on that is kept no longer than it holds.
func (tr *tracer) resolver(st traceState) resolver {
	return resolver{
		positions: tr.positions,
		closures:  tr.closures,
		enter: func(param *ssa.Parameter, expand func()) {
			tr.memo.trace(memoKey{v: param, st: st, param: true}, false, func() verdict {
				expand()
				return neutral
			})
		},
	}
}

// traceAddressAt traces addr, an address that read uses, such as the
// receiver of Logger.Info, which has a pointer receiver. A nil read stands
// for every read of addr. ok is false when addr is not one whose stores
// storesReaching can find.
func (tr *tracer) traceAddressAt(addr ssa.Value, read ssa.Instruction, st traceState) (found verdict, ok bool) {
	if !resolvesThrough(addr) {
		return no, false
	}
	return tr.traceStoredValues(addr, read, st)
}

// traceArguments traces the arguments passed for param, when its function is
// a func literal that is only ever called where it is written, as an IIFE
// is. Every call must pass an argument with context.
//
//	func(e *zerolog.Event) {
//	    e.Msg("x")              // e is the argument below
//	}(logger.Info().Ctx(ctx))
func (tr *tracer) traceArguments(param *ssa.Parameter, st traceState) verdict {
	fn := param.Parent()
	calls, ok := tr.closures.callsOf(fn)
	if !ok || len(calls) == 0 {
		return no
	}
	idx := slices.Index(fn.Params, param)
	// An argument passed on by a recursive call, or a chain of event methods
	// on it, leads back to param, and is neutral as an update in a loop is
	return all(verdictsOf(calls, func(call ssa.CallInstruction) verdict {
		return tr.traceValue(call.Common().Args[idx], st)
	}))
}

// traceFuncLiteralResult traces the result idx of call, when call runs a
// func literal: one called on the spot as an IIFE (Immediately Invoked
// Function Expression), through a variable, or one of several a variable
// can hold. One that captures nothing is called as the function itself, not
// as a closure. Every func literal must return a value with context. ok is
// false when call runs no func literal that is known.
func (tr *tracer) traceFuncLiteralResult(call *ssa.Call, idx int, st traceState) (found verdict, ok bool) {
	fns, ok := tr.closures.funcLiteralsOf(call.Call.Value, make(map[ssa.Value]bool))
	if sfn := call.Call.StaticCallee(); sfn != nil {
		fns, ok = []*ssa.Function{sfn}, sfn.Parent() != nil
	}
	if !ok || len(fns) == 0 {
		return no, false
	}
	return all(verdictsOf(fns, func(fn *ssa.Function) verdict {
		return tr.traceReturns(fn, idx, call.Call.Args, st)
	})), true
}

// traceArgumentReturned traces the argument that args pass for result,
// returned by fn, when it is one of its parameters, or a chain of event
// methods on one. The result of a call is what that call passes, not what
// every call of fn does, which traceArguments asks:
//
//	id := func(e *zerolog.Event) *zerolog.Event { return e }
//	a := id(logger.Info().Ctx(ctx)) // a is this argument
//	b := id(logger.Info())          // and b this one
//
// It is neutral when result is not a parameter: there is no argument to
// trace in place of it.
func (tr *tracer) traceArgumentReturned(result ssa.Value, fn *ssa.Function, args []ssa.Value, st traceState) verdict {
	for v := result; v != nil; v = sameEventCalleeRecv(v) {
		param, ok := v.(*ssa.Parameter)
		if !ok {
			continue
		}
		if idx := slices.Index(fn.Params, param); idx >= 0 && idx < len(args) {
			return tr.traceValue(args[idx], st)
		}
		return no
	}
	return neutral
}

// receiverHasCtx reports whether the event c runs on, the receiver of the
// terminator at, has context, as eventHasCtx does. For a method expression of
// a promoted method, the event is read from the struct it is embedded in,
// and only the context of its own counts.
//
//declscope:shared // checker.go asks this of each terminator
func (tr *tracer) receiverHasCtx(c callee, at ssa.Instruction) bool {
	if c.embedded != nil {
		return tr.traceEmbeddedReceiver(c, at, traceEvent) == yes ||
			tr.traceEmbeddedCtxSetInPlace(c, at)
	}
	return tr.eventHasCtx(c.recv, at)
}

// traceCalleeReceiver traces the receiver of c, which call runs, as
// traceReceiver does, or through the struct it is embedded in.
func (tr *tracer) traceCalleeReceiver(c callee, call ssa.Instruction, st traceState) verdict {
	if c.embedded != nil {
		return tr.traceEmbeddedReceiver(c, call, st)
	}
	return tr.traceReceiver(c.recv, call, st)
}

// traceEmbeddedReceiver traces the receiver of c, a method expression of a
// promoted method, which call runs. c.recv is the struct the receiver is
// embedded in, or a pointer to it, and the receiver is what the stores that
// reach call left at the embedded fields of it:
//
//	t5 = *t1                               // the struct, loaded
//	t6 = (myEvent).Msg$thunk(t5, "x")      // reads t1.Event
//
// The struct must be loaded from an address, or be a pointer, whose stores
// storesReachingAt can find. Otherwise nothing is found.
func (tr *tracer) traceEmbeddedReceiver(c callee, call ssa.Instruction, st traceState) verdict {
	addr, typ, read, ok := c.embeddedStart(call)
	if !ok {
		return no
	}
	at, _ := placeOf(addr)
	return tr.traceEmbeddedAt(at, typ, c.embedded, read, call, st)
}

// traceEmbeddedAt traces the receiver at the embedded fields of the struct of
// type typ at the address at, as read reads it. call is the call
// of the method expression.
//
// An embedded field but the last may be a pointer. The receiver is then at
// the rest of the fields of what each pointer that can be stored there
// points to, as call reads it, and every one must have context:
//
//	type outer struct{ *inner }
//	o := outer{&inner{e}}  // t3 = new inner ; *&t3.Event = e ; *&t1.inner = t3
//	outer.Msg(o, "x")      // reads t1.inner, then t3.Event
func (tr *tracer) traceEmbeddedAt(
	at place,
	typ types.Type,
	embedded []int,
	read, call ssa.Instruction,
	st traceState,
) verdict {
	at, n, field := at.withEmbedded(typ, embedded)
	stored, ok := storesReachingAt(at, read, tr.positions)
	switch {
	case n == len(embedded) && ok:
		return tr.traceResolved(tr.resolver(st).resolveStored(stored), st)
	case !ok || stored.unknown || stored.captured != nil || len(stored.params) > 0:
		return no
	}
	elem := field.Underlying().(*types.Pointer).Elem()
	return all(verdictsOf(stored.values, func(v ssa.Value) verdict {
		if isNilConst(v) {
			return neutral // A nil pointer has no event to log
		}
		next, _ := placeOf(v)
		return tr.traceEmbeddedAt(next, elem, embedded[n:], call, call, st)
	}))
}

// traceEmbeddedCtxSetInPlace reports whether Event.Ctx is called in place,
// with its result unused or not, on the event at the embedded fields of
// c.recv before call reads it, as traceCtxCalledBefore does for an event in
// memory. A Ctx call on any read of the event where the struct is counts,
// unless another event is stored there in between: a load of the event, or
// a selection of it from a load of the struct, or of a struct it is embedded
// in, as each load of a struct value of a local variable is:
//
//	me := myEvent{logger.Info()}
//	me.Ctx(ctx)            // t4 = *t1 ; t5 = t4.Event ; Ctx(t5, ctx)
//	myEvent.Msg(me, "x")   // Msg$thunk(t4, "x") reads t4.Event
//
// Each embedded field but the last must be a struct, not a pointer to one.
func (tr *tracer) traceEmbeddedCtxSetInPlace(c callee, call ssa.Instruction) bool {
	addr, typ, _, ok := c.embeddedStart(call)
	if !ok {
		return false
	}
	base, _ := placeOf(addr)
	at, n, _ := base.withEmbedded(typ, c.embedded)
	if n < len(c.embedded) {
		return false // Reading past a pointer is a load, which a path does not take
	}
	_, overwrites, ok := storeLoadsAt(at)
	if !ok {
		return false
	}
	fn := call.Parent()
	calls := make(map[ssa.Instruction]bool)
	for k := len(base.path); k <= len(at.path); k++ {
		loads, _, _ := storeLoadsAt(at.prefix(k))
		for _, load := range loads {
			for _, ev := range selectedFields(load, c.embedded[k-len(base.path):]) {
				tr.traceCtxCalls(ev, fn, calls, make(map[ssa.Value]bool))
			}
		}
	}
	_, local := at.root.(*ssa.Alloc)
	return tr.traceCtxCalledOnEveryPath(calls, overwrites, local, walkPointBefore(call, tr.positions))
}

// traceReceiver traces recv, the receiver of call, a method call. It is the
// first argument, or what a method value is bound to. A pointer receiver that
// is a variable, or a field or element of one, holds what the stores that
// reach the call left there.
func (tr *tracer) traceReceiver(recv ssa.Value, call ssa.Instruction, st traceState) verdict {
	if found, ok := tr.traceAddressAt(recv, call, st); ok {
		return found
	}
	return tr.traceValue(recv, st)
}

// traceReturns traces the value at resultIndex from every return of fn, as
// for an IIFE (Immediately Invoked Function Expression). Every one must have
// context. args are what the call whose result is traced passes, which a
// parameter returned stands for.
func (tr *tracer) traceReturns(fn *ssa.Function, resultIndex int, args []ssa.Value, st traceState) verdict {
	// A result of another type, such as an interface, is traced through to
	// the value it holds.
	return all(func(yield func(verdict) bool) {
		for instr := range instrsIn(fn) {
			ret, ok := instr.(*ssa.Return)
			if !ok {
				continue
			}
			found := no
			if resultIndex < len(ret.Results) {
				// The value itself, or the argument passed for it, will do
				result := ret.Results[resultIndex]
				found = tr.traceValue(result, st).or(func() verdict {
					return tr.traceArgumentReturned(result, fn, args, st)
				})
			}
			if !yield(found) {
				return
			}
		}
	})
}
