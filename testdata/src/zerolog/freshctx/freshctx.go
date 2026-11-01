// Package freshctx covers fresh contexts (context.Background, context.TODO) given
// in place of the context in scope.
package freshctx

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

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

// ===== FRESH CONTEXTS AFTER Ctx(ctx) =====

// Event.Ctx replaces the context of the event, so the last Ctx call on a
// path decides. A fresh context given after Ctx(ctx) replaces ctx (#258).

func badEventCtxBackgroundAfterCtxChained(ctx context.Context, log zerolog.Logger) {
	log.Info().Ctx(ctx).Ctx(context.Background()).Msg("x") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxTODOAfterCtxInVariable(ctx context.Context, log zerolog.Logger) {
	e := log.Info().Ctx(ctx)
	e.Ctx(context.TODO()).Msg("x") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxBackgroundInPlaceOverCtx(ctx context.Context, log zerolog.Logger) {
	e := log.Info().Ctx(ctx)
	e.Ctx(context.Background())
	e.Msg("Background in place over ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxBackgroundInPlaceAfterCtxInPlace(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	e.Ctx(ctx)
	e.Ctx(context.Background())
	e.Msg("Background in place after ctx in place") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxBackgroundInPlaceOverCtxLogger(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(ctx).Logger()
	e := l.Info()
	e.Ctx(context.Background())
	e.Msg("Background in place over the logger's ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxBackgroundOnOnePath(ctx context.Context, log zerolog.Logger, detached bool) {
	e := log.Info().Ctx(ctx)
	if detached {
		e.Ctx(context.Background())
	}
	e.Msg("Background on one path") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxOrBackgroundOnBranches(ctx context.Context, log zerolog.Logger, detached bool) {
	e := log.Info()
	if detached {
		e.Ctx(context.Background())
	} else {
		e.Ctx(ctx)
	}
	e.Msg("ctx on one path, Background on the other") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxBackgroundAfterDefer(ctx context.Context, log zerolog.Logger) {
	e := log.Info().Ctx(ctx)
	defer e.Msg("deferred, Background before the return") // want `zerolog call chain missing .Ctx\(ctx\)`
	e.Ctx(context.Background())
}

func badEventCtxBackgroundAfterCtxAfterDefer(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	defer e.Msg("deferred, Background after ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
	e.Ctx(ctx)
	e.Ctx(context.TODO())
}

func badEventCtxBackgroundAfterCtxInLoop(ctx context.Context, log zerolog.Logger, n int) {
	for range n {
		e := log.Info().Ctx(ctx)
		e.Ctx(context.Background())
		e.Msg("Background after ctx in a loop") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

// The documented form on a Context: a fresh context replaces ctx there too.
func badContextCtxBackgroundAfterCtx(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(ctx).Ctx(context.Background()).Logger()
	l.Info().Msg("Background after ctx on the logger") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventCtxAfterBackgroundChained(ctx context.Context, log zerolog.Logger) {
	log.Info().Ctx(context.Background()).Ctx(ctx).Msg("x") // OK - ctx is the last
}

func goodEventCtxInPlaceAfterBackground(ctx context.Context, log zerolog.Logger) {
	e := log.Info().Ctx(context.TODO())
	e.Ctx(ctx)
	e.Msg("ctx in place after TODO") // OK - ctx is the last
}

func goodEventCtxAfterBackgroundInPlace(ctx context.Context, log zerolog.Logger) {
	e := log.Info().Ctx(ctx)
	e.Ctx(context.Background())
	e.Ctx(ctx).Msg("ctx after Background in place") // OK - ctx is the last
}

func goodEventCtxAfterBackgroundOnOnePath(ctx context.Context, log zerolog.Logger, detached bool) {
	e := log.Info().Ctx(ctx)
	if detached {
		e.Ctx(context.Background())
		e.Ctx(ctx)
	}
	e.Msg("ctx after Background on one path") // OK - ctx is the last on each path
}

func goodEventCtxAfterBackgroundAfterDefer(ctx context.Context, log zerolog.Logger) {
	e := log.Info().Ctx(ctx)
	defer e.Msg("deferred, ctx before the return") // OK - ctx is the last
	e.Ctx(context.Background())
	e.Ctx(ctx)
}

func goodEventCtxBackgroundOnAnotherEvent(ctx context.Context, log zerolog.Logger) {
	e := log.Info().Ctx(ctx)
	other := log.Warn().Ctx(ctx)
	other.Ctx(context.Background())
	e.Msg("Background on another event") // OK - e keeps ctx
}

// A Ctx call that may run on another event may leave the context as it was,
// so a fresh one only counts when it runs on the event alone.
func goodEventCtxBackgroundOnEitherEvent(ctx context.Context, log zerolog.Logger, cond bool) {
	e := log.Info().Ctx(ctx)
	other := log.Warn().Ctx(ctx)
	either := e
	if cond {
		either = other
	}
	either.Ctx(context.Background())
	e.Msg("Background on e or another event") // OK - either may be the other
}

// LIMITATION (false negative): A fresh context given to Ctx counts only for
// a receiver that holds the same run of an event made in a loop on every
// path. last holds the run before, whose Ctx call is not looked at.
func limitationEventCtxBackgroundOnRunBefore(ctx context.Context, log zerolog.Logger, n int) {
	var last *zerolog.Event
	for range n {
		e := log.Info().Ctx(ctx)
		e.Ctx(context.Background())
		if last != nil {
			last.Msg("the run before") // LIMITATION: should report but doesn't
		}
		last = e
	}
}

// LIMITATION (false negative): A fresh context given to Ctx in a func literal
// called in place is not seen, so it does not replace the context the event
// had.
func limitationEventCtxBackgroundInFuncLiteralOverCtx(ctx context.Context, log zerolog.Logger) {
	e := log.Info().Ctx(ctx)
	func() { e.Ctx(context.Background()) }()
	e.Msg("Background in a func literal over ctx") // LIMITATION: should report but doesn't
}

// ===== FRESH CONTEXTS IN CLOSURES AND FUNC LITERALS =====

// A variable a closure captures holds what is stored to it before the
// closure runs, and a parameter of a func literal what its calls pass (#214).

func badEventCtxBackgroundCaptured(ctx context.Context, log zerolog.Logger) {
	bg := context.Background()
	func() {
		log.Info().Ctx(bg).Msg("Background captured") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

func badEventCtxBackgroundCapturedTwice(ctx context.Context, log zerolog.Logger) {
	bg := context.Background()
	func() {
		func() {
			log.Info().Ctx(bg).Msg("Background captured twice") // want `zerolog call chain missing .Ctx\(ctx\)`
		}()
	}()
}

func badEventCtxBackgroundCapturedByEscapingClosure(ctx context.Context, log zerolog.Logger) {
	bg := context.Background()
	freshContextRun(func() {
		log.Info().Ctx(bg).Msg("Background captured, run elsewhere") // want `zerolog call chain missing .Ctx\(ctx\)`
	})
}

func badEventCtxBackgroundThroughFuncLiteralParam(ctx context.Context, log zerolog.Logger) {
	bg := context.Background()
	f := func(p *context.Context) {
		log.Info().Ctx(*p).Msg("Background through a parameter") // want `zerolog call chain missing .Ctx\(\*p\)`
	}
	f(&bg)
}

type freshContextCounted struct {
	ctx context.Context
	n   int
}

func badEventCtxBackgroundThroughRecursiveFuncLiteral(ctx context.Context, log zerolog.Logger) {
	var f func(c freshContextCounted)
	f = func(c freshContextCounted) {
		log.Info().Ctx(c.ctx).Msg("Background passed on by a recursive call") // want `zerolog call chain missing .Ctx\(ctx\)`
		if c.n > 0 {
			c.n--
			f(c)
		}
	}
	f(freshContextCounted{ctx: context.Background(), n: 2})
}

func goodEventCtxCapturedUpdatedLater(ctx context.Context, log zerolog.Logger) {
	bg := context.Background()
	defer func() {
		log.Info().Ctx(bg).Msg("deferred, ctx stored before the return") // OK - ctx at the return
	}()
	go func() {
		log.Info().Ctx(bg).Msg("go, ctx stored after") // OK - may run after ctx is stored
	}()
	bg = ctx
}

func goodEventCtxThroughFuncLiteralParamUpdatedByClosure(ctx context.Context, log zerolog.Logger) {
	bg := context.Background()
	f := func(p *context.Context) {
		log.Info().Ctx(*p).Msg("updated by another closure") // OK - not known to be fresh
	}
	func() { bg = ctx }()
	f(&bg)
}

func goodEventCtxCapturedPointerUpdatedByClosure(ctx context.Context, log zerolog.Logger, pc *context.Context) {
	*pc = context.Background()
	func() { *pc = ctx }()
	func() {
		log.Info().Ctx(*pc).Msg("updated by another closure") // OK - not known to be fresh
	}()
}

func goodEventCtxCapturedUpdatedByNestedClosure(ctx context.Context, log zerolog.Logger) {
	bg := context.Background()
	func() {
		func() { bg = ctx }()
		log.Info().Ctx(bg).Msg("updated by a nested closure") // OK - not known to be fresh
	}()
}

// A store that a walk of the function does not see, by a closure or through
// a pointer passed on, may put another context in the variable.

func goodEventCtxBackgroundUpdatedByClosureBeforeRead(ctx context.Context, log zerolog.Logger) {
	bg := context.Background()
	func() { bg = ctx }()
	log.Info().Ctx(bg).Msg("updated by a closure before") // OK - not known to be fresh
}

func goodEventCtxBackgroundPassedOn(ctx context.Context, log zerolog.Logger) {
	bg := context.Background()
	freshContextSet(&bg, ctx)
	log.Info().Ctx(bg).Msg("passed on by pointer") // OK - not known to be fresh
	func() {
		log.Info().Ctx(bg).Msg("captured, passed on by pointer") // OK - not known to be fresh
	}()
}

// LIMITATION (false negative): A pointer to a field of another variable,
// captured by a closure, may point anywhere, so what it points to is not
// taken as fresh.
func limitationEventCtxBackgroundThroughCapturedFieldPointer(ctx context.Context, log zerolog.Logger) {
	h := freshContextHolder{ctx: context.Background()}
	pc := &h.ctx
	func() {
		log.Info().Ctx(*pc).Msg("captured pointer to a field") // LIMITATION: should report but doesn't
	}()
}

func freshContextRun(f func()) { f() }

func freshContextSet(p *context.Context, c context.Context) { *p = c }
