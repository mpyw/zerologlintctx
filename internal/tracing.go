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

// =============================================================================
// TraceResult - Validated State Pattern
// =============================================================================

// traceResult represents the outcome of checking a call for context.
// Using a struct instead of multiple return values makes the API clearer
// and prevents invalid state combinations.
type traceResult struct {
	kind        traceResultKind
	delegate    tracer
	delegateVal ssa.Value
}

type traceResultKind int

const (
	// traceResultFound indicates context was definitely found.
	traceResultFound traceResultKind = iota
	// traceResultDelegate indicates tracing should continue with another tracer.
	traceResultDelegate
	// traceResultContinue indicates tracing should continue with the current tracer.
	traceResultContinue
)

// found creates a result indicating context was found.
func found() traceResult {
	return traceResult{kind: traceResultFound}
}

// delegateTo creates a result indicating delegation to another tracer.
func delegateTo(t tracer, v ssa.Value) traceResult {
	return traceResult{kind: traceResultDelegate, delegate: t, delegateVal: v}
}

// continueTracing creates a result indicating tracing should continue.
func continueTracing() traceResult {
	return traceResult{kind: traceResultContinue}
}

// =============================================================================
// Tracer Interface
// =============================================================================

// tracer defines the strategy for tracing a specific zerolog type.
// Each implementation knows how to check for context on its type and
// when to delegate to other tracers.
type tracer interface {
	// checkContext examines a call and returns the tracing result.
	// Possible outcomes:
	//   - found(): context is definitely set
	//   - delegateTo(t, v): continue tracing value v with tracer t
	//   - continueTracing(): continue with current tracer on receiver
	checkContext(call *ssa.Call, callee *ssa.Function, recv *types.Var) traceResult

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
func (c *checker) traceValue(v ssa.Value, t tracer, visited map[ssa.Value]bool) bool {
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
	// e.g., func() *zerolog.Event { return logger.Info().Ctx(ctx) }().Msg("...")
	if _, ok := call.Call.Value.(*ssa.MakeClosure); ok {
		if c.traceIIFEReturns(callee, visited, t) {
			return true
		}
	}

	recv := call.Call.Signature().Recv()

	// Ask the tracer to check for context
	result := t.checkContext(call, callee, recv)
	switch result.kind {
	case traceResultFound:
		return true
	case traceResultDelegate:
		return c.traceValue(result.delegateVal, result.delegate, visited)
	case traceResultContinue:
		// Continue with current tracer
	}

	// Continue tracing through receiver if type matches
	if t.continueOnReceiverType(recv) {
		return c.traceReceiver(call, visited, t)
	}

	return false
}

// =============================================================================
// Common SSA Value Handling
// =============================================================================

// traceCommon handles common SSA value types (Phi, UnOp, FreeVar, etc.).
// It provides shared tracing logic that works with any tracer strategy.
//
// Most SSA value types simply wrap an inner value that needs tracing.
// Special cases (Phi, UnOp, Alloc, FreeVar) require custom handling.
func (c *checker) traceCommon(v ssa.Value, visited map[ssa.Value]bool, tracer tracer) bool {
	// Handle special cases that require custom logic
	switch val := v.(type) {
	case *ssa.Phi:
		return c.tracePhi(val, visited, tracer)
	case *ssa.UnOp:
		return c.traceUnOp(val, visited, tracer)
	case *ssa.Alloc:
		return c.traceAlloc(val, visited, tracer)
	case *ssa.FreeVar:
		return c.traceFreeVar(val, visited, tracer)
	}

	// Handle simple wrapper types that just need inner value tracing
	if inner := unwrapInner(v); inner != nil {
		return c.traceValue(inner, tracer, visited)
	}

	return false
}

// unwrapInner extracts the inner value from SSA wrapper types.
// These types simply wrap another value without adding semantic meaning
// for context tracing purposes.
//
// Returns nil if the value type doesn't have a simple inner value.
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
// For context tracking, ALL non-cyclic, non-nil edges must have context set.
//
// Skipped edges:
//   - Cyclic edges (loop back-edges) depend on the initial value
//   - Nil constant edges can never reach method calls (would panic)
//
// Example:
//
//	var e *zerolog.Event
//	if cond { e = logger.Info().Ctx(ctx) }
//	if e != nil { e.Msg("...") }  // Only non-nil edge matters
func (c *checker) tracePhi(phi *ssa.Phi, visited map[ssa.Value]bool, tracer tracer) bool {
	if len(phi.Edges) == 0 {
		return false
	}

	hasValidEdge := false
	for _, edge := range phi.Edges {
		// Skip edges that would cycle back to this Phi
		if edgeLeadsTo(edge, phi, visited) {
			continue
		}

		// Skip nil constant edges - nil pointers can't have methods called on them
		if isNilConst(edge) {
			continue
		}

		hasValidEdge = true

		// Clone visited for independent tracing of each branch
		edgeVisited := make(map[ssa.Value]bool)
		maps.Copy(edgeVisited, visited)

		if !c.traceValue(edge, tracer, edgeVisited) {
			return false
		}
	}

	// Need at least one valid (non-cyclic, non-nil) edge to check
	return hasValidEdge
}

// isNilConst checks if a value is a nil constant.
// Nil pointers cannot have methods called on them, so they are safe to skip
// when tracing Phi nodes (the nil path would panic before reaching the call).
func isNilConst(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	if !ok {
		return false
	}
	// For nil pointer constants, Value is nil
	return c.Value == nil
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

	// Handle special cases first
	switch val := v.(type) {
	case *ssa.Call:
		// Check receiver (first arg for method calls)
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

	// Handle simple wrapper types using the same unwrapper as traceCommon
	if inner := unwrapInner(v); inner != nil {
		return edgeLeadsToImpl(inner, target, seen)
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

// traceAlloc handles SSA Alloc nodes (local variable allocation).
// When a variable is used as a receiver (e.g., logger.Info()), we need to
// find what value was stored into that variable.
//
// Example:
//
//	logger := log.Ctx(ctx).With().Logger()  // t0 = new Logger; *t0 = result
//	logger.Info().Msg("test")               // uses t0 (address of logger)
func (c *checker) traceAlloc(alloc *ssa.Alloc, visited map[ssa.Value]bool, tracer tracer) bool {
	if stored := findStoredValue(alloc); stored != nil {
		return c.traceValue(stored, tracer, visited)
	}
	return false
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

// traceIIFEReturns traces through an IIFE (Immediately Invoked Function Expression).
// It finds all return statements in the function and traces the returned values.
//
// Example:
//
//	func() *zerolog.Event {
//	    return logger.Info().Ctx(ctx)
//	}().Msg("iife with ctx")
//
// The analyzer traces through the IIFE's return value to find .Ctx(ctx).
func (c *checker) traceIIFEReturns(fn *ssa.Function, visited map[ssa.Value]bool, tracer tracer) bool {
	// Check if the function returns a relevant type
	results := fn.Signature.Results()
	if results == nil || results.Len() == 0 {
		return false
	}

	// Only trace if return type is Event, Logger, or Context
	retType := results.At(0).Type()
	if !isEvent(retType) && !isLogger(retType) && !isContext(retType) {
		return false
	}

	// Find all return statements and trace their values
	// All return paths must have context for this to return true
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

			// Clone visited to trace each return path independently
			retVisited := make(map[ssa.Value]bool)
			maps.Copy(retVisited, visited)

			if !c.traceValue(ret.Results[0], tracer, retVisited) {
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

func (t *eventTracer) checkContext(
	call *ssa.Call,
	callee *ssa.Function,
	recv *types.Var,
) traceResult {
	// Event.Ctx(ctx) or Context.Ctx(ctx) - direct context setting
	if callee.Name() == ctxMethod && recv != nil {
		if isEvent(recv.Type()) || isContext(recv.Type()) {
			return found()
		}
	}

	// zerolog.Ctx(ctx) - returns Logger with context
	if isCtxFunc(callee) {
		return found()
	}

	// Logger methods that return Event (Info, Debug, Err, WithLevel, etc.) - delegate to logger tracer
	if recv != nil && isLogger(recv.Type()) && returnsEvent(callee) {
		if len(call.Call.Args) > 0 {
			return delegateTo(t.logger, call.Call.Args[0])
		}
	}

	// Context methods that return Logger - delegate to context tracer
	if recv != nil && isContext(recv.Type()) && returnsLogger(callee) {
		if len(call.Call.Args) > 0 {
			return delegateTo(t.context, call.Call.Args[0])
		}
	}

	return continueTracing()
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

func (t *loggerTracer) checkContext(
	call *ssa.Call,
	callee *ssa.Function,
	recv *types.Var,
) traceResult {
	// zerolog.Ctx(ctx) - returns Logger with context
	if isCtxFunc(callee) {
		return found()
	}

	// Context methods that return Logger - delegate to context tracer
	if recv != nil && isContext(recv.Type()) && returnsLogger(callee) {
		if len(call.Call.Args) > 0 {
			return delegateTo(t.context, call.Call.Args[0])
		}
	}

	// Logger methods that return Context (With) - trace parent Logger (self-delegation via context)
	if recv != nil && isLogger(recv.Type()) && returnsContext(callee) {
		if len(call.Call.Args) > 0 {
			return delegateTo(t, call.Call.Args[0])
		}
	}

	return continueTracing()
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

func (t *contextTracer) checkContext(
	call *ssa.Call,
	callee *ssa.Function,
	recv *types.Var,
) traceResult {
	// Context.Ctx(ctx) - direct context setting
	if callee.Name() == ctxMethod && recv != nil && isContext(recv.Type()) {
		return found()
	}

	// Logger methods that return Context (With) - delegate to logger tracer
	if recv != nil && isLogger(recv.Type()) && returnsContext(callee) {
		if len(call.Call.Args) > 0 {
			return delegateTo(t.logger, call.Call.Args[0])
		}
	}

	return continueTracing()
}

func (t *contextTracer) continueOnReceiverType(recv *types.Var) bool {
	return recv != nil && isContext(recv.Type())
}

// =============================================================================
// Tracer Registry
// =============================================================================

// tracerRegistry manages the interconnected tracer instances.
// This encapsulates the circular references between tracers.
type tracerRegistry struct {
	event   *eventTracer
	logger  *loggerTracer
	context *contextTracer
}

// newTracerRegistry creates and wires up all tracer instances.
func newTracerRegistry() *tracerRegistry {
	r := &tracerRegistry{
		context: &contextTracer{},
		logger:  &loggerTracer{},
		event:   &eventTracer{},
	}

	// Wire up the circular references
	r.context.logger = r.logger
	r.logger.context = r.context
	r.event.logger = r.logger
	r.event.context = r.context

	return r
}

// EventTracer returns the tracer for zerolog.Event values.
func (r *tracerRegistry) EventTracer() tracer {
	return r.event
}
