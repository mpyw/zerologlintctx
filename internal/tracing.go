package internal

import (
	"go/token"
	"go/types"
	"maps"

	"golang.org/x/tools/go/ssa"
)

// =============================================================================
// Value Tracing - Strategy Pattern
// =============================================================================
//
// The tracing system follows SSA values backwards to find if context was set.
// It uses a Strategy Pattern with three tracers for zerolog types:
//
//   eventTracer   - traces *zerolog.Event values
//   loggerTracer  - traces zerolog.Logger values
//   contextTracer - traces zerolog.Context values (from With())
//
// Each tracer implements tracer interface with type-specific context checks.
// Cross-type delegation happens when values flow between types (e.g., Event
// created from Logger).
//
// Architecture:
//
//   ┌─────────────┐     ┌─────────────┐     ┌───────────────┐
//   │ eventTracer │────▶│loggerTracer │────▶│ contextTracer │
//   │  (Event)    │◀────│  (Logger)   │◀────│   (Context)   │
//   └─────────────┘     └─────────────┘     └───────────────┘
//         │                   │                    │
//         └───────────────────┴────────────────────┘
//                             │
//                      ┌──────▼──────┐
//                      │ traceCommon │  (handles Phi, UnOp, etc.)
//                      └─────────────┘

// tracer defines the strategy for tracing a specific zerolog type.
// Each implementation knows how to check for context on its type and
// when to delegate to other tracers.
type tracer interface {
	// hasContext checks if this call sets or inherits context.
	// Returns:
	//   - found=true if context is definitely set
	//   - delegate=non-nil to continue tracing with another tracer
	//   - found=false, delegate=nil to continue with current tracer
	hasContext(
		call *ssa.Call,
		callee *ssa.Function,
		recv *types.Var,
	) (found bool, delegate tracer, delegateVal ssa.Value)

	// continueOnReceiverType returns true if this tracer should continue
	// tracing when the receiver matches its type (for chained method calls).
	continueOnReceiverType(recv *types.Var) bool
}

// =============================================================================
// Unified Value Tracing
// =============================================================================

// traceValue is the unified tracing function that works with any tracer.
// It handles the common tracing logic and delegates type-specific checks
// to the provided tracer strategy.
func (c *checker) traceValue(v ssa.Value, tracer tracer, visited map[ssa.Value]bool) bool {
	if visited[v] {
		return false
	}
	visited[v] = true

	call, ok := v.(*ssa.Call)
	if !ok {
		return c.traceCommon(v, visited, tracer)
	}

	callee := call.Call.StaticCallee()
	if callee == nil {
		return c.traceReceiver(call, visited, tracer)
	}

	recv := call.Call.Signature().Recv()

	// Ask the tracer to check for context
	found, delegateTracer, delegateVal := tracer.hasContext(call, callee, recv)
	if found {
		return true
	}

	// If tracer wants to delegate, switch to the new tracer
	if delegateTracer != nil && delegateVal != nil {
		return c.traceValue(delegateVal, delegateTracer, visited)
	}

	// Continue tracing through receiver if type matches
	if tracer.continueOnReceiverType(recv) {
		return c.traceReceiver(call, visited, tracer)
	}

	return false
}

// =============================================================================
// Common SSA Value Handling
// =============================================================================

// traceCommon handles common SSA value types (Phi, UnOp, FreeVar, etc.).
// It provides shared tracing logic that works with any tracer strategy.
func (c *checker) traceCommon(v ssa.Value, visited map[ssa.Value]bool, tracer tracer) bool {
	switch val := v.(type) {
	case *ssa.Phi:
		return c.tracePhi(val, visited, tracer)
	case *ssa.Extract:
		return c.traceValue(val.Tuple, tracer, visited)
	case *ssa.UnOp:
		return c.traceUnOp(val, visited, tracer)
	case *ssa.ChangeType:
		return c.traceValue(val.X, tracer, visited)
	case *ssa.MakeInterface:
		return c.traceValue(val.X, tracer, visited)
	case *ssa.TypeAssert:
		return c.traceValue(val.X, tracer, visited)
	case *ssa.FieldAddr:
		return c.traceValue(val.X, tracer, visited)
	case *ssa.Field:
		return c.traceValue(val.X, tracer, visited)
	case *ssa.IndexAddr:
		return c.traceValue(val.X, tracer, visited)
	case *ssa.Index:
		return c.traceValue(val.X, tracer, visited)
	case *ssa.Lookup:
		return c.traceValue(val.X, tracer, visited)
	case *ssa.FreeVar:
		return c.traceFreeVar(val, visited, tracer)
	}
	return false
}

// =============================================================================
// Phi Node Handling
// =============================================================================

// tracePhi handles SSA Phi nodes where multiple control flow paths merge.
// For context tracking, ALL non-cyclic edges must have context set.
// Cyclic edges (loop back-edges) are skipped as they depend on the
// initial value (e.g., loops like: x := init; for { x = f(x) }).
func (c *checker) tracePhi(phi *ssa.Phi, visited map[ssa.Value]bool, tracer tracer) bool {
	if len(phi.Edges) == 0 {
		return false
	}

	hasNonCyclicEdge := false
	for _, edge := range phi.Edges {
		// Skip edges that would cycle back to this Phi
		if edgeLeadsTo(edge, phi, visited) {
			continue
		}
		hasNonCyclicEdge = true

		// Clone visited for independent tracing of each branch
		edgeVisited := make(map[ssa.Value]bool)
		maps.Copy(edgeVisited, visited)

		if !c.traceValue(edge, tracer, edgeVisited) {
			return false
		}
	}

	// If all edges are cyclic, we need at least one valid edge to check
	return hasNonCyclicEdge
}

// edgeLeadsTo checks if tracing this edge would eventually lead back to target.
// This detects loop back-edges in Phi nodes.
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
		// Check receiver (first arg for method calls)
		if len(val.Call.Args) > 0 {
			return edgeLeadsToImpl(val.Call.Args[0], target, seen)
		}
	case *ssa.Phi:
		for _, edge := range val.Edges {
			if edgeLeadsToImpl(edge, target, seen) {
				return true
			}
		}
	case *ssa.UnOp:
		return edgeLeadsToImpl(val.X, target, seen)
	case *ssa.ChangeType:
		return edgeLeadsToImpl(val.X, target, seen)
	}
	return false
}

// =============================================================================
// Special Value Handling
// =============================================================================

// traceUnOp handles SSA unary operations, especially pointer dereferences.
// For dereference (*ptr), it tries to find what was stored at that address.
func (c *checker) traceUnOp(unop *ssa.UnOp, visited map[ssa.Value]bool, tracer tracer) bool {
	if unop.Op == token.MUL {
		if stored := findStoredValue(unop.X); stored != nil {
			return c.traceValue(stored, tracer, visited)
		}
	}
	return c.traceValue(unop.X, tracer, visited)
}

// traceFreeVar traces a FreeVar back to the value bound in MakeClosure.
// FreeVars are variables captured from an enclosing function scope.
func (c *checker) traceFreeVar(fv *ssa.FreeVar, visited map[ssa.Value]bool, tracer tracer) bool {
	fn := fv.Parent()
	if fn == nil {
		return false
	}

	// Find the index of this FreeVar in the function's FreeVars list
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

	// Look for MakeClosure instructions in the parent that create this closure
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
			// Check if this MakeClosure creates our function
			closureFn, ok := mc.Fn.(*ssa.Function)
			if !ok || closureFn != fn {
				continue
			}
			// mc.Bindings[idx] is the value bound to this FreeVar
			if idx < len(mc.Bindings) {
				if c.traceValue(mc.Bindings[idx], tracer, visited) {
					return true
				}
			}
		}
	}
	return false
}

// traceReceiver traces the receiver (first argument) of a method call.
func (c *checker) traceReceiver(call *ssa.Call, visited map[ssa.Value]bool, tracer tracer) bool {
	if len(call.Call.Args) > 0 {
		return c.traceValue(call.Call.Args[0], tracer, visited)
	}
	return false
}

// =============================================================================
// Store Tracking
// =============================================================================

// findStoredValue finds the value that was stored at the given address.
// This handles cases like:
//
//	h := holder{event: logger.Info().Ctx(ctx)}
//	h.event.Msg("test")  // Need to trace back through h.event
//
// In SSA this becomes:
//
//	t1 = &t0.event        (FieldAddr)
//	t2 = (*Event).Ctx(...)
//	*t1 = t2              (Store)
//	t3 = &t0.event        (FieldAddr - same field)
//	t4 = *t3              (UnOp - dereference)
//	(*Event).Msg(t4, ...) (we need to trace t4 back to t2)
func findStoredValue(addr ssa.Value) ssa.Value {
	// Get the parent function of this value
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

	// Look for Store instructions that write to a matching address
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			store, ok := instr.(*ssa.Store)
			if !ok {
				continue
			}
			// Check if this Store writes to an equivalent address
			if addressesMatch(store.Addr, addr) {
				return store.Val
			}
		}
	}
	return nil
}

// addressesMatch checks if two addresses refer to the same memory location.
// This is a simplified comparison - it checks for structural equivalence
// of FieldAddr/IndexAddr operations on the same base value.
func addressesMatch(a, b ssa.Value) bool {
	// Direct equality
	if a == b {
		return true
	}

	// Check for equivalent FieldAddr (same base, same field index)
	fa1, ok1 := a.(*ssa.FieldAddr)
	fa2, ok2 := b.(*ssa.FieldAddr)
	if ok1 && ok2 {
		return fa1.X == fa2.X && fa1.Field == fa2.Field
	}

	// Check for equivalent IndexAddr (same base, same index)
	ia1, ok1 := a.(*ssa.IndexAddr)
	ia2, ok2 := b.(*ssa.IndexAddr)
	if ok1 && ok2 {
		// For constant indices, compare them directly
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

// =============================================================================
// Tracer Implementations
// =============================================================================

// eventTracer traces *zerolog.Event values for context.
//
// Context sources:
//   - Event.Ctx(ctx): direct context setting
//   - Context.Ctx(ctx): inherited from Context builder
//   - zerolog.Ctx(ctx): Logger returned already has context
//   - Logger.Info/Debug/etc(): inherits from parent Logger
//   - Context.Logger(): inherits from Context builder
type eventTracer struct {
	logger  *loggerTracer
	context *contextTracer
}

func (t *eventTracer) hasContext(
	call *ssa.Call,
	callee *ssa.Function,
	recv *types.Var,
) (bool, tracer, ssa.Value) {
	// Event.Ctx(ctx) or Context.Ctx(ctx) - direct context setting
	if callee.Name() == ctxMethod && recv != nil {
		if isEvent(recv.Type()) || isContext(recv.Type()) {
			return true, nil, nil
		}
	}

	// zerolog.Ctx(ctx) - returns Logger with context
	if isCtxFunc(callee) {
		return true, nil, nil
	}

	// Logger methods that return Event (Info, Debug, Err, WithLevel, etc.) - delegate to logger tracer
	if recv != nil && isLogger(recv.Type()) && returnsEvent(callee) {
		if len(call.Call.Args) > 0 {
			return false, t.logger, call.Call.Args[0]
		}
	}

	// Context methods that return Logger - delegate to context tracer
	if recv != nil && isContext(recv.Type()) && returnsLogger(callee) {
		if len(call.Call.Args) > 0 {
			return false, t.context, call.Call.Args[0]
		}
	}

	return false, nil, nil
}

func (t *eventTracer) continueOnReceiverType(recv *types.Var) bool {
	return recv != nil && isEvent(recv.Type())
}

// loggerTracer traces zerolog.Logger values for context.
//
// Context sources:
//   - zerolog.Ctx(ctx): returns Logger from context
//   - Context.Logger(): inherits from Context builder
//   - Logger.With(): inherits from parent Logger (via Context)
type loggerTracer struct {
	context *contextTracer
}

func (t *loggerTracer) hasContext(
	call *ssa.Call,
	callee *ssa.Function,
	recv *types.Var,
) (bool, tracer, ssa.Value) {
	// zerolog.Ctx(ctx) - returns Logger with context
	if isCtxFunc(callee) {
		return true, nil, nil
	}

	// Context methods that return Logger - delegate to context tracer
	if recv != nil && isContext(recv.Type()) && returnsLogger(callee) {
		if len(call.Call.Args) > 0 {
			return false, t.context, call.Call.Args[0]
		}
	}

	// Logger methods that return Context (With) - trace parent Logger (self-delegation via context)
	if recv != nil && isLogger(recv.Type()) && returnsContext(callee) {
		if len(call.Call.Args) > 0 {
			return false, t, call.Call.Args[0]
		}
	}

	return false, nil, nil
}

func (t *loggerTracer) continueOnReceiverType(recv *types.Var) bool {
	return recv != nil && isLogger(recv.Type())
}

// contextTracer traces zerolog.Context values for context.
//
// Context sources:
//   - Context.Ctx(ctx): direct context setting
//   - Logger.With(): inherits from parent Logger
type contextTracer struct {
	logger *loggerTracer
}

func (t *contextTracer) hasContext(
	call *ssa.Call,
	callee *ssa.Function,
	recv *types.Var,
) (bool, tracer, ssa.Value) {
	// Context.Ctx(ctx) - direct context setting
	if callee.Name() == ctxMethod && recv != nil && isContext(recv.Type()) {
		return true, nil, nil
	}

	// Logger methods that return Context (With) - delegate to logger tracer
	if recv != nil && isLogger(recv.Type()) && returnsContext(callee) {
		if len(call.Call.Args) > 0 {
			return false, t.logger, call.Call.Args[0]
		}
	}

	return false, nil, nil
}

func (t *contextTracer) continueOnReceiverType(recv *types.Var) bool {
	return recv != nil && isContext(recv.Type())
}

// newTracers creates the interconnected tracer instances.
func newTracers() *eventTracer {
	context := &contextTracer{}
	logger := &loggerTracer{context: context}
	context.logger = logger
	event := &eventTracer{logger: logger, context: context}
	return event
}
