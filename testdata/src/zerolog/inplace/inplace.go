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
// terminator in a closure, in a func literal the event is passed to, or on
// another variable the event was copied to (#139). The event reaches it
// through another reference, which is not followed back to the calls on it.

func limitationEventCtxResultUnusedClosure(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	e.Ctx(ctx)
	func() {
		e.Msg("ctx result unused, closure") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

func limitationEventCtxResultUnusedOnCopy(ctx context.Context, log zerolog.Logger) {
	var e, copied *zerolog.Event
	func() { _, _ = e, copied }()
	e = log.Info()
	copied = e
	e.Ctx(ctx)
	copied.Msg("ctx result unused, on a copy") // want `zerolog call chain missing .Ctx\(ctx\)`
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
