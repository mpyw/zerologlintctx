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

// isCtxSetBy reports whether a call to callee, with receiver recv, gives the
// value t traces its context:
//   - Event tracer: Event.Ctx(ctx), Context.Ctx(ctx), zerolog.Ctx(ctx)
//   - Logger tracer: zerolog.Ctx(ctx)
//   - Context tracer: Context.Ctx(ctx)
func (t tracerType) isCtxSetBy(callee *ssa.Function, recv *types.Var) bool {
	isCtxMethod := callee.Name() == typeutil.CtxMethod && recv != nil
	switch t {
	case tracerEvent:
		onEventOrContext := isCtxMethod &&
			(tracerEvent.isTracedType(recv.Type()) || tracerContext.isTracedType(recv.Type()))
		return onEventOrContext || isCtxCallee(callee)
	case tracerLogger:
		return isCtxCallee(callee)
	case tracerContext:
		return isCtxMethod && tracerContext.isTracedType(recv.Type())
	}
	return false
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

// handoffAt returns the tracer to go on with from the receiver of call, when
// call is a handoff that t follows.
func (t tracerType) handoffAt(call *ssa.Call, callee *ssa.Function, recv *types.Var) (tracerType, bool) {
	if recv == nil || len(call.Call.Args) == 0 {
		return 0, false
	}
	for _, h := range tracerHandoffs[t] {
		if h.from.isTracedType(recv.Type()) && h.to.isReturnedBy(callee.Signature) {
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
type tracer struct{}

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
//	│     │                                                            │
//	│     ├─ Is *ssa.Call?                                             │
//	│     │     │                                                      │
//	│     │     ├─ No static callee? → trace receiver                 │
//	│     │     │                                                      │
//	│     │     ├─ Is IIFE? → trace return values                     │
//	│     │     │                                                      │
//	│     │     ├─ Sets context? → return true                        │
//	│     │     ├─ Handoff? → trace receiver with its own tracer      │
//	│     │     └─ Receiver of the traced type? → trace receiver      │
//	│     │                                                            │
//	│     └─ Not a Call → traceCommon (Phi, UnOp, Alloc, etc.)        │
//	└─────────────────────────────────────────────────────────────────┘
func (tr *tracer) traceValue(v ssa.Value, t tracerType, visited map[ssa.Value]bool) bool {
	if visited[v] {
		return false
	}
	visited[v] = true

	call, ok := v.(*ssa.Call)
	if !ok {
		return tr.traceCommon(v, t, visited)
	}

	callee := call.Call.StaticCallee()
	if callee == nil {
		return tr.traceReceiver(call, t, visited)
	}

	// Check if this is an IIFE (Immediately Invoked Function Expression)
	if _, ok := call.Call.Value.(*ssa.MakeClosure); ok {
		if tr.traceIIFEReturns(callee, t, visited) {
			return true
		}
	}

	recv := call.Call.Signature().Recv()

	if t.isCtxSetBy(callee, recv) {
		return true
	}
	if next, ok := t.handoffAt(call, callee, recv); ok {
		return tr.traceValue(call.Call.Args[0], next, visited)
	}

	// Continue tracing through receiver if type matches
	if recv != nil && t.isTracedType(recv.Type()) {
		return tr.traceReceiver(call, t, visited)
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
	}

	// Handle simple wrapper types that just need inner value tracing
	if inner := unwrapInner(v); inner != nil {
		return tr.traceValue(inner, t, visited)
	}

	return false
}

// =============================================================================
// Phi Node Handling
// =============================================================================

// tracePhi handles SSA Phi nodes where multiple control flow paths merge.
//
// All edges must have context set for the Phi node to be considered valid.
// Cyclic edges and nil constants are skipped.
func (tr *tracer) tracePhi(phi *ssa.Phi, t tracerType, visited map[ssa.Value]bool) bool {
	if len(phi.Edges) == 0 {
		return false
	}

	hasValidEdge := false
	for _, edge := range phi.Edges {
		// Skip edges that would cycle back to this Phi
		if edgeLeadsTo(edge, phi, visited) {
			continue
		}

		// Skip nil constant edges
		if isNilConst(edge) {
			continue
		}

		hasValidEdge = true

		// Clone visited for independent tracing of each branch
		edgeVisited := maps.Clone(visited)
		if !tr.traceValue(edge, t, edgeVisited) {
			return false
		}
	}

	return hasValidEdge
}

// =============================================================================
// Special Value Handling
// =============================================================================

// traceUnOp handles SSA unary operations, especially pointer dereferences.
func (tr *tracer) traceUnOp(unop *ssa.UnOp, t tracerType, visited map[ssa.Value]bool) bool {
	if unop.Op == token.MUL {
		storedValues := findAllStoredValues(unop.X)
		if len(storedValues) > 0 {
			return tr.traceAllStoredValues(storedValues, t, visited)
		}
	}
	return tr.traceValue(unop.X, t, visited)
}

// traceAlloc handles SSA Alloc nodes (local variable allocation).
func (tr *tracer) traceAlloc(alloc *ssa.Alloc, t tracerType, visited map[ssa.Value]bool) bool {
	storedValues := findAllStoredValues(alloc)
	if len(storedValues) > 0 {
		return tr.traceAllStoredValues(storedValues, t, visited)
	}
	return false
}

// traceAllStoredValues traces all stored values and returns true only if ALL have context.
// This is similar to Phi node handling - all paths must have context.
func (tr *tracer) traceAllStoredValues(storedValues []ssa.Value, t tracerType, visited map[ssa.Value]bool) bool {
	for _, stored := range storedValues {
		// Clone visited for independent tracing of each store
		storeVisited := maps.Clone(visited)
		if !tr.traceValue(stored, t, storeVisited) {
			return false
		}
	}
	return true
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

// traceReceiver traces the receiver (first argument) of a method call.
func (tr *tracer) traceReceiver(call *ssa.Call, t tracerType, visited map[ssa.Value]bool) bool {
	if len(call.Call.Args) > 0 {
		return tr.traceValue(call.Call.Args[0], t, visited)
	}
	return false
}

// traceIIFEReturns traces through an IIFE (Immediately Invoked Function Expression).
func (tr *tracer) traceIIFEReturns(fn *ssa.Function, t tracerType, visited map[ssa.Value]bool) bool {
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
