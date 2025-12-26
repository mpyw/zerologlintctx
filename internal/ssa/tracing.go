package ssa

import (
	"go/token"
	"go/types"
	"maps"

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

// =============================================================================
// Context Checking Result
// =============================================================================

// checkResult represents the outcome of checking a call for context.
type checkResult struct {
	found       bool       // Context was definitely found
	delegate    bool       // Should delegate to another tracer
	delegateTo  tracerType // Which tracer to delegate to
	delegateVal ssa.Value  // Value to continue tracing
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
//	│     │     └─ checkContext()                                     │
//	│     │           │                                                │
//	│     │           ├─ Found → return true                          │
//	│     │           ├─ Delegate → traceValue(delegateVal, newTracer)│
//	│     │           └─ Continue → trace receiver if type matches    │
//	│     │                                                            │
//	│     └─ Not a Call → traceCommon (Phi, UnOp, Alloc, etc.)        │
//	└─────────────────────────────────────────────────────────────────┘
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
	result := c.checkContext(call, callee, recv, t)
	if result.found {
		return true
	}
	if result.delegate {
		return c.traceValue(result.delegateVal, result.delegateTo, visited)
	}

	// Continue tracing through receiver if type matches
	if c.shouldContinueOnReceiver(recv, t) {
		return c.traceReceiver(call, visited, t)
	}

	return false
}

// checkContext examines a call and determines if context was set.
//
// Context can be set via:
//   - Event.Ctx(ctx) or Context.Ctx(ctx): Direct context setting
//   - zerolog.Ctx(ctx): Returns Logger with context
//
// Delegation happens when type changes:
//   - Logger.Info() returns Event: delegate to logger tracer
//   - Context.Logger() returns Logger: delegate to context tracer
//   - Logger.With() returns Context: delegate to logger tracer
func (c *Checker) checkContext(
	call *ssa.Call,
	callee *ssa.Function,
	recv *types.Var,
	t tracerType,
) checkResult {
	switch t {
	case tracerEvent:
		return c.checkContextForEvent(call, callee, recv)
	case tracerLogger:
		return c.checkContextForLogger(call, callee, recv)
	case tracerContext:
		return c.checkContextForContext(call, callee, recv)
	}
	return checkResult{}
}

// checkContextForEvent checks context for Event tracing.
func (c *Checker) checkContextForEvent(
	call *ssa.Call,
	callee *ssa.Function,
	recv *types.Var,
) checkResult {
	// Event.Ctx(ctx) or Context.Ctx(ctx) - direct context setting
	if callee.Name() == typeutil.CtxMethod && recv != nil {
		if typeutil.IsEvent(recv.Type()) || typeutil.IsContext(recv.Type()) {
			return checkResult{found: true}
		}
	}

	// zerolog.Ctx(ctx) - returns Logger with context
	if typeutil.IsCtxFunc(callee) {
		return checkResult{found: true}
	}

	// Logger methods that return Event - delegate to logger tracer
	if recv != nil && typeutil.IsLogger(recv.Type()) && typeutil.ReturnsEvent(callee) {
		if len(call.Call.Args) > 0 {
			return checkResult{delegate: true, delegateTo: tracerLogger, delegateVal: call.Call.Args[0]}
		}
	}

	// Context methods that return Logger - delegate to context tracer
	if recv != nil && typeutil.IsContext(recv.Type()) && typeutil.ReturnsLogger(callee) {
		if len(call.Call.Args) > 0 {
			return checkResult{delegate: true, delegateTo: tracerContext, delegateVal: call.Call.Args[0]}
		}
	}

	return checkResult{}
}

// checkContextForLogger checks context for Logger tracing.
func (c *Checker) checkContextForLogger(
	call *ssa.Call,
	callee *ssa.Function,
	recv *types.Var,
) checkResult {
	// zerolog.Ctx(ctx) - returns Logger with context
	if typeutil.IsCtxFunc(callee) {
		return checkResult{found: true}
	}

	// Context methods that return Logger - delegate to context tracer
	if recv != nil && typeutil.IsContext(recv.Type()) && typeutil.ReturnsLogger(callee) {
		if len(call.Call.Args) > 0 {
			return checkResult{delegate: true, delegateTo: tracerContext, delegateVal: call.Call.Args[0]}
		}
	}

	// Logger.With() returns Context - continue tracing parent Logger
	if recv != nil && typeutil.IsLogger(recv.Type()) && typeutil.ReturnsContext(callee) {
		if len(call.Call.Args) > 0 {
			return checkResult{delegate: true, delegateTo: tracerLogger, delegateVal: call.Call.Args[0]}
		}
	}

	return checkResult{}
}

// checkContextForContext checks context for Context tracing.
func (c *Checker) checkContextForContext(
	call *ssa.Call,
	callee *ssa.Function,
	recv *types.Var,
) checkResult {
	// Context.Ctx(ctx) - direct context setting
	if callee.Name() == typeutil.CtxMethod && recv != nil && typeutil.IsContext(recv.Type()) {
		return checkResult{found: true}
	}

	// Logger.With() returns Context - delegate to logger tracer
	if recv != nil && typeutil.IsLogger(recv.Type()) && typeutil.ReturnsContext(callee) {
		if len(call.Call.Args) > 0 {
			return checkResult{delegate: true, delegateTo: tracerLogger, delegateVal: call.Call.Args[0]}
		}
	}

	return checkResult{}
}

// shouldContinueOnReceiver returns true if we should continue tracing
// through the receiver for the given tracer type.
func (c *Checker) shouldContinueOnReceiver(recv *types.Var, t tracerType) bool {
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

// unwrapInner extracts the inner value from SSA wrapper types.
func unwrapInner(v ssa.Value) ssa.Value {
	switch val := v.(type) {
	case *ssa.Extract:
		return val.Tuple
	case *ssa.MakeInterface:
		return val.X
	case *ssa.TypeAssert:
		return val.X
	case *ssa.FieldAddr:
		return val.X
	case *ssa.Field:
		return val.X
	case *ssa.IndexAddr:
		return val.X
	case *ssa.Index:
		return val.X
	case *ssa.Lookup:
		return val.X
	}
	return nil
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

// isNilConst checks if a value is a nil constant.
func isNilConst(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	return ok && c.Value == nil
}

// edgeLeadsTo checks if tracing this edge would eventually lead back to target.
func edgeLeadsTo(edge ssa.Value, target *ssa.Phi, visited map[ssa.Value]bool) bool {
	seen := maps.Clone(visited)
	return edgeLeadsToImpl(edge, target, seen)
}

func edgeLeadsToImpl(v ssa.Value, target *ssa.Phi, seen map[ssa.Value]bool) bool {
	if v == target {
		return true
	}
	if seen[v] {
		return false
	}
	seen[v] = true

	switch val := v.(type) {
	case *ssa.Call:
		if len(val.Call.Args) > 0 {
			return edgeLeadsToImpl(val.Call.Args[0], target, seen)
		}
		return false
	case *ssa.Phi:
		for _, edge := range val.Edges {
			if edgeLeadsToImpl(edge, target, seen) {
				return true
			}
		}
		return false
	}

	if inner := unwrapInner(v); inner != nil {
		return edgeLeadsToImpl(inner, target, seen)
	}

	return false
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

	idx := -1
	for i, v := range fn.FreeVars {
		if v == fv {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false
	}

	parent := fn.Parent()
	if parent == nil {
		return false
	}

	for _, block := range parent.Blocks {
		for _, instr := range block.Instrs {
			mc, ok := instr.(*ssa.MakeClosure)
			if !ok {
				continue
			}
			closureFn, ok := mc.Fn.(*ssa.Function)
			if !ok || closureFn != fn {
				continue
			}
			if idx < len(mc.Bindings) {
				if c.traceValue(mc.Bindings[idx], t, visited) {
					return true
				}
			}
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
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
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
	}

	return hasReturn
}

// =============================================================================
// Store Tracking
// =============================================================================

// findAllStoredValues finds all values that were stored at the given address.
// Multiple stores can occur in different control flow paths (e.g., if/else branches).
// All stored values must be checked for context to handle cases like:
//
//	e := logger.Info().Ctx(ctx)
//	ptr := &e
//	if cond {
//	    *ptr = logger.Warn()  // no ctx in this branch!
//	}
//	(*ptr).Msg("msg")  // should report: one branch lacks ctx
//
// Self-referential stores (where the value loads from the same address) are skipped:
//
//	e := logger.Info().Ctx(ctx)
//	ptr := &e
//	for i := 0; i < 3; i++ {
//	    *ptr = (*ptr).Str("k", "v")  // self-referential: skipped
//	}
//	(*ptr).Msg("msg")  // only traces initial store, finds ctx
func findAllStoredValues(addr ssa.Value) []ssa.Value {
	var fn *ssa.Function
	switch v := addr.(type) {
	case *ssa.FieldAddr:
		fn = v.Parent()
	case *ssa.IndexAddr:
		fn = v.Parent()
	case *ssa.Alloc:
		fn = v.Parent()
	default:
		if instr, ok := addr.(ssa.Instruction); ok {
			fn = instr.Parent()
		}
	}
	if fn == nil {
		return nil
	}

	var storedValues []ssa.Value
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			store, ok := instr.(*ssa.Store)
			if !ok {
				continue
			}
			if addressesMatch(store.Addr, addr) {
				// Skip self-referential stores where the value loads from the same address.
				// These just transform the existing value (e.g., *ptr = (*ptr).Str(...))
				// and would cause infinite recursion during tracing.
				if valueLoadsFrom(store.Val, addr) {
					continue
				}
				storedValues = append(storedValues, store.Val)
			}
		}
	}
	return storedValues
}

// valueLoadsFrom checks if a value (or its receiver chain) loads from the given address.
// This is used to detect self-referential stores like: *ptr = (*ptr).Str(...)
func valueLoadsFrom(v ssa.Value, addr ssa.Value) bool {
	switch val := v.(type) {
	case *ssa.UnOp:
		// Check if this is a dereference of the address
		if val.Op == token.MUL && addressesMatch(val.X, addr) {
			return true
		}
		return valueLoadsFrom(val.X, addr)
	case *ssa.Call:
		// Check receiver (first argument for method calls)
		if len(val.Call.Args) > 0 {
			return valueLoadsFrom(val.Call.Args[0], addr)
		}
	case *ssa.Phi:
		// Check all edges
		for _, edge := range val.Edges {
			if valueLoadsFrom(edge, addr) {
				return true
			}
		}
	}
	return false
}

// addressesMatch checks if two addresses refer to the same memory location.
func addressesMatch(a, b ssa.Value) bool {
	if a == b {
		return true
	}

	fa1, ok1 := a.(*ssa.FieldAddr)
	fa2, ok2 := b.(*ssa.FieldAddr)
	if ok1 && ok2 {
		return fa1.X == fa2.X && fa1.Field == fa2.Field
	}

	ia1, ok1 := a.(*ssa.IndexAddr)
	ia2, ok2 := b.(*ssa.IndexAddr)
	if ok1 && ok2 && ia1.X == ia2.X {
		c1, ok1 := ia1.Index.(*ssa.Const)
		c2, ok2 := ia2.Index.(*ssa.Const)
		if ok1 && ok2 {
			return c1.Value == c2.Value
		}
	}

	return false
}
