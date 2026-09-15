package ssa

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
	//declscope:package
	tracerEvent tracerType = iota
	// tracerLogger traces zerolog.Logger values.
	tracerLogger
	// tracerContext traces zerolog.Context values.
	tracerContext
)

// =============================================================================
// Context Checking Result
// =============================================================================

// tracingDelegation hands tracing over to a different tracer, which is needed
// whenever the traced value changes zerolog type. The zero value (nil val)
// means "no tracingDelegation".
type tracingDelegation struct {
	to  tracerType // Tracer to switch to
	val ssa.Value  // Value to continue tracing
}

// traceResult represents the outcome of checking a call for context.
//
// tracingDelegation is embedded so a result can be built with promoted field keys,
// e.g. traceResult{to: tracerLogger, val: recv}.
type traceResult struct {
	found bool // Context was definitely found
	tracingDelegation
}

// =============================================================================
// Unified Value Tracing
// =============================================================================

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
//	│     │     └─ traceContext()                                     │
//	│     │           │                                                │
//	│     │           ├─ Found → return true                          │
//	│     │           ├─ Delegate → traceValue(result.val, result.to) │
//	│     │           └─ Continue → trace receiver if type matches    │
//	│     │                                                            │
//	│     └─ Not a Call → traceCommon (Phi, UnOp, Alloc, etc.)        │
//	└─────────────────────────────────────────────────────────────────┘
//
//declscope:package
func (c *Checker) traceValue(v ssa.Value, t tracerType, visited map[ssa.Value]bool) bool {
	if visited[v] {
		return false
	}
	visited[v] = true

	call, ok := v.(*ssa.Call)
	if !ok {
		return c.traceCommon(v, visited, t)
	}

	callee := call.Call.StaticCallee()
	if callee == nil {
		return c.traceReceiver(call, visited, t)
	}

	// Check if this is an IIFE (Immediately Invoked Function Expression)
	if _, ok := call.Call.Value.(*ssa.MakeClosure); ok {
		if c.traceIIFEReturns(callee, visited, t) {
			return true
		}
	}

	recv := call.Call.Signature().Recv()

	// Check for context
	result := c.traceContext(call, callee, recv, t)
	if result.found {
		return true
	}
	if result.val != nil {
		return c.traceValue(result.val, result.to, visited)
	}

	// Continue tracing through receiver if type matches
	if c.tracerContinuesOnReceiver(recv, t) {
		return c.traceReceiver(call, visited, t)
	}

	return false
}

// traceContext examines a call and determines if context was set.
//
// Context can be set via:
//   - Event.Ctx(ctx) or Context.Ctx(ctx): Direct context setting
//   - zerolog.Ctx(ctx): Returns Logger with context
//
// Delegation happens when type changes:
//   - Logger.Info() returns Event: delegate to logger tracer
//   - Context.Logger() returns Logger: delegate to context tracer
//   - Logger.With() returns Context: delegate to logger tracer
func (c *Checker) traceContext(
	call *ssa.Call,
	callee *ssa.Function,
	recv *types.Var,
	t tracerType,
) traceResult {
	switch t {
	case tracerEvent:
		return c.traceContextForEvent(call, callee, recv)
	case tracerLogger:
		return c.traceContextForLogger(call, callee, recv)
	case tracerContext:
		return c.traceContextForContext(call, callee, recv)
	}
	return traceResult{}
}

// traceContextForEvent checks context for Event tracing.
func (c *Checker) traceContextForEvent(
	call *ssa.Call,
	callee *ssa.Function,
	recv *types.Var,
) traceResult {
	// Event.Ctx(ctx) or Context.Ctx(ctx) - direct context setting
	if callee.Name() == typeutil.CtxMethod && recv != nil {
		if typeutil.IsEvent(recv.Type()) || typeutil.IsContext(recv.Type()) {
			return traceResult{found: true}
		}
	}

	// zerolog.Ctx(ctx) - returns Logger with context
	if typeutil.IsCtxFunc(callee) {
		return traceResult{found: true}
	}

	// Logger methods that return Event - delegate to logger tracer
	if recv != nil && typeutil.IsLogger(recv.Type()) && typeutil.ReturnsEvent(callee) {
		if len(call.Call.Args) > 0 {
			return traceResult{to: tracerLogger, val: call.Call.Args[0]}
		}
	}

	// Context methods that return Logger - delegate to context tracer
	if recv != nil && typeutil.IsContext(recv.Type()) && typeutil.ReturnsLogger(callee) {
		if len(call.Call.Args) > 0 {
			return traceResult{to: tracerContext, val: call.Call.Args[0]}
		}
	}

	return traceResult{}
}

// traceContextForLogger checks context for Logger tracing.
func (c *Checker) traceContextForLogger(
	call *ssa.Call,
	callee *ssa.Function,
	recv *types.Var,
) traceResult {
	// zerolog.Ctx(ctx) - returns Logger with context
	if typeutil.IsCtxFunc(callee) {
		return traceResult{found: true}
	}

	// Context methods that return Logger - delegate to context tracer
	if recv != nil && typeutil.IsContext(recv.Type()) && typeutil.ReturnsLogger(callee) {
		if len(call.Call.Args) > 0 {
			return traceResult{to: tracerContext, val: call.Call.Args[0]}
		}
	}

	// Logger.With() returns Context - continue tracing parent Logger
	if recv != nil && typeutil.IsLogger(recv.Type()) && typeutil.ReturnsContext(callee) {
		if len(call.Call.Args) > 0 {
			return traceResult{to: tracerLogger, val: call.Call.Args[0]}
		}
	}

	return traceResult{}
}

// traceContextForContext checks context for Context tracing.
func (c *Checker) traceContextForContext(
	call *ssa.Call,
	callee *ssa.Function,
	recv *types.Var,
) traceResult {
	// Context.Ctx(ctx) - direct context setting
	if callee.Name() == typeutil.CtxMethod && recv != nil && typeutil.IsContext(recv.Type()) {
		return traceResult{found: true}
	}

	// Logger.With() returns Context - delegate to logger tracer
	if recv != nil && typeutil.IsLogger(recv.Type()) && typeutil.ReturnsContext(callee) {
		if len(call.Call.Args) > 0 {
			return traceResult{to: tracerLogger, val: call.Call.Args[0]}
		}
	}

	return traceResult{}
}

// tracerContinuesOnReceiver returns true if we should continue tracing
// through the receiver for the given tracer type.
func (c *Checker) tracerContinuesOnReceiver(recv *types.Var, t tracerType) bool {
	if recv == nil {
		return false
	}
	switch t {
	case tracerEvent:
		return typeutil.IsEvent(recv.Type())
	case tracerLogger:
		return typeutil.IsLogger(recv.Type())
	case tracerContext:
		return typeutil.IsContext(recv.Type())
	}
	return false
}

// =============================================================================
// Common SSA Value Handling
// =============================================================================

// traceCommon handles common SSA value types (Phi, UnOp, FreeVar, etc.).
func (c *Checker) traceCommon(v ssa.Value, visited map[ssa.Value]bool, t tracerType) bool {
	switch val := v.(type) {
	case *ssa.Phi:
		return c.tracePhi(val, visited, t)
	case *ssa.UnOp:
		return c.traceUnOp(val, visited, t)
	case *ssa.Alloc:
		return c.traceAlloc(val, visited, t)
	case *ssa.FreeVar:
		return c.traceFreeVar(val, visited, t)
	}

	// Handle simple wrapper types that just need inner value tracing
	if inner := unwrapInner(v); inner != nil {
		return c.traceValue(inner, t, visited)
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
func (c *Checker) tracePhi(phi *ssa.Phi, visited map[ssa.Value]bool, t tracerType) bool {
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
		if !c.traceValue(edge, t, edgeVisited) {
			return false
		}
	}

	return hasValidEdge
}

// =============================================================================
// Special Value Handling
// =============================================================================

// traceUnOp handles SSA unary operations, especially pointer dereferences.
func (c *Checker) traceUnOp(unop *ssa.UnOp, visited map[ssa.Value]bool, t tracerType) bool {
	if unop.Op == token.MUL {
		storedValues := findAllStoredValues(unop.X)
		if len(storedValues) > 0 {
			return c.traceAllStoredValues(storedValues, visited, t)
		}
	}
	return c.traceValue(unop.X, t, visited)
}

// traceAlloc handles SSA Alloc nodes (local variable allocation).
func (c *Checker) traceAlloc(alloc *ssa.Alloc, visited map[ssa.Value]bool, t tracerType) bool {
	storedValues := findAllStoredValues(alloc)
	if len(storedValues) > 0 {
		return c.traceAllStoredValues(storedValues, visited, t)
	}
	return false
}

// traceAllStoredValues traces all stored values and returns true only if ALL have context.
// This is similar to Phi node handling - all paths must have context.
func (c *Checker) traceAllStoredValues(storedValues []ssa.Value, visited map[ssa.Value]bool, t tracerType) bool {
	for _, stored := range storedValues {
		// Clone visited for independent tracing of each store
		storeVisited := maps.Clone(visited)
		if !c.traceValue(stored, t, storeVisited) {
			return false
		}
	}
	return true
}

// traceFreeVar traces a FreeVar back to the value bound in MakeClosure.
func (c *Checker) traceFreeVar(fv *ssa.FreeVar, visited map[ssa.Value]bool, t tracerType) bool {
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
		if idx < len(mc.Bindings) && c.traceValue(mc.Bindings[idx], t, visited) {
			return true
		}
	}
	return false
}

// traceReceiver traces the receiver (first argument) of a method call.
func (c *Checker) traceReceiver(call *ssa.Call, visited map[ssa.Value]bool, t tracerType) bool {
	if len(call.Call.Args) > 0 {
		return c.traceValue(call.Call.Args[0], t, visited)
	}
	return false
}

// traceIIFEReturns traces through an IIFE (Immediately Invoked Function Expression).
func (c *Checker) traceIIFEReturns(fn *ssa.Function, visited map[ssa.Value]bool, t tracerType) bool {
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
		if !c.traceValue(ret.Results[0], t, retVisited) {
			return false
		}
	}

	return hasReturn
}

// =============================================================================
// Store Tracking
// =============================================================================
