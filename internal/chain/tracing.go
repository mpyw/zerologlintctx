package chain

import (
	"go/token"
	"go/types"
	"iter"
	"maps"
	"slices"

	"golang.org/x/tools/go/ssa"

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
//declscope:shared // phi.go traces each edge of a Phi in the Phi's state
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
//declscope:shared // checker.go holds one per checked function
type tracer struct {
	// hasCtx remembers each value found to have context, per trace state.
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

// newTracer creates a tracer with nothing found yet.
//
//declscope:shared // checker.go creates one per package
func newTracer() *tracer {
	return &tracer{hasCtx: make(map[tracedValue]bool), positions: make(instrPositions)}
}

// tracedValue is a value traced in one state.
type tracedValue struct {
	v  ssa.Value
	st traceState
}

// eventHasCtx traces an Event value, the receiver of the terminator at, to
// check if .Ctx() was called.
func (tr *tracer) eventHasCtx(v ssa.Value, at ssa.Instruction) bool {
	if tr.traceValue(v, traceEvent, make(map[ssa.Value]bool)) {
		return true
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

// loggerHasCtx traces a Logger value, the receiver of the direct logging
// call at, such as Logger.Print, to check if the Logger carries a context.
// Logger.Print builds its event from the Logger, as Logger.Info does, so
// the event takes the Logger's context.
//
//declscope:shared // checker.go asks this of each direct logging call's receiver
func (tr *tracer) loggerHasCtx(v ssa.Value, at ssa.Instruction) bool {
	return tr.traceReceiver(v, at, traceLogger, make(map[ssa.Value]bool))
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
// it is the same event. A path that makes the event again, in a loop, has a
// new one after. The recover block, reached after a panic, is not looked at,
// as it has no path from d.
func (tr *tracer) traceCtxSetAtReturns(v ssa.Value, d *ssa.Defer) bool {
	calls := make(map[ssa.Instruction]bool)
	defs := make(map[ssa.Instruction]bool) // Where the event is made, which starts another one in a loop
	for w := v; w != nil; w = traceEventReceiver(w) {
		tr.traceCtxCalls(w, d.Parent(), calls, make(map[ssa.Value]bool))
		if def, ok := w.(ssa.Instruction); ok {
			defs[def] = true
		}
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
		i := slices.IndexFunc(s.block.Instrs[s.from:], func(instr ssa.Instruction) bool {
			return calls[instr] || defs[instr]
		})
		if i >= 0 && defs[s.block.Instrs[s.from+i]] {
			return false // The event is made again, so a Ctx call after is on another one
		}
		if i >= 0 {
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

// traceEventReceiver returns the event that v, a call of an event method
// that returns the event it runs on, runs on, or nil for any other value.
// Event.CreateDict returns another event, so it is not followed back.
func traceEventReceiver(v ssa.Value) ssa.Value {
	call, ok := v.(*ssa.Call)
	if !ok {
		return nil
	}
	c, ok := calleeOf(&call.Call)
	if !ok || c.recv == nil || c.embedded != nil || !c.returnsSameEvent() {
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
// Each method of the event but CreateDict returns the event it runs on, so a
// Ctx call on any value of the chain counts. For a Phi, each edge must have one before the
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
// Event.Ctx called on it before the end of the block it comes from, or has
// context of its own.
func (tr *tracer) traceCtxSetOnEdges(phi *ssa.Phi, done map[tracePointValue]bool) bool {
	found := false
	for i, edge := range phi.Edges {
		if isNilConst(edge) {
			continue
		}
		pred := phi.Block().Preds[i]
		// An edge with context of its own needs no Ctx call in place
		if !tr.traceCtxSetInPlace(edge, tracePoint{block: pred, index: len(pred.Instrs)}, done) &&
			!tr.traceValue(edge, traceEvent, make(map[ssa.Value]bool)) {
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
	makes := make(map[ssa.Instruction]bool) // Where another event is in v on the path
	def, made := v.(ssa.Instruction)        // A parameter or a free variable is not made, but given at the entry
	if load, ok := asLoad(v); ok && parentFunc(load.X) == fn {
		// An event in memory, read by a load of its own at each use. A Ctx call
		// on any load of the same address counts, unless another event is
		// stored there in between.
		if loads, overwrites, ok := storeLoadsOf(load.X); ok {
			for _, other := range loads {
				tr.traceCtxCalls(other, fn, calls, make(map[ssa.Value]bool))
			}
			makes = overwrites
			at, _ := placeOf(load.X)
			_, local := at.root.(*ssa.Alloc)
			made = local
		}
	}
	if len(makes) == 0 && made {
		makes[def] = true
	}
	tr.traceCtxCalls(v, fn, calls, make(map[ssa.Value]bool))
	return tr.traceCtxCalledOnEveryPath(calls, makes, made, before)
}

// traceCtxCalledOnEveryPath reports whether every path that leads to before
// passes one of calls, the Event.Ctx calls on an event, after the last of
// makes on it, where another event is in its place. Such an instruction that
// stores an event with context of its own ends a path too. A path that leads
// back to the entry of the function passes none of makes, and has the event
// only when made is false: the event is given at the entry.
func (tr *tracer) traceCtxCalledOnEveryPath(
	calls, makes map[ssa.Instruction]bool,
	made bool,
	before tracePoint,
) bool {
	if len(calls) == 0 {
		return false
	}
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
			} else if makes[instr] {
				if !tr.traceStoredEventHasCtx(instr) {
					return false // v is made on this path, and Ctx does not run after
				}
				stopped = true // An event with context of its own is stored on this path
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

// traceStoredEventHasCtx reports whether instr, where another event is in a
// variable on the path, stores an event that has context of its own:
//
//	e := logger.Info()
//	e.Ctx(ctx)                      // in place, on the first iteration
//	for range n {
//	    e.Msg("x")
//	    e = logger.Warn().Ctx(ctx)  // of its own, on the later ones
//	}
func (tr *tracer) traceStoredEventHasCtx(instr ssa.Instruction) bool {
	store, ok := instr.(*ssa.Store)
	return ok && zerologapi.TypeOf(store.Val.Type()) == zerologapi.TypeEvent && tr.traceValue(store.Val, traceEvent, make(map[ssa.Value]bool))
}

// traceCtxCalls adds to calls the Event.Ctx calls in fn on the event v,
// directly or through a method value, or on a value that a method of v
// returns, which is the same event. A Ctx call given a fresh
// context.Background() or TODO() does not count, nor does one on the event
// Event.CreateDict returns, which is another one.
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
			if !ok || c.recv != v || c.embedded != nil || c.api.Recv != zerologapi.TypeEvent {
				continue
			}
			if c.api.Kind == zerologapi.KindSource {
				if !tr.givesFreshContext(&call.Call) {
					calls[call] = true
				}
			} else if c.returnsSameEvent() {
				tr.traceCtxCalls(call, fn, calls, seen)
			}
		}
	}
}

// foundBefore reports whether v was found to have context before, in the
// state st, so that a caller can skip the copy of visited it would trace v with.
//
//declscope:shared // phi.go skips the copy for an edge found before
func (tr *tracer) foundBefore(v ssa.Value, st traceState) bool {
	return tr.hasCtx[tracedValue{v: v, st: st}]
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
func (tr *tracer) traceValue(v ssa.Value, st traceState, visited map[ssa.Value]bool) bool {
	if visited[v] {
		return false
	}
	key := tracedValue{v: v, st: st}
	if tr.hasCtx[key] {
		return true
	}
	visited[v] = true

	if !tr.traceUncached(v, st, visited) {
		return false
	}
	tr.hasCtx[key] = true
	return true
}

// traceUncached does the work of traceValue for a value not yet found to
// have context.
func (tr *tracer) traceUncached(v ssa.Value, st traceState, visited map[ssa.Value]bool) bool {
	call, ok := v.(*ssa.Call)
	if !ok {
		return tr.traceCommon(v, st, visited)
	}

	c, declared := calleeOf(&call.Call)
	if !declared {
		if found, ok := tr.traceFuncLiteralResult(call, 0, st, visited); ok {
			return found
		}
		if call.Call.StaticCallee() != nil || len(call.Call.Args) == 0 {
			return false
		}
		// Dynamic dispatch: the method is not known
		return tr.traceReceiver(call.Call.Args[0], call, st, visited)
	}

	if st.setsCtx(c.api) {
		// A fresh context.Background() or TODO() given in place of the one in
		// scope replaces what the chain had
		return !tr.givesFreshContext(&call.Call)
	}
	if next, ok := st.receiverState(c.api); ok {
		return tr.traceCalleeReceiver(c, call, next, visited)
	}
	return false
}

// givesFreshContext reports whether cc, a call of a Ctx of zerolog, gives it
// a fresh context.Background() or TODO() (see isFreshContext). The context is
// the last argument in each form of the call.
func (tr *tracer) givesFreshContext(cc *ssa.CallCommon) bool {
	return len(cc.Args) > 0 && isFreshContext(cc.Args[len(cc.Args)-1], tr.positions)
}

// =============================================================================
// Common SSA Value Handling
// =============================================================================

// traceCommon handles common SSA value types (Phi, UnOp, FreeVar, etc.).
func (tr *tracer) traceCommon(v ssa.Value, st traceState, visited map[ssa.Value]bool) bool {
	switch val := v.(type) {
	case *ssa.Field, *ssa.Index:
		if found, ok := tr.traceSelectedValue(v, st, visited); ok {
			return found
		}
	case *ssa.Extract:
		if call, ok := val.Tuple.(*ssa.Call); ok {
			if found, ok := tr.traceFuncLiteralResult(call, val.Index, st, visited); ok {
				return found
			}
		}
		return tr.traceValue(val.Tuple, st, visited)
	case *ssa.Phi:
		return tr.tracePhi(val, st, visited)
	case *ssa.UnOp:
		return tr.traceUnOp(val, st, visited)
	case *ssa.Alloc:
		return tr.traceAlloc(val, st, visited)
	case *ssa.FreeVar:
		return tr.traceFreeVar(val, nil, st, visited)
	case *ssa.Parameter:
		// A pointer parameter can be updated in place; see storesReaching.
		if found, _ := tr.traceAddressAt(val, nil, st, visited); found {
			return true
		}
		return tr.traceArguments(val, st, visited)
	case *ssa.FieldAddr, *ssa.IndexAddr:
		// The address of a field or element is itself the receiver of a
		// pointer method, such as Logger.Info on s.log.
		if found, _ := tr.traceStoredValues(val, nil, st, visited); found {
			return true
		}
	}

	// Handle simple wrapper types that just need inner value tracing
	if inner := unwrapInner(v); inner != nil {
		return tr.traceValue(inner, st, visited)
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
func (tr *tracer) traceSelectedValue(v ssa.Value, st traceState, visited map[ssa.Value]bool) (found, ok bool) {
	var selections []sel
	for {
		from, s, ok := valueSel(v)
		if !ok {
			break
		}
		selections = append(selections, s)
		v = from
	}
	load, isLoad := asLoad(v)
	if !isLoad {
		return false, false
	}
	slices.Reverse(selections)
	at, _ := placeOf(load.X)
	stored, ok := storesReachingAt(at.with(selections...), load, tr.positions)
	if !ok {
		return false, false
	}
	return tr.traceStored(stored, st, visited), true
}

// =============================================================================
// Special Value Handling
// =============================================================================

// traceUnOp handles SSA unary operations, especially pointer dereferences.
// A load reads what the stores that reach it left at its address.
func (tr *tracer) traceUnOp(unop *ssa.UnOp, st traceState, visited map[ssa.Value]bool) bool {
	if unop.Op == token.MUL {
		if found, ok := tr.traceStoredValues(unop.X, unop, st, visited); ok {
			return found
		}
	}
	return tr.traceValue(unop.X, st, visited)
}

// traceAlloc handles SSA Alloc nodes (local variable allocation), reached
// where the read that uses it is not known, such as an edge of a Phi.
func (tr *tracer) traceAlloc(alloc *ssa.Alloc, st traceState, visited map[ssa.Value]bool) bool {
	found, _ := tr.traceStoredValues(alloc, nil, st, visited)
	return found
}

// traceStoredValues traces the values that can be at addr when read runs,
// or at any read of addr when read is nil. ok is false when storesReaching
// knows nothing of addr.
//
// Like the edges of a Phi, every value must have context, and a value that
// is already being traced is skipped: it is an update in a loop, which keeps
// the context of the value before it.
func (tr *tracer) traceStoredValues(addr ssa.Value, read ssa.Instruction, st traceState, visited map[ssa.Value]bool) (found, ok bool) {
	stored, ok := storesReaching(addr, read, tr.positions)
	if !ok {
		return false, false
	}
	return tr.traceStored(stored, st, visited), true
}

// traceStored reports whether what storesReaching found can be at an
// address has context, on every path, and on some path at all.
func (tr *tracer) traceStored(stored storeReaching, st traceState, visited map[ssa.Value]bool) bool {
	if stored.unknown {
		return false
	}
	found := false
	if stored.captured != nil {
		if !tr.traceFreeVar(stored.captured, stored.capturedPath, st, maps.Clone(visited)) {
			return false
		}
		found = true
	}
	for _, pp := range stored.params {
		if !tr.traceParamPath(pp, st, maps.Clone(visited)) {
			return false
		}
		found = true
	}
	for _, v := range stored.values {
		if visited[v] || isNilConst(v) {
			continue // A nil event logs nothing, as for an edge of a Phi
		}
		if tr.foundBefore(v, st) {
			found = true // With no need to copy visited for it
			continue
		}
		// Each store is traced on its own copy of visited, unless it is the
		// only one: copying on every step of a long chain of updates would
		// take time quadratic in its length.
		edgeVisited := visited
		if len(stored.values) > 1 {
			edgeVisited = maps.Clone(visited)
		}
		if !tr.traceValue(v, st, edgeVisited) {
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
func (tr *tracer) traceParamPath(pp storeParamPath, st traceState, visited map[ssa.Value]bool) bool {
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
		if !tr.traceArgumentAt(call.Common().Args[idx], call, pp, st, maps.Clone(visited)) {
			return false
		}
	}
	return true
}

// traceArgumentAt traces the value at the selections of pp from arg, which
// call passes for the parameter of pp.
func (tr *tracer) traceArgumentAt(arg ssa.Value, call ssa.CallInstruction, pp storeParamPath, st traceState, visited map[ssa.Value]bool) bool {
	addr, read := arg, ssa.Instruction(call)
	switch {
	case !pp.pointer:
		// A struct or array passed by value is loaded from where it is
		load, ok := asLoad(arg)
		if !ok {
			return false
		}
		addr, read = load.X, load
	case len(pp.path) == 0:
		return tr.traceReceiver(arg, call, st, visited)
	}
	at, _ := placeOf(addr)
	stored, ok := storesReachingAt(at.with(pp.path...), read, tr.positions)
	return ok && tr.traceStored(stored, st, visited)
}

// traceAddressAt traces addr, an address that read uses, such as the
// receiver of Logger.Info, which has a pointer receiver. A nil read stands
// for every read of addr. ok is false when addr is not one whose stores
// storesReaching can find.
func (tr *tracer) traceAddressAt(addr ssa.Value, read ssa.Instruction, st traceState, visited map[ssa.Value]bool) (found, ok bool) {
	if zerologapi.TypeOf(addr.Type()) == zerologapi.TypeEvent {
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
	return tr.traceStoredValues(addr, read, st, visited)
}

// traceArguments traces the arguments passed for param, when its function is
// a func literal that is only ever called where it is written, as an IIFE
// is. Every call must pass an argument with context.
//
//	func(e *zerolog.Event) {
//	    e.Msg("x")              // e is the argument below
//	}(logger.Info().Ctx(ctx))
func (tr *tracer) traceArguments(param *ssa.Parameter, st traceState, visited map[ssa.Value]bool) bool {
	fn := param.Parent()
	calls, ok := traceFuncLiteralCalls(fn)
	if !ok || len(calls) == 0 {
		return false
	}
	idx := slices.Index(fn.Params, param)
	passesOn := func(arg ssa.Value) bool {
		for v := arg; v != nil; v = traceEventReceiver(v) {
			if v == param {
				return true
			}
		}
		return false
	}
	found := false
	for _, call := range calls {
		arg := call.Common().Args[idx]
		if passesOn(arg) {
			continue // Passed on by a recursive call, as an update in a loop is
		}
		if !tr.traceValue(arg, st, maps.Clone(visited)) {
			return false
		}
		found = true
	}
	return found
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
		if load, ok := asLoad(v); ok {
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
func (tr *tracer) traceFuncLiteralResult(call *ssa.Call, idx int, st traceState, visited map[ssa.Value]bool) (found, ok bool) {
	fns, ok := traceFuncLiteralsOf(call.Call.Value, make(map[ssa.Value]bool))
	if sfn := call.Call.StaticCallee(); sfn != nil {
		fns, ok = []*ssa.Function{sfn}, sfn.Parent() != nil
	}
	if !ok || len(fns) == 0 {
		return false, false
	}
	return !slices.ContainsFunc(fns, func(fn *ssa.Function) bool {
		return !tr.traceReturns(fn, idx, call.Call.Args, st, maps.Clone(visited))
	}), true
}

// traceArgumentReturned reports whether result, returned by fn, is one of its
// parameters, or a chain of event methods on one, and the argument that args
// pass for it has context. The result of a call is what that call passes, not
// what every call of fn does, which traceArguments asks:
//
//	id := func(e *zerolog.Event) *zerolog.Event { return e }
//	a := id(logger.Info().Ctx(ctx)) // a is this argument
//	b := id(logger.Info())          // and b this one
func (tr *tracer) traceArgumentReturned(result ssa.Value, fn *ssa.Function, args []ssa.Value, st traceState, visited map[ssa.Value]bool) bool {
	for v := result; v != nil; v = traceEventReceiver(v) {
		param, ok := v.(*ssa.Parameter)
		if !ok {
			continue
		}
		idx := slices.Index(fn.Params, param)
		return idx >= 0 && idx < len(args) && tr.traceValue(args[idx], st, maps.Clone(visited))
	}
	return false
}

// traceFuncLiteralsOf returns the func literals that v can be, when it is a
// func literal or a closure of one, a Phi of such values, or a load of a
// variable that only such values are stored to:
//
//	ev := func() *zerolog.Event { return logger.Info().Ctx(ctx) }
//	if debug {
//	    ev = func() *zerolog.Event { return logger.Debug().Ctx(ctx) }
//	}
//	ev().Msg("x")           // ev is a Phi of the two
func traceFuncLiteralsOf(v ssa.Value, seen map[ssa.Value]bool) ([]*ssa.Function, bool) {
	if mc, ok := v.(*ssa.MakeClosure); ok {
		v = mc.Fn
	}
	if fn, ok := v.(*ssa.Function); ok {
		return []*ssa.Function{fn}, fn.Parent() != nil
	}
	if seen[v] {
		return nil, true // A loop, which adds nothing new
	}
	seen[v] = true
	var values []ssa.Value
	switch val := v.(type) {
	case *ssa.Phi:
		values = val.Edges
	case *ssa.UnOp:
		stored, ok := traceStoredFuncs(val)
		if !ok {
			return nil, false
		}
		values = stored
	default:
		return nil, false
	}
	var fns []*ssa.Function
	for _, value := range values {
		more, ok := traceFuncLiteralsOf(value, seen)
		if !ok {
			return nil, false
		}
		fns = append(fns, more...)
	}
	return fns, true
}

// traceStoredFuncs returns the values stored to the variable that load reads,
// when it is a local variable, or one a closure captures, whose address is
// only stored to, loaded and captured, in the function that declares it and
// the closures declared there:
//
//	f := func() *zerolog.Event { return logger.Warn() }
//	func() { _ = f }()      // f is kept in memory, and loaded to be called
//	f().Msg("x")
func traceStoredFuncs(load *ssa.UnOp) ([]ssa.Value, bool) {
	if load.Op != token.MUL {
		return nil, false
	}
	v := load.X
	for fv, ok := v.(*ssa.FreeVar); ok; fv, ok = v.(*ssa.FreeVar) {
		v = traceFreeVarBinding(fv)
	}
	alloc, ok := v.(*ssa.Alloc)
	if !ok {
		return nil, false // Not a local variable, or its binding is not found
	}
	aliases := map[ssa.Value]bool{alloc: true}
	scope := alloc.Parent()
	for changed := true; changed; {
		changed = false
		for instr := range traceInstrsInScope(scope) {
			mc, ok := instr.(*ssa.MakeClosure)
			if !ok {
				continue
			}
			closure, _ := mc.Fn.(*ssa.Function)
			for i, b := range mc.Bindings {
				if aliases[b] && closure != nil && !aliases[closure.FreeVars[i]] {
					aliases[closure.FreeVars[i]], changed = true, true
				}
			}
		}
	}
	var stored []ssa.Value
	for instr := range traceInstrsInScope(scope) {
		for _, op := range instr.Operands(nil) {
			if !aliases[*op] {
				continue
			}
			switch in := instr.(type) {
			case *ssa.Store:
				if op != &in.Addr {
					return nil, false // The address itself is stored
				}
				stored = append(stored, in.Val)
			case *ssa.UnOp, *ssa.MakeClosure:
				// A load of the variable, or a capture of it
			default:
				return nil, false // The address is passed on
			}
		}
	}
	return stored, true
}

// traceFreeVarBinding returns the value bound to fv where its closure is
// made, or nil when that is not found.
func traceFreeVarBinding(fv *ssa.FreeVar) ssa.Value {
	fn := fv.Parent()
	idx := slices.Index(fn.FreeVars, fv)
	var binding ssa.Value
	if parent := fn.Parent(); parent != nil { // Not a wrapper, such as a method value
		for instr := range instrsIn(parent) {
			if mc, ok := instr.(*ssa.MakeClosure); ok && mc.Fn == fn {
				binding = mc.Bindings[idx]
			}
		}
	}
	return binding
}

// traceIsFuncValue reports whether v is fn, or a closure of it.
func traceIsFuncValue(v ssa.Value, fn *ssa.Function) bool {
	if mc, ok := v.(*ssa.MakeClosure); ok {
		return mc.Fn == fn
	}
	return v == fn
}

// traceFreeVar traces a FreeVar back to the variable bound in MakeClosure, at
// path, the selections from it: nil for the variable itself.
func (tr *tracer) traceFreeVar(fv *ssa.FreeVar, path []sel, st traceState, visited map[ssa.Value]bool) bool {
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
		if idx < len(mc.Bindings) && tr.traceBinding(mc, idx, path, st, visited) {
			return true
		}
	}
	return false
}

// traceBinding traces what is at path in the variable that mc binds as its
// free variable idx, as the closure reads it: what can be there when the closure runs, not at every
// read of the variable in the function that makes it.
//
//	l := base
//	l.Debug().Ctx(ctx).Msg("start") // reads base, before the closure is made
//	l = l.With().Ctx(ctx).Logger()
//	defer func() { l.Info().Msg("end") }()
func (tr *tracer) traceBinding(mc *ssa.MakeClosure, idx int, path []sel, st traceState, visited map[ssa.Value]bool) bool {
	stored := storesReachingClosure(mc.Bindings[idx], path, mc, tr.positions)
	return tr.traceStored(stored, st, visited)
}

// receiverHasCtx reports whether the event c runs on, the receiver of the
// terminator at, has context, as eventHasCtx does. For a method expression of
// a promoted method, the event is read from the struct it is embedded in,
// and only the context of its own counts.
//
//declscope:shared // checker.go asks this of each terminator
func (tr *tracer) receiverHasCtx(c callee, at ssa.Instruction) bool {
	if c.embedded != nil {
		return tr.traceEmbeddedReceiver(c, at, traceEvent, make(map[ssa.Value]bool)) ||
			tr.traceEmbeddedCtxSetInPlace(c, at)
	}
	return tr.eventHasCtx(c.recv, at)
}

// traceCalleeReceiver traces the receiver of c, which call runs, as
// traceReceiver does, or through the struct it is embedded in.
func (tr *tracer) traceCalleeReceiver(c callee, call ssa.Instruction, st traceState, visited map[ssa.Value]bool) bool {
	if c.embedded != nil {
		return tr.traceEmbeddedReceiver(c, call, st, visited)
	}
	return tr.traceReceiver(c.recv, call, st, visited)
}

// traceEmbeddedReceiver traces the receiver of c, a method expression of a
// promoted method, which call runs. c.recv is the struct the receiver is
// embedded in, or a pointer to it, and the receiver is what the stores that
// reach call left at the embedded fields of it:
//
//	t5 = *t1                               // the struct, loaded
//	t6 = (myEvent).Msg$thunk(t5, "x")      // reads t1.Event
//
// The struct must be loaded from an address, or be a pointer, whose stores
// storesReachingAt can find. Otherwise nothing is found.
func (tr *tracer) traceEmbeddedReceiver(c callee, call ssa.Instruction, st traceState, visited map[ssa.Value]bool) bool {
	addr, typ, read, ok := traceEmbeddedStart(c, call)
	if !ok {
		return false
	}
	at, _ := placeOf(addr)
	return tr.traceEmbeddedAt(at, typ, c.embedded, read, call, st, visited)
}

// traceEmbeddedStart returns where the struct c.recv is, for c, a method
// expression of a promoted method that call runs: the address the struct is
// at, its type, and the instruction that reads it there. ok is false when the
// struct is a value that is not loaded from an address.
func traceEmbeddedStart(c callee, call ssa.Instruction) (addr ssa.Value, typ types.Type, read ssa.Instruction, ok bool) {
	if ptr, isPtr := c.recv.Type().Underlying().(*types.Pointer); isPtr {
		return c.recv, ptr.Elem(), call, true
	}
	load, isLoad := asLoad(c.recv)
	if !isLoad {
		return nil, nil, nil, false
	}
	return load.X, c.recv.Type(), load, true
}

// traceEmbeddedAt traces the receiver at the embedded fields of the struct of
// type typ at the address at, as read reads it. call is the call
// of the method expression.
//
// An embedded field but the last may be a pointer. The receiver is then at
// the rest of the fields of what each pointer that can be stored there
// points to, as call reads it, and every one must have context:
//
//	type outer struct{ *inner }
//	o := outer{&inner{e}}  // t3 = new inner ; *&t3.Event = e ; *&t1.inner = t3
//	outer.Msg(o, "x")      // reads t1.inner, then t3.Event
func (tr *tracer) traceEmbeddedAt(
	at place,
	typ types.Type,
	embedded []int,
	read, call ssa.Instruction,
	st traceState,
	visited map[ssa.Value]bool,
) bool {
	at, n, field := traceEmbeddedPath(typ, at, embedded)
	stored, ok := storesReachingAt(at, read, tr.positions)
	if n == len(embedded) {
		return ok && tr.traceStored(stored, st, visited)
	}
	if !ok || stored.unknown || stored.captured != nil || len(stored.params) > 0 {
		return false
	}
	found := false
	for _, v := range stored.values {
		if isNilConst(v) {
			continue // A nil pointer has no event to log
		}
		next, _ := placeOf(v)
		elem := field.Underlying().(*types.Pointer).Elem()
		if !tr.traceEmbeddedAt(next, elem, embedded[n:], call, call, st, maps.Clone(visited)) {
			return false
		}
		found = true
	}
	return found
}

// traceEmbeddedPath returns at, the address of a struct of type typ, with the
// embedded fields selected from it, one after another, up to the first that
// is a pointer to a struct, which is a field but the last. n is how many it
// selected, and field is the type of the last of them.
func traceEmbeddedPath(typ types.Type, at place, embedded []int) (_ place, n int, field types.Type) {
	at = at.with()
	for i, index := range embedded {
		// Each embedded field but the last is a struct or a pointer to one
		typ = typ.Underlying().(*types.Struct).Field(index).Type()
		at.path = append(at.path, sel{field: index})
		if _, isPtr := typ.Underlying().(*types.Pointer); isPtr && i < len(embedded)-1 {
			return at, i + 1, typ
		}
	}
	return at, len(embedded), typ
}

// traceEmbeddedCtxSetInPlace reports whether Event.Ctx is called in place,
// with its result unused or not, on the event at the embedded fields of
// c.recv before call reads it, as traceCtxCalledBefore does for an event in
// memory. A Ctx call on any read of the event where the struct is counts,
// unless another event is stored there in between: a load of the event, or
// a selection of it from a load of the struct, or of a struct it is embedded
// in, as each load of a struct value of a local variable is:
//
//	me := myEvent{logger.Info()}
//	me.Ctx(ctx)            // t4 = *t1 ; t5 = t4.Event ; Ctx(t5, ctx)
//	myEvent.Msg(me, "x")   // Msg$thunk(t4, "x") reads t4.Event
//
// Each embedded field but the last must be a struct, not a pointer to one.
func (tr *tracer) traceEmbeddedCtxSetInPlace(c callee, call ssa.Instruction) bool {
	addr, typ, _, ok := traceEmbeddedStart(c, call)
	if !ok {
		return false
	}
	base, _ := placeOf(addr)
	at, n, _ := traceEmbeddedPath(typ, base, c.embedded)
	if n < len(c.embedded) {
		return false // Reading past a pointer is a load, which a path does not take
	}
	_, overwrites, ok := storeLoadsAt(at)
	if !ok {
		return false
	}
	fn := call.Parent()
	calls := make(map[ssa.Instruction]bool)
	for k := len(base.path); k <= len(at.path); k++ {
		loads, _, _ := storeLoadsAt(at.prefix(k))
		for _, load := range loads {
			for _, ev := range traceEmbeddedSelections(load, c.embedded[k-len(base.path):]) {
				tr.traceCtxCalls(ev, fn, calls, make(map[ssa.Value]bool))
			}
		}
	}
	_, local := at.root.(*ssa.Alloc)
	before := tracePoint{block: call.Block(), index: tr.positions.of(call)}
	return tr.traceCtxCalledOnEveryPath(calls, overwrites, local, before)
}

// traceEmbeddedSelections returns the Field instructions that select the embedded
// fields from the struct value v, one after another: each value of the last
// field the function selects from v, or v itself when there are none.
func traceEmbeddedSelections(v ssa.Value, embedded []int) []ssa.Value {
	values := []ssa.Value{v}
	for _, index := range embedded {
		var next []ssa.Value
		for _, w := range values {
			for _, ref := range *w.Referrers() {
				if f, ok := ref.(*ssa.Field); ok && f.X == w && f.Field == index {
					next = append(next, f)
				}
			}
		}
		values = next
	}
	return values
}

// traceReceiver traces recv, the receiver of call, a method call. It is the
// first argument, or what a method value is bound to. A pointer receiver that
// is a variable, or a field or element of one, holds what the stores that
// reach the call left there.
func (tr *tracer) traceReceiver(recv ssa.Value, call ssa.Instruction, st traceState, visited map[ssa.Value]bool) bool {
	if found, ok := tr.traceAddressAt(recv, call, st, visited); ok {
		return found
	}
	return tr.traceValue(recv, st, visited)
}

// traceReturns reports whether the value at resultIndex from every return of
// fn has context, as for an IIFE (Immediately Invoked Function Expression).
// args are what the call whose result is traced passes, which a parameter
// returned stands for.
func (tr *tracer) traceReturns(fn *ssa.Function, resultIndex int, args []ssa.Value, st traceState, visited map[ssa.Value]bool) bool {
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
		result := ret.Results[resultIndex]
		if !tr.traceValue(result, st, maps.Clone(visited)) && !tr.traceArgumentReturned(result, fn, args, st, visited) {
			return false
		}
	}

	return hasReturn
}
