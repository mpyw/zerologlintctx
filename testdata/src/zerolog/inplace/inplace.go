// Package inplace covers context set in place: Event.Ctx with its result unused, and
// Logger.UpdateContext.
package inplace

import (
	"context"

	"github.com/rs/zerolog"
)

// ===== Ctx IN PLACE (RESULT UNUSED) =====

// Event.Ctx sets the context on the event it runs on, so a call whose result
// is not used still counts (#113).

func goodEventCtxResultUnused(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	e.Ctx(ctx)
	e.Msg("ctx result unused") // OK - .Ctx() set the context of e
}

func goodEventCtxResultUnusedLaterInChain(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	e2 := e.Str("key", "value")
	e2.Ctx(ctx)
	e.Int("n", 1).Msg("ctx on a later value of the chain") // OK - e2 is e
}

func goodEventCtxResultUnusedViaMethodValue(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	setCtx := e.Ctx
	setCtx(ctx)
	e.Msg("ctx via method value") // OK - .Ctx() set the context of e
}

func goodEventCtxResultUnusedOnBothBranches(ctx context.Context, log zerolog.Logger, cond bool) {
	var e *zerolog.Event
	if cond {
		e = log.Info()
		e.Ctx(ctx)
	} else {
		e = log.Warn()
		e.Ctx(ctx)
	}
	e.Msg("ctx on both branches") // OK - each event has its context set
}

func goodEventCtxResultUnusedAfterOtherUse(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	func(*zerolog.Event) {}(e)
	e.Ctx(ctx)
	e.Msg("ctx after another use") // OK - .Ctx() set the context of e
}

// Event.CreateDict returns a new event, not the one it runs on, so a Ctx call
// on the dict does not count for the parent (#194).

func badEventCtxOnCreatedDict(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	e.CreateDict().Ctx(ctx)
	e.Msg("ctx on the dict only") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxOnCreatedDictPassedToDict(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	d := e.CreateDict().Ctx(ctx).Str("k", "v")
	e.Dict("d", d).Msg("ctx on the dict only") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxOnCreatedDictDeferred(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	defer e.Msg("ctx on the dict only, deferred") // want `zerolog call chain missing .Ctx\(ctx\)`
	e.CreateDict().Ctx(ctx)
}

func goodEventCtxWithCreatedDict(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	d := e.CreateDict().Str("k", "v")
	e.Ctx(ctx)
	e.Dict("d", d).Msg("ctx on the parent") // OK
}

func goodEventCtxResultUnusedOnEachBranchOfOneEvent(ctx context.Context, other context.Context, log zerolog.Logger, cond bool) {
	e := log.Info()
	if cond {
		e.Ctx(ctx)
	} else {
		e.Ctx(other)
	}
	e.Msg("ctx on each branch of one event") // OK - .Ctx() runs on every path (#139)
}

func badEventParamCtxOnOneBranch(ctx context.Context, e *zerolog.Event, cond bool) {
	if cond {
		e.Ctx(ctx)
	}
	e.Msg("param, ctx on one branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventParamCtxResultUnused(ctx context.Context, e *zerolog.Event) {
	e.Ctx(ctx)
	e.Msg("param, ctx result unused") // OK - .Ctx() set the context of e
}

func badEventCtxResultUnusedInLoopOnly(ctx context.Context, log zerolog.Logger, n int) {
	e := log.Info()
	for i := 0; i < n; i++ {
		e.Ctx(ctx)
	}
	e.Msg("ctx in loop only") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// An event in memory is read by a load of its own at each use. A Ctx call on
// an earlier load counts, unless another event is stored in between (#182).

type inPlaceHolder struct{ e *zerolog.Event }

func goodEventCtxResultUnusedCapturedVar(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	defer func() { _ = e }()
	e.Ctx(ctx)
	e.Msg("ctx result unused, captured variable") // OK - .Ctx() set the context of e
}

func goodEventCtxResultUnusedField(ctx context.Context, log zerolog.Logger) {
	var h inPlaceHolder
	h.e = log.Info()
	h.e.Ctx(ctx)
	h.e.Msg("ctx result unused, field") // OK - .Ctx() set the context of h.e
}

func badEventCtxResultUnusedFieldReplaced(ctx context.Context, log zerolog.Logger) {
	var h inPlaceHolder
	h.e = log.Info()
	h.e.Ctx(ctx)
	h.e = log.Warn()
	h.e.Msg("ctx result unused, field replaced") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxResultUnusedHolderReplaced(ctx context.Context, log zerolog.Logger, other inPlaceHolder) {
	var h inPlaceHolder
	h.e = log.Info()
	h.e.Ctx(ctx)
	h = other
	h.e.Msg("ctx result unused, holder replaced") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxResultUnusedCapturedVarOnOneBranch(ctx context.Context, log zerolog.Logger, cond bool) {
	e := log.Info()
	defer func() { _ = e }()
	if cond {
		e.Ctx(ctx)
	}
	e.Msg("ctx result unused, captured variable, one branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxResultUnusedOtherField(ctx context.Context, log zerolog.Logger) {
	var a, b inPlaceHolder
	a.e = log.Info()
	b.e = log.Warn()
	a.e.Ctx(ctx)
	b.e.Msg("ctx result unused, other field") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxResultUnusedPointerParamField(ctx context.Context, h *inPlaceHolder, cond bool) {
	if cond {
		h.e.Ctx(ctx)
	}
	h.e.Msg("ctx result unused, pointer param field") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventCtxResultUnusedPointerParamField(ctx context.Context, h *inPlaceHolder) {
	h.e.Ctx(ctx)
	h.e.Msg("ctx result unused, pointer param field") // OK - .Ctx() set the context of h.e
}

// Another variable or field the event is copied to holds the same event, so a
// Ctx call on either counts for both (#215).

func goodEventCtxResultUnusedOnCopy(ctx context.Context, log zerolog.Logger) {
	var e, copied *zerolog.Event
	func() { _, _ = e, copied }()
	e = log.Info()
	copied = e
	e.Ctx(ctx)
	copied.Msg("ctx result unused, on a copy") // OK - copied holds the event of e
}

func goodEventCtxResultUnusedThenCopiedToField(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	e.Ctx(ctx)
	h := inPlaceHolder{e}
	h.e.Msg("ctx result unused, then copied to a field") // OK - h.e holds the event of e
}

func goodEventCtxResultUnusedOnFieldThenOriginal(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	h := inPlaceHolder{e}
	h.e.Ctx(ctx)
	e.Msg("ctx result unused on a field, then the original") // OK - h.e holds the event of e
}

func badEventCtxResultUnusedOnCopyThenReplaced(ctx context.Context, log zerolog.Logger) {
	var e, copied *zerolog.Event
	func() { _, _ = e, copied }()
	e = log.Info()
	copied = e
	e.Ctx(ctx)
	copied = log.Warn()
	copied.Msg("ctx result unused, copy replaced") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventCtxResultUnusedFieldUpdated(ctx context.Context, log zerolog.Logger) {
	var h inPlaceHolder
	h.e = log.Info()
	h.e.Ctx(ctx)
	h.e = h.e.Str("k", "v")
	h.e.Msg("ctx result unused, field updated") // OK - the update returns the same event
}

func goodEventCtxResultUnusedOnPhi(ctx context.Context, log zerolog.Logger, cond bool) {
	var e *zerolog.Event
	if cond {
		e = log.Info()
	} else {
		e = log.Warn()
	}
	e.Ctx(ctx)
	e.Msg("ctx result unused, after the branches") // OK - Ctx runs on whichever event e is
}

func goodEventCtxInPlaceOrStoredWithCtx(ctx context.Context, log zerolog.Logger, cond bool) {
	var h inPlaceHolder
	h.e = log.Info()
	if cond {
		h.e.Ctx(ctx)
	} else {
		h.e = log.Warn().Ctx(ctx)
	}
	h.e.Msg("ctx in place, or stored with ctx") // OK - each path has ctx one way or the other
}

func badEventCtxInPlaceOrStoredWithoutCtx(ctx context.Context, log zerolog.Logger, cond bool) {
	var h inPlaceHolder
	h.e = log.Info()
	if cond {
		h.e.Ctx(ctx)
	} else {
		h.e = log.Warn()
	}
	h.e.Msg("ctx in place, or stored without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceOnOtherPickedHolder(ctx context.Context, log zerolog.Logger, cond bool, q *inPlaceHolder) {
	h := &inPlaceHolder{e: log.Info()}
	h.e.Ctx(ctx)
	if cond {
		h = q
	}
	h.e.Msg("ctx in place on one holder, the caller's on the other path") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxOnPhiWithAnotherEvent(ctx context.Context, log zerolog.Logger, cond bool) {
	a := log.Info()
	e := a
	if cond {
		e = log.Warn()
	}
	e.Ctx(ctx)
	a.Msg("ctx on a value that may be another event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func inPlaceEventPointer(log zerolog.Logger) **zerolog.Event {
	e := log.Info()
	return &e
}

func goodEventCtxInPlaceThroughPointerArgument(ctx context.Context, log zerolog.Logger) {
	func(p **zerolog.Event) {
		(*p).Ctx(ctx)
		(*p).Msg("ctx in place through a pointer argument") // OK - both read the event p points to
	}(inPlaceEventPointer(log))
}

func goodEventCtxInPlaceThroughPointerArgumentRecursive(ctx context.Context, log zerolog.Logger) {
	var rec func(p **zerolog.Event, n int)
	rec = func(p **zerolog.Event, n int) {
		if n > 0 {
			rec(p, n-1)
			return
		}
		(*p).Ctx(ctx)
		(*p).Msg("ctx in place through a pointer argument, recursive") // OK - both read the event p points to
	}
	rec(inPlaceEventPointer(log), 1)
}

func badEventFreshCtxAfterLoggerWithCtx(ctx context.Context, log zerolog.Logger) {
	withCtx := log.With().Ctx(ctx).Logger()
	withCtx.Info().Ctx(context.Background()).Msg("fresh ctx replaces the logger's") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A path that stores an event with context of its own needs no Ctx call in
// place (#184).

func goodEventCtxInPlaceThenStoredWithCtx(ctx context.Context, log zerolog.Logger, n int) {
	e := log.Info()
	defer func() { _ = e }()
	e.Ctx(ctx)
	for range n {
		e.Msg("ctx in place, then stored with ctx") // OK - each path has ctx one way or the other
		e = log.Warn().Ctx(ctx)
	}
}

func goodEventCtxInPlaceThenReplacedWithCtxInLoop(ctx context.Context, log zerolog.Logger, n int) {
	withCtx := log.With().Ctx(ctx).Logger()
	e := log.Info()
	e.Ctx(ctx)
	for range n {
		e.Msg("ctx in place, then replaced with ctx in a loop") // OK - each edge has ctx one way or the other
		e = withCtx.Warn()
	}
}

func badEventCtxInPlaceThenReplacedWithoutCtxInLoop(ctx context.Context, log zerolog.Logger, n int) {
	e := log.Info()
	e.Ctx(ctx)
	for range n {
		e.Msg("ctx in place, then replaced without ctx in a loop") // want `zerolog call chain missing .Ctx\(ctx\)`
		e = log.Warn()
	}
}

func badEventCtxInPlaceThenStoredWithoutCtx(ctx context.Context, log zerolog.Logger, n int) {
	e := log.Info()
	defer func() { _ = e }()
	e.Ctx(ctx)
	for range n {
		e.Msg("ctx in place, then stored without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
		e = log.Warn()
	}
}

func badEventCtxInPlaceHolderReplacedWithCtxField(ctx context.Context, log zerolog.Logger, other inPlaceHolder) {
	var h inPlaceHolder
	h.e = log.Info()
	h.e.Ctx(ctx)
	h = other
	h.e.Msg("ctx in place, holder replaced") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A struct value of a local variable is loaded whole at each use, and a
// promoted method, or the field the event is embedded in, is selected from
// that load. A Ctx call on the event selected from an earlier load counts,
// unless another event is stored in between (#234).

type inPlaceEmbedded struct{ *zerolog.Event }

type inPlaceOuter struct{ inPlaceEmbedded }

func goodEventCtxInPlaceEmbeddedPromoted(ctx context.Context, log zerolog.Logger) {
	h := inPlaceEmbedded{log.Info()}
	h.Ctx(ctx)
	h.Msg("ctx in place, promoted") // OK - .Ctx() set the context of h.Event
}

func goodEventCtxInPlaceEmbeddedMethodValue(ctx context.Context, log zerolog.Logger) {
	h := inPlaceEmbedded{log.Info()}
	h.Ctx(ctx)
	m := h.Msg
	m("ctx in place, promoted method value") // OK - .Ctx() set the context of h.Event
}

func goodEventCtxInPlaceEmbeddedFieldThenPromoted(ctx context.Context, log zerolog.Logger) {
	h := inPlaceEmbedded{log.Info()}
	h.Event.Ctx(ctx)
	h.Msg("ctx in place on the field, then promoted") // OK - the same event
}

func goodEventCtxInPlaceEmbeddedPromotedThenField(ctx context.Context, log zerolog.Logger) {
	h := inPlaceEmbedded{log.Info()}
	h.Ctx(ctx)
	h.Event.Msg("ctx in place promoted, then on the field") // OK - the same event
}

func goodEventCtxInPlaceEmbeddedTwoLevels(ctx context.Context, log zerolog.Logger) {
	o := inPlaceOuter{inPlaceEmbedded{log.Info()}}
	o.Ctx(ctx)
	o.Msg("ctx in place, promoted through two levels") // OK - .Ctx() set the context of o.Event
}

func goodEventCtxInPlaceEmbeddedInnerThenOuter(ctx context.Context, log zerolog.Logger) {
	o := inPlaceOuter{inPlaceEmbedded{log.Info()}}
	o.inPlaceEmbedded.Ctx(ctx)
	o.Msg("ctx in place on the inner struct, then on the outer") // OK - the same event
}

func goodEventCtxInPlaceEmbeddedReplacedWithCtx(ctx context.Context, log zerolog.Logger) {
	h := inPlaceEmbedded{log.Info()}
	h.Ctx(ctx)
	h = inPlaceEmbedded{log.Warn().Ctx(ctx)}
	h.Msg("ctx in place, replaced with ctx") // OK - the new event has context of its own
}

func badEventCtxInPlaceEmbeddedOtherHolder(ctx context.Context, log zerolog.Logger) {
	a := inPlaceEmbedded{log.Info()}
	b := inPlaceEmbedded{log.Warn()}
	a.Ctx(ctx)
	b.Msg("ctx in place, other holder") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceEmbeddedReplaced(ctx context.Context, log zerolog.Logger) {
	h := inPlaceEmbedded{log.Info()}
	h.Ctx(ctx)
	h = inPlaceEmbedded{log.Warn()}
	h.Msg("ctx in place, holder replaced") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceEmbeddedFieldReplaced(ctx context.Context, log zerolog.Logger) {
	h := inPlaceEmbedded{log.Info()}
	h.Ctx(ctx)
	h.Event = log.Warn()
	h.Msg("ctx in place, field replaced") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceEmbeddedMethodValueReplaced(ctx context.Context, log zerolog.Logger) {
	h := inPlaceEmbedded{log.Info()}
	h.Ctx(ctx)
	h = inPlaceEmbedded{log.Warn()}
	m := h.Msg
	m("ctx in place, holder replaced, method value") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceEmbeddedFreshCtx(ctx context.Context, log zerolog.Logger) {
	h := inPlaceEmbedded{log.Info()}
	h.Ctx(context.Background())
	h.Msg("fresh ctx in place, promoted") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceEmbeddedOnOneBranch(ctx context.Context, log zerolog.Logger, cond bool) {
	h := inPlaceEmbedded{log.Info()}
	if cond {
		h.Ctx(ctx)
	}
	h.Msg("ctx in place on one branch, promoted") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// LIMITATION(#139) (false positive): An in-place Ctx call is not seen by a
// terminator in a closure, or in a func literal the event is passed to. The
// event reaches it through another reference, which is not followed back to
// the calls on it.

func limitationEventCtxResultUnusedClosure(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	e.Ctx(ctx)
	func() {
		e.Msg("ctx result unused, closure") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

func limitationEventCtxResultUnusedPassedToFuncLiteral(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	e.Ctx(ctx)
	func(ev *zerolog.Event) {
		ev.Msg("ctx result unused, passed to a func literal") // want `zerolog call chain missing .Ctx\(ctx\)`
	}(e)
}

func badEventCtxResultUnusedOnOneBranch(ctx context.Context, log zerolog.Logger, cond bool) {
	e := log.Info()
	if cond {
		e.Ctx(ctx)
	}
	e.Msg("ctx on one branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxResultUnusedOnOtherPath(ctx context.Context, log zerolog.Logger, cond bool) {
	e := log.Info()
	if cond {
		e.Msg("before ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
		return
	}
	e.Ctx(ctx)
	e.Msg("after ctx") // OK - .Ctx() runs first on this path
}

// ===== Ctx IN PLACE ACROSS ITERATIONS =====

// A call in a loop makes an event on each run. A Ctx call on the event of an
// earlier run does not set the context of the latest one (#246).

func badEventCtxOnPreviousIterationPhi(ctx context.Context, log zerolog.Logger) {
	var last *zerolog.Event
	for range 2 {
		e := log.Info()
		if last != nil {
			last.Ctx(ctx)
			e.Msg("ctx on the event of the previous iteration") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		last = e
	}
}

func badEventCtxInIIFEOnPreviousIteration(ctx context.Context, log zerolog.Logger) {
	var last *zerolog.Event
	for range 2 {
		e := log.Info()
		if last != nil {
			func() { last.Ctx(ctx) }()
			e.Msg("ctx in an IIFE on the event of the previous iteration") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		last = e
	}
}

func goodEventCtxInIIFEOnPreviousIterationLoggedThere(ctx context.Context, log zerolog.Logger) {
	var last *zerolog.Event
	for range 2 {
		e := log.Info()
		if last != nil {
			func() { last.Ctx(ctx) }()
			last.Msg("ctx in an IIFE on the event of the previous iteration, logged") // OK
		}
		last = e
	}
}

func badEventCtxInIIFEOnPreviousIterationField(ctx context.Context, log zerolog.Logger) {
	var h inPlaceHolder
	for range 2 {
		e := log.Info()
		if h.e != nil {
			func() { h.e.Ctx(ctx) }()
			e.Msg("ctx in an IIFE on the event of the previous iteration, in a field") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		h.e = e
	}
}

func goodEventCtxInIIFEOnPreviousIterationFieldLoggedThere(ctx context.Context, log zerolog.Logger) {
	var h inPlaceHolder
	for range 2 {
		e := log.Info()
		if h.e != nil {
			func() { h.e.Ctx(ctx) }()
			h.e.Msg("ctx in an IIFE on the event of the previous iteration in a field, logged") // OK
		}
		h.e = e
	}
}

// The receiver is taken out through every func literal in between, and
// through the values of the func literal that hold what it read (#297).

func goodEventCtxInNestedIIFEOnPreviousIterationLoggedThere(ctx context.Context, log zerolog.Logger) {
	var last *zerolog.Event
	for range 2 {
		e := log.Info()
		if last != nil {
			func() {
				func() { last.Ctx(ctx) }()
			}()
			last.Msg("ctx in a nested IIFE on the event of the previous iteration, logged") // OK
		}
		last = e
	}
}

func badEventCtxInNestedIIFEOnPreviousIteration(ctx context.Context, log zerolog.Logger) {
	var last *zerolog.Event
	for range 2 {
		e := log.Info()
		if last != nil {
			func() {
				func() { last.Ctx(ctx) }()
			}()
			e.Msg("ctx in a nested IIFE on the event of the previous iteration") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		last = e
	}
}

func goodEventCtxInIIFEOnPreviousIterationBoxedLoggedThere(ctx context.Context, log zerolog.Logger) {
	var last *zerolog.Event
	for range 2 {
		e := log.Info()
		if last != nil {
			func() { (&inPlaceHolder{last}).e.Ctx(ctx) }()
			last.Msg("ctx in an IIFE on the boxed event of the previous iteration, logged") // OK
		}
		last = e
	}
}

func badEventCtxInIIFEOnPreviousIterationBoxed(ctx context.Context, log zerolog.Logger) {
	var last *zerolog.Event
	for range 2 {
		e := log.Info()
		if last != nil {
			func() { (&inPlaceHolder{last}).e.Ctx(ctx) }()
			e.Msg("ctx in an IIFE on the boxed event of the previous iteration") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		last = e
	}
}

func goodEventCtxInIIFEOnPreviousIterationCopiedLoggedThere(ctx context.Context, log zerolog.Logger, other *zerolog.Event) {
	var last *zerolog.Event
	for range 2 {
		e := log.Info()
		if last != nil {
			func() {
				x := other
				x = last
				x.Ctx(ctx)
			}()
			last.Msg("ctx in an IIFE on the copied event of the previous iteration, logged") // OK
		}
		last = e
	}
}

func badEventCtxInIIFEOnPreviousIterationReplacedFirst(ctx context.Context, log zerolog.Logger) {
	var last *zerolog.Event
	for range 2 {
		e := log.Info()
		if last != nil {
			func() {
				last = log.Warn()
				last.Ctx(ctx)
			}()
			e.Msg("ctx in an IIFE on an event it stored first") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		last = e
	}
}

func badEventCtxInIIFEOnPreviousIterationBoxedInLoop(ctx context.Context, log zerolog.Logger) {
	var last *zerolog.Event
	for range 2 {
		e := log.Info()
		if last != nil {
			func() {
				var b inPlaceHolder
				for range 2 {
					b.e = b.e
				}
				b.e.Ctx(ctx)
			}()
			e.Msg("ctx in an IIFE on an event that leads back to itself") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		last = e
	}
}

func badEventCtxOnPreviousIterationField(ctx context.Context, log zerolog.Logger) {
	var h inPlaceHolder
	for range 2 {
		e := log.Info()
		if h.e != nil {
			h.e.Ctx(ctx)
			e.Msg("ctx on the event of the previous iteration, in a field") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		h.e = e
	}
}

func badEventCtxOnPreviousIterationLoadedBefore(ctx context.Context, log zerolog.Logger) {
	var h inPlaceHolder
	for range 2 {
		old := h.e
		h.e = log.Info()
		if old != nil {
			old.Ctx(ctx)
			h.e.Msg("ctx on the event read before the new one") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
	}
}

func badEventCtxOnOtherElementMadeInLoop(ctx context.Context, log zerolog.Logger) {
	var evs [2]*zerolog.Event
	for i := range 2 {
		evs[i] = log.Info()
	}
	evs[0].Ctx(ctx)
	evs[1].Msg("ctx on another element") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxOnOtherElementMadeInLoopReadThroughBox(ctx context.Context, log zerolog.Logger) {
	var evs [2]*zerolog.Event
	for i := range 2 {
		evs[i] = log.Info()
	}
	evs[0].Ctx(ctx)
	(&inPlaceHolder{evs[1]}).e.Msg("ctx on another element, read through a box") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxThroughBoxOnOtherElementMadeInLoop(ctx context.Context, log zerolog.Logger) {
	var evs [2]*zerolog.Event
	for i := range 2 {
		evs[i] = log.Info()
	}
	(&inPlaceHolder{evs[0]}).e.Ctx(ctx)
	evs[1].Msg("ctx through a box on another element") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventCtxOnEachElementMadeInLoopReadThroughBox(ctx context.Context, log zerolog.Logger) {
	var evs [2]*zerolog.Event
	for i := range 2 {
		e := log.Info()
		e.Ctx(ctx)
		evs[i] = e
	}
	(&inPlaceHolder{evs[1]}).e.Msg("ctx on each element, read through a box") // OK
}

func goodEventCtxOnPreviousIterationReadThroughBox(ctx context.Context, log zerolog.Logger) {
	var last *zerolog.Event
	for range 2 {
		e := log.Info()
		e.Ctx(ctx)
		if last != nil {
			(&inPlaceHolder{last}).e.Msg("ctx on each iteration, read through a box") // OK
		}
		last = e
	}
}

func badEventCtxOnLatestForPreviousIteration(ctx context.Context, log zerolog.Logger) {
	var last *zerolog.Event
	for range 2 {
		e := log.Info()
		if last != nil {
			e.Ctx(ctx)
			last.Msg("ctx on the event of this iteration only") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		last = e
	}
}

func badEventDeferredCtxOnPreviousIteration(ctx context.Context, log zerolog.Logger) {
	var last *zerolog.Event
	for range 2 {
		e := log.Info()
		defer e.Msg("deferred, ctx on the event of the previous iteration") // want `zerolog call chain missing .Ctx\(ctx\)`
		if last != nil {
			last.Ctx(ctx)
		}
		last = e
	}
}

func goodEventCtxOnSameIteration(ctx context.Context, log zerolog.Logger) {
	for range 2 {
		e := log.Info()
		e.Ctx(ctx)
		e.Msg("ctx on the same iteration") // OK
	}
}

func goodEventCtxOnSameIterationField(ctx context.Context, log zerolog.Logger) {
	var h inPlaceHolder
	for range 2 {
		h.e = log.Info()
		h.e.Ctx(ctx)
		h.e.Msg("ctx on the same iteration, in a field") // OK
	}
}

func goodEventCtxOnPreviousIterationForIt(ctx context.Context, log zerolog.Logger) {
	var last *zerolog.Event
	for range 2 {
		e := log.Info()
		if last != nil {
			last.Ctx(ctx)
			last.Msg("ctx on the event of the previous iteration, logged") // OK
		}
		last = e
	}
}

func goodEventCtxOnLatestBeforeKept(ctx context.Context, log zerolog.Logger) {
	var last *zerolog.Event
	for range 2 {
		e := log.Info()
		e.Ctx(ctx)
		if last != nil {
			last.Msg("ctx on each event before it is kept") // OK - on the previous iteration
		}
		last = e
	}
}

func goodEventCtxInLoopFieldReadAfter(ctx context.Context, log zerolog.Logger) {
	var h inPlaceHolder
	for range 2 {
		h.e = log.Info()
		h.e.Ctx(ctx)
	}
	h.e.Msg("ctx on each event of the loop") // OK
}

func goodEventCtxOnSameElementMadeInLoop(ctx context.Context, log zerolog.Logger) {
	var evs [2]*zerolog.Event
	for i := range 2 {
		evs[i] = log.Info()
	}
	evs[0].Ctx(ctx)
	evs[0].Msg("ctx on the same element") // OK
}

func goodEventCtxOnSameElementAtComputedIndex(ctx context.Context, log zerolog.Logger, j int) {
	var evs [2]*zerolog.Event
	for i := range 2 {
		evs[i] = log.Info()
	}
	evs[j].Ctx(ctx)
	evs[j].Msg("ctx on the same element, at a computed index") // OK
}

func goodEventCtxOnEachElementInLoop(ctx context.Context, log zerolog.Logger) {
	var evs [2]*zerolog.Event
	for i := range 2 {
		evs[i] = log.Info()
		evs[i].Ctx(ctx)
	}
	evs[1].Msg("ctx on each element") // OK
}

func goodEventDeferredCtxOnSameIteration(ctx context.Context, log zerolog.Logger) {
	for range 2 {
		e := log.Info()
		defer e.Msg("deferred, ctx on the same iteration") // OK
		e.Ctx(ctx)
	}
}

// LIMITATION: last holds the event of this iteration on one path, and of the
// previous one on the other. A Ctx call on a value that can hold either
// counts.
func limitationEventCtxOnLatestOrPreviousIteration(ctx context.Context, log zerolog.Logger, cond bool) {
	var last *zerolog.Event
	for range 2 {
		e := log.Info()
		if cond {
			last = e
		}
		if last != nil {
			last.Ctx(ctx)
			e.Msg("ctx on this or the previous event") // LIMITATION: not reported
		}
	}
}

// ===== EVENTS STORED BACK =====

// LIMITATION (false negative): e can be a or b at the Ctx call, and a or b at
// the terminator, so the call counts for both. On the path where e is b at
// the call and a is stored back, a has no Ctx call.
func limitationEventCtxBeforeEventStoredBack(ctx context.Context, log zerolog.Logger, cond, back bool) {
	a := log.Info()
	b := log.Warn()
	e := a
	if cond {
		e = b
	}
	e.Ctx(ctx)
	if back {
		e = a
	}
	e.Msg("a stored back after Ctx on b") // LIMITATION: should report but doesn't
}

// LIMITATION (false negative): The caller's event at p.e has a Ctx call. q
// holds another event of the caller's, whose stores are not seen, so copying
// it over p is not taken to store another event.
func limitationEventCtxBeforeCallerEventCopiedOver(ctx context.Context, p, q *inPlaceHolder) {
	p.e.Ctx(ctx)
	*p = *q
	p.e.Msg("caller's event copied over after Ctx") // LIMITATION: should report but doesn't
}

// ===== CONSTANT CONDITIONS =====

// A branch on a constant takes one successor only. The path that skips the
// Ctx call never runs (#266).
func goodEventCtxInPlaceUnderIfTrue(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	if true {
		e.Ctx(ctx)
	}
	e.Msg("ctx in place under if true") // OK - the branch always runs
}

func badEventCtxInPlaceUnderIfFalse(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	if false {
		e.Ctx(ctx)
	}
	e.Msg("ctx in place under if false") // want `zerolog call chain missing .Ctx\(ctx\)`
}

const (
	inPlaceDebug   = true
	inPlaceVerbose = false
)

func goodEventCtxInPlaceUnderTrueConstant(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	if inPlaceDebug {
		e.Ctx(ctx)
	}
	e.Msg("ctx in place under a true constant") // OK - the branch always runs
}

func badEventCtxInPlaceUnderFalseConstant(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	if inPlaceVerbose {
		e.Ctx(ctx)
	}
	e.Msg("ctx in place under a false constant") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventCtxInPlaceInElseOfFalseConstant(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	if inPlaceVerbose {
		e.Str("k", "v")
	} else {
		e.Ctx(ctx)
	}
	e.Msg("ctx in place in the else of a false constant") // OK - the else always runs
}

func badEventCtxInPlaceInElseOfTrueConstant(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	if inPlaceDebug {
		e.Str("k", "v")
	} else {
		e.Ctx(ctx)
	}
	e.Msg("ctx in place in the else of a true constant") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventCtxInPlaceUnderConstantInLoop(ctx context.Context, log zerolog.Logger, n int) {
	for range n {
		e := log.Info()
		if inPlaceDebug {
			e.Ctx(ctx)
		}
		e.Msg("ctx in place under a constant in a loop") // OK - the branch always runs
	}
}

func goodEventCtxInPlaceUnderConstantInClosure(ctx context.Context, log zerolog.Logger) {
	func() {
		e := log.Info()
		if true {
			e.Ctx(ctx)
		}
		e.Msg("ctx in place under a constant in a closure") // OK - the branch always runs
	}()
}

func goodEventCtxInPlaceInClosureUnderConstant(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	func() {
		if inPlaceDebug {
			e.Ctx(ctx)
		}
	}()
	e.Msg("ctx in place in a closure under a constant") // OK - the branch always runs
}

func goodEventCtxInPlaceUnderNestedConstants(ctx context.Context, log zerolog.Logger, cond bool) {
	e := log.Info()
	if !inPlaceVerbose && true {
		if cond {
			e.Ctx(ctx)
		} else {
			e.Ctx(ctx)
		}
	}
	e.Msg("ctx in place under nested constants") // OK - every path that runs has a Ctx call
}

func badEventCtxInPlaceUnderCondition(ctx context.Context, log zerolog.Logger, cond bool) {
	e := log.Info()
	if cond {
		e.Ctx(ctx)
	}
	e.Msg("ctx in place under a condition") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventMadeUnderFalseConstant(ctx context.Context, log zerolog.Logger) {
	if inPlaceVerbose {
		e := log.Info()
		e.Msg("made under a false constant") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

// LIMITATION(#266) (false positive): the check of `for range 1` is not
// folded. The path that skips the loop is still taken to reach the
// terminator.
func limitationEventCtxInPlaceInLoopOfOneRun(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	for range 1 {
		e.Ctx(ctx)
	}
	e.Msg("ctx in place under for range 1") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== UPDATECONTEXT (changes the Logger in place) =====

// UpdateContext keeps only the fields its func writes. What Context.Ctx sets
// in the func is dropped, so the Logger has no context after it (#152).
func badUpdateContextCtxDropped(ctx context.Context, l zerolog.Logger) {
	l.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Ctx(ctx)
	})
	l.Info().Msg("after update") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badUpdateContextCtxDroppedOnPointer(ctx context.Context, l *zerolog.Logger) {
	l.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("k", "v").Ctx(ctx)
	})
	l.Info().Msg("after update on pointer") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badUpdateContextCtxDroppedThenWith(ctx context.Context, l zerolog.Logger) {
	l.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Ctx(ctx)
	})
	derived := l.With().Str("k", "v").Logger()
	derived.Info().Msg("derived after update") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodUpdateContextKeepsCtx(ctx context.Context, l zerolog.Logger) {
	l = l.With().Ctx(ctx).Logger()
	l.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("k", "v")
	})
	l.Info().Msg("ctx before update") // OK - UpdateContext keeps the ctx the Logger had
}

func badUpdateContextWithoutCtx(ctx context.Context, l zerolog.Logger) {
	l.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("k", "v")
	})
	l.Info().Msg("after update without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badLogBeforeUpdateContext(ctx context.Context, l zerolog.Logger) {
	l.Info().Msg("before update") // want `zerolog call chain missing .Ctx\(ctx\)`
	l.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Ctx(ctx)
	})
}

func badUpdateContextInBranch(ctx context.Context, l zerolog.Logger, cond bool) {
	if cond {
		l.UpdateContext(func(c zerolog.Context) zerolog.Context {
			return c.Ctx(ctx)
		})
	}
	l.Info().Msg("update on one path") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badUpdateContextOnOneReturn(ctx context.Context, l zerolog.Logger, cond bool) {
	l.UpdateContext(func(c zerolog.Context) zerolog.Context {
		if cond {
			return c
		}
		return c.Ctx(ctx)
	})
	l.Info().Msg("ctx on one return") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badUpdateContextThenReplaced(ctx context.Context, l zerolog.Logger, other zerolog.Logger) {
	l.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Ctx(ctx)
	})
	l = other
	l.Info().Msg("replaced after update") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badPointerParamWithoutUpdate(ctx context.Context, l *zerolog.Logger) {
	l.Info().Msg("pointer param") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func addServiceKey(c zerolog.Context) zerolog.Context {
	return c.Str("service", "api")
}

func badUpdateContextWithDeclaredFunc(ctx context.Context, l zerolog.Logger) {
	l.UpdateContext(addServiceKey)
	l.Info().Msg("declared func without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// The func is not known statically, so it cannot count.
func badUpdateContextWithFuncValue(ctx context.Context, l zerolog.Logger, update func(zerolog.Context) zerolog.Context) {
	l.UpdateContext(update)
	l.Info().Msg("func value") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== Ctx IN PLACE THROUGH A POINTER NOT IN A VARIABLE =====

// An event behind a pointer that is not in a variable, such as one a call
// returns, is not known to the function. Two reads of the same place through
// the same pointer see the same event, as for a pointer parameter.

func newInPlaceHolder() *inPlaceHolder { return &inPlaceHolder{} }

var inPlaceGlobalHolder inPlaceHolder

var inPlaceGlobalPtr *inPlaceHolder

func goodEventCtxInPlaceReturnedHolder(ctx context.Context) {
	h := newInPlaceHolder()
	h.e.Ctx(ctx)
	h.e.Msg("ctx in place, holder a call returns") // OK - the same event
}

func goodEventCtxInPlaceDoublePointer(ctx context.Context, pp **inPlaceHolder) {
	(*pp).e.Ctx(ctx)
	(*pp).e.Msg("ctx in place, through a pointer to a pointer") // OK
}

func goodEventCtxInPlaceDoublePointerEvent(ctx context.Context, pp **zerolog.Event) {
	(*pp).Ctx(ctx)
	(*pp).Msg("ctx in place, pointer to a pointer to an event") // OK
}

func goodEventCtxInPlaceSliceElement(ctx context.Context, log zerolog.Logger) {
	s := []*zerolog.Event{log.Info()}
	s[0].Ctx(ctx)
	s[0].Msg("ctx in place, the same element of a slice") // OK
}

func badEventCtxInPlaceOtherSliceElement(ctx context.Context, log zerolog.Logger) {
	s := []*zerolog.Event{log.Info(), log.Warn()}
	s[0].Ctx(ctx)
	s[1].Msg("ctx in place on another element") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceSliceElementStoredBetween(ctx context.Context, log zerolog.Logger) {
	s := []*zerolog.Event{log.Info()}
	s[0].Ctx(ctx)
	s[0] = log.Warn()
	s[0].Msg("another event stored in between") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceReturnedHolderStoredBetween(ctx context.Context, log zerolog.Logger) {
	h := newInPlaceHolder()
	h.e.Ctx(ctx)
	h.e = log.Info()
	h.e.Msg("another event stored in between") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceOtherHolder(ctx context.Context) {
	h1, h2 := newInPlaceHolder(), newInPlaceHolder()
	h1.e.Ctx(ctx)
	h2.e.Msg("ctx on another holder") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceDoublePointerReassigned(ctx context.Context, pp **inPlaceHolder, other *inPlaceHolder) {
	(*pp).e.Ctx(ctx)
	*pp = other
	(*pp).e.Msg("another holder stored in between") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A global can be changed by another goroutine between the two reads, so the
// two reads are not taken for the same event.

func badEventCtxInPlaceGlobalHolder(ctx context.Context) {
	inPlaceGlobalHolder.e.Ctx(ctx)
	inPlaceGlobalHolder.e.Msg("global holder") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceGlobalPointer(ctx context.Context) {
	inPlaceGlobalPtr.e.Ctx(ctx)
	inPlaceGlobalPtr.e.Msg("global pointer to a holder") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== Ctx IN PLACE IN A FUNC LITERAL CALLED IN PLACE =====

// A func literal called where it is made runs at the call, and so do the Ctx
// calls it makes on every run.

func goodEventCtxInPlaceInIIFE(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	func() { e.Ctx(ctx) }()
	e.Msg("ctx in place in an IIFE") // OK
}

func goodEventCtxInPlaceInClosureCalled(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	setCtx := func() { e.Ctx(ctx) }
	setCtx()
	e.Msg("ctx in place in a closure called before") // OK
}

func goodEventCtxInPlaceInNestedIIFE(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	func() {
		func() { e.Ctx(ctx) }()
	}()
	e.Msg("ctx in place in a nested IIFE") // OK
}

func goodEventCtxInPlaceInNestedIIFEEitherEvent(ctx context.Context, log zerolog.Logger, cond bool) {
	var e *zerolog.Event
	if cond {
		e = log.Info()
	} else {
		e = log.Warn()
	}
	func() {
		func() { e.Ctx(ctx) }()
	}()
	e.Msg("ctx in place in a nested IIFE, on either event") // OK
}

func badEventCtxInPlaceInNestedIIFEEitherEventReplaced(ctx context.Context, log zerolog.Logger, cond bool) {
	var e *zerolog.Event
	if cond {
		e = log.Info()
	} else {
		e = log.Warn()
	}
	func() {
		func() { e.Ctx(ctx) }()
	}()
	e = log.Error()
	e.Msg("ctx in place in a nested IIFE, on events replaced after") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventCtxInPlaceInIIFEDeferredTerminator(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	defer e.Msg("deferred, ctx in place in an IIFE after") // OK
	func() { e.Ctx(ctx) }()
}

func badEventCtxInPlaceInIIFEOneBranch(ctx context.Context, log zerolog.Logger, cond bool) {
	e := log.Info()
	func() {
		if cond {
			e.Ctx(ctx)
		}
	}()
	e.Msg("ctx in place in an IIFE on one branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventCtxInPlaceInIIFEBothBranches(ctx context.Context, log zerolog.Logger, cond bool) {
	e := log.Info()
	func() {
		if cond {
			e.Ctx(ctx)
		} else {
			e.Ctx(ctx)
		}
	}()
	e.Msg("ctx in place in an IIFE on both branches") // OK
}

func goodEventCtxInPlaceInIIFEEveryCase(ctx context.Context, log zerolog.Logger, n int) {
	e := log.Info()
	func() {
		switch n {
		case 0:
			e.Ctx(ctx)
			return
		case 1:
			e.Ctx(ctx)
		default:
			e.Ctx(ctx)
		}
	}()
	e.Msg("ctx in place in an IIFE in every case") // OK
}

func badEventCtxInPlaceInIIFEBranchesOtherEvent(ctx context.Context, log zerolog.Logger, cond bool) {
	e := log.Info()
	other := log.Warn()
	func() {
		if cond {
			e.Ctx(ctx)
		} else {
			other.Ctx(ctx)
		}
	}()
	e.Msg("ctx in place in an IIFE, on another event on one branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceInIIFEBranchesEitherEvent(ctx context.Context, log zerolog.Logger, cond, pick bool) {
	e := log.Info()
	either := e
	if pick {
		either = log.Warn()
	}
	func() {
		if cond {
			e.Ctx(ctx)
		} else {
			either.Ctx(ctx)
		}
	}()
	e.Msg("ctx in place in an IIFE, on either event on one branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceInIIFEEarlyReturn(ctx context.Context, log zerolog.Logger, cond bool) {
	e := log.Info()
	func() {
		if cond {
			return
		}
		e.Ctx(ctx)
	}()
	e.Msg("ctx in place in an IIFE after an early return") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceInClosureCalledAfter(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	setCtx := func() { e.Ctx(ctx) }
	e.Msg("closure called after the terminator") // want `zerolog call chain missing .Ctx\(ctx\)`
	setCtx()
}

func badEventCtxInPlaceInGoroutine(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	go func() { e.Ctx(ctx) }()
	e.Msg("ctx in place in a goroutine") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceInDeferredClosure(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	defer func() { e.Ctx(ctx) }()
	e.Msg("ctx in place in a deferred closure") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceInIIFEOtherEvent(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	other := log.Warn()
	func() { other.Ctx(ctx) }()
	e.Msg("ctx in place on another event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// The caller's event, read through a pointer the closure loads from the
// variable it captures, is the one read through the same pointer after it.

func goodEventCtxInPlaceInIIFEPointerParamField(ctx context.Context, h *inPlaceHolder) {
	func() { h.e.Ctx(ctx) }()
	h.e.Msg("ctx in place in an IIFE, through a pointer parameter") // OK
}

func goodEventCtxInPlaceInIIFESliceElement(ctx context.Context, log zerolog.Logger) {
	s := []*zerolog.Event{log.Info()}
	func() { s[0].Ctx(ctx) }()
	s[0].Msg("ctx in place in an IIFE, on a slice element") // OK
}

func badEventCtxInPlaceInIIFEOtherPointerParam(ctx context.Context, h, g *inPlaceHolder) {
	func() { g.e.Ctx(ctx) }()
	h.e.Msg("ctx in place through another pointer") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceInIIFEPointerReplaced(ctx context.Context, h, g *inPlaceHolder) {
	func() { h.e.Ctx(ctx) }()
	h = g
	h.e.Msg("ctx in place through a pointer replaced after") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventCtxInPlaceInIIFEPointerParamFieldInside(ctx context.Context, h *inPlaceHolder) {
	func() {
		h.e.Ctx(ctx)
		h.e.Msg("ctx in place and terminator in an IIFE, through a pointer parameter") // OK
	}()
}

func goodEventCtxInPlaceInIIFEPointerParamFieldOnBranches(ctx context.Context, h *inPlaceHolder, cond bool) {
	if cond {
		func() { h.e.Ctx(ctx) }()
		h.e.Msg("ctx in place in an IIFE, on the first branch") // OK
	} else {
		func() { h.e.Ctx(ctx) }()
		h.e.Msg("ctx in place in an IIFE, on the second branch") // OK
	}
}

func badEventCtxInPlaceInIIFEPointerParamFieldOtherBranch(ctx context.Context, h *inPlaceHolder, cond bool) {
	if cond {
		func() { h.e.Ctx(ctx) }()
		h.e.Msg("ctx in place in an IIFE, on this branch") // OK
	} else {
		func() { _ = h }()
		h.e.Msg("ctx in place in an IIFE on the other branch") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

func goodEventCtxInPlaceInIIFEPointerToPointer(ctx context.Context, pp **inPlaceHolder) {
	func() { (*pp).e.Ctx(ctx) }()
	(*pp).e.Msg("ctx in place in an IIFE, through two loads") // OK
}

func badEventCtxInPlaceInIIFEPointerToPointerReplaced(ctx context.Context, pp **inPlaceHolder, g *inPlaceHolder) {
	func() { (*pp).e.Ctx(ctx) }()
	*pp = g
	(*pp).e.Msg("ctx in place through two loads, the pointer replaced after") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A func literal in between that only passes the variable on has no read of
// its own, so the place is taken out one func literal at a time (#296).

func goodEventCtxInPlaceInNestedIIFEPointerParamField(ctx context.Context, h *inPlaceHolder) {
	func() {
		func() { h.e.Ctx(ctx) }()
	}()
	h.e.Msg("ctx in place in a nested IIFE, through a pointer parameter") // OK
}

func goodEventCtxInPlaceInIIFEThreeLevelsPointerParamField(ctx context.Context, h *inPlaceHolder) {
	func() { func() { func() { h.e.Ctx(ctx) }() }() }()
	h.e.Msg("ctx in place three IIFEs down, through a pointer parameter") // OK
}

func goodEventCtxInPlaceInNestedIIFEPointerToPointer(ctx context.Context, pp **inPlaceHolder) {
	func() { func() { (*pp).e.Ctx(ctx) }() }()
	(*pp).e.Msg("ctx in place in a nested IIFE, through two loads") // OK
}

func goodEventCtxInPlaceInNestedIIFESliceElement(ctx context.Context, log zerolog.Logger) {
	s := []*zerolog.Event{log.Info()}
	func() { func() { s[0].Ctx(ctx) }() }()
	s[0].Msg("ctx in place in a nested IIFE, on a slice element") // OK
}

func goodEventCtxInPlaceInNestedIIFEReadInBetween(ctx context.Context, h *inPlaceHolder) {
	func() {
		func() { h.e.Ctx(ctx) }()
		h.e.Msg("ctx in place in a nested IIFE, logged in between") // OK
	}()
}

func goodEventCtxInPlaceInNestedIIFEArrayOfPointers(ctx context.Context, hs [1]*inPlaceHolder) {
	func() { func() { hs[0].e.Ctx(ctx) }() }()
	hs[0].e.Msg("ctx in place in a nested IIFE, through an array of pointers") // OK
}

func goodEventCtxInPlaceInNestedIIFEEventOfTheOneInBetween(ctx context.Context, log zerolog.Logger) {
	func() {
		e := log.Info()
		func() { e.Ctx(ctx) }()
		e.Msg("ctx in place in a nested IIFE, on the event of the one in between") // OK
	}()
}

func badEventCtxInPlaceInNestedIIFEEventOfTheOneInBetween(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	func() {
		e := log.Warn()
		func() { e.Ctx(ctx) }()
	}()
	e.Msg("ctx in place in a nested IIFE, on the event of the one in between") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceInNestedIIFEOtherPointerParam(ctx context.Context, h, g *inPlaceHolder) {
	func() { func() { g.e.Ctx(ctx) }() }()
	h.e.Msg("ctx in place in a nested IIFE through another pointer") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceInNestedIIFEPointerReplacedInBetween(ctx context.Context, h, g *inPlaceHolder) {
	func() {
		func() { h.e.Ctx(ctx) }()
		h = g
	}()
	h.e.Msg("ctx in place in a nested IIFE, the pointer replaced in between") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceInNestedIIFEPointerToPointerStoredThrough(ctx context.Context, pp **inPlaceHolder, g *inPlaceHolder) {
	func() {
		func() { (*pp).e.Ctx(ctx) }()
		*pp = g
	}()
	(*pp).e.Msg("ctx in place in a nested IIFE, stored through in between") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceInNestedIIFEOwnPointer(ctx context.Context, h, g *inPlaceHolder) {
	func() {
		h := g
		func() { h.e.Ctx(ctx) }()
	}()
	h.e.Msg("ctx in place in a nested IIFE, on the pointer declared there") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A func literal in between that reads the place only through the ones it
// calls on its branches has no read of its own to take the place to, so the
// two reads are matched with each other (#324).

func goodEventCtxInPlaceInNestedIIFEPointerParamFieldOnBranches(ctx context.Context, h *inPlaceHolder, cond bool) {
	func() {
		if cond {
			func() { h.e.Ctx(ctx) }()
		} else {
			func() { h.e.Ctx(ctx) }()
		}
	}()
	h.e.Msg("ctx in place in a nested IIFE on both branches") // OK
}

func goodEventCtxInPlaceInNestedIIFEPointerToPointerOnBranches(ctx context.Context, pp **inPlaceHolder, cond bool) {
	func() {
		if cond {
			func() { (*pp).e.Ctx(ctx) }()
		} else {
			func() { (*pp).e.Ctx(ctx) }()
		}
	}()
	(*pp).e.Msg("ctx in place in a nested IIFE on both branches, through two loads") // OK
}

func goodEventCtxInPlaceInIIFEThreeLevelsPointerParamFieldOnBranches(ctx context.Context, h *inPlaceHolder, cond bool) {
	func() {
		func() {
			if cond {
				func() { h.e.Ctx(ctx) }()
			} else {
				func() { h.e.Ctx(ctx) }()
			}
		}()
	}()
	h.e.Msg("ctx in place three IIFEs down on both branches") // OK
}

func badEventCtxInPlaceInNestedIIFEPointerParamFieldOneBranch(ctx context.Context, h *inPlaceHolder, cond bool) {
	func() {
		if cond {
			func() { h.e.Ctx(ctx) }()
		} else {
			func() { _ = h }()
		}
	}()
	h.e.Msg("ctx in place in a nested IIFE on one branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceInNestedIIFEOtherPointerParamOnBranch(ctx context.Context, h, g *inPlaceHolder, cond bool) {
	func() {
		if cond {
			func() { h.e.Ctx(ctx) }()
		} else {
			func() { g.e.Ctx(ctx) }()
		}
	}()
	h.e.Msg("ctx in place in a nested IIFE, through another pointer on a branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

type inPlacePair struct{ e, f *zerolog.Event }

func badEventCtxInPlaceInNestedIIFEOtherFieldOnBranch(ctx context.Context, h *inPlacePair, cond bool) {
	func() {
		if cond {
			func() { h.e.Ctx(ctx) }()
		} else {
			func() { h.f.Ctx(ctx) }()
		}
	}()
	h.e.Msg("ctx in place in a nested IIFE, on another field on a branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceInNestedIIFEPointerReplacedOnBranch(ctx context.Context, h, g *inPlaceHolder, cond bool) {
	func() {
		if cond {
			func() { h.e.Ctx(ctx) }()
		} else {
			func() { h.e.Ctx(ctx) }()
			h = g
		}
	}()
	h.e.Msg("ctx in place in a nested IIFE, the pointer replaced on a branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceInNestedIIFEPointerStoredInInnerOnBranch(ctx context.Context, h, g *inPlaceHolder, cond bool) {
	func() {
		if cond {
			func() { h.e.Ctx(ctx) }()
		} else {
			func() {
				func() { h.e.Ctx(ctx) }()
				h = g
			}()
		}
	}()
	h.e.Msg("ctx in place in a nested IIFE, the pointer replaced further in on a branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// LIMITATION (false positive): a store by a closure to a variable it captures
// is not seen after the closure is called, so h.e after the IIFE is not taken
// for the g.e it set the context of.
func limitationEventCtxInPlaceInIIFEPointerReplacedInside(ctx context.Context, h, g *inPlaceHolder) {
	func() {
		h = g
		h.e.Ctx(ctx)
	}()
	h.e.Msg("ctx in place through the pointer the IIFE stored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceInIIFEFreshContext(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	func() { e.Ctx(context.Background()) }()
	e.Msg("fresh context in an IIFE") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceInIIFEOwnEvent(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	func() {
		e := log.Info()
		e.Ctx(ctx)
	}()
	e.Msg("ctx in place on the IIFE's own event") // want `zerolog call chain missing .Ctx\(ctx\)`
}
