// event.go tells which event a value of a chain is, and whether Event.Ctx
// runs on that event in place, with its result unused or not. Event.Ctx sets
// the context on the event it runs on, and returns that same event:
//
//	e := logger.Info()
//	e.Ctx(ctx)              // sets the context of e
//	e.Str("k", "v").Msg("x")
//
// An event is known by its origin, the call that makes it. A Ctx call counts
// for a terminator when it runs on a value with the same origin, on every
// path from the origin to the terminator. resolve.go finds what a read from
// memory sees, so an event in a variable or a field is the same event at each
// read, until another one is stored there.

package chain

import (
	"iter"
	"slices"

	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/zerologapi"
)

// eventOrigin walks v back through the methods of the event that return the
// event they run on, such as Event.Str and Event.Ctx, to the value that made
// it. Event.CreateDict returns another event, so it is not followed back.
//
//declscope:shared // tracing.go takes a func literal's result back to a parameter by it
func eventOrigin(v ssa.Value) ssa.Value {
	for {
		call, ok := v.(*ssa.Call)
		if !ok {
			return v
		}
		c, ok := calleeOf(&call.Call)
		if !ok || c.recv == nil || c.embedded != nil || !c.returnsSameEvent() {
			return v
		}
		v = c.recv
	}
}

// eventSource is one event a value can be: the value that made it, or, when
// v is nil, the one the function cannot tell that is at the place at, such as
// the one the caller passed through a pointer parameter. Two reads of the same
// place that see only such an event see the same one.
type eventSource struct {
	v  ssa.Value
	at placeKey
}

// eventStep is what a value of a chain is one step back: other values that
// are the same event, and the events it can be that go back no further.
type eventStep struct {
	next    []ssa.Value
	sources []eventSource
	// read is the place the value is read from, for a read from memory.
	read *placeKey
}

// eventCtxCall is a call of Event.Ctx given a context that is not fresh, and
// the events it can run on.
type eventCtxCall struct {
	call    *ssa.Call
	sources []eventSource
}

// eventIndex finds the events each value of a chain can be, and keeps what it
// found, which depends on the SSA alone.
//
//declscope:shared // tracing.go keeps one per tracer
type eventIndex struct {
	// positions finds where each instruction is in its block.
	//
	//declscope:private
	positions instrPositions
	// closures indexes the closures and func literals of the package.
	//
	//declscope:private
	closures *closures
	// steps holds the step back of each value asked about.
	//
	//declscope:private
	steps map[ssa.Value]eventStep
	// ctxCalls holds the Ctx calls of each function asked about.
	//
	//declscope:private
	ctxCalls map[*ssa.Function][]eventCtxCall
	// active holds the parameters of func literals being resolved.
	//
	//declscope:private
	active map[*ssa.Parameter]bool
}

// newEventIndex creates an index with nothing found yet.
//
//declscope:shared // tracing.go creates one per tracer
func newEventIndex(positions instrPositions, closures *closures) *eventIndex {
	return &eventIndex{
		positions: positions,
		closures:  closures,
		steps:     make(map[ssa.Value]eventStep),
		ctxCalls:  make(map[*ssa.Function][]eventCtxCall),
		active:    make(map[*ssa.Parameter]bool),
	}
}

// eventCtxSetInPlace reports whether Event.Ctx is called in place on the event
// that c, the terminator at, runs on, before at on every path. For a deferred
// terminator, a Ctx call on every path from the defer statement to a return
// counts too, since the terminator runs there:
//
//	e := logger.Info()
//	defer e.Msg("x")        // runs at the return, after Ctx
//	e.Ctx(ctx)
//
// Each event the receiver can be must have one, on the paths that carry it.
// hasCtx tells whether a value has context of its own, which a path that
// stores it, or takes it at a Phi, needs no Ctx call for.
//
//declscope:shared // tracing.go asks this of each terminator whose chain has no context
func (ix *eventIndex) eventCtxSetInPlace(c callee, at ssa.Instruction, hasCtx func(ssa.Value) bool) bool {
	q := ix.queryOf(c, at, hasCtx)
	if len(q.sources) == 0 {
		return false
	}
	before := walkPointBefore(at, ix.positions)
	d, deferred := at.(*ssa.Defer)
	for _, src := range q.sources {
		calls := q.ctxCallsOn(src)
		if !q.ctxRunsBefore(src, calls, before) && (!deferred || !q.ctxRunsAfterDefer(src, calls, d)) {
			return false
		}
	}
	return true
}

// eventQuery is what a terminator runs on: the events it can be, and the
// values it is read through, which a path takes one of the events by.
type eventQuery struct {
	ix     *eventIndex
	fn     *ssa.Function
	hasCtx func(ssa.Value) bool
	// sources are the events the receiver can be.
	sources []eventSource
	// values are the values the receiver is the same event as, back to the
	// sources: the Phis it takes an edge of, and the values stored where it
	// is read from.
	values map[ssa.Value]bool
	// reads are the places it is read from on the way.
	reads map[placeKey]bool
}

// queryOf finds the events that c, the terminator at, can run on.
func (ix *eventIndex) queryOf(c callee, at ssa.Instruction, hasCtx func(ssa.Value) bool) eventQuery {
	q := eventQuery{
		ix:     ix,
		fn:     at.Parent(),
		hasCtx: hasCtx,
		values: make(map[ssa.Value]bool),
		reads:  make(map[placeKey]bool),
	}
	q.sources = ix.expand(ix.calleeStep(c, at), q.values, q.reads)
	return q
}

// ctxCallsOn returns the Ctx calls of the function that run on src: on a
// value that can be src, and no event the receiver cannot be.
func (q eventQuery) ctxCallsOn(src eventSource) map[ssa.Instruction]bool {
	calls := make(map[ssa.Instruction]bool)
	for _, cc := range q.ix.ctxCallsIn(q.fn) {
		if slices.Contains(cc.sources, src) && eventSourcesWithin(cc.sources, q.sources) {
			calls[cc.call] = true
		}
	}
	return calls
}

// ctxRunsBefore reports whether every path that leads to before, and carries
// src, passes one of calls after src is made. A path that leads back to the
// entry of the function carries src only when the function does not make it,
// as for a parameter. A path carries another event when it takes an edge of a
// Phi, or stores a value, that is not src or has context of its own.
func (q eventQuery) ctxRunsBefore(src eventSource, calls map[ssa.Instruction]bool, before walkPoint) bool {
	def := q.defOf(src)
	visit := func(instr ssa.Instruction) walkStep {
		switch {
		case calls[instr]:
			return walkStop // Ctx runs on this path
		case instr == def:
			return walkAbort // src is made on this path, and Ctx does not run after
		case q.storesOther(instr, src):
			return walkStop // Another event is stored on this path
		}
		return walkOn
	}
	atEntry := func() bool { return def != nil }
	edge := func(b *ssa.BasicBlock, i int, s struct{}) (struct{}, bool) {
		return s, q.edgeCarries(b, i, src)
	}
	return walkBackWith([]walkFrom[struct{}]{{point: before}}, visit, atEntry, edge)
}

// ctxRunsAfterDefer reports whether, on every path from d, a deferred
// terminator, to a return, one of calls runs on src. A path that makes src
// again, in a loop, has another event after. The recover block, reached after
// a panic, is not looked at, as it has no path from d.
func (q eventQuery) ctxRunsAfterDefer(src eventSource, calls map[ssa.Instruction]bool, d *ssa.Defer) bool {
	if len(calls) == 0 {
		return false // A path that never returns does not end the walk
	}
	def := q.defOf(src)
	return walkFwd(walkPointAfter(d, q.ix.positions), func(instr ssa.Instruction) walkStep {
		switch {
		case instr == def:
			return walkAbort // The event is made again, so a Ctx call after is on another one
		case calls[instr]:
			return walkStop // Ctx runs on this path
		}
		if _, ok := instr.(*ssa.Return); ok {
			return walkAbort
		}
		return walkOn
	})
}

// defOf returns the instruction of the function that makes src, or nil when
// the function is given it: a parameter, a free variable, an event made in
// another function, or one it cannot tell.
func (q eventQuery) defOf(src eventSource) ssa.Instruction {
	if instr, ok := src.v.(ssa.Instruction); ok && instr.Parent() == q.fn {
		return instr
	}
	return nil
}

// storesOther reports whether instr stores, where the receiver is read from,
// a value it is read through, which is another event than src, or has
// context of its own:
//
//	e := logger.Info()
//	e.Ctx(ctx)                      // in place, on the first iteration
//	for range n {
//	    e.Msg("x")
//	    e = logger.Warn().Ctx(ctx)  // of its own, on the later ones
//	}
func (q eventQuery) storesOther(instr ssa.Instruction, src eventSource) bool {
	store, ok := instr.(*ssa.Store)
	if !ok || !q.values[store.Val] {
		return false
	}
	at, _ := placeOf(store.Addr)
	return q.reads[at.key()] && q.isOther(store.Val, src)
}

// edgeCarries reports whether a path that reaches the top of b goes on into
// its i-th predecessor carrying src: no Phi of b that the receiver is read
// through takes nil there, or another event than src, or one with context of
// its own.
func (q eventQuery) edgeCarries(b *ssa.BasicBlock, i int, src eventSource) bool {
	for _, instr := range b.Instrs {
		phi, ok := instr.(*ssa.Phi)
		if !ok {
			break
		}
		if !q.values[phi] {
			continue
		}
		if edge := phi.Edges[i]; isNilConst(edge) || q.isOther(edge, src) {
			return false
		}
	}
	return true
}

// isOther reports whether v, a value the receiver is read through, is another
// event than src, or has context of its own. A value that leads back to
// itself, as an update in a loop does, can be any of them.
func (q eventQuery) isOther(v ssa.Value, src eventSource) bool {
	sources := q.ix.sourcesOf(v)
	return (len(sources) > 0 && !slices.Contains(sources, src)) || q.hasCtx(v)
}

// eventSourcesWithin reports whether each of sources is one of within.
func eventSourcesWithin(sources, within []eventSource) bool {
	for _, src := range sources {
		if !slices.Contains(within, src) {
			return false
		}
	}
	return true
}

// ctxCallsIn returns the calls of Event.Ctx in fn given a context that is not
// fresh, with the events each runs on. A Ctx call given a fresh
// context.Background() or TODO() does not set the context.
func (ix *eventIndex) ctxCallsIn(fn *ssa.Function) []eventCtxCall {
	if calls, ok := ix.ctxCalls[fn]; ok {
		return calls
	}
	var calls []eventCtxCall
	for instr := range instrsIn(fn) {
		call, ok := instr.(*ssa.Call)
		if !ok {
			continue
		}
		c, ok := calleeOf(&call.Call)
		if !ok || c.recv == nil || c.api.Kind != zerologapi.KindSource || c.api.Recv != zerologapi.TypeEvent {
			continue
		}
		if args := call.Call.Args; len(args) > 0 && isFreshContext(args[len(args)-1], ix.positions, ix.closures) {
			continue
		}
		sources := ix.expand(ix.calleeStep(c, call), make(map[ssa.Value]bool), nil)
		calls = append(calls, eventCtxCall{call: call, sources: sources})
	}
	ix.ctxCalls[fn] = calls
	return calls
}

// sourcesOf returns the events v can be.
func (ix *eventIndex) sourcesOf(v ssa.Value) []eventSource {
	return ix.expand(eventStep{next: []ssa.Value{v}}, make(map[ssa.Value]bool), nil)
}

// expand returns the events that start can be, adding to values each value
// it is the same event as on the way, and to reads, when not nil, each place
// it is read from. A value already in values adds nothing, so a cycle ends.
func (ix *eventIndex) expand(start eventStep, values map[ssa.Value]bool, reads map[placeKey]bool) []eventSource {
	var sources []eventSource
	add := func(step eventStep) {
		for _, src := range step.sources {
			if !slices.Contains(sources, src) {
				sources = append(sources, src)
			}
		}
		if step.read != nil && reads != nil {
			reads[*step.read] = true
		}
	}
	var visit func(v ssa.Value)
	visit = func(v ssa.Value) {
		if values[v] {
			return
		}
		values[v] = true
		step := ix.stepOf(v)
		add(step)
		for _, next := range step.next {
			visit(next)
		}
	}
	add(start)
	for _, next := range start.next {
		visit(next)
	}
	return sources
}

// calleeStep is the step back from the receiver of c, which call runs: the
// receiver itself, or, for a method expression of a promoted method, what is
// at the embedded fields of the struct it is passed.
func (ix *eventIndex) calleeStep(c callee, call ssa.Instruction) eventStep {
	if c.embedded == nil {
		return eventStep{next: []ssa.Value{c.recv}}
	}
	addr, typ, read, ok := c.embeddedStart(call)
	if !ok {
		return eventStep{sources: []eventSource{{v: c.recv}}}
	}
	at, _ := placeOf(addr)
	return ix.resolvedStep(ix.resolver().resolveEmbedded(at, typ, c.embedded, read, call), at.withFields(c.embedded))
}

// stepOf returns the step back from v, which it finds once:
//
//   - A method of the event that returns the event it runs on goes back to
//     its receiver (see eventOrigin).
//   - A Phi goes back to each edge but nil.
//   - A read from memory goes back to what resolve finds there.
//
// Any other value is an event of its own.
func (ix *eventIndex) stepOf(v ssa.Value) eventStep {
	if step, ok := ix.steps[v]; ok {
		return step
	}
	step := ix.findStep(v)
	ix.steps[v] = step
	return step
}

// findStep does the work of stepOf for a value not asked about yet.
func (ix *eventIndex) findStep(v ssa.Value) eventStep {
	switch val := v.(type) {
	case *ssa.Const:
		if isNilConst(val) {
			return eventStep{} // A nil event logs nothing
		}
	case *ssa.Call:
		if c, ok := calleeOf(&val.Call); ok && c.recv != nil && c.returnsSameEvent() {
			return ix.calleeStep(c, val)
		}
	case *ssa.Phi:
		var step eventStep
		for _, edge := range val.Edges {
			if !isNilConst(edge) {
				step.next = append(step.next, edge)
			}
		}
		return step
	case *ssa.UnOp:
		if _, isLoad := asLoad(val); isLoad {
			if sources, ok := ix.resolver().resolve(val.X, val); ok {
				at, _ := placeOf(val.X)
				return ix.resolvedStep(sources, at)
			}
		}
	case *ssa.Field, *ssa.Index:
		if at, load, ok := loadedSelection(v); ok {
			if sources, ok := ix.resolver().resolveAt(at, load); ok {
				return ix.resolvedStep(sources, at)
			}
		}
	}
	return eventStep{sources: []eventSource{{v: v}}}
}

// resolvedStep is the step back to sources, what a read of the place at can
// see. A source that resolve cannot tell is the event at that place.
func (ix *eventIndex) resolvedStep(sources iter.Seq[resolved], at place) eventStep {
	read := at.key()
	step := eventStep{read: &read}
	for src := range sources {
		switch {
		case src.v == nil:
			step.sources = append(step.sources, eventSource{at: read})
		case src.pointer:
			step.sources = append(step.sources, eventSource{at: place{root: src.v}.key()})
		case !isNilConst(src.v):
			step.next = append(step.next, src.v)
		}
	}
	return step
}

// resolver returns a resolver that resolves a parameter of a func literal
// once at a time: one reached again, through a recursive call, adds nothing.
func (ix *eventIndex) resolver() resolver {
	return resolver{
		positions: ix.positions,
		closures:  ix.closures,
		enter: func(param *ssa.Parameter, expand func()) {
			if ix.active[param] {
				return
			}
			ix.active[param] = true
			expand()
			delete(ix.active, param)
		},
	}
}
