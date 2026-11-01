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
// path from the origin to the terminator. ssaflow.Resolver finds what a read
// from memory sees, so an event in a variable or a field is the same event at
// each read, until another one is stored there.
//
// An origin in a loop makes an event on each run. A value that holds the
// event of an earlier run, as one kept from the last iteration does, is not
// the same event as one that holds the latest (see eventRuns).
//
// A Ctx call given a fresh context.Background() or TODO() replaces the
// context of the event too, so the last Ctx call on a path decides:
//
//	e := logger.Info().Ctx(ctx)
//	e.Ctx(context.TODO())   // replaces ctx
//	e.Msg("x")              // reported

package chain

import (
	"iter"

	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/ssaflow"
	"github.com/mpyw/zerologlintctx/internal/zerologapi"
)

// eventOrigin walks v back through the methods of the event that return the
// event they run on, such as Event.Str and Event.Ctx, to the value that made
// it. Event.CreateDict returns another event, so it is not followed back.
//
//declscope:shared // tracing.go takes a func literal's result back to a parameter by it
func eventOrigin(v ssa.Value, closures *ssaflow.Closures) ssa.Value {
	for {
		call, ok := v.(*ssa.Call)
		if !ok {
			return v
		}
		c, ok := calleeOf(&call.Call, closures)
		if !ok || c.Recv == nil || c.Embedded != nil || !c.returnsSameEvent() {
			return v
		}
		v = c.Recv
	}
}

// eventSource is one event a value can be: the value that made it, or, when
// v is nil, the one the function cannot tell that is at the place at, such as
// the one the caller passed through a pointer parameter. Two reads of the same
// place that see only such an event see the same one.
type eventSource struct {
	v  ssa.Value
	at ssaflow.PlaceKey
}

// eventSources is a set of events, in the order they were found.
type eventSources struct {
	list []eventSource
	set  map[eventSource]bool
}

// add adds src, unless it is in the set already.
func (s *eventSources) add(src eventSource) {
	if s.set[src] {
		return
	}
	if s.set == nil {
		s.set = make(map[eventSource]bool)
	}
	s.set[src] = true
	s.list = append(s.list, src)
}

// has reports whether src is in the set.
func (s *eventSources) has(src eventSource) bool {
	return s.set[src]
}

// same reports whether s and o are the same set.
func (s *eventSources) same(o *eventSources) bool {
	if len(s.list) != len(o.list) {
		return false
	}
	for _, src := range s.list {
		if !o.has(src) {
			return false
		}
	}
	return true
}

// eventStep is what a value of a chain is one step back: other values that
// are the same event, and the events it can be that go back no further.
type eventStep struct {
	next    []ssa.Value
	sources []eventSource
	// read is the place the value is read from, for a read from memory.
	read *ssaflow.PlaceKey
	// readVariable reports that read is an element at an index that is not
	// a constant.
	readVariable bool
	// more are the other steps the value is too, each a read of its own, for
	// a value selected from a Phi of struct or array values.
	more []eventStep
	// load is the load a step of more reads from, and through the edges of
	// Phis the value loaded is taken through, as ssaflow.LoadedRead has them.
	load    ssa.Instruction
	through []ssaflow.LoadedEdge
}

// eventCtxCall is a call of Event.Ctx, and the events it can run on.
type eventCtxCall struct {
	call    *ssa.Call
	sources *eventSources
	// fresh reports that the call is given a fresh context (see
	// isFreshContext), which replaces the context of the event.
	fresh bool
	// recv is the step back from the receiver.
	recv eventStep
	// runs is what eventIndex.ctxRunsOf finds runs in, built at the first
	// source asked about.
	runs *eventRunGraph
}

// eventCtxCalls are the calls of Event.Ctx of a function, and those given a
// context that is not fresh that a func literal called in place runs, at each
// call of it (see eventIndex.addClosureCtxCalls).
type eventCtxCalls struct {
	// at holds the calls that run at each instruction.
	at map[ssa.Instruction][]*eventCtxCall
	// on holds the calls given a context that is not fresh that can run on
	// each event.
	on map[eventSource][]*eventCtxCall
	// fresh reports that one of the calls is given a fresh context.
	fresh bool
}

// eventCtxFound is what a Ctx call at an instruction does to an event.
type eventCtxFound int

const (
	// eventCtxNone is no Ctx call on the event.
	eventCtxNone eventCtxFound = iota
	// eventCtxSet is a Ctx call that sets the context in scope.
	eventCtxSet
	// eventCtxFresh is a Ctx call that replaces the context with a fresh one.
	eventCtxFresh
)

// eventReturnWalk is a walk to the returns of a function, from a point, with
// the event's context set there or not.
type eventReturnWalk struct {
	from ssaflow.WalkPoint
	set  bool
}

// eventIndex finds the events each value of a chain can be, and keeps what it
// found, which depends on the SSA alone.
//
//declscope:shared // tracing.go keeps one per tracer
type eventIndex struct {
	// positions finds where each instruction is in its block.
	//
	//declscope:private
	positions ssaflow.Positions
	// closures indexes the closures and func literals of the package.
	//
	//declscope:private
	closures *ssaflow.Closures
	// places keys the places read through a pointer that is not in a
	// variable, whose events resolve cannot follow.
	//
	//declscope:private
	places *ssaflow.MatchedPlaces
	// steps holds the step back of each value asked about.
	//
	//declscope:private
	steps map[ssa.Value]eventStep
	// sources holds the events each value asked about can be.
	//
	//declscope:private
	sources map[ssa.Value]*eventSources
	// ctxCalls holds the Ctx calls of each function asked about.
	//
	//declscope:private
	ctxCalls map[*ssa.Function]*eventCtxCalls
	// active holds the parameters of func literals being resolved.
	//
	//declscope:private
	active map[*ssa.Parameter]bool
	// loops holds, for each origin asked about, whether it can run again
	// after it runs, in a loop.
	//
	//declscope:private
	loops map[ssa.Instruction]bool
	// reachable holds the test of eventIndex.reaches for each point and
	// origin asked about.
	//
	//declscope:private
	reachable map[eventReach]func(ssaflow.WalkPoint) bool
	// ctxRuns holds the runs each Ctx call can run on, for each origin in a
	// loop asked about.
	//
	//declscope:private
	ctxRuns map[eventCtxRun]*eventRuns
	// work counts the values expanded, the instructions and edges walked,
	// and the events compared, which the tests bound.
	//
	//declscope:private
	work int
}

// newEventIndex creates an index with nothing found yet.
//
//declscope:shared // tracing.go creates one per tracer
func newEventIndex(positions ssaflow.Positions, closures *ssaflow.Closures) *eventIndex {
	return &eventIndex{
		positions: positions,
		closures:  closures,
		places:    ssaflow.NewMatchedPlaces(),
		steps:     make(map[ssa.Value]eventStep),
		sources:   make(map[ssa.Value]*eventSources),
		ctxCalls:  make(map[*ssa.Function]*eventCtxCalls),
		active:    make(map[*ssa.Parameter]bool),
		loops:     make(map[ssa.Instruction]bool),
		reachable: make(map[eventReach]func(ssaflow.WalkPoint) bool),
		ctxRuns:   make(map[eventCtxRun]*eventRuns),
	}
}

// eventCtxSetInPlace reports whether the event that c, the terminator at,
// runs on has context at at, by the Ctx calls that run on it in place. The
// last Ctx call on a path decides. inChain tells that the chain of the
// receiver has context of its own, which holds on a path with no Ctx call:
//
//   - With inChain, no path to at may end with a Ctx call given a fresh
//     context.
//   - Without it, every path to at must pass a Ctx call given the context in
//     scope, after the event is made, and no fresh one after that.
//
// For a deferred terminator, what holds at each return after the defer
// statement decides, since the terminator runs there:
//
//	e := logger.Info()
//	defer e.Msg("x")        // runs at the return, after Ctx
//	e.Ctx(ctx)
//
// Each event the receiver can be must have context, on the paths that carry
// it. hasCtx tells whether a value has context of its own, which a path that
// stores it, or takes it at a Phi, needs no Ctx call for.
//
//declscope:shared // tracing.go asks this of each terminator
func (ix *eventIndex) eventCtxSetInPlace(c callee, at ssa.Instruction, inChain bool, hasCtx func(ssa.Value) bool) bool {
	calls := ix.ctxCallsIn(at.Parent())
	if len(calls.at) == 0 || inChain && !calls.fresh {
		return inChain // No Ctx call that changes the context
	}
	q := ix.queryOf(c, at, hasCtx, calls)
	if len(q.sources.list) == 0 {
		return inChain
	}
	before := ssaflow.WalkPointBefore(at, ix.positions)
	d, deferred := at.(*ssa.Defer)
	for _, src := range q.sources.list {
		set := q.ctxSetBefore(src, before, inChain)
		if deferred {
			set = q.ctxSetAfterDefer(src, d, set)
		}
		if !set {
			return false
		}
	}
	return true
}

// ctxSetBefore reports whether src has context at before, by the last Ctx
// call on src on each path that carries it there. inChain tells that src has
// context with no Ctx call, as eventIndex.eventCtxSetInPlace has it.
func (q *eventQuery) ctxSetBefore(src eventSource, before ssaflow.WalkPoint, inChain bool) bool {
	if inChain {
		return !q.freshCtxLastBefore(src, before)
	}
	return q.ctxRunsBefore(src, before)
}

// eventQuery is what a terminator runs on: the events it can be, and the
// values it is read through, which a path takes one of the events by.
type eventQuery struct {
	ix     *eventIndex
	fn     *ssa.Function
	hasCtx func(ssa.Value) bool
	// at is the terminator, and recv the step back from its receiver.
	at   ssa.Instruction
	recv eventStep
	// runs holds the runs the receiver can hold of each source asked about.
	runs map[eventSource]*eventRuns
	// graph is what runs is found in, built at the first source asked about.
	graph *eventRunGraph
	// calls are the Ctx calls of the function.
	calls *eventCtxCalls
	// within holds, for each Ctx call looked at, whether it runs on no event
	// the receiver cannot be.
	within map[*eventCtxCall]bool
	// sources are the events the receiver can be.
	sources *eventSources
	// values are the values the receiver is the same event as, back to the
	// sources: the Phis it takes an edge of, and the values stored where it
	// is read from.
	values map[ssa.Value]bool
	// reads are the places it is read from on the way.
	reads map[ssaflow.PlaceKey]bool
}

// queryOf finds the events that c, the terminator at, can run on.
func (ix *eventIndex) queryOf(c callee, at ssa.Instruction, hasCtx func(ssa.Value) bool, calls *eventCtxCalls) *eventQuery {
	q := &eventQuery{
		ix:     ix,
		fn:     at.Parent(),
		hasCtx: hasCtx,
		at:     at,
		recv:   ix.calleeStep(c, at),
		runs:   make(map[eventSource]*eventRuns),
		calls:  calls,
		within: make(map[*eventCtxCall]bool),
		values: make(map[ssa.Value]bool),
		reads:  make(map[ssaflow.PlaceKey]bool),
	}
	q.sources = ix.expand(q.recv, q.values, q.reads)
	return q
}

// ctxCallOn tells what instr does to src as a Ctx call:
//
//   - eventCtxSet, for a call given the context in scope that runs on a value
//     that can be src, and on no event the receiver cannot be. Unless anyRun
//     is set, the value must also be able to hold the same run of src as the
//     receiver.
//   - eventCtxFresh, for a call given a fresh context that runs on src alone,
//     and on the same run of src as the receiver. With anyRun set, none is.
//
// Otherwise, it is eventCtxNone.
func (q *eventQuery) ctxCallOn(instr ssa.Instruction, src eventSource, anyRun bool) eventCtxFound {
	for _, cc := range q.calls.at[instr] {
		switch {
		case cc.fresh:
			if !anyRun && q.freshOn(cc, src) {
				return eventCtxFresh
			}
		case q.counts(cc, src) && (anyRun || q.runsOf(src).sameRun(q.ix.ctxRunsOf(cc, src))):
			return eventCtxSet
		}
	}
	return eventCtxNone
}

// freshOn reports whether cc, a Ctx call given a fresh context, runs on src
// and on no other event, and on the same run of src as the receiver. A call
// that may run on another event may leave the context of src as it was.
func (q *eventQuery) freshOn(cc *eventCtxCall, src eventSource) bool {
	q.ix.work++
	return len(cc.sources.list) == 1 && cc.sources.has(src) &&
		q.runsOf(src).alwaysSameRun(q.ix.ctxRunsOf(cc, src))
}

// runsOf returns the runs of src the receiver can hold, or nil when src is
// not made in a loop.
func (q *eventQuery) runsOf(src eventSource) *eventRuns {
	runs, ok := q.runs[src]
	if !ok {
		if origin := q.ix.loopOrigin(src, q.fn); origin != nil {
			if q.graph == nil {
				q.graph = q.ix.runGraphOf(q.recv, q.at)
			}
			runs = q.graph.runsOf(src, origin)
		}
		q.runs[src] = runs
	}
	return runs
}

// hasCtxCallOn reports whether some Ctx call of the function given the
// context in scope runs on src.
func (q *eventQuery) hasCtxCallOn(src eventSource) bool {
	for _, cc := range q.calls.on[src] {
		if q.counts(cc, src) {
			return true
		}
	}
	return false
}

// counts reports whether cc runs on src, and on no event the receiver
// cannot be. It compares the events of each call once.
func (q *eventQuery) counts(cc *eventCtxCall, src eventSource) bool {
	if !cc.sources.has(src) {
		return false
	}
	within, ok := q.within[cc]
	if !ok {
		within = true
		for _, s := range cc.sources.list {
			q.ix.work++
			if !q.sources.has(s) {
				within = false
				break
			}
		}
		q.within[cc] = within
	}
	return within
}

// ctxRunsBefore reports whether every path that leads to before, and carries
// src, passes a Ctx call on src after src is made, with no Ctx call given a
// fresh context after it. A path that leads back to
// the entry of the function carries src only when the function does not make
// it, as for a parameter. A path carries another event when it takes an edge
// of a Phi, or stores a value, that is not src or has context of its own.
//
// A receiver that holds only an earlier run of src, made in a loop, holds an
// event made before the latest run. A path that reaches the latest run goes
// on before it, where a Ctx call on any run of src counts, until the run
// before:
//
//	var last *zerolog.Event
//	for range n {
//	    e := logger.Info()  // the latest run
//	    e.Ctx(ctx)          // on the run before, for the next iteration
//	    if last != nil {
//	        last.Msg("x")   // OK: last holds the run before
//	    }
//	    last = e
//	}
func (q *eventQuery) ctxRunsBefore(src eventSource, before ssaflow.WalkPoint) bool {
	def := q.defOf(src)
	runs := q.runsOf(src)
	// earlier tells that the walk is before the latest run of src
	earlier, reachesLatest := false, false
	visit := func(instr ssa.Instruction) ssaflow.WalkStep {
		q.ix.work++
		switch q.ctxCallOn(instr, src, earlier) {
		case eventCtxSet:
			return ssaflow.WalkStop // Ctx runs on this path
		case eventCtxFresh:
			return ssaflow.WalkAbort // The last Ctx on this path is given a fresh context
		case eventCtxNone:
		}
		switch {
		case instr == def && (earlier || !runs.onlyEarlier()):
			return ssaflow.WalkAbort // src is made on this path, and Ctx does not run after
		case instr == def:
			reachesLatest = true
			return ssaflow.WalkStop // The latest run, after the one the receiver holds
		case q.storesOther(instr, src):
			return ssaflow.WalkStop // Another event is stored on this path
		}
		return ssaflow.WalkOn
	}
	atEntry := func() bool { return def != nil }
	edge := func(b *ssa.BasicBlock, i int, s struct{}) (struct{}, bool) {
		return s, q.edgeCarries(b, i, src)
	}
	if !ssaflow.WalkBackWith([]ssaflow.WalkFrom[struct{}]{{Point: before}}, visit, atEntry, edge) {
		return false
	}
	if !reachesLatest {
		return true
	}
	earlier = true
	from := ssaflow.WalkPointBefore(def, q.ix.positions)
	return ssaflow.WalkBackWith([]ssaflow.WalkFrom[struct{}]{{Point: from}}, visit, atEntry, edge)
}

// freshCtxLastBefore reports whether a path that leads to before, and carries
// src, ends with a Ctx call on src given a fresh context: no Ctx call given
// the context in scope runs after it, and no other event is stored.
func (q *eventQuery) freshCtxLastBefore(src eventSource, before ssaflow.WalkPoint) bool {
	def := q.defOf(src)
	visit := func(instr ssa.Instruction) ssaflow.WalkStep {
		q.ix.work++
		switch q.ctxCallOn(instr, src, false) {
		case eventCtxFresh:
			return ssaflow.WalkAbort
		case eventCtxSet:
			return ssaflow.WalkStop
		case eventCtxNone:
		}
		if instr == def || q.storesOther(instr, src) {
			return ssaflow.WalkStop // The context of the chain, or of another event
		}
		return ssaflow.WalkOn
	}
	edge := func(b *ssa.BasicBlock, i int, s struct{}) (struct{}, bool) {
		return s, q.edgeCarries(b, i, src)
	}
	return !ssaflow.WalkBackWith([]ssaflow.WalkFrom[struct{}]{{Point: before}}, visit, nil, edge)
}

// ctxSetAfterDefer reports whether src has context at each return after d, a
// deferred terminator, given whether it has context at d. The last Ctx call
// on src on each path from d decides. The recover block, reached after a
// panic, is not looked at, as it has no path from d.
func (q *eventQuery) ctxSetAfterDefer(src eventSource, d *ssa.Defer, set bool) bool {
	if !set && !q.hasCtxCallOn(src) {
		return false // A path that never returns does not end the walk
	}
	return q.ctxSetAtReturns(src, ssaflow.WalkPointAfter(d, q.ix.positions), set, make(map[eventReturnWalk]bool))
}

// ctxSetAtReturns reports whether, on every path from the point from to a
// return, src has context there: the last Ctx call on src on the path is
// given the context in scope, or there is none, and set tells that src has
// context at from. A path that makes src again, in a loop, has another event
// after, so what src has there holds. A walk reached again while it is in
// progress, through a loop, adds nothing.
func (q *eventQuery) ctxSetAtReturns(src eventSource, from ssaflow.WalkPoint, set bool, walks map[eventReturnWalk]bool) bool {
	key := eventReturnWalk{from: from, set: set}
	if done, ok := walks[key]; ok {
		return done
	}
	walks[key] = true
	def := q.defOf(src)
	end := ssaflow.WalkAbort
	if set {
		end = ssaflow.WalkStop
	}
	done := ssaflow.WalkFwd(from, func(instr ssa.Instruction) ssaflow.WalkStep {
		q.ix.work++
		if instr == def {
			return end // The event is made again, so a Ctx call after is on another one
		}
		if found := q.ctxCallOn(instr, src, false); found != eventCtxNone {
			after := ssaflow.WalkPointAfter(instr, q.ix.positions)
			if q.ctxSetAtReturns(src, after, found == eventCtxSet, walks) {
				return ssaflow.WalkStop
			}
			return ssaflow.WalkAbort
		}
		if _, ok := instr.(*ssa.Return); ok {
			return end
		}
		return ssaflow.WalkOn
	})
	walks[key] = done
	return done
}

// defOf returns the instruction of the function that makes src, or nil when
// the function is given it: a parameter, a free variable, an event made in
// another function, or one it cannot tell.
func (q *eventQuery) defOf(src eventSource) ssa.Instruction {
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
func (q *eventQuery) storesOther(instr ssa.Instruction, src eventSource) bool {
	store, ok := instr.(*ssa.Store)
	if !ok || !q.values[store.Val] {
		return false
	}
	at, _ := ssaflow.PlaceOf(store.Addr)
	return q.reads[at.Key()] && q.isOther(store.Val, src)
}

// edgeCarries reports whether a path that reaches the top of b goes on into
// its i-th predecessor carrying src: no Phi of b that the receiver is read
// through takes nil there, or another event than src, or one with context of
// its own.
func (q *eventQuery) edgeCarries(b *ssa.BasicBlock, i int, src eventSource) bool {
	for _, instr := range b.Instrs {
		phi, ok := instr.(*ssa.Phi)
		if !ok {
			break
		}
		q.ix.work++
		if !q.values[phi] {
			continue
		}
		if edge := phi.Edges[i]; ssaflow.IsNilConst(edge) || q.isOther(edge, src) {
			return false
		}
	}
	return true
}

// isOther reports whether v, a value the receiver is read through, is another
// event than src, or has context of its own. A value that leads back to
// itself, as an update in a loop does, can be any of them.
func (q *eventQuery) isOther(v ssa.Value, src eventSource) bool {
	q.ix.work++
	sources := q.ix.sourcesOf(v)
	return (len(sources.list) > 0 && !sources.has(src)) || q.hasCtx(v)
}

// ctxCallsIn returns the calls of Event.Ctx in fn, with the events each runs
// on. A Ctx call given a fresh context.Background() or TODO() does not set
// the context; it replaces the one the event had. The Ctx calls given a
// context that is not fresh that a func literal called in place runs are
// among them, at each call of it (see addClosureCtxCalls).
func (ix *eventIndex) ctxCallsIn(fn *ssa.Function) *eventCtxCalls {
	if calls, ok := ix.ctxCalls[fn]; ok {
		return calls
	}
	calls := &eventCtxCalls{
		at: make(map[ssa.Instruction][]*eventCtxCall),
		on: make(map[eventSource][]*eventCtxCall),
	}
	for instr := range ssaflow.InstrsIn(fn) {
		switch in := instr.(type) {
		case *ssa.Call:
			if cc, ok := ix.ctxCallOf(in); ok {
				calls.add(in, cc)
			}
		case *ssa.MakeClosure:
			ix.addClosureCtxCalls(calls, in)
		}
	}
	ix.ctxCalls[fn] = calls
	return calls
}

// ctxCallOf returns call as a Ctx call, when it is a call of Event.Ctx.
func (ix *eventIndex) ctxCallOf(call *ssa.Call) (*eventCtxCall, bool) {
	c, ok := calleeOf(&call.Call, ix.closures)
	if !ok || c.Recv == nil || c.api.Kind != zerologapi.KindSource || c.api.Recv != zerologapi.TypeEvent {
		return nil, false
	}
	args := call.Call.Args
	fresh := len(args) > 0 && isFreshContext(args[len(args)-1], ix.positions, ix.closures)
	recv := ix.calleeStep(c, call)
	return &eventCtxCall{call: call, sources: ix.expand(recv, make(map[ssa.Value]bool), nil), fresh: fresh, recv: recv}, true
}

// addClosureCtxCalls adds to calls the Ctx calls given a context that is not
// fresh that the func literal of mc runs on every run, at each call of mc
// where it is made, as for an IIFE. Such a call runs the func literal there,
// and the Ctx calls with it:
//
//	e := logger.Info()
//	func() { e.Ctx(ctx) }() // runs Ctx on e here
//	e.Msg("x")              // OK
//
// The Ctx calls on the same events run on every run when every path from the
// entry of the func literal to a return passes one of them (see
// eventRunsOnEveryReturn). The events each runs on are what it reads there,
// as for any Ctx call. A go or defer statement runs the func literal at
// another time, and is not one of the calls.
func (ix *eventIndex) addClosureCtxCalls(calls *eventCtxCalls, mc *ssa.MakeClosure) {
	fn := mc.Fn.(*ssa.Function) // Always a function, a func literal or a wrapper
	if fn.Parent() == nil {
		return // A wrapper, such as a method value, passes on what it is given
	}
	sites := ssaflow.CallsOfClosure(mc)
	if len(sites) == 0 {
		return
	}
	inner := ix.ctxCallsIn(fn)
	var set []*eventCtxCall
	for instr := range ssaflow.InstrsIn(fn) {
		for _, cc := range inner.at[instr] {
			if !cc.fresh {
				set = append(set, cc)
			}
		}
	}
	for _, cc := range set {
		if !eventRunsOnEveryReturn(cc, set, fn) {
			continue
		}
		for _, site := range sites {
			calls.add(site, &eventCtxCall{call: site, sources: ix.sourcesAtRun(cc.sources, mc, site), recv: cc.recv})
		}
	}
}

// sourcesAtRun returns sources, the events a Ctx call in the func literal of
// mc runs on, as the function that makes mc has them where site runs it. An
// event the closure cannot tell, at a place it reads through a pointer it
// loads from a variable it captures, is the one at the same place through the
// variable bound there (see MatchedPlaces.KeyAtRun):
//
//	func(ctx context.Context, h *holder) {
//	    func() { h.e.Ctx(ctx) }() // the caller's event at h.e, read in the closure
//	    h.e.Msg("x")              // the same event, read here
//	}
func (ix *eventIndex) sourcesAtRun(sources *eventSources, mc *ssa.MakeClosure, site ssa.Instruction) *eventSources {
	mapped := &eventSources{}
	for _, src := range sources.list {
		if src.v == nil {
			if at, ok := ix.places.KeyAtRun(src.at, mc, site); ok {
				src.at = at
			}
		}
		mapped.add(src)
	}
	return mapped
}

// eventRunsOnEveryReturn reports whether cc, or another call of set on the
// same events, runs on every path from the entry of fn to a return:
//
//	func() {
//	    if cond {
//	        e.Ctx(ctx)
//	    } else {
//	        e.Ctx(ctx)      // one of the two runs on every path
//	    }
//	}()
//
// The recover block, reached after a panic, is not looked at.
func eventRunsOnEveryReturn(cc *eventCtxCall, set []*eventCtxCall, fn *ssa.Function) bool {
	passed := make(map[*ssa.BasicBlock]bool)
	for _, other := range set {
		if other.sources.same(cc.sources) {
			passed[other.call.Block()] = true
		}
	}
	queue := []*ssa.BasicBlock{fn.Blocks[0]}
	seen := map[*ssa.BasicBlock]bool{fn.Blocks[0]: true}
	for len(queue) > 0 {
		b := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if passed[b] {
			continue // A Ctx call runs on this path
		}
		if _, isReturn := b.Instrs[len(b.Instrs)-1].(*ssa.Return); isReturn {
			return false
		}
		for _, succ := range b.Succs {
			if !seen[succ] {
				seen[succ] = true
				queue = append(queue, succ)
			}
		}
	}
	return true
}

// add adds cc, which runs at instr.
func (calls *eventCtxCalls) add(instr ssa.Instruction, cc *eventCtxCall) {
	calls.at[instr] = append(calls.at[instr], cc)
	if cc.fresh {
		calls.fresh = true
		return
	}
	for _, src := range cc.sources.list {
		calls.on[src] = append(calls.on[src], cc)
	}
}

// sourcesOf returns the events v can be, which it finds once.
func (ix *eventIndex) sourcesOf(v ssa.Value) *eventSources {
	if sources, ok := ix.sources[v]; ok {
		return sources
	}
	sources := ix.expand(eventStep{next: []ssa.Value{v}}, make(map[ssa.Value]bool), nil)
	ix.sources[v] = sources
	return sources
}

// expand returns the events that start can be, adding to values each value
// it is the same event as on the way, and to reads, when not nil, each place
// it is read from. A value already in values adds nothing, so a cycle ends.
func (ix *eventIndex) expand(start eventStep, values map[ssa.Value]bool, reads map[ssaflow.PlaceKey]bool) *eventSources {
	sources := &eventSources{}
	add := func(step eventStep) {
		ix.work += 1 + len(step.sources)
		for _, src := range step.sources {
			sources.add(src)
		}
		if step.read != nil && reads != nil {
			reads[*step.read] = true
		}
	}
	var visit func(v ssa.Value)
	var walk func(step eventStep)
	visit = func(v ssa.Value) {
		if values[v] {
			return
		}
		values[v] = true
		walk(ix.stepOf(v))
	}
	walk = func(step eventStep) {
		add(step)
		for _, next := range step.next {
			visit(next)
		}
		for _, m := range step.more {
			walk(m)
		}
	}
	walk(start)
	return sources
}

// calleeStep is the step back from the receiver of c, which call runs: the
// receiver itself, or, for a method expression of a promoted method, what is
// at the embedded fields of the struct it is passed.
func (ix *eventIndex) calleeStep(c callee, call ssa.Instruction) eventStep {
	if c.Embedded == nil {
		return eventStep{next: []ssa.Value{c.Recv}}
	}
	call = c.ReadAt(call)
	addr, typ, read, ok := c.EmbeddedStart(call)
	if !ok {
		return eventStep{sources: []eventSource{{v: c.Recv}}}
	}
	at, _ := ssaflow.PlaceOf(addr)
	return ix.resolvedStep(ix.resolver().ResolveEmbedded(at, typ, c.Embedded, read, call), at.WithFields(c.Embedded))
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
		if ssaflow.IsNilConst(val) {
			return eventStep{} // A nil event logs nothing
		}
	case *ssa.Call:
		if c, ok := calleeOf(&val.Call, ix.closures); ok && c.Recv != nil && c.returnsSameEvent() {
			return ix.calleeStep(c, val)
		}
	case *ssa.Phi:
		var step eventStep
		for _, edge := range val.Edges {
			if !ssaflow.IsNilConst(edge) {
				step.next = append(step.next, edge)
			}
		}
		return step
	case *ssa.UnOp:
		if _, isLoad := ssaflow.AsLoad(val); isLoad {
			at, _ := ssaflow.PlaceOf(val.X)
			if sources, ok := ix.resolver().Resolve(val.X, val); ok {
				return ix.resolvedStep(sources, at)
			}
			if key, ok := ix.places.KeyOf(at); ok {
				// The event the function cannot tell that is at the place,
				// as for a pointer parameter
				return eventStep{sources: []eventSource{{at: key}}, read: &key}
			}
		}
	case *ssa.Field, *ssa.Index:
		if step, ok := ix.selectedStep(v); ok {
			return step
		}
	}
	return eventStep{sources: []eventSource{{v: v}}}
}

// selectedStep is the step back from v, a field of a struct value or an
// element of an array value, to what each read of it from memory can see
// (see ssaflow.LoadedSelection). A value selected from a Phi of such values
// is a read for each load an edge leads to. Each read is a step of the more
// of the step, which has nothing of its own, and none for an edge that is
// the zero value. ok is false when a read cannot be resolved.
func (ix *eventIndex) selectedStep(v ssa.Value) (eventStep, bool) {
	reads, ok := ssaflow.LoadedSelection(v)
	if !ok {
		return eventStep{}, false
	}
	var steps []eventStep
	for _, rd := range reads {
		sources, ok := ix.resolver().ResolveAt(rd.At, rd.Load)
		if !ok {
			return eventStep{}, false
		}
		step := ix.resolvedStep(sources, rd.At)
		step.load, step.through = rd.Load, rd.Through
		steps = append(steps, step)
	}
	return eventStep{more: steps}, true
}

// resolvedStep is the step back to sources, what a read of the place at can
// see. A source that resolve cannot tell is the event at that place.
func (ix *eventIndex) resolvedStep(sources iter.Seq[ssaflow.Resolved], at ssaflow.Place) eventStep {
	read := at.Key()
	step := eventStep{read: &read, readVariable: at.Variable()}
	for src := range sources {
		switch {
		case src.V == nil:
			step.sources = append(step.sources, eventSource{at: read})
		case src.Pointer:
			step.sources = append(step.sources, eventSource{at: ssaflow.PlaceAt(src.V).Key()})
		case !ssaflow.IsNilConst(src.V):
			step.next = append(step.next, src.V)
		}
	}
	return step
}

// resolver returns a resolver that resolves a parameter of a func literal
// once at a time: one reached again, through a recursive call, adds nothing.
func (ix *eventIndex) resolver() ssaflow.Resolver {
	return ssaflow.Resolver{
		Positions: ix.positions,
		Closures:  ix.closures,
		Model:     zerologapi.Model{},
		Enter: func(param *ssa.Parameter, expand func()) {
			if ix.active[param] {
				return
			}
			ix.active[param] = true
			expand()
			delete(ix.active, param)
		},
	}
}

// eventRuns are the runs of an origin in a loop that a value can hold, each
// event it makes. The latest run is the one that ran last. An earlier run
// ran before it, as for a value kept from the last iteration. An element
// stored at an index computed in a loop, and read at another index, can hold
// any run.
type eventRuns struct {
	latest, earlier bool
	// anyAt are the places of the reads that can hold any run.
	anyAt map[ssaflow.PlaceKey]bool
	// anyVariable reports that one of anyAt is an element at an index that is
	// not a constant.
	anyVariable bool
}

// add adds what a path that ends in run holds.
func (r *eventRuns) add(run eventRunState) {
	switch run.run {
	case eventRunLatest:
		r.latest = true
	case eventRunEarlier:
		r.earlier = true
	case eventRunAny:
		r.anyAt[run.at] = true
		r.anyVariable = r.anyVariable || run.variable
	}
}

// onlyEarlier reports whether a value with r holds only earlier runs, or
// any run, but never the latest. A nil r is for an origin not in a loop, which
// has only one run.
func (r *eventRuns) onlyEarlier() bool {
	return r != nil && !r.latest
}

// alwaysSameRun reports whether a value with r and a value with o hold the
// event of the same run on every path: the origin is not in a loop, or both
// hold only the latest run.
func (r *eventRuns) alwaysSameRun(o *eventRuns) bool {
	onlyLatest := func(r *eventRuns) bool { return r.latest && !r.earlier && len(r.anyAt) == 0 }
	if r == nil || o == nil {
		return r == nil && o == nil
	}
	return onlyLatest(r) && onlyLatest(o)
}

// sameRun reports whether a value with r and a value with o can hold the
// event of the same run. Any run can be the same as any other, but at two
// elements at constant indices, which are two events:
//
//	for i := range 2 {
//	    evs[i] = logger.Info()
//	}
//	evs[0].Ctx(ctx)
//	evs[1].Msg("x")         // another run
func (r *eventRuns) sameRun(o *eventRuns) bool {
	if r == nil || o == nil {
		return true
	}
	known := func(r *eventRuns) bool { return r.latest || r.earlier }
	switch {
	case r.latest && o.latest, r.earlier && o.earlier:
		return true
	case len(r.anyAt) > 0 && known(o), len(o.anyAt) > 0 && known(r):
		return true
	case len(r.anyAt) == 0 || len(o.anyAt) == 0:
		return false
	case r.anyVariable || o.anyVariable:
		return true
	}
	for at := range r.anyAt {
		if o.anyAt[at] {
			return true
		}
	}
	return false
}

// eventRun is which run of an origin in a loop a value holds, on one path back
// to the origin.
type eventRun int

const (
	// eventRunLatest is the event of the latest run.
	eventRunLatest eventRun = iota
	// eventRunEarlier is the event of an earlier run.
	eventRunEarlier
	// eventRunAny is the event of any run.
	eventRunAny
)

// eventRunState is the run a path back to an origin holds so far, and, for
// any run, the read where it became any.
type eventRunState struct {
	run eventRun
	// at is the place read, for any run.
	at ssaflow.PlaceKey
	// variable reports that at is an element at an index that is not a
	// constant.
	variable bool
}

// eventCtxRun is a Ctx call, and an origin it can run on.
type eventCtxRun struct {
	cc  *eventCtxCall
	src eventSource
}

// eventReach is a point, and an origin a path from it must not run.
type eventReach struct {
	from  ssaflow.WalkPoint
	avoid ssa.Instruction
}

// ctxRunsOf returns the runs of src that cc can run on, as eventQuery.runsOf
// does for the receiver.
func (ix *eventIndex) ctxRunsOf(cc *eventCtxCall, src eventSource) *eventRuns {
	key := eventCtxRun{cc, src}
	runs, ok := ix.ctxRuns[key]
	if !ok {
		if origin := ix.loopOrigin(src, cc.call.Parent()); origin != nil {
			if cc.runs == nil {
				cc.runs = ix.runGraphOf(cc.recv, cc.call)
			}
			runs = cc.runs.runsOf(src, origin)
		}
		ix.ctxRuns[key] = runs
	}
	return runs
}

// loopOrigin returns the instruction that makes src when it is in fn, and can
// run again after it runs, in a loop, or nil.
func (ix *eventIndex) loopOrigin(src eventSource, fn *ssa.Function) ssa.Instruction {
	origin, ok := src.v.(ssa.Instruction)
	if !ok || origin.Parent() != fn {
		return nil
	}
	loop, ok := ix.loops[origin]
	if !ok {
		after := ssaflow.WalkPointAfter(origin, ix.positions)
		loop = ix.reaches(after, origin)(ssaflow.WalkPointBefore(origin, ix.positions))
		ix.loops[origin] = loop
	}
	if !loop {
		return nil
	}
	return origin
}

// reaches returns a test of whether a path from the point from reaches a
// point without running origin, as ssaflow.WalkReachable does.
func (ix *eventIndex) reaches(from ssaflow.WalkPoint, origin ssa.Instruction) func(ssaflow.WalkPoint) bool {
	key := eventReach{from, origin}
	reaches, ok := ix.reachable[key]
	if !ok {
		reaches = ssaflow.WalkReachable(from, origin, ix.positions, &ix.work)
		ix.reachable[key] = reaches
	}
	return reaches
}

// eventRunGraph holds the values a receiver is the same event as, back to the
// events it can be, as eventIndex.expand finds them, with the point where
// each value is used. runsOf follows it from an event to the receiver, to
// find the runs of the event the receiver can hold.
type eventRunGraph struct {
	ix *eventIndex
	fn *ssa.Function
	// root stands for the receiver.
	root *eventRunVertex
	// vertices holds each value at each point it is used.
	vertices map[eventRunNode]*eventRunVertex
	// made holds the vertices that are each event, or the root.
	made map[eventSource][]*eventRunVertex
}

// eventRunNode is a value, and the point where it is used. The point is none
// out of the function of the receiver.
type eventRunNode struct {
	v   ssa.Value
	use ssaflow.WalkPoint
}

// eventRunVertex is a value at a point it is used, and its users: the values
// it is the same event as one step closer to the receiver.
type eventRunVertex struct {
	eventRunNode
	users []eventRunUse
}

// eventRunUse is a user of a value. stored is set when the user reads the
// value from memory.
type eventRunUse struct {
	by     *eventRunVertex
	stored *eventRunStore
}

// eventRunStore is a store that a read sees.
type eventRunStore struct {
	// after is the point after the store, and read the point of the read.
	after, read ssaflow.WalkPoint
	// at is the place read, and variable reports that it is an element at an
	// index that is not a constant.
	at       ssaflow.PlaceKey
	variable bool
	// other reports that the store is at an element whose index is not a
	// constant, and the read is at another place.
	other bool
}

// runGraphOf builds the graph of start, the step back from a receiver of the
// call at.
func (ix *eventIndex) runGraphOf(start eventStep, at ssa.Instruction) *eventRunGraph {
	g := &eventRunGraph{
		ix:       ix,
		fn:       at.Parent(),
		root:     &eventRunVertex{},
		vertices: make(map[eventRunNode]*eventRunVertex),
		made:     make(map[eventSource][]*eventRunVertex),
	}
	g.step(g.root, start, at)
	return g
}

// runsOf returns the runs of src, which origin makes in a loop, that the
// receiver can hold. A value holds an earlier run when,
// on its way from the origin, it is kept somewhere, such as at a Phi or in
// memory, and every path from there to where it is used runs the origin
// again:
//
//	var last *zerolog.Event // last = Phi(nil, e) at the top of the loop
//	for range n {
//	    e := logger.Info()
//	    if last != nil {
//	        last.Ctx(ctx)   // an earlier run
//	        e.Msg("x")      // the latest run
//	    }
//	    last = e
//	}
func (g *eventRunGraph) runsOf(src eventSource, origin ssa.Instruction) *eventRuns {
	runs := &eventRuns{anyAt: make(map[ssaflow.PlaceKey]bool)}
	type visit struct {
		x     *eventRunVertex
		state eventRunState
	}
	seen := make(map[visit]bool)
	var up func(x *eventRunVertex, state eventRunState)
	up = func(x *eventRunVertex, state eventRunState) {
		if x == g.root {
			runs.add(state)
			return
		}
		if def := g.pointAfter(x.v); state.run == eventRunLatest && def != nil && !g.reachesFrom(*def, x.use, origin) {
			state.run = eventRunEarlier // The origin runs again before the use
		}
		if seen[visit{x, state}] {
			return
		}
		seen[visit{x, state}] = true
		g.ix.work++
		for _, u := range x.users {
			next := state
			switch st := u.stored; {
			case st == nil:
			case st.other:
				// The closest read to the receiver tells which element it is
				next = eventRunState{run: eventRunAny, at: st.at, variable: st.variable}
			case next.run == eventRunLatest && !g.reachesFrom(st.after, st.read, origin):
				next.run = eventRunEarlier
			}
			up(u.by, next)
		}
	}
	for _, x := range g.made[src] {
		up(x, eventRunState{})
	}
	return runs
}

// reachesFrom reports whether a path from the point from reaches to without
// running origin. A point that is none, out of the function, is reached.
func (g *eventRunGraph) reachesFrom(from, to ssaflow.WalkPoint, origin ssa.Instruction) bool {
	if from == (ssaflow.WalkPoint{}) || to == (ssaflow.WalkPoint{}) {
		return true
	}
	return g.ix.reaches(from, origin)(to)
}

// step adds what step, the step back from the value of x used by at, leads
// to.
func (g *eventRunGraph) step(x *eventRunVertex, step eventStep, at ssa.Instruction) {
	for _, src := range step.sources {
		g.made[src] = append(g.made[src], x)
	}
	use := g.pointBefore(at)
	for _, next := range step.next {
		if step.read == nil {
			g.value(next, use, eventRunUse{by: x})
		} else {
			g.stored(next, use, *step.read, step.readVariable, x)
		}
	}
	for _, m := range step.more {
		g.selected(x, m, at)
	}
}

// selected adds what m, a read of step.more for the value of x used by at,
// leads to. The value is taken from the load through each Phi of m.through,
// used at the end of the predecessor of the edge, as value does for a Phi of
// events, so that a Phi at the top of a loop tells an earlier run. The load
// reads what the stores before it left:
//
//	t2 = phi [0: W{}, 6: t12] #last   // at the top of the loop
//	t14 = t2.Event                    // x: read from the load of t12, past t2
func (g *eventRunGraph) selected(x *eventRunVertex, m eventStep, at ssa.Instruction) {
	use := g.pointBefore(at)
	hop := func(v ssa.Value) {
		node := eventRunNode{v, use}
		next, ok := g.vertices[node]
		if !ok {
			next = &eventRunVertex{eventRunNode: node}
			g.vertices[node] = next
			g.ix.work++
		}
		next.users = append(next.users, eventRunUse{by: x})
		x = next
	}
	for _, e := range m.through {
		hop(e.Phi)
		use = ssaflow.WalkPoint{}
		pred := e.Phi.Block().Preds[e.Index]
		if p := g.pointAfter(pred.Instrs[len(pred.Instrs)-1]); p != nil {
			use = *p
		}
	}
	hop(m.load.(ssa.Value))
	g.step(x, m, m.load)
}

// value adds v, used at the point use by u.
func (g *eventRunGraph) value(v ssa.Value, use ssaflow.WalkPoint, u eventRunUse) {
	node := eventRunNode{v, use}
	if x, ok := g.vertices[node]; ok {
		x.users = append(x.users, u)
		return
	}
	x := &eventRunVertex{eventRunNode: node, users: []eventRunUse{u}}
	g.vertices[node] = x
	g.ix.work++
	phi, ok := v.(*ssa.Phi)
	if !ok {
		instr, _ := v.(ssa.Instruction)
		g.step(x, g.ix.stepOf(v), instr)
		return
	}
	for i, edge := range phi.Edges {
		if ssaflow.IsNilConst(edge) {
			continue
		}
		pred := phi.Block().Preds[i]
		end := ssaflow.WalkPoint{}
		if p := g.pointAfter(pred.Instrs[len(pred.Instrs)-1]); p != nil {
			end = *p
		}
		g.value(edge, end, eventRunUse{by: x})
	}
}

// stored adds v, a value stored that a read of the place at sees at the point
// read, as used by x. The value is used where it is stored.
func (g *eventRunGraph) stored(v ssa.Value, read ssaflow.WalkPoint, at ssaflow.PlaceKey, variable bool, x *eventRunVertex) {
	var stores []*ssa.Store
	for _, ref := range *v.Referrers() {
		store, ok := ref.(*ssa.Store)
		if !ok || store.Val != v || store.Parent() != g.fn {
			continue
		}
		if place, _ := ssaflow.PlaceOf(store.Addr); place.Key() == at || place.Variable() {
			stores = append(stores, store)
		}
	}
	if len(stores) == 0 {
		g.value(v, read, eventRunUse{by: x}) // Stored where it cannot be told
		return
	}
	for _, store := range stores {
		place, _ := ssaflow.PlaceOf(store.Addr)
		st := &eventRunStore{after: *g.pointAfter(store), read: read, at: at, variable: variable, other: place.Key() != at}
		g.value(v, g.pointBefore(store), eventRunUse{by: x, stored: st})
	}
}

// pointBefore returns the point before instr, or none when instr is nil or
// not in the function.
func (g *eventRunGraph) pointBefore(instr ssa.Instruction) ssaflow.WalkPoint {
	if instr == nil || instr.Parent() != g.fn {
		return ssaflow.WalkPoint{}
	}
	return ssaflow.WalkPointBefore(instr, g.ix.positions)
}

// pointAfter returns the point after v, an instruction of the function, or nil
// for any other value.
func (g *eventRunGraph) pointAfter(v any) *ssaflow.WalkPoint {
	instr, ok := v.(ssa.Instruction)
	if !ok || instr.Parent() != g.fn {
		return nil
	}
	p := ssaflow.WalkPointAfter(instr, g.ix.positions)
	return &p
}
