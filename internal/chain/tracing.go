package chain

import (
	"go/token"
	"go/types"
	"iter"
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

// eventHasCtx traces an Event value, the receiver of the terminator at, to
// check if .Ctx() was called.
//
//declscope:shared // checker.go asks this of each terminator's receiver
func (tr *tracer) eventHasCtx(v ssa.Value, at ssa.Instruction) bool {
	if tr.traceValue(v, tracerEvent, make(map[ssa.Value]bool)) {
		return true
	}
	if tr.positions == nil {
		tr.positions = make(instrPositions)
	}
	done := make(map[tracePointValue]bool)
	before := tracePoint{block: at.Block(), index: tr.positions.of(at)}
	if tr.traceCtxSetInPlace(v, before, done) {
		return true
	}
	if _, deferred := at.(*ssa.Defer); deferred {
		return tr.traceCtxSetAtReturns(v, at.Parent(), done)
	}
	return false
}

// traceCtxSetAtReturns reports whether Event.Ctx is called on the event v
// before each return of fn, which is where a deferred terminator runs:
//
//	e := logger.Info()
//	defer e.Msg("x")        // runs at the return, after Ctx
//	e.Ctx(ctx)
func (tr *tracer) traceCtxSetAtReturns(v ssa.Value, fn *ssa.Function, done map[tracePointValue]bool) bool {
	found := false
	for _, b := range fn.Blocks {
		last := len(b.Instrs) - 1
		if _, ok := b.Instrs[last].(*ssa.Return); !ok || b == fn.Recover {
			continue // The recover block returns after a panic, which is not looked at
		}
		if !tr.traceCtxSetInPlace(v, tracePoint{block: b, index: last}, done) {
			return false
		}
		found = true
	}
	return found
}

// tracePoint is a point in a function: before the instruction at index of
// block, or at its end when index is the number of its instructions.
type tracePoint struct {
	block *ssa.BasicBlock
	index int
}

// tracePointValue is an event asked about at a point.
type tracePointValue struct {
	v     ssa.Value
	point tracePoint
}

// traceCtxSetInPlace reports whether Event.Ctx is called on the event v
// before point, on every path, with its result unused or not. Event.Ctx sets
// the context on the event it runs on, and returns that same event:
//
//	e := logger.Info()
//	e.Ctx(ctx)              // sets the context of e
//	e.Str("k", "v").Msg("x")
//
// Each method of the event returns the event it runs on, so a Ctx call on any
// value of the chain counts. For a Phi, each edge must have one before the
// end of the block it comes from.
//
// done holds the result for each value already asked about. A value still
// being worked out is taken to have context: it is reached again only through
// an edge of a Phi that leads back to it, which carries the same event. done is
// worked out for each terminator, and not kept, since a call before one
// terminator may come after another.
func (tr *tracer) traceCtxSetInPlace(v ssa.Value, before tracePoint, done map[tracePointValue]bool) bool {
	key := tracePointValue{v: v, point: before}
	if found, ok := done[key]; ok {
		return found
	}
	done[key] = true
	found := tr.traceCtxCalledBefore(v, before, make(map[ssa.Value]bool))
	if !found {
		switch val := v.(type) {
		case *ssa.Call:
			c, ok := calleeOf(&val.Call)
			found = ok && c.recv != nil && tracerEvent.isTracedType(c.recvType()) &&
				tr.traceCtxSetInPlace(c.recv, before, done)
		case *ssa.Phi:
			found = tr.traceCtxSetOnEdges(val, done)
		}
	}
	done[key] = found
	return found
}

// traceCtxSetOnEdges reports whether each edge of phi, apart from nil, has
// Event.Ctx called on it before the end of the block it comes from.
func (tr *tracer) traceCtxSetOnEdges(phi *ssa.Phi, done map[tracePointValue]bool) bool {
	found := false
	for i, edge := range phi.Edges {
		if isNilConst(edge) {
			continue
		}
		pred := phi.Block().Preds[i]
		if !tr.traceCtxSetInPlace(edge, tracePoint{block: pred, index: len(pred.Instrs)}, done) {
			return false
		}
		found = true
	}
	return found
}

// traceCtxCalledBefore reports whether Event.Ctx is called on v, directly or
// through a method value, at a point that every path to before passes first.
// A call on a value that a method of v returns is a call on v too, since it
// is the same event.
func (tr *tracer) traceCtxCalledBefore(v ssa.Value, before tracePoint, seen map[ssa.Value]bool) bool {
	refs := v.Referrers()
	if seen[v] || refs == nil {
		return false
	}
	seen[v] = true
	for _, ref := range *refs {
		call, ok := ref.(*ssa.Call)
		if !ok {
			// A method value bound to v, called later
			if mc, ok := ref.(*ssa.MakeClosure); ok && tr.traceCtxCalledByAny(mc, v, before, seen) {
				return true
			}
			continue
		}
		if tr.traceCtxCallOn(call, v, before, seen) {
			return true
		}
	}
	return false
}

// traceCtxCalledByAny reports whether a call of mc, a method value bound to v,
// is one that traceCtxCallOn accepts.
func (tr *tracer) traceCtxCalledByAny(mc *ssa.MakeClosure, v ssa.Value, before tracePoint, seen map[ssa.Value]bool) bool {
	for _, use := range *mc.Referrers() {
		if call, ok := use.(*ssa.Call); ok && tr.traceCtxCallOn(call, v, before, seen) {
			return true
		}
	}
	return false
}

// traceCtxCallOn reports whether call, a method call on the event v, is
// Event.Ctx run before before, or returns v for a later Ctx call to run on.
func (tr *tracer) traceCtxCallOn(call *ssa.Call, v ssa.Value, before tracePoint, seen map[ssa.Value]bool) bool {
	c, ok := calleeOf(&call.Call)
	if !ok || c.recv != v || !tracerEvent.isTracedType(c.recvType()) {
		return false
	}
	if c.isCtx() && call.Parent() == before.block.Parent() {
		if call.Block() == before.block {
			return tr.positions.of(call) < before.index
		}
		return call.Block().Dominates(before.block)
	}
	return tracerEvent.isReturnedBy(c.signature()) && tr.traceCtxCalledBefore(call, before, seen)
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
		return sfn.Parent() != nil && tr.traceReturns(sfn, t, visited)
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
	calls, ok := traceFuncLiteralCalls(fn)
	if !ok || len(calls) == 0 {
		return false
	}
	idx := slices.Index(fn.Params, param)
	for _, cc := range calls {
		if !tr.traceValue(cc.Args[idx], t, maps.Clone(visited)) {
			return false
		}
	}
	return true
}

// traceFuncLiteralCalls finds every call of fn, a func literal. ok is false
// when fn is a declared function, or may be called where it cannot be seen.
//
// A func literal can only be used inside the function it is written in, and
// the closures declared there. It may be kept in a variable on the way, and a
// closure that calls it captures that variable:
//
//	t2 = new func(e *Event) (logIt)  // a variable that holds fn
//	*t2 = f$1
//	t3 = *t2                         // holds fn
//	t4 = t3(t6)                      // a call of fn
//	t8 = make closure f$2 [t2]       // logIt is a free variable of f$2
//
// Each value that holds fn may only be called, or stored to such a local
// variable.
// Each such variable may only be stored fn, loaded, or captured. Any other
// use passes fn on, to be called with anything.
func traceFuncLiteralCalls(fn *ssa.Function) (calls []*ssa.CallCommon, ok bool) {
	parent := fn.Parent()
	if parent == nil {
		return nil, false // A declared function can be called from anywhere
	}
	vars := make(map[ssa.Value]bool) // The variables that hold fn
	holdsFn := func(v ssa.Value) bool {
		if load, ok := v.(*ssa.UnOp); ok && load.Op == token.MUL {
			return vars[load.X]
		}
		return traceIsFuncValue(v, fn)
	}
	for changed := true; changed; {
		changed = false
		for instr := range traceInstrsInScope(parent) {
			switch in := instr.(type) {
			case *ssa.Store:
				if _, isLocal := in.Addr.(*ssa.Alloc); isLocal && holdsFn(in.Val) && !vars[in.Addr] {
					vars[in.Addr], changed = true, true
				}
			case *ssa.MakeClosure:
				closure, _ := in.Fn.(*ssa.Function)
				for i, b := range in.Bindings {
					if vars[b] && closure != nil && !vars[closure.FreeVars[i]] {
						vars[closure.FreeVars[i]], changed = true, true
					}
				}
			}
		}
	}
	for instr := range traceInstrsInScope(parent) {
		if mc, ok := instr.(*ssa.MakeClosure); ok && mc.Fn == fn {
			continue // Where the closure is made
		}
		for _, op := range instr.Operands(nil) {
			switch {
			case holdsFn(*op):
				if call, isCall := instr.(ssa.CallInstruction); isCall && op == &call.Common().Value {
					calls = append(calls, call.Common())
					continue
				}
				if store, isStore := instr.(*ssa.Store); isStore && op == &store.Val && vars[store.Addr] {
					continue // To a local variable that holds fn
				}
				return nil, false // The func is passed on, and may be called where it cannot be seen
			case vars[*op]:
				if !traceUsesFuncVar(instr, op, holdsFn) {
					return nil, false
				}
			}
		}
	}
	return calls, true
}

// traceUsesFuncVar reports whether instr uses op, a variable that holds a func
// literal, in a way that keeps it from being called where it cannot be seen:
// a store of the func, a load, or a capture by a closure.
func traceUsesFuncVar(instr ssa.Instruction, op *ssa.Value, holdsFn func(ssa.Value) bool) bool {
	switch in := instr.(type) {
	case *ssa.Store:
		return op == &in.Addr && holdsFn(in.Val)
	case *ssa.UnOp:
		return in.Op == token.MUL
	case *ssa.MakeClosure:
		return true
	}
	return false
}

// traceInstrsInScope yields every instruction of fn, and of the func literals
// declared in it, at any depth.
func traceInstrsInScope(fn *ssa.Function) iter.Seq[ssa.Instruction] {
	return func(yield func(ssa.Instruction) bool) {
		for instr := range instrsIn(fn) {
			if !yield(instr) {
				return
			}
		}
		for _, anon := range fn.AnonFuncs {
			for instr := range traceInstrsInScope(anon) {
				if !yield(instr) {
					return
				}
			}
		}
	}
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
