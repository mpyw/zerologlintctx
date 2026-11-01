package chain

import (
	"go/token"
	"iter"
	"slices"

	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/ssaflow"
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
//declscope:shared // checker.go holds one per package
type tracer struct {
	// memo keeps the verdict of each value traced, per trace state, so that
	// it is not worked out again for the next call that reaches it. Without
	// it, a chain of Phis whose edges all lead to the previous one is traced
	// once per path, 2^n times in all.
	//
	//declscope:private
	memo traceMemo
	// positions finds where each instruction is in its block, for the
	// resolver, which walks back from a read once per trace of it.
	//
	//declscope:private
	positions ssaflow.Positions
	// closures indexes the closures and func literals of the package.
	//
	//declscope:private
	closures *ssaflow.Closures
	// events finds which event each value of a chain is, for a Ctx call in
	// place.
	//
	//declscope:private
	events *eventIndex
}

// newTracer creates a tracer with nothing found yet.
//
//declscope:shared // checker.go creates one per package
func newTracer() *tracer {
	positions, closures := make(ssaflow.Positions), ssaflow.NewClosures()
	return &tracer{
		memo:      newTraceMemo(),
		positions: positions,
		closures:  closures,
		events:    newEventIndex(positions, closures),
	}
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
//	│     │     ├─ Handoff? → trace receiver in the state of its type │
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
		// Dynamic dispatch: the method is not known, and the first argument
		// is taken for its receiver
		recv := ssaflow.CalleeReceiver(&call.Call)
		if recv == nil {
			return no
		}
		return tr.traceReceiver(recv, call, st)
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
	return len(cc.Args) > 0 && isFreshContext(cc.Args[len(cc.Args)-1], tr.positions, tr.closures)
}

// traceSource traces v, one of the values something can be, such as an edge
// of a Phi or a value stored. A nil event logs nothing, so it is neutral.
func (tr *tracer) traceSource(v ssa.Value, st traceState) verdict {
	if ssaflow.IsNilConst(v) {
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
		return tr.traceResolved(tr.resolver(st).ResolveFreeVar(val, nil), st)
	case *ssa.Parameter:
		// A pointer parameter can be updated in place; see Resolver.Resolve.
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
			return found.or(func() verdict { return tr.traceValue(ssaflow.UnwrapInner(v), st) })
		}
	}

	// Handle simple wrapper types that just need inner value tracing
	if inner := ssaflow.UnwrapInner(v); inner != nil {
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
// For a value selected from a Phi of such values, each load an edge leads to
// is a read of its own, and every one must have context (see
// ssaflow.LoadedSelection).
//
// ok is false when the value it selects from is not loaded from an address
// whose stores Resolver.ResolveAt can find.
func (tr *tracer) traceSelectedValue(v ssa.Value, st traceState) (found verdict, ok bool) {
	reads, ok := ssaflow.LoadedSelection(v)
	if !ok {
		return no, false
	}
	resolved := make([]iter.Seq[ssaflow.Resolved], 0, len(reads))
	for _, rd := range reads {
		sources, ok := tr.resolver(st).ResolveAt(rd.At, rd.Load)
		if !ok {
			return no, false
		}
		resolved = append(resolved, sources)
	}
	return all(verdictsOf(resolved, func(sources iter.Seq[ssaflow.Resolved]) verdict {
		return tr.traceResolved(sources, st)
	})), true
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
// or at any read of addr when read is nil. ok is false when Resolver.Resolve
// knows nothing of addr.
//
// Like the edges of a Phi, every value must have context, and a value that
// is already being traced is neutral: it is an update in a loop, which keeps
// the context of the value before it.
func (tr *tracer) traceStoredValues(addr ssa.Value, read ssa.Instruction, st traceState) (found verdict, ok bool) {
	sources, ok := tr.resolver(st).Resolve(addr, read)
	if !ok {
		return no, false
	}
	return tr.traceResolved(sources, st), true
}

// traceResolved combines the verdicts of the sources a resolver found, as
// traceSource judges each: every one must have context. An unknown source
// has none, and a pointer that the resolver could not follow is traced as a
// value, through what it points to.
func (tr *tracer) traceResolved(sources iter.Seq[ssaflow.Resolved], st traceState) verdict {
	return all(func(yield func(verdict) bool) {
		for src := range sources {
			found := no
			switch {
			case src.V == nil:
			case src.Pointer:
				found = tr.traceValue(src.V, st)
			default:
				found = tr.traceSource(src.V, st)
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
func (tr *tracer) resolver(st traceState) ssaflow.Resolver {
	return ssaflow.Resolver{
		Positions: tr.positions,
		Closures:  tr.closures,
		Model:     zerologapi.Model{},
		Enter: func(param *ssa.Parameter, expand func()) {
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
// Resolver.Resolve can find.
func (tr *tracer) traceAddressAt(addr ssa.Value, read ssa.Instruction, st traceState) (found verdict, ok bool) {
	if !tr.resolver(st).ResolvesThrough(addr) {
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
	calls, ok := tr.closures.CallsOf(fn)
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
	fns, ok := tr.closures.FuncLiteralsOf(call.Call.Value, make(map[ssa.Value]bool))
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
	param, ok := eventOrigin(result).(*ssa.Parameter)
	if !ok {
		return neutral
	}
	if idx := slices.Index(fn.Params, param); idx >= 0 && idx < len(args) {
		return tr.traceValue(args[idx], st)
	}
	return no
}

// receiverHasCtx reports whether the event c runs on, the receiver of the
// terminator at, has context: its chain has a Ctx call, or one runs on it in
// place before at (see eventIndex.eventCtxSetInPlace). For a method
// expression of a promoted method, the event is read from the struct it is
// embedded in.
//
//declscope:shared // checker.go asks this of each terminator
func (tr *tracer) receiverHasCtx(c callee, at ssa.Instruction) bool {
	if tr.traceCalleeReceiver(c, at, traceEvent) == yes {
		return true
	}
	return tr.events.eventCtxSetInPlace(c, at, func(v ssa.Value) bool {
		return tr.traceValue(v, traceEvent) == yes
	})
}

// traceCalleeReceiver traces the receiver of c, which call runs, as
// traceReceiver does, or through the struct it is embedded in.
func (tr *tracer) traceCalleeReceiver(c callee, call ssa.Instruction, st traceState) verdict {
	if c.Embedded != nil {
		return tr.traceEmbeddedReceiver(c, call, st)
	}
	return tr.traceReceiver(c.Recv, call, st)
}

// traceEmbeddedReceiver traces the receiver of c, a method expression of a
// promoted method, which call runs. c.Recv is the struct the receiver is
// embedded in, or a pointer to it, and the receiver is what the stores that
// reach call left at the embedded fields of it:
//
//	t5 = *t1                               // the struct, loaded
//	t6 = (myEvent).Msg$thunk(t5, "x")      // reads t1.Event
//
// The struct must be loaded from an address, or be a pointer, whose stores
// Resolver.ResolveEmbedded can find. Otherwise nothing is found.
func (tr *tracer) traceEmbeddedReceiver(c callee, call ssa.Instruction, st traceState) verdict {
	addr, typ, read, ok := c.EmbeddedStart(call)
	if !ok {
		return no
	}
	at, _ := ssaflow.PlaceOf(addr)
	return tr.traceResolved(tr.resolver(st).ResolveEmbedded(at, typ, c.Embedded, read, call), st)
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
		for instr := range ssaflow.InstrsIn(fn) {
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
