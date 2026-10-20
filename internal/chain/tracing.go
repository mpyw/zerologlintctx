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
	if d, deferred := at.(*ssa.Defer); deferred {
		return tr.traceCtxSetAtReturns(v, d)
	}
	return false
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
// it is the same event. The recover block, reached after a panic, is not
// looked at, as it has no path from d.
func (tr *tracer) traceCtxSetAtReturns(v ssa.Value, d *ssa.Defer) bool {
	calls := make(map[ssa.Instruction]bool)
	for w := v; w != nil; w = traceEventReceiver(w) {
		tr.traceCtxCalls(w, d.Parent(), calls, make(map[ssa.Value]bool))
	}
	if len(calls) == 0 {
		return false
	}
	type step struct {
		block *ssa.BasicBlock
		from  int // Index of the first instruction of block to look at
	}
	queue := []step{{d.Block(), tr.positions.of(d) + 1}}
	seen := make(map[*ssa.BasicBlock]bool)
	for len(queue) > 0 {
		s := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if slices.ContainsFunc(s.block.Instrs[s.from:], func(instr ssa.Instruction) bool { return calls[instr] }) {
			continue // Ctx runs on this path
		}
		if _, ok := s.block.Instrs[len(s.block.Instrs)-1].(*ssa.Return); ok {
			return false
		}
		for _, succ := range s.block.Succs {
			if !seen[succ] {
				seen[succ] = true
				queue = append(queue, step{succ, 0})
			}
		}
	}
	return true
}

// traceEventReceiver returns the event that v, a call of an event method,
// runs on, or nil for any other value.
func traceEventReceiver(v ssa.Value) ssa.Value {
	call, ok := v.(*ssa.Call)
	if !ok {
		return nil
	}
	c, ok := calleeOf(&call.Call)
	if !ok || c.recv == nil || !tracerEvent.isTracedType(c.recvType()) {
		return nil
	}
	return c.recv
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
	found := tr.traceCtxCalledBefore(v, before)
	if !found {
		if recv := traceEventReceiver(v); recv != nil {
			found = tr.traceCtxSetInPlace(recv, before, done)
		} else if phi, ok := v.(*ssa.Phi); ok {
			found = tr.traceCtxSetOnEdges(phi, done)
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

// traceCtxCalledBefore reports whether, on every path from v to before,
// Event.Ctx is called on the event v, directly or through a method value. A
// path that leads back to the entry of the function without passing v does
// not have v, and needs no call, unless v is a parameter or a free variable,
// which the entry gives.
func (tr *tracer) traceCtxCalledBefore(v ssa.Value, before tracePoint) bool {
	fn := before.block.Parent()
	calls := make(map[ssa.Instruction]bool)
	tr.traceCtxCalls(v, fn, calls, make(map[ssa.Value]bool))
	if len(calls) == 0 {
		return false
	}
	def, made := v.(ssa.Instruction) // A parameter or a free variable is not made, but given at the entry
	type step struct {
		block *ssa.BasicBlock
		to    int // Index after the last instruction of block to look at
	}
	queue := []step{{before.block, before.index}}
	seen := make(map[*ssa.BasicBlock]bool)
	for len(queue) > 0 {
		s := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		stopped := false
		for i := s.to - 1; i >= 0 && !stopped; i-- {
			instr := s.block.Instrs[i]
			if calls[instr] {
				stopped = true // Ctx runs on this path
			} else if made && instr == def {
				return false // v is made on this path, and Ctx does not run after
			}
		}
		if stopped {
			continue
		}
		if len(s.block.Preds) == 0 && !made {
			return false
		}
		for _, pred := range s.block.Preds {
			if !seen[pred] {
				seen[pred] = true
				queue = append(queue, step{pred, len(pred.Instrs)})
			}
		}
	}
	return true
}

// traceCtxCalls adds to calls the Event.Ctx calls in fn on the event v,
// directly or through a method value, or on a value that a method of v
// returns, which is the same event.
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
			if !ok || c.recv != v || !tracerEvent.isTracedType(c.recvType()) {
				continue
			}
			if c.isCtx() {
				calls[call] = true
			} else if tracerEvent.isReturnedBy(c.signature()) {
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
		if found, ok := tr.traceFuncLiteralResult(call, 0, t, visited); ok {
			return found
		}
		if call.Call.StaticCallee() != nil || len(call.Call.Args) == 0 {
			return false
		}
		// Dynamic dispatch: the method is not known
		return tr.traceReceiver(call.Call.Args[0], call, t, visited)
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
	case *ssa.Field, *ssa.Index:
		if found, ok := tr.traceSelectedValue(v, t, visited); ok {
			return found
		}
	case *ssa.Extract:
		if call, ok := val.Tuple.(*ssa.Call); ok {
			if found, ok := tr.traceFuncLiteralResult(call, val.Index, t, visited); ok {
				return found
			}
		}
		return tr.traceValue(val.Tuple, t, visited)
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

// traceSelectedValue traces v, a field of a struct value or an element of an
// array value, selected from one loaded whole from an address. What is at v is
// what the stores that reach the load left at the same selection there:
//
//	t8 = *t4                // the whole struct, loaded
//	t9 = t8.Logger          // v, read by value
//
// ok is false when the value it selects from is not loaded from an address
// whose stores storesReachingAt can find.
func (tr *tracer) traceSelectedValue(v ssa.Value, t tracerType, visited map[ssa.Value]bool) (found, ok bool) {
	var selections []ssa.Value
	for {
		inner := unwrapInner(v)
		switch v.(type) {
		case *ssa.Field, *ssa.Index:
			selections = append(selections, v)
			v = inner
			continue
		}
		break
	}
	load, isLoad := v.(*ssa.UnOp)
	if !isLoad || load.Op != token.MUL {
		return false, false
	}
	slices.Reverse(selections)
	root, path := aggregatePath(load.X)
	if tr.positions == nil {
		tr.positions = make(instrPositions)
	}
	stored, ok := storesReachingAt(root, append(path, selections...), load, tr.positions)
	if !ok {
		return false, false
	}
	return tr.traceStored(stored, t, visited), true
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
// the context of the value before it.
func (tr *tracer) traceStoredValues(addr ssa.Value, read ssa.Instruction, t tracerType, visited map[ssa.Value]bool) (found, ok bool) {
	if tr.positions == nil {
		tr.positions = make(instrPositions)
	}
	stored, ok := storesReaching(addr, read, tr.positions)
	if !ok {
		return false, false
	}
	return tr.traceStored(stored, t, visited), true
}

// traceStored reports whether what storesReaching found can be at an
// address has context, on every path, and on some path at all.
func (tr *tracer) traceStored(stored storeReaching, t tracerType, visited map[ssa.Value]bool) bool {
	if stored.unknown {
		return false
	}
	found := false
	if stored.captured != nil {
		if !tr.traceFreeVar(stored.captured, t, maps.Clone(visited)) {
			return false
		}
		found = true
	}
	for _, pp := range stored.params {
		if !tr.traceParamPath(pp, t, maps.Clone(visited)) {
			return false
		}
		found = true
	}
	for _, v := range stored.values {
		if visited[v] || isNilConst(v) {
			continue // A nil event logs nothing, as for an edge of a Phi
		}
		// Each store is traced on its own copy of visited, unless it is the
		// only one: copying on every step of a long chain of updates would
		// take time quadratic in its length.
		edgeVisited := visited
		if len(stored.values) > 1 {
			edgeVisited = maps.Clone(visited)
		}
		if !tr.traceValue(v, t, edgeVisited) {
			return false
		}
		found = true
	}
	return found
}

// traceParamPath traces the value at pp, in a parameter of a func literal,
// as each call of the func literal passes it. The value is what is at the
// same selections from the argument when the call runs:
//
//	func(p *zerolog.Logger) {
//	    p.Info().Msg("x")       // reads *p, which is l below
//	}(&l)
//
// A call that passes the parameter on, as a recursive call does, reaches the
// same parameter again. It is skipped there, as an update in a loop is: the
// other calls give the value.
func (tr *tracer) traceParamPath(pp storeParamPath, t tracerType, visited map[ssa.Value]bool) bool {
	if visited[pp.param] {
		return true
	}
	visited[pp.param] = true
	fn := pp.param.Parent()
	calls, ok := traceFuncLiteralCalls(fn)
	if !ok || len(calls) == 0 {
		return false
	}
	idx := slices.Index(fn.Params, pp.param)
	for _, call := range calls {
		if !tr.traceArgumentAt(call.Common().Args[idx], call, pp, t, maps.Clone(visited)) {
			return false
		}
	}
	return true
}

// traceArgumentAt traces the value at the selections of pp from arg, which
// call passes for the parameter of pp.
func (tr *tracer) traceArgumentAt(arg ssa.Value, call ssa.CallInstruction, pp storeParamPath, t tracerType, visited map[ssa.Value]bool) bool {
	addr, read := arg, ssa.Instruction(call)
	switch {
	case !pp.pointer:
		// A struct or array passed by value is loaded from where it is
		load, ok := arg.(*ssa.UnOp)
		if !ok || load.Op != token.MUL {
			return false
		}
		addr, read = load.X, load
	case len(pp.path) == 0:
		return tr.traceReceiver(arg, call, t, visited)
	}
	root, path := aggregatePath(addr)
	stored, ok := storesReachingAt(root, append(path, pp.path...), read, tr.positions)
	return ok && tr.traceStored(stored, t, visited)
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
	for _, call := range calls {
		if !tr.traceValue(call.Common().Args[idx], t, maps.Clone(visited)) {
			return false
		}
	}
	return true
}

// traceFuncLiteralCalls finds every call that may run fn, a func literal. ok
// is false when fn is a declared function, or may be called where it cannot
// be seen.
//
// A func literal can only be used inside the function it is written in, and
// the closures declared there. It may be kept in a variable on the way, and a
// closure that calls it captures that variable:
//
//	t2 = new func(e *Event) (logIt)  // a variable that may hold fn
//	*t2 = f$1
//	t3 = *t2                         // may be fn
//	t4 = t3(t6)                      // a call that may run fn
//	t8 = make closure f$2 [t2]       // logIt is a free variable of f$2
//
// A value that may be fn is fn, a closure of it, a load of such a variable,
// or a Phi with such an edge. It may only be called, stored to a local
// variable, or merged in a Phi. Such a variable may only be stored to,
// loaded, or captured. Any other use passes fn on, to be called with anything.
// A call that may run another func too still counts: every call that may
// run fn must pass an argument with context.
func traceFuncLiteralCalls(fn *ssa.Function) (calls []ssa.CallInstruction, ok bool) {
	parent := fn.Parent()
	if parent == nil {
		return nil, false // A declared function can be called from anywhere
	}
	vars := make(map[ssa.Value]bool) // The variables that may hold fn
	phis := make(map[ssa.Value]bool) // The Phis that may be fn
	mayBeFn := func(v ssa.Value) bool {
		if load, ok := v.(*ssa.UnOp); ok && load.Op == token.MUL {
			return vars[load.X]
		}
		return phis[v] || traceIsFuncValue(v, fn)
	}
	for changed := true; changed; {
		changed = false
		for instr := range traceInstrsInScope(parent) {
			switch in := instr.(type) {
			case *ssa.Store:
				if _, isLocal := in.Addr.(*ssa.Alloc); isLocal && mayBeFn(in.Val) && !vars[in.Addr] {
					vars[in.Addr], changed = true, true
				}
			case *ssa.Phi:
				if !phis[in] && slices.ContainsFunc(in.Edges, mayBeFn) {
					phis[in], changed = true, true
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
			case mayBeFn(*op):
				if !traceUsesFuncValue(instr, op, vars) {
					return nil, false // The func is passed on, and may be called where it cannot be seen
				}
				if call, isCall := instr.(ssa.CallInstruction); isCall {
					calls = append(calls, call)
				}
			case vars[*op]:
				if !traceUsesFuncVar(instr) {
					return nil, false
				}
			}
		}
	}
	return calls, true
}

// traceUsesFuncValue reports whether instr uses op, a value that may be a
// func literal, in a way that keeps it from being called where it cannot be
// seen: a call of it, a store of it to a local variable in vars, or a Phi.
func traceUsesFuncValue(instr ssa.Instruction, op *ssa.Value, vars map[ssa.Value]bool) bool {
	switch in := instr.(type) {
	case ssa.CallInstruction:
		return op == &in.Common().Value
	case *ssa.Store:
		return op == &in.Val && vars[in.Addr]
	case *ssa.Phi:
		return true
	}
	return false
}

// traceUsesFuncVar reports whether instr, a use of a variable that may hold a
// func literal, keeps it from being called where it cannot be seen: a store
// to it, a load, or a capture by a closure.
func traceUsesFuncVar(instr ssa.Instruction) bool {
	switch in := instr.(type) {
	case *ssa.Store, *ssa.MakeClosure:
		return true
	case *ssa.UnOp:
		return in.Op == token.MUL
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

// traceFuncLiteralResult reports whether the result idx of call has context,
// when call runs a func literal: one called on the spot as an IIFE
// (Immediately Invoked Function Expression), through a variable, or one of
// several a variable can hold. One that captures nothing is called as the
// function itself, not as a closure. ok is false when call runs no func
// literal that is known.
func (tr *tracer) traceFuncLiteralResult(call *ssa.Call, idx int, t tracerType, visited map[ssa.Value]bool) (found, ok bool) {
	fns, ok := traceFuncLiteralsOf(call.Call.Value, make(map[ssa.Value]bool))
	if sfn := call.Call.StaticCallee(); sfn != nil {
		fns, ok = []*ssa.Function{sfn}, sfn.Parent() != nil
	}
	if !ok || len(fns) == 0 {
		return false, false
	}
	return !slices.ContainsFunc(fns, func(fn *ssa.Function) bool {
		return !tr.traceReturns(fn, idx, t, maps.Clone(visited))
	}), true
}

// traceFuncLiteralsOf returns the func literals that v, a Phi, can be, when
// every edge is a func literal or a closure of one, or another such Phi:
//
//	ev := func() *zerolog.Event { return logger.Info().Ctx(ctx) }
//	if debug {
//	    ev = func() *zerolog.Event { return logger.Debug().Ctx(ctx) }
//	}
//	ev().Msg("x")           // ev is a Phi of the two
func traceFuncLiteralsOf(v ssa.Value, seen map[ssa.Value]bool) ([]*ssa.Function, bool) {
	phi, ok := v.(*ssa.Phi)
	if !ok || seen[v] {
		return nil, ok
	}
	seen[v] = true
	var fns []*ssa.Function
	for _, edge := range phi.Edges {
		if mc, ok := edge.(*ssa.MakeClosure); ok {
			edge = mc.Fn
		}
		if fn, ok := edge.(*ssa.Function); ok && fn.Parent() != nil {
			fns = append(fns, fn)
			continue
		}
		more, ok := traceFuncLiteralsOf(edge, seen)
		if !ok {
			return nil, false
		}
		fns = append(fns, more...)
	}
	return fns, true
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
		if idx < len(mc.Bindings) && tr.traceBinding(mc, idx, t, visited) {
			return true
		}
	}
	return false
}

// traceBinding traces the variable that mc binds as its free variable idx, as
// the closure reads it: what can be there when the closure runs, not at every
// read of the variable in the function that makes it.
//
//	l := base
//	l.Debug().Ctx(ctx).Msg("start") // reads base, before the closure is made
//	l = l.With().Ctx(ctx).Logger()
//	defer func() { l.Info().Msg("end") }()
func (tr *tracer) traceBinding(mc *ssa.MakeClosure, idx int, t tracerType, visited map[ssa.Value]bool) bool {
	if tr.positions == nil {
		tr.positions = make(instrPositions)
	}
	stored := storesReachingClosure(mc.Bindings[idx], mc, tr.positions)
	return tr.traceStored(stored, t, visited)
}

// traceReceiver traces recv, the receiver of call, a method call. It is the
// first argument, or what a method value is bound to. A pointer receiver that
// is a variable, or a field or element of one, holds what the stores that
// reach the call left there.
func (tr *tracer) traceReceiver(recv ssa.Value, call ssa.Instruction, t tracerType, visited map[ssa.Value]bool) bool {
	if found, ok := tr.traceAddressAt(recv, call, t, visited); ok {
		return found
	}
	return tr.traceValue(recv, t, visited)
}

// traceReturns reports whether the value at resultIndex from every return of
// fn has context, as for an IIFE (Immediately Invoked Function Expression).
func (tr *tracer) traceReturns(fn *ssa.Function, resultIndex int, t tracerType, visited map[ssa.Value]bool) bool {
	// Find all return statements and trace their values. A result of
	// another type, such as an interface, is traced through to the value it
	// holds.
	hasReturn := false
	for instr := range instrsIn(fn) {
		ret, ok := instr.(*ssa.Return)
		if !ok {
			continue
		}
		if len(ret.Results) <= resultIndex {
			return false
		}

		hasReturn = true
		retVisited := maps.Clone(visited)
		if !tr.traceValue(ret.Results[resultIndex], t, retVisited) {
			return false
		}
	}

	return hasReturn
}
