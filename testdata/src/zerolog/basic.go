package zerolog

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ===== SHOULD REPORT =====

func badNoCtx(ctx context.Context, log zerolog.Logger) {
	log.Info().Str("key", "value").Msg("hello") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badNoCtxMsgf(ctx context.Context, log zerolog.Logger) {
	log.Error().Msgf("error: %v", "oops") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badNoCtxSend(ctx context.Context, log zerolog.Logger) {
	log.Debug().Send() // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badNoCtxWarn(ctx context.Context, log zerolog.Logger) {
	log.Warn().Str("a", "b").Int("n", 1).Msg("warn") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badNoCtxFromEvent(ctx context.Context, event *zerolog.Event) {
	event.Str("key", "value").Msg("from event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== NESTED FUNCTIONS - SHOULD REPORT =====

func badNestedInnerFunc(ctx context.Context, log zerolog.Logger) {
	innerFunc := func() {
		log.Info().Msg("inner") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	innerFunc()
}

func badNestedInClosure(ctx context.Context, log zerolog.Logger) {
	func() {
		log.Info().Msg("closure") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

func badNestedDeep(ctx context.Context, log zerolog.Logger) {
	func() {
		func() {
			log.Info().Msg("deep") // want `zerolog call chain missing .Ctx\(ctx\)`
		}()
	}()
}

// ===== SHOULD NOT REPORT =====

func goodWithCtx(ctx context.Context, log zerolog.Logger) {
	log.Info().Ctx(ctx).Str("key", "value").Msg("hello") // OK
}

func goodWithCtxFirst(ctx context.Context, log zerolog.Logger) {
	log.Info().Ctx(ctx).Msg("hello") // OK
}

func goodWithCtxMiddle(ctx context.Context, log zerolog.Logger) {
	log.Info().Str("a", "b").Ctx(ctx).Str("c", "d").Msg("hello") // OK
}

func goodNoContextParam(log zerolog.Logger) {
	log.Info().Msg("hello")
}

// ===== NESTED - SHOULD NOT REPORT =====

func goodNestedWithCtx(ctx context.Context, log zerolog.Logger) {
	innerFunc := func() {
		log.Info().Ctx(ctx).Msg("inner") // OK - uses ctx from outer scope
	}
	innerFunc()
}

func goodNestedInClosureWithCtx(ctx context.Context, log zerolog.Logger) {
	func() {
		log.Info().Ctx(ctx).Msg("closure") // OK
	}()
}

func goodNestedInnerHasOwnCtx(outerCtx context.Context, log zerolog.Logger) {
	innerFunc := func(ctx context.Context) {
		log.Info().Ctx(ctx).Msg("inner") // OK - uses inner ctx
	}
	innerFunc(outerCtx)
}

// ===== EDGE CASES =====

func goodDifferentLogger(ctx context.Context) {
	// Not zerolog, should not report
	type fakeLogger struct{}
	var log fakeLogger
	_ = log
}

// ===== VARIABLE TRACKING (SSA) =====
// These test cases verify SSA-based tracking of zerolog.Event through variables.

func badEventInVariable(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	e.Str("key", "value")
	e.Msg("variable stored event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventReassigned(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	e = e.Str("key", "value")
	e.Msg("reassigned event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventInMultipleVars(ctx context.Context, log zerolog.Logger) {
	e1 := log.Info()
	e2 := e1.Str("key", "value")
	e2.Msg("multiple vars") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventInVariableWithCtx(ctx context.Context, log zerolog.Logger) {
	e := log.Info().Ctx(ctx)
	e.Str("key", "value")
	e.Msg("variable with ctx") // OK - .Ctx() was called
}

func goodEventCtxAddedLater(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	e = e.Ctx(ctx)
	e.Msg("ctx added later") // OK - .Ctx() was called via reassignment
}

func goodEventCtxInChain(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	e.Ctx(ctx).Str("key", "value").Msg("ctx in chain") // OK
}

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

func badNilEvent(ctx context.Context) {
	var e *zerolog.Event
	e.Msg("nil event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A deferred terminator runs at each return, so a Ctx call after the defer
// statement counts when it runs before every return (#122).

func goodDeferredMsgCtxAfterDefer(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	defer e.Msg("deferred, ctx after defer") // OK - .Ctx() runs before the return
	e.Ctx(ctx)
}

func goodDeferredMsgAfterEarlyReturn(ctx context.Context, log zerolog.Logger, cond bool) {
	if cond {
		return
	}
	e := log.Info()
	defer e.Msg("deferred after an early return") // OK - .Ctx() runs before the only return after it (#134)
	e.Ctx(ctx)
}

func goodDeferredMsgInBranch(ctx context.Context, log zerolog.Logger, cond bool) {
	if cond {
		e := log.Info()
		defer e.Msg("deferred in a branch") // OK - .Ctx() runs before the return after it (#134)
		e.Ctx(ctx)
	}
}

// An event made in a loop is another one on each iteration, so a Ctx call on
// a later one does not count (#156).
func badDeferredMsgCtxOnLaterIteration(ctx context.Context, log zerolog.Logger) {
	for i := 0; ; i++ {
		e := log.Info()
		if i == 0 {
			defer e.Msg("deferred, ctx on a later iteration") // want `zerolog call chain missing .Ctx\(ctx\)`
			continue
		}
		e.Ctx(ctx)
		return
	}
}

func goodDeferredMsgInLoopCtxSameIteration(ctx context.Context, log zerolog.Logger, n int) {
	for range n {
		e := log.Info()
		defer e.Msg("deferred in a loop, ctx on the same iteration") // OK - .Ctx() runs before the event is made again
		e.Ctx(ctx)
	}
}

func badDeferredMsgCtxBeforeOneReturn(ctx context.Context, log zerolog.Logger, cond bool) {
	e := log.Info()
	defer e.Msg("deferred, ctx before one return") // want `zerolog call chain missing .Ctx\(ctx\)`
	if cond {
		return
	}
	e.Ctx(ctx)
}

func badDeferredMsgNeverReturns(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	defer e.Msg("deferred, never returns") // want `zerolog call chain missing .Ctx\(ctx\)`
	for {
	}
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

func badDeferredMsgCtxOnOneBranch(ctx context.Context, log zerolog.Logger, cond bool) {
	e := log.Info()
	defer e.Str("k", "v").Msg("deferred, ctx on one branch") // want `zerolog call chain missing .Ctx\(ctx\)`
	if cond {
		e.Ctx(ctx)
	}
}

func goodDeferredChainCtxOnEachBranch(ctx context.Context, other context.Context, log zerolog.Logger, cond bool) {
	e := log.Info()
	defer e.Str("k", "v").Msg("deferred chain, ctx on each branch") // OK - .Ctx() runs on every path to the return
	if cond {
		e.Ctx(ctx)
	} else {
		e.Ctx(other)
	}
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

// LIMITATION (false positive): An in-place Ctx call is not seen by a
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

// ===== zerolog.Ctx() PATTERNS =====
// These test cases verify that zerolog.Ctx(ctx) is recognized as context-aware.

func goodZerologCtx(ctx context.Context) {
	// zerolog.Ctx(ctx) already uses context, so .Ctx() is not needed
	zerolog.Ctx(ctx).Info().Msg("using zerolog.Ctx") // OK - ctx already used
}

func goodZerologCtxWithFields(ctx context.Context) {
	zerolog.Ctx(ctx).Info().Str("key", "value").Msg("with fields") // OK
}

// ===== FRESH CONTEXTS =====
// A fresh context.Background() or TODO() carries nothing of the caller's, so
// a Ctx call given one does not count while a context is in scope (#197).

func badEventCtxBackground(ctx context.Context, log zerolog.Logger) {
	log.Info().Ctx(context.Background()).Msg("Background") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badZerologCtxTODO(ctx context.Context) {
	zerolog.Ctx(context.TODO()).Info().Msg("TODO") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badLogCtxBackground(ctx context.Context) {
	log.Ctx(context.Background()).Info().Msg("Background") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badContextCtxBackground(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(context.Background()).Logger()
	l.Info().Msg("Background on the logger") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxBackgroundVariable(ctx context.Context, log zerolog.Logger, debug bool) {
	bg := context.Background()
	if debug {
		bg = context.TODO()
	}
	log.Info().Ctx(bg).Msg("Background in a variable") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxBackgroundInMemory(ctx context.Context, log zerolog.Logger) {
	bg := context.Background()
	_ = &bg
	log.Info().Ctx(bg).Msg("Background in memory") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxBackgroundInPlace(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	e.Ctx(context.Background())
	e.Msg("Background in place") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A fresh context replaces the one the chain had.
func badEventCtxBackgroundOverCtxLogger(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(ctx).Logger()
	l.Info().Ctx(context.Background()).Msg("Background replaces ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxBackgroundUpdatedInLoop(ctx context.Context, log zerolog.Logger, n int, debug bool) {
	bg := context.Background()
	for range n {
		if debug {
			bg = context.TODO()
		}
		log.Info().Ctx(bg).Msg("Background updated in a loop") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

type freshContextHolder struct{ ctx context.Context }

func badEventCtxBackgroundInField(ctx context.Context, log zerolog.Logger) {
	var h freshContextHolder
	h.ctx = context.Background()
	log.Info().Ctx(h.ctx).Msg("Background in a field") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxBackgroundStoredThroughPointer(ctx context.Context, log zerolog.Logger, pc *context.Context) {
	*pc = context.Background()
	log.Info().Ctx(*pc).Msg("Background stored through a pointer") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventCtxThroughPointer(ctx context.Context, log zerolog.Logger, pc *context.Context) {
	log.Info().Ctx(*pc).Msg("through a pointer") // OK - the caller's value is not known
}

func goodEventCtxBackgroundUpdatedByClosure(ctx context.Context, log zerolog.Logger) {
	bg := context.Background()
	func() {
		bg = ctx
		log.Info().Ctx(bg).Msg("updated by a closure") // OK - ctx is stored
	}()
	func() {
		log.Info().Ctx(bg).Msg("captured, updated elsewhere") // OK - not known to be fresh
	}()
}

func goodEventCtxFromChannel(ctx context.Context, log zerolog.Logger, ch chan context.Context) {
	log.Info().Ctx(<-ch).Msg("from a channel") // OK - not known to be fresh
}

var globalBackground = context.Background()

// A global can be changed anywhere, so it is not taken as fresh.
func goodEventCtxGlobalBackground(ctx context.Context, log zerolog.Logger) {
	log.Info().Ctx(globalBackground).Msg("global") // OK - not known to be fresh
}

func goodEventCtxFromFuncValue(ctx context.Context, log zerolog.Logger, mk func() context.Context) {
	log.Info().Ctx(mk()).Msg("func value") // OK - not known to be fresh
}

func freshContextHelper(ctx context.Context) context.Context { return ctx }

func goodEventCtxFromHelper(ctx context.Context, log zerolog.Logger) {
	log.Info().Ctx(freshContextHelper(ctx)).Msg("from a helper") // OK - not Background itself
}

func goodEventCtxDerivedFromBackground(ctx context.Context, log zerolog.Logger) {
	derived := context.WithoutCancel(context.Background())
	log.Info().Ctx(derived).Msg("derived context") // OK - not Background itself
}

func goodEventCtxBackgroundOrCtx(ctx context.Context, log zerolog.Logger, detached bool) {
	c := ctx
	if detached {
		c = context.Background()
	}
	log.Info().Ctx(c).Msg("ctx on one path") // OK - ctx on one path
}

func goodEventCtxBackgroundWithoutCtxInScope(log zerolog.Logger) {
	log.Info().Ctx(context.Background()).Msg("no ctx in scope") // OK - no ctx to pass
}

// LIMITATION (false negative): A fresh context given to Ctx in place, with
// its result unused, does not count, but it does not replace the context the
// event had either. A variable a closure captures is not followed back to
// what is stored in it, so it is not taken as fresh.

func limitationEventCtxBackgroundInPlaceOverCtx(ctx context.Context, log zerolog.Logger) {
	e := log.Info().Ctx(ctx)
	e.Ctx(context.Background())
	e.Msg("Background in place over ctx") // LIMITATION: should report but doesn't
}

func limitationEventCtxBackgroundCaptured(ctx context.Context, log zerolog.Logger) {
	bg := context.Background()
	func() {
		log.Info().Ctx(bg).Msg("Background captured") // LIMITATION: should report but doesn't
	}()
}

// ===== GLOBAL LOGGER (github.com/rs/zerolog/log) =====
// Global logger usage should be reported when context is available.

func badGlobalLogger(ctx context.Context) {
	log.Info().Msg("global logger") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badGlobalLoggerWithFields(ctx context.Context) {
	log.Error().Str("error", "msg").Msg("error") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodGlobalLoggerWithCtx(ctx context.Context) {
	log.Info().Ctx(ctx).Msg("global with ctx") // OK
}

func goodGlobalLogCtx(ctx context.Context) {
	log.Ctx(ctx).Info().Msg("using log.Ctx") // OK - ctx already used
}

// ===== VERBOSE GLOBAL LOGGER (log.Logger.Info()) =====

func badVerboseGlobalLogger(ctx context.Context) {
	log.Logger.Info().Msg("verbose global") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodVerboseGlobalLoggerWithCtx(ctx context.Context) {
	log.Logger.Info().Ctx(ctx).Msg("verbose with ctx") // OK
}

// ===== IGNORE COMMENTS =====

func goodIgnoredSameLine(ctx context.Context, log zerolog.Logger) {
	log.Info().Msg("ignored") //zerologlintctx:ignore
}

func goodIgnoredPreviousLine(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore
	log.Info().Msg("ignored")
}

// An ignore above a chain that spans several lines reaches the whole chain (#76).
func goodIgnoredAboveMultiLineChain(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore
	log.Info().
		Str("k", "v").
		Msg("ignored")
}

func goodIgnoredInsideMultiLineChain(ctx context.Context, log zerolog.Logger) {
	log.Info().
		Str("k", "v").
		//zerologlintctx:ignore
		Msg("ignored")
}

func goodIgnoredAboveMultiLineDefer(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore
	defer log.Info().
		Str("k", "v").
		Msg("ignored")
}

// A go or defer call is reported at the "(" of the call, like any other, so a
// trailing ignore on that line reaches it (#100).
func goodTrailingIgnoreOnMultiLineDefer(ctx context.Context, log zerolog.Logger) {
	defer log.Info().
		Str("k", "v").
		Msg("ignored") //zerologlintctx:ignore
}

func goodTrailingIgnoreOnMultiLineGo(ctx context.Context, log zerolog.Logger) {
	go log.Info().
		Str("k", "v").
		Msg("ignored") //zerologlintctx:ignore
}

func badMultiLineDeferReportedAtCall(ctx context.Context, log zerolog.Logger) {
	defer log.Info().
		Str("k", "v").
		Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// An ignore above a call does not reach the calls inside a closure passed to it.
func badIgnoreAboveCallDoesNotReachClosure(ctx context.Context, log zerolog.Logger, run func(func())) {
	//zerologlintctx:ignore // want `unused zerologlintctx:ignore directive`
	run(func() {
		log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
	})
}

// A trailing ignore reaches its own line only, not the line below (#77).
func badTrailingIgnoreDoesNotReachNextLine(ctx context.Context, log zerolog.Logger) {
	log.Info().Msg("ignored")     //zerologlintctx:ignore
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badTrailingIgnoreOnBlockOpener(ctx context.Context, log zerolog.Logger, cond bool) {
	if cond { //zerologlintctx:ignore // want `unused zerologlintctx:ignore directive`
		log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

// A space after "//" makes it malformed, and it suppresses nothing.
func badIgnoreWithSpaceSameLine(ctx context.Context, log zerolog.Logger) {
	log.Info().Msg("not ignored") // zerologlintctx:ignore // want `zerolog call chain missing .Ctx\(ctx\)` `malformed zerologlintctx directive: write it as //zerologlintctx:name`
}

func goodIgnoredWithReason(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore - intentionally not passing context
	log.Info().Msg("ignored")
}

func badIgnoreWithSpacePreviousLine(ctx context.Context, log zerolog.Logger) {
	// zerologlintctx:ignore // want `malformed zerologlintctx directive: write it as //zerologlintctx:name`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A directive name that only starts with "ignore" is not the ignore directive.
func badIgnoreLookalikeName(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignored // want `unknown directive zerologlintctx:ignored`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A trailing comment is a reason, with or without a space before it.
func goodIgnoredWithSlashReason(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore // intentionally not passing context
	log.Info().Msg("ignored")
}

func goodIgnoredWithSlashReasonNoSpace(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore //intentionally not passing context
	log.Info().Msg("ignored")
}

func goodIgnoredWithGluedSlashReason(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore//intentionally not passing context
	log.Info().Msg("ignored")
}

func goodIgnoredWithDashAndSlashReason(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore - intentionally // not passing context
	log.Info().Msg("ignored")
}

// The ignore takes no argument. Text that is not after "//" or " - " is
// reported, and the ignore suppresses nothing.
func badIgnoreWithFreeText(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore intentionally detached // want `zerologlintctx:ignore takes no argument; write a reason after //`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A misspelled name is an unknown directive, and suppresses nothing.
func badIgnoreMisspelled(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignre // want `unknown directive zerologlintctx:ignre`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A reason glued on with "-" makes the name unknown.
func badIgnoreGluedDashReason(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore-reason // want `unknown directive zerologlintctx:ignore-reason`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A tool name that only starts with "zerologlintctx" is not this tool.
func badIgnoreLookalikeTool(ctx context.Context, log zerolog.Logger) {
	//zerologlintctxx:ignore
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== MALFORMED DIRECTIVES =====

func badMalformedSpaceAfterColon(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx: ignore // want `malformed zerologlintctx directive: write it as //zerologlintctx:name`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badMalformedBlockComment(ctx context.Context, log zerolog.Logger) {
	/*zerologlintctx:ignore*/ // want `malformed zerologlintctx directive: write it as //zerologlintctx:name`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// Directive names are lowercase.
func badMalformedUppercaseName(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:Ignore // want `malformed zerologlintctx directive: write it as //zerologlintctx:name`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// Prose that mentions zerologlintctx:ignore in the middle of a comment is not
// a directive, so it is not reported as malformed.
func goodProseMentioningDirective(ctx context.Context, log zerolog.Logger) {
	// Use zerologlintctx:ignore to suppress a report.
	log.Info().Ctx(ctx).Msg("ok")
}

// ===== UNUSED IGNORE DIRECTIVES =====

func badUnusedIgnore(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore  // want `unused zerologlintctx:ignore directive`
	log.Info().Ctx(ctx).Msg("already has ctx, ignore not needed")
}

func badUnusedIgnoreSameLine(ctx context.Context, log zerolog.Logger) {
	log.Info().Ctx(ctx).Msg("already has ctx") //zerologlintctx:ignore  // want `unused zerologlintctx:ignore directive`
}

func badUnusedIgnoreNoLog(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore  // want `unused zerologlintctx:ignore directive`
	_ = ctx
}
