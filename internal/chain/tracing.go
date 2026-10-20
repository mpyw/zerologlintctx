package chain

import (
	"go/token"
	"go/types"
	"maps"
	"slices"

	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/typeutil"
)

// =============================================================================
// Tracer Type
// =============================================================================

// tracerType identifies which zerolog type we're currently tracing.
// The tracing logic differs based on what type of value we're following.
//
//declscope:shared // phi.go traces each edge with the Phi's tracer
type tracerType int

const (
	// tracerEvent traces *zerolog.Event values.
	tracerEvent tracerType = iota
	// tracerLogger traces zerolog.Logger values.
	tracerLogger
	// tracerContext traces zerolog.Context values.
	tracerContext
)

// isTracedType reports whether typ is the zerolog type t traces.
func (t tracerType) isTracedType(typ types.Type) bool {
	switch t {
	case tracerEvent:
		return typeutil.IsEvent(typ)
	case tracerLogger:
		return typeutil.IsLogger(typ)
	case tracerContext:
		return typeutil.IsContext(typ)
	}
	return false
}

// isReturnedBy reports whether sig returns exactly the zerolog type t traces.
func (t tracerType) isReturnedBy(sig *types.Signature) bool {
	switch t {
	case tracerEvent:
		return typeutil.ReturnsEvent(sig)
	case tracerLogger:
		return typeutil.ReturnsLogger(sig)
	case tracerContext:
		return typeutil.ReturnsContext(sig)
	}
	return false
}

// isCtxSetBy reports whether a call to c gives the value t traces its
// context:
//   - Event tracer: Event.Ctx(ctx), Context.Ctx(ctx), zerolog.Ctx(ctx)
//   - Logger tracer: zerolog.Ctx(ctx)
//   - Context tracer: Context.Ctx(ctx)
//
// The Event and Logger tracers take any Ctx of zerolog, since a value they
// trace only reaches one of the above.
func (t tracerType) isCtxSetBy(c callee) bool {
	if !c.isCtx() {
		return false
	}
	if t == tracerContext {
		recv := c.recvType()
		return recv != nil && tracerContext.isTracedType(recv)
	}
	return true
}

// =============================================================================
// Handoffs Between Tracers
// =============================================================================

// tracerHandoff is a call that turns a value of one zerolog type into another.
// Tracing goes on from the call's receiver, with the receiver's tracer.
type tracerHandoff struct {
	from tracerType // The receiver's type, and the tracer to go on with
	to   tracerType // The type the call returns
}

var (
	// traceLoggerOfEvent follows Logger.Info(), Logger.Debug(), ... back to the Logger.
	traceLoggerOfEvent = tracerHandoff{from: tracerLogger, to: tracerEvent}
	// traceContextOfLogger follows Context.Logger() back to the Context.
	traceContextOfLogger = tracerHandoff{from: tracerContext, to: tracerLogger}
	// traceLoggerOfContext follows Logger.With() back to the Logger.
	traceLoggerOfContext = tracerHandoff{from: tracerLogger, to: tracerContext}
)

// tracerHandoffs lists the handoffs each tracer follows, in the order tried.
var tracerHandoffs = map[tracerType][]tracerHandoff{
	tracerEvent:   {traceLoggerOfEvent, traceContextOfLogger},
	tracerLogger:  {traceContextOfLogger, traceLoggerOfContext},
	tracerContext: {traceLoggerOfContext},
}

// handoffAt returns the tracer to go on with from the receiver of a call to
// c, when the call is a handoff that t follows.
func (t tracerType) handoffAt(c callee) (tracerType, bool) {
	recv := c.recvType()
	if recv == nil {
		return 0, false
	}
	for _, h := range tracerHandoffs[t] {
		if h.from.isTracedType(recv) && h.to.isReturnedBy(c.signature()) {
			return h.from, true
		}
	}
	return 0, false
}

// =============================================================================
// Tracer
// =============================================================================

// tracer follows SSA values backwards to find whether context was set.
//
//declscope:shared // checker.go holds one per checked function
type tracer struct {
	// hasCtx remembers each value found to have context, per tracer type.
	// Only found results are kept: a value not found may have been cut short
	// by a cycle, and found elsewhere. Without it, a chain of Phis whose edges
	// all lead to the previous one is traced once per path, 2^n times in all.
	//
	//declscope:private
	hasCtx map[tracedValue]bool
	// positions finds where each instruction is in its block, for
	// storesReaching, which walks back from a read once per trace of it.
	//
	//declscope:private
	positions instrPositions
}

// tracedValue is a value traced by one tracer type.
type tracedValue struct {
	v ssa.Value
	t tracerType
}

// eventHasCtx traces an Event value to check if .Ctx() was called.
//
//declscope:shared // checker.go asks this of each terminator's receiver
func (tr *tracer) eventHasCtx(v ssa.Value) bool {
	return tr.traceValue(v, tracerEvent, make(map[ssa.Value]bool))
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
//	│     ├─ Already visited? → return false (cycle detection)        │
//	│     ├─ Found before? → return true (cached)                     │
//	│     │                                                            │
//	│     ├─ Is *ssa.Call?                                             │
//	│     │     │                                                      │
//	│     │     ├─ No static callee? → trace receiver                 │
//	│     │     │                                                      │
//	│     │     ├─ Func literal? → trace return values                │
//	│     │     │                                                      │
//	│     │     ├─ Sets context? → return true                        │
//	│     │     ├─ Handoff? → trace receiver with its own tracer      │
//	│     │     └─ Receiver of the traced type? → trace receiver      │
//	│     │                                                            │
//	│     └─ Not a Call → traceCommon (Phi, UnOp, Alloc, etc.)        │
//	└─────────────────────────────────────────────────────────────────┘
//
//declscope:shared // phi.go traces each edge of a Phi
func (tr *tracer) traceValue(v ssa.Value, t tracerType, visited map[ssa.Value]bool) bool {
	if visited[v] {
		return false
	}
	key := tracedValue{v: v, t: t}
	if tr.hasCtx[key] {
		return true
	}
	visited[v] = true

	if !tr.traceUncached(v, t, visited) {
		return false
	}
	if tr.hasCtx == nil {
		tr.hasCtx = make(map[tracedValue]bool)
	}
	tr.hasCtx[key] = true
	return true
}

// traceUncached does the work of traceValue for a value not yet found to
// have context.
func (tr *tracer) traceUncached(v ssa.Value, t tracerType, visited map[ssa.Value]bool) bool {
	call, ok := v.(*ssa.Call)
	if !ok {
		return tr.traceCommon(v, t, visited)
	}

	c, declared := calleeOf(&call.Call)
	if !declared {
		sfn := call.Call.StaticCallee()
		if sfn == nil {
			// Dynamic dispatch: the method is not known
			if len(call.Call.Args) == 0 {
				return false
			}
			return tr.traceReceiver(call.Call.Args[0], call, t, visited)
		}
		// A func literal, called on the spot as an IIFE (Immediately Invoked
		// Function Expression) or through a variable. One that captures
		// nothing is called as the function itself, not as a closure.
		if sfn.Parent() != nil {
			return tr.traceReturns(sfn, t, visited)
		}
		return false
	}

	if t.isCtxSetBy(c) {
		return true
	}
	if next, ok := t.handoffAt(c); ok {
		return tr.traceReceiver(c.recv, call, next, visited)
	}

	// Continue tracing through receiver if type matches
	if recv := c.recvType(); recv != nil && t.isTracedType(recv) {
		return tr.traceReceiver(c.recv, call, t, visited)
	}

	return false
}

// =============================================================================
// Common SSA Value Handling
// =============================================================================

// traceCommon handles common SSA value types (Phi, UnOp, FreeVar, etc.).
func (tr *tracer) traceCommon(v ssa.Value, t tracerType, visited map[ssa.Value]bool) bool {
	switch val := v.(type) {
	case *ssa.Phi:
		return tr.tracePhi(val, t, visited)
	case *ssa.UnOp:
		return tr.traceUnOp(val, t, visited)
	case *ssa.Alloc:
		return tr.traceAlloc(val, t, visited)
	case *ssa.FreeVar:
		return tr.traceFreeVar(val, t, visited)
	case *ssa.Parameter:
		// A pointer parameter can be updated in place; see storesReaching.
		if found, _ := tr.traceAddressAt(val, nil, t, visited); found {
			return true
		}
		return tr.traceArguments(val, t, visited)
	case *ssa.FieldAddr, *ssa.IndexAddr:
		// The address of a field or element is itself the receiver of a
		// pointer method, such as Logger.Info on s.log.
		if found, _ := tr.traceStoredValues(val, nil, t, visited); found {
			return true
		}
	}

	// Handle simple wrapper types that just need inner value tracing
	if inner := unwrapInner(v); inner != nil {
		return tr.traceValue(inner, t, visited)
	}

	return false
}

// =============================================================================
// Special Value Handling
// =============================================================================

// traceUnOp handles SSA unary operations, especially pointer dereferences.
// A load reads what the stores that reach it left at its address.
func (tr *tracer) traceUnOp(unop *ssa.UnOp, t tracerType, visited map[ssa.Value]bool) bool {
	if unop.Op == token.MUL {
		if found, ok := tr.traceStoredValues(unop.X, unop, t, visited); ok {
			return found
		}
	}
	return tr.traceValue(unop.X, t, visited)
}

// traceAlloc handles SSA Alloc nodes (local variable allocation), reached
// where the read that uses it is not known, such as an edge of a Phi.
func (tr *tracer) traceAlloc(alloc *ssa.Alloc, t tracerType, visited map[ssa.Value]bool) bool {
	found, _ := tr.traceStoredValues(alloc, nil, t, visited)
	return found
}

// traceStoredValues traces the values that can be at addr when read runs,
// or at any read of addr when read is nil. ok is false when storesReaching
// knows nothing of addr.
//
// Like the edges of a Phi, every value must have context, and a value that
// is already being traced is skipped: it is an update in a loop, which keeps
// the context of the value before it. A path through a Logger.UpdateContext
// call whose returned Context has context on every path needs no value.
func (tr *tracer) traceStoredValues(addr ssa.Value, read ssa.Instruction, t tracerType, visited map[ssa.Value]bool) (found, ok bool) {
	updaterSetsCtx := func(fn *ssa.Function) bool {
		return tr.traceReturns(fn, tracerContext, maps.Clone(visited))
	}
	if tr.positions == nil {
		tr.positions = make(instrPositions)
	}
	stored, ok := storesReaching(addr, read, updaterSetsCtx, tr.positions)
	if !ok {
		return false, false
	}
	if stored.unknown {
		return false, true
	}
	found = stored.covered
	if stored.captured != nil {
		if !tr.traceFreeVar(stored.captured, t, maps.Clone(visited)) {
			return false, true
		}
		found = true
	}
	for _, v := range stored.values {
		if visited[v] {
			continue
		}
		// Each store is traced on its own copy of visited, unless it is the
		// only one: copying on every step of a long chain of updates would
		// take time quadratic in its length.
		edgeVisited := visited
		if len(stored.values) > 1 {
			edgeVisited = maps.Clone(visited)
		}
		if !tr.traceValue(v, t, edgeVisited) {
			return false, true
		}
		found = true
	}
	return found, true
}

// traceAddressAt traces addr, an address that read uses, such as the
// receiver of Logger.Info, which has a pointer receiver. A nil read stands
// for every read of addr. ok is false when addr is not one whose stores
// storesReaching can find.
func (tr *tracer) traceAddressAt(addr ssa.Value, read ssa.Instruction, t tracerType, visited map[ssa.Value]bool) (found, ok bool) {
	if typeutil.IsEvent(addr.Type()) {
		return false, false // A *zerolog.Event is the value itself, not a variable it is read from
	}
	switch a := addr.(type) {
	case *ssa.Alloc, *ssa.FieldAddr, *ssa.IndexAddr, *ssa.FreeVar:
	case *ssa.Parameter:
		if _, isPtr := a.Type().Underlying().(*types.Pointer); !isPtr {
			return false, false
		}
	default:
		return false, false
	}
	return tr.traceStoredValues(addr, read, t, visited)
}

// traceArguments traces the arguments passed for param, when its function is
// a func literal that is only ever called where it is written, as an IIFE
// is. Every call must pass an argument with context.
//
//	func(e *zerolog.Event) {
//	    e.Msg("x")              // e is the argument below
//	}(logger.Info().Ctx(ctx))
func (tr *tracer) traceArguments(param *ssa.Parameter, t tracerType, visited map[ssa.Value]bool) bool {
	fn := param.Parent()
	parent := fn.Parent()
	if parent == nil {
		return false // A declared function can be called from anywhere
	}
	idx := slices.Index(fn.Params, param)
	var calls []*ssa.CallCommon
	for instr := range instrsIn(parent) {
		if mc, ok := instr.(*ssa.MakeClosure); ok && mc.Fn == fn {
			continue // Where the closure is made
		}
		call, isCall := instr.(ssa.CallInstruction)
		for _, op := range instr.Operands(nil) {
			if !traceIsFuncValue(*op, fn) {
				continue
			}
			if !isCall || op != &call.Common().Value {
				return false // The func is passed on, and may be called where it cannot be seen
			}
			calls = append(calls, call.Common())
		}
	}
	if len(calls) == 0 {
		return false
	}
	for _, cc := range calls {
		if !tr.traceValue(cc.Args[idx], t, maps.Clone(visited)) {
			return false
		}
	}
	return true
}

// traceIsFuncValue reports whether v is fn, or a closure of it.
func traceIsFuncValue(v ssa.Value, fn *ssa.Function) bool {
	if mc, ok := v.(*ssa.MakeClosure); ok {
		return mc.Fn == fn
	}
	return v == fn
}

// traceFreeVar traces a FreeVar back to the value bound in MakeClosure.
func (tr *tracer) traceFreeVar(fv *ssa.FreeVar, t tracerType, visited map[ssa.Value]bool) bool {
	fn := fv.Parent()
	if fn == nil {
		return false
	}

	idx := slices.Index(fn.FreeVars, fv)
	if idx < 0 {
		return false
	}

	parent := fn.Parent()
	if parent == nil {
		return false
	}

	for instr := range instrsIn(parent) {
		mc, ok := instr.(*ssa.MakeClosure)
		if !ok {
			continue
		}
		closureFn, ok := mc.Fn.(*ssa.Function)
		if !ok || closureFn != fn {
			continue
		}
		if idx < len(mc.Bindings) && tr.traceValue(mc.Bindings[idx], t, visited) {
			return true
		}
	}
	return false
}

// traceReceiver traces recv, the receiver of call, a method call. It is the
// first argument, or what a method value is bound to. A pointer receiver that
// is a variable, or a field or element of one, holds what the stores that
// reach the call left there.
func (tr *tracer) traceReceiver(recv ssa.Value, call *ssa.Call, t tracerType, visited map[ssa.Value]bool) bool {
	if found, ok := tr.traceAddressAt(recv, call, t, visited); ok {
		return found
	}
	return tr.traceValue(recv, t, visited)
}

// traceReturns reports whether every value fn returns has context, as for an
// IIFE (Immediately Invoked Function Expression) or a func passed to
// Logger.UpdateContext.
func (tr *tracer) traceReturns(fn *ssa.Function, t tracerType, visited map[ssa.Value]bool) bool {
	results := fn.Signature.Results()
	if results == nil || results.Len() == 0 {
		return false
	}

	// Only trace if return type is Event, Logger, or Context
	retType := results.At(0).Type()
	if !typeutil.IsEvent(retType) && !typeutil.IsLogger(retType) && !typeutil.IsContext(retType) {
		return false
	}

	// Find all return statements and trace their values
	hasReturn := false
	for instr := range instrsIn(fn) {
		ret, ok := instr.(*ssa.Return)
		if !ok || len(ret.Results) == 0 {
			continue
		}

		hasReturn = true
		retVisited := maps.Clone(visited)
		if !tr.traceValue(ret.Results[0], t, retVisited) {
			return false
		}
	}

	return hasReturn
}
