package ssa

import (
	"go/token"
	"maps"

	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/ssa/tracer"
	"github.com/mpyw/zerologlintctx/internal/typeutil"
)

// =============================================================================
// Unified Value Tracing
// =============================================================================

// traceValue is the unified tracing function that works with any tracer.
// It handles the common tracing logic and delegates type-specific checks
// to the provided tracer strategy.
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
//	│     │     └─ Ask tracer.CheckContext()                          │
//	│     │           │                                                │
//	│     │           ├─ Found → return true                          │
//	│     │           ├─ Delegate → traceValue(delegateVal, delegate) │
//	│     │           └─ Continue → trace receiver if type matches    │
//	│     │                                                            │
//	│     └─ Not a Call → traceCommon (Phi, UnOp, Alloc, etc.)        │
//	└─────────────────────────────────────────────────────────────────┘
func (c *Checker) traceValue(v ssa.Value, t tracer.Tracer, visited map[ssa.Value]bool) bool {
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

	// Ask the tracer to check for context
	result := t.CheckContext(call, callee, recv)
	if result.IsFound() {
		return true
	}
	if result.IsDelegate() {
		delegate, delegateVal := result.Delegate()
		return c.traceValue(delegateVal, delegate, visited)
	}

	// Continue tracing through receiver if type matches
	if t.ContinueOnReceiverType(recv) {
		return c.traceReceiver(call, visited, t)
	}

	return false
}

// =============================================================================
// Common SSA Value Handling
// =============================================================================

// traceCommon handles common SSA value types (Phi, UnOp, FreeVar, etc.).
func (c *Checker) traceCommon(v ssa.Value, visited map[ssa.Value]bool, t tracer.Tracer) bool {
	// Handle special cases that require custom logic
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
// Phi nodes appear when a variable can have different values depending on
// which control flow path was taken:
//
//	var e *zerolog.Event
//	if cond {
//	    e = log.Info().Ctx(ctx)  // Edge 1: has context
//	} else {
//	    e = log.Warn().Ctx(ctx)  // Edge 2: has context
//	}
//	e.Msg("test")  // Phi node merges both edges
//
// SSA representation:
//
//	       ┌─────────────────┐
//	       │  if cond goto   │
//	       │    then, else   │
//	       └────────┬────────┘
//	                │
//	     ┌──────────┴──────────┐
//	     ▼                     ▼
//	┌─────────┐          ┌─────────┐
//	│ t0=Info │          │ t1=Warn │
//	│ t2=Ctx  │          │ t3=Ctx  │
//	└────┬────┘          └────┬────┘
//	     │                    │
//	     └────────┬───────────┘
//	              ▼
//	      ┌──────────────┐
//	      │ t4 = Phi(t2, │  ← ALL edges must have context
//	      │          t3) │
//	      └──────────────┘
//
// Skipped edges:
//   - Cyclic (loop back-edges): depend on initial value
//   - Nil constants: would panic before method call
func (c *Checker) tracePhi(phi *ssa.Phi, visited map[ssa.Value]bool, t tracer.Tracer) bool {
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
		edgeVisited := make(map[ssa.Value]bool)
		maps.Copy(edgeVisited, visited)

		if !c.traceValue(edge, t, edgeVisited) {
			return false
		}
	}

	return hasValidEdge
}

// isNilConst checks if a value is a nil constant.
func isNilConst(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	if !ok {
		return false
	}
	return c.Value == nil
}

// edgeLeadsTo checks if tracing this edge would eventually lead back to target.
func edgeLeadsTo(edge ssa.Value, target *ssa.Phi, visited map[ssa.Value]bool) bool {
	seen := make(map[ssa.Value]bool)
	for k := range visited {
		seen[k] = true
	}
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
func (c *Checker) traceUnOp(unop *ssa.UnOp, visited map[ssa.Value]bool, t tracer.Tracer) bool {
	if unop.Op == token.MUL {
		if stored := findStoredValue(unop.X); stored != nil {
			return c.traceValue(stored, t, visited)
		}
	}
	return c.traceValue(unop.X, t, visited)
}

// traceAlloc handles SSA Alloc nodes (local variable allocation).
func (c *Checker) traceAlloc(alloc *ssa.Alloc, visited map[ssa.Value]bool, t tracer.Tracer) bool {
	if stored := findStoredValue(alloc); stored != nil {
		return c.traceValue(stored, t, visited)
	}
	return false
}

// traceFreeVar traces a FreeVar back to the value bound in MakeClosure.
//
// FreeVars are variables captured from an enclosing function scope:
//
//	func outer(ctx context.Context) {
//	    e := log.Info().Ctx(ctx)      // e is captured
//	    go func() {
//	        e.Msg("from closure")     // e is a FreeVar here
//	    }()
//	}
//
// SSA representation:
//
//	outer:
//	    t0 = (*Logger).Info(log)
//	    t1 = (*Event).Ctx(t0, ctx)
//	    t2 = make closure outer$1 [t1]  ← t1 bound to FreeVar
//	    go t2()
//
//	outer$1:
//	    t0 = FreeVar <*Event>           ← refers to t1 from outer
//	    (*Event).Msg(t0, "from closure")
//
// We find the MakeClosure in the parent and trace the bound value.
func (c *Checker) traceFreeVar(fv *ssa.FreeVar, visited map[ssa.Value]bool, t tracer.Tracer) bool {
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
func (c *Checker) traceReceiver(call *ssa.Call, visited map[ssa.Value]bool, t tracer.Tracer) bool {
	if len(call.Call.Args) > 0 {
		return c.traceValue(call.Call.Args[0], t, visited)
	}
	return false
}

// traceIIFEReturns traces through an IIFE (Immediately Invoked Function Expression).
func (c *Checker) traceIIFEReturns(fn *ssa.Function, visited map[ssa.Value]bool, t tracer.Tracer) bool {
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
			if !ok {
				continue
			}
			if len(ret.Results) == 0 {
				continue
			}

			hasReturn = true

			retVisited := make(map[ssa.Value]bool)
			maps.Copy(retVisited, visited)

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

// findStoredValue finds the value that was stored at the given address.
//
// This handles cases where values are accessed through struct fields or
// local variables:
//
//	type holder struct{ event *zerolog.Event }
//
//	h := holder{event: log.Info().Ctx(ctx)}
//	h.event.Msg("test")
//
// SSA representation:
//
//	t0 = local holder (h)
//	t1 = &t0.event              ← FieldAddr
//	t2 = (*Logger).Info(log)
//	t3 = (*Event).Ctx(t2, ctx)
//	*t1 = t3                    ← Store: t3 stored at t1
//	t4 = &t0.event              ← FieldAddr (same field)
//	t5 = *t4                    ← UnOp: load from t4
//	(*Event).Msg(t5, "test")
//
// When we trace t5 (UnOp), we find the Store to a matching address (t1)
// and trace the stored value (t3).
func findStoredValue(addr ssa.Value) ssa.Value {
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

	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			store, ok := instr.(*ssa.Store)
			if !ok {
				continue
			}
			if addressesMatch(store.Addr, addr) {
				return store.Val
			}
		}
	}
	return nil
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
	if ok1 && ok2 {
		if ia1.X == ia2.X {
			c1, ok1 := ia1.Index.(*ssa.Const)
			c2, ok2 := ia2.Index.(*ssa.Const)
			if ok1 && ok2 {
				return c1.Value == c2.Value
			}
		}
	}

	return false
}
