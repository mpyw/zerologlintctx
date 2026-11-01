// Package iife covers immediately invoked func literals, func literals called
// through a variable, their parameters, and helper functions.
package iife

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ===== IMMEDIATE INVOCATION OF CLOSURE =====

func evilImmediateClosureInvocation(ctx context.Context, logger zerolog.Logger) {
	func(l zerolog.Logger) {
		l.Info().Msg("immediate closure") // want `zerolog call chain missing .Ctx\(ctx\)`
	}(logger)
}

func evilImmediateClosureInvocationWithCtx(ctx context.Context, logger zerolog.Logger) {
	func(l zerolog.Logger, c context.Context) {
		l.Info().Ctx(c).Msg("immediate closure with ctx") // OK
	}(logger, ctx)
}

// ===== FUNCTION RETURNING LOGGER =====

func getLogger(base zerolog.Logger) zerolog.Logger {
	return base.With().Str("from", "getLogger").Logger()
}

func evilFunctionReturningLogger(ctx context.Context, logger zerolog.Logger) {
	derived := getLogger(logger)
	derived.Info().Msg("from function") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// Note: This is a known limitation - we can't trace across function boundaries
// unless the function itself adds .Ctx(ctx)

// ===== IIFE (Immediately Invoked Function Expression) =====

func badIIFEReturnsEvent(ctx context.Context, logger zerolog.Logger) {
	// IIFE returns event, then terminates
	func() *zerolog.Event {
		return logger.Info()
	}().Msg("iife") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badIIFEReturnsEventChained(ctx context.Context, logger zerolog.Logger) {
	// IIFE returns event with fields, then terminates
	func() *zerolog.Event {
		return logger.Info().Str("from", "iife")
	}().Str("key", "value").Msg("chained iife") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodIIFEReturnsEventWithCtx(ctx context.Context, logger zerolog.Logger) {
	// IIFE with ctx is now properly traced
	func() *zerolog.Event {
		return logger.Info().Ctx(ctx)
	}().Msg("iife with ctx") // OK - traced through IIFE return
}

func badIIFENestedReturnsEvent(ctx context.Context, logger zerolog.Logger) {
	// Nested IIFE without ctx
	func() *zerolog.Event {
		return func() *zerolog.Event {
			return logger.Info()
		}()
	}().Msg("nested iife") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodIIFENestedReturnsEventWithCtx(ctx context.Context, logger zerolog.Logger) {
	// Nested IIFE with ctx - traced recursively through both IIFEs
	func() *zerolog.Event {
		return func() *zerolog.Event {
			return logger.Info().Ctx(ctx)
		}()
	}().Msg("nested iife with ctx") // OK - traced through nested IIFE returns
}

func badIIFEMultipleReturnsPartialCtx(ctx context.Context, logger zerolog.Logger, cond bool) {
	// IIFE with multiple return paths - one without ctx
	func() *zerolog.Event {
		if cond {
			return logger.Info().Ctx(ctx)
		}
		return logger.Info() // no ctx in this branch
	}().Msg("multiple returns") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodIIFEMultipleReturnsAllCtx(ctx context.Context, logger zerolog.Logger, cond bool) {
	// IIFE with multiple return paths - all with ctx
	func() *zerolog.Event {
		if cond {
			return logger.Info().Ctx(ctx)
		}
		return logger.Debug().Ctx(ctx)
	}().Msg("multiple returns all ctx") // OK - all return paths have ctx
}

func goodIIFEMultipleResultsUsesSecond(ctx context.Context, logger zerolog.Logger) {
	_, e := func() (int, *zerolog.Event) {
		return 1, logger.Info().Ctx(ctx)
	}()
	e.Msg("second result has ctx") // OK - the extracted event result has ctx
}

func badIIFEMultipleResultsUsesSecond(ctx context.Context, logger zerolog.Logger) {
	_, e := func() (*zerolog.Event, *zerolog.Event) {
		return logger.Info().Ctx(ctx), logger.Info()
	}()
	e.Msg("second result has no ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A result of an interface type is traced through to the value it holds.

func goodIIFEResultAsInterface(ctx context.Context, logger zerolog.Logger) {
	v, _ := func() (any, bool) { return logger.Info().Ctx(ctx), true }()
	v.(*zerolog.Event).Msg("iife result as interface") // OK - the value it holds has ctx
}

func badIIFEResultAsInterface(ctx context.Context, logger zerolog.Logger) {
	v := func() any { return logger.Info() }()
	v.(*zerolog.Event).Msg("iife result as interface") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A func literal that captures nothing is called as the function itself, not
// through a closure (#112).

func goodIIFEWithoutCapturesReturnsEvent(ctx context.Context) {
	e := func(c context.Context) *zerolog.Event {
		return log.Ctx(c).Info()
	}(ctx)
	e.Msg("iife without captures") // OK - the IIFE returns an event with ctx
}

func goodFuncLiteralWithoutCapturesReturnsEvent(ctx context.Context) {
	mk := func(c context.Context) *zerolog.Event { return log.Info().Ctx(c) }
	mk(ctx).Msg("func literal without captures") // OK - mk returns an event with ctx
}

func goodIIFEWithoutCapturesReturnsLogger(ctx context.Context) {
	l := func(c context.Context) zerolog.Logger {
		return log.With().Ctx(c).Logger()
	}(ctx)
	l.Info().Msg("iife without captures returns logger") // OK - the IIFE returns a logger with ctx
}

func badIIFEWithoutCapturesReturnsEvent(ctx context.Context) {
	e := func() *zerolog.Event {
		return log.Info()
	}()
	e.Msg("iife without captures, no ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A variable that holds one of several func literals is a Phi of them, and
// each one's returns are traced (#140).

func goodFuncLiteralReplacedOnBranch(ctx context.Context, logger zerolog.Logger, debug bool) {
	ev := func() *zerolog.Event { return logger.Info().Ctx(ctx) }
	if debug {
		ev = func() *zerolog.Event { return logger.Debug().Ctx(ctx) }
	}
	ev().Msg("func literal replaced on a branch") // OK - both return an event with ctx
}

func goodFuncLiteralReplacedInLoop(ctx context.Context, logger zerolog.Logger, n int) {
	ev := func() *zerolog.Event { return logger.Info().Ctx(ctx) }
	for range n {
		ev().Msg("func literal replaced in a loop") // OK - both return an event with ctx
		ev = func() *zerolog.Event { return logger.Debug().Ctx(ctx) }
	}
}

func goodFuncLiteralReplacedOnTwoBranches(ctx context.Context, logger zerolog.Logger, a, b bool) {
	ev := func() *zerolog.Event { return logger.Info().Ctx(ctx) }
	if a {
		ev = func() *zerolog.Event { return logger.Warn().Ctx(ctx) }
	}
	logger.Info().Ctx(ctx).Msg("between the branches") // OK
	if b {
		ev = func() *zerolog.Event { return logger.Error().Ctx(ctx) }
	}
	ev().Msg("func literal replaced on two branches") // OK - all return an event with ctx
}

// A call of one of several func literals is a call of each, for their
// parameters, and gives the result it is extracted for (#154).

func goodFuncLiteralsOnBranchReturnParam(ctx context.Context, logger zerolog.Logger, cond bool) {
	f := func(e *zerolog.Event) *zerolog.Event { return e }
	if cond {
		f = func(e *zerolog.Event) *zerolog.Event { return e.Str("k", "v") }
	}
	f(logger.Info().Ctx(ctx)).Msg("func literals on a branch return their param") // OK - the argument has ctx
}

func badFuncLiteralsOnBranchReturnParam(ctx context.Context, logger zerolog.Logger, cond bool) {
	f := func(e *zerolog.Event) *zerolog.Event { return e }
	if cond {
		f = func(e *zerolog.Event) *zerolog.Event { return e.Str("k", "v") }
	}
	f(logger.Info()).Msg("func literals on a branch return their param") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFuncLiteralsOnBranchSecondResult(ctx context.Context, logger zerolog.Logger, cond bool) {
	f := func() (*zerolog.Event, *zerolog.Event) { return logger.Info().Ctx(ctx), logger.Info() }
	if cond {
		f = func() (*zerolog.Event, *zerolog.Event) { return logger.Info().Ctx(ctx), logger.Warn() }
	}
	_, e := f()
	e.Msg("func literals on a branch, second result") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodFuncLiteralsOnBranchSecondResult(ctx context.Context, logger zerolog.Logger, cond bool) {
	f := func() (*zerolog.Event, *zerolog.Event) { return logger.Info(), logger.Info().Ctx(ctx) }
	if cond {
		f = func() (*zerolog.Event, *zerolog.Event) { return logger.Warn(), logger.Warn().Ctx(ctx) }
	}
	_, e := f()
	e.Msg("func literals on a branch, second result") // OK - every second result has ctx
}

// A func literal in a variable that a closure captures is called through a
// load of the variable, and the func literals stored there are traced (#157).

func badCapturedFuncLiteralReturnsOther(ctx context.Context, logger zerolog.Logger) {
	f := func(p *zerolog.Event) *zerolog.Event { return logger.Warn() }
	func() { _ = f }()
	f(logger.Info().Ctx(ctx)).Msg("captured func literal returns another event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodCapturedFuncLiteral(ctx context.Context, logger zerolog.Logger) {
	f := func() *zerolog.Event { return logger.Info().Ctx(ctx) }
	func() { _ = f }()
	f().Msg("captured func literal") // OK - it returns an event with ctx
}

func goodFuncLiteralCalledInClosure(ctx context.Context, logger zerolog.Logger) {
	f := func() *zerolog.Event { return logger.Info().Ctx(ctx) }
	func() {
		f().Msg("func literal called in a closure") // OK - it returns an event with ctx
	}()
}

func goodCapturedFuncReplacedByValue(ctx context.Context, logger zerolog.Logger, other func(*zerolog.Event) *zerolog.Event) {
	f := func(e *zerolog.Event) *zerolog.Event { return e }
	func() { f = other }()
	f(logger.Info().Ctx(ctx)).Msg("captured func replaced by a value") // OK - a func not known is taken to keep its first argument
}

func badCapturedFuncAddressTaken(ctx context.Context, logger zerolog.Logger, keep func(*func() *zerolog.Event)) {
	f := func() *zerolog.Event { return logger.Info().Ctx(ctx) }
	keep(&f)
	f().Msg("captured func address taken") // want `zerolog call chain missing .Ctx\(ctx\)`
}

type funcHolder struct{ f *func() *zerolog.Event }

func badCapturedFuncAddressStored(ctx context.Context, logger zerolog.Logger, h *funcHolder) {
	f := func() *zerolog.Event { return logger.Info().Ctx(ctx) }
	func() { _ = f }()
	h.f = &f
	f().Msg("captured func address stored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

var globalEventFunc = func() *zerolog.Event { return log.Info() }

func badGlobalFuncVariable(ctx context.Context) {
	globalEventFunc().Msg("global func variable") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFuncFromChannel(ctx context.Context, ch chan func() *zerolog.Event) {
	(<-ch)().Msg("func from a channel") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodCapturedFuncReplacedOnBranchInLoop(ctx context.Context, logger zerolog.Logger, n int, cond bool) {
	f := func() *zerolog.Event { return logger.Info().Ctx(ctx) }
	for range n {
		if cond {
			f = func() *zerolog.Event { return logger.Warn().Ctx(ctx) }
		}
	}
	f().Msg("func replaced on a branch in a loop") // OK - both return an event with ctx
}

func badFuncLiteralReplacedOnBranch(ctx context.Context, logger zerolog.Logger, debug bool) {
	ev := func() *zerolog.Event { return logger.Info().Ctx(ctx) }
	if debug {
		ev = func() *zerolog.Event { return logger.Debug() }
	}
	ev().Msg("func literal replaced on a branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFuncValueOrLiteral(ctx context.Context, logger zerolog.Logger, debug bool, other func() *zerolog.Event) {
	ev := func() *zerolog.Event { return logger.Info().Ctx(ctx) }
	if debug {
		ev = other
	}
	ev().Msg("func value or literal") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== IIFE PARAMETERS =====

// A func literal that is only called where it is written takes the context
// of the arguments it is called with (#103).

func goodIIFEEventParam(ctx context.Context, logger zerolog.Logger) {
	func(e *zerolog.Event) {
		e.Msg("iife param") // OK - the argument has ctx
	}(logger.Info().Ctx(ctx))
}

func goodIIFELoggerParam(ctx context.Context, logger zerolog.Logger) {
	func(l zerolog.Logger) {
		l.Info().Msg("iife logger param") // OK - the argument has ctx
	}(logger.With().Ctx(ctx).Logger())
}

func goodIIFEParamWithCapture(ctx context.Context, logger zerolog.Logger) {
	key := "k"
	func(e *zerolog.Event) {
		e.Str(key, "v").Msg("iife param with capture") // OK - the argument has ctx
	}(logger.Info().Ctx(ctx))
}

func goodDeferredIIFEParam(ctx context.Context, logger zerolog.Logger) {
	defer func(e *zerolog.Event) {
		e.Msg("deferred iife param") // OK - the argument has ctx
	}(logger.Info().Ctx(ctx))
}

func goodFuncLiteralCalledTwice(ctx context.Context, logger zerolog.Logger) {
	send := func(e *zerolog.Event) {
		e.Msg("called twice") // OK - both arguments have ctx
	}
	send(logger.Info().Ctx(ctx))
	send(logger.Warn().Ctx(ctx))
}

// A func literal in a variable that a closure captures is kept in that
// variable, and called through it (#111).

func goodFuncLiteralCalledFromClosure(ctx context.Context, logger zerolog.Logger) {
	send := func(e *zerolog.Event) {
		e.Msg("called from closure") // OK - both arguments have ctx
	}
	send(logger.Info().Ctx(ctx))
	func() { send(logger.Warn().Ctx(ctx)) }()
}

func goodFuncLiteralCalledFromNestedClosures(ctx context.Context, logger zerolog.Logger) {
	send := func(e *zerolog.Event) {
		e.Msg("called from nested closures") // OK - the argument has ctx
	}
	go func() {
		defer func() { send(logger.Info().Ctx(ctx)) }()
	}()
}

// A pointer parameter, or a struct parameter, reads what the argument holds
// when the call runs (#126).

type iifeLoggerHolder struct{ l zerolog.Logger }

func goodIIFELoggerPointerParam(ctx context.Context, logger zerolog.Logger) {
	l := logger.With().Ctx(ctx).Logger()
	func(p *zerolog.Logger) {
		p.Info().Msg("iife logger pointer param") // OK - l has ctx
	}(&l)
}

func goodIIFELoggerPointerParamFromCtx(ctx context.Context) {
	func(p *zerolog.Logger) {
		p.Info().Msg("iife logger pointer param from ctx") // OK - zerolog.Ctx(ctx)
	}(zerolog.Ctx(ctx))
}

func goodIIFEStructPointerParam(ctx context.Context, logger zerolog.Logger) {
	h := iifeLoggerHolder{l: logger.With().Ctx(ctx).Logger()}
	func(h *iifeLoggerHolder) {
		h.l.Info().Msg("iife struct pointer param") // OK - h.l has ctx
	}(&h)
}

func goodIIFEStructParam(ctx context.Context, logger zerolog.Logger) {
	func(h iifeLoggerHolder) {
		h.l.Info().Msg("iife struct param") // OK - h.l has ctx
	}(iifeLoggerHolder{l: logger.With().Ctx(ctx).Logger()})
}

// A recursive call that passes the parameter on keeps what it holds (#131).

func goodRecursiveFuncLiteralPointerParam(ctx context.Context, logger zerolog.Logger, n int) {
	l := logger.With().Ctx(ctx).Logger()
	var walk func(p *zerolog.Logger, n int)
	walk = func(p *zerolog.Logger, n int) {
		if n > 0 {
			walk(p, n-1)
		}
		p.Info().Msg("recursive pointer param") // OK - every call passes on &l
	}
	walk(&l, n)
}

func goodRecursiveFuncLiteralStructParam(ctx context.Context, logger zerolog.Logger, n int) {
	var walk func(h iifeLoggerHolder, n int)
	walk = func(h iifeLoggerHolder, n int) {
		if n > 0 {
			walk(h, n-1)
		}
		h.l.Info().Msg("recursive struct param") // OK - every call passes on the holder
	}
	walk(iifeLoggerHolder{l: logger.With().Ctx(ctx).Logger()}, n)
}

// A pointer parameter traced as a value, here through a Phi, has what the
// calls pass for it.

func badIIFEPointerParamPicked(ctx context.Context, logger zerolog.Logger, cond bool) {
	l := logger
	other := logger.With().Ctx(ctx).Logger()
	func(p *zerolog.Logger) {
		q := p
		if cond {
			q = &other
		}
		q.Info().Msg("pointer param picked") // want `zerolog call chain missing .Ctx\(ctx\)`
	}(&l)
}

func goodIIFEPointerParamPicked(ctx context.Context, logger zerolog.Logger, cond bool) {
	l := logger.With().Ctx(ctx).Logger()
	other := logger.With().Ctx(ctx).Logger()
	func(p *zerolog.Logger) {
		q := p
		if cond {
			q = &other
		}
		q.Info().Msg("pointer param picked") // OK - both have ctx
	}(&l)
}

// A recursive func literal whose result is its own call's result returns
// what its other returns give.

func goodRecursiveFuncLiteralResult(ctx context.Context, logger zerolog.Logger, n int) {
	var f func(n int) *zerolog.Event
	f = func(n int) *zerolog.Event {
		if n > 0 {
			return f(n - 1)
		}
		return logger.Info().Ctx(ctx)
	}
	f(n).Msg("recursive result") // OK - the only other return has ctx
}

func badRecursiveFuncLiteralResult(ctx context.Context, logger zerolog.Logger, n int) {
	var f func(n int) *zerolog.Event
	f = func(n int) *zerolog.Event {
		if n > 0 {
			return f(n - 1)
		}
		return logger.Info()
	}
	f(n).Msg("recursive result") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A recursive call that passes an event parameter on, updated or not, keeps
// the event it had (#171).

func goodRecursiveFuncLiteralEventParam(ctx context.Context, logger zerolog.Logger, n int) {
	var walk func(e *zerolog.Event, n int)
	walk = func(e *zerolog.Event, n int) {
		if n > 0 {
			walk(e.Int("n", n), n-1)
			return
		}
		e.Msg("recursive event param") // OK - the outer call passes ctx
	}
	walk(logger.Info().Ctx(ctx), n)
}

func badRecursiveFuncLiteralEventParam(ctx context.Context, logger zerolog.Logger, n int) {
	var walk func(e *zerolog.Event, n int)
	walk = func(e *zerolog.Event, n int) {
		if n > 0 {
			walk(e.Int("n", n), n-1)
			return
		}
		e.Msg("recursive event param") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	walk(logger.Info(), n)
}

func badRecursiveFuncLiteralEventParamOnBranch(ctx context.Context, logger zerolog.Logger, n int, cond bool) {
	var walk func(e *zerolog.Event, n int)
	walk = func(e *zerolog.Event, n int) {
		if n > 0 {
			next := e
			if cond {
				next = logger.Warn()
			}
			walk(next, n-1)
			return
		}
		e.Msg("recursive event param on a branch") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	walk(logger.Info().Ctx(ctx), n)
}

func badRecursiveFuncLiteralEventParamReplaced(ctx context.Context, logger zerolog.Logger, n int) {
	var walk func(e *zerolog.Event, n int)
	walk = func(e *zerolog.Event, n int) {
		if n > 0 {
			walk(logger.Warn(), n-1)
			return
		}
		e.Msg("recursive event param replaced") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	walk(logger.Info().Ctx(ctx), n)
}

func badRecursiveFuncLiteralPointerParam(ctx context.Context, logger zerolog.Logger, n int) {
	l := logger
	var walk func(p *zerolog.Logger, n int)
	walk = func(p *zerolog.Logger, n int) {
		if n > 0 {
			walk(p, n-1)
		}
		p.Info().Msg("recursive pointer param") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	walk(&l, n)
}

func badRecursiveFuncLiteralPointerParamOverwritten(ctx context.Context, logger zerolog.Logger, n int) {
	l := logger.With().Ctx(ctx).Logger()
	other := logger
	var walk func(p *zerolog.Logger, n int)
	walk = func(p *zerolog.Logger, n int) {
		p.Info().Msg("recursive pointer param overwritten") // want `zerolog call chain missing .Ctx\(ctx\)`
		if n > 0 {
			walk(&other, n-1)
		}
	}
	walk(&l, n)
}

// A deferred func literal reads what a pointer argument points to at the
// returns after the defer, and a go one at any point after (#155).

func badDeferredFuncLiteralPointerParamChangedAfter(ctx context.Context, logger zerolog.Logger) {
	l := logger.With().Ctx(ctx).Logger()
	defer func(p *zerolog.Logger) {
		p.Info().Msg("deferred, pointer changed after") // want `zerolog call chain missing .Ctx\(ctx\)`
	}(&l)
	l = logger
}

func goodDeferredFuncLiteralPointerParamSetAfter(ctx context.Context, logger zerolog.Logger) {
	l := logger
	defer func(p *zerolog.Logger) {
		p.Info().Msg("deferred, pointer set after") // OK - l has ctx at the return
	}(&l)
	l = logger.With().Ctx(ctx).Logger()
}

func badDeferredFuncLiteralFieldChangedAfter(ctx context.Context, logger zerolog.Logger) {
	h := iifeLoggerHolder{l: logger.With().Ctx(ctx).Logger()}
	defer func(h *iifeLoggerHolder) {
		h.l.Info().Msg("deferred, field changed after") // want `zerolog call chain missing .Ctx\(ctx\)`
	}(&h)
	h.l = logger
}

func badGoFuncLiteralPointerParamChangedAfter(ctx context.Context, logger zerolog.Logger) {
	l := logger.With().Ctx(ctx).Logger()
	go func(p *zerolog.Logger) {
		p.Info().Msg("go, pointer changed after") // want `zerolog call chain missing .Ctx\(ctx\)`
	}(&l)
	l = logger
}

func goodGoFuncLiteralPointerParam(ctx context.Context, logger zerolog.Logger) {
	l := logger.With().Ctx(ctx).Logger()
	go func(p *zerolog.Logger) {
		p.Info().Msg("go, pointer") // OK - l has ctx, before and after
	}(&l)
}

func badDeferredFuncLiteralPointerParamInLoop(ctx context.Context, logger zerolog.Logger) {
	for i := 0; ; i++ {
		l := logger
		if i == 0 {
			defer func(p *zerolog.Logger) {
				p.Info().Msg("deferred in a loop, ctx on a later iteration") // want `zerolog call chain missing .Ctx\(ctx\)`
			}(&l)
			continue
		}
		l = logger.With().Ctx(ctx).Logger()
		return
	}
}

func badIIFELoggerPointerParam(ctx context.Context, logger zerolog.Logger) {
	l := logger
	func(p *zerolog.Logger) {
		p.Info().Msg("iife logger pointer param") // want `zerolog call chain missing .Ctx\(ctx\)`
	}(&l)
}

func badIIFEStructParam(ctx context.Context, logger zerolog.Logger) {
	func(h iifeLoggerHolder) {
		h.l.Info().Msg("iife struct param") // want `zerolog call chain missing .Ctx\(ctx\)`
	}(iifeLoggerHolder{l: logger})
}

func badIIFEStructParamNotLoaded(ctx context.Context, logger zerolog.Logger, h iifeLoggerHolder) {
	func(h iifeLoggerHolder) {
		h.l.Info().Msg("iife struct param not loaded") // want `zerolog call chain missing .Ctx\(ctx\)`
	}(h)
}

func badIIFEStructPointerParamFromCall(ctx context.Context, newHolder func() *iifeLoggerHolder) {
	func(h *iifeLoggerHolder) {
		h.l.Info().Msg("iife struct pointer param from call") // want `zerolog call chain missing .Ctx\(ctx\)`
	}(newHolder())
}

func badIIFELoggerPointerParamOverwritten(ctx context.Context, logger zerolog.Logger) {
	l := logger.With().Ctx(ctx).Logger()
	func(p *zerolog.Logger) {
		*p = logger
		p.Info().Msg("iife logger pointer param overwritten") // want `zerolog call chain missing .Ctx\(ctx\)`
	}(&l)
}

func badFuncLiteralPointerParamPassedOn(ctx context.Context, logger zerolog.Logger, run func(func(*zerolog.Logger))) {
	f := func(p *zerolog.Logger) {
		p.Info().Msg("pointer param passed on") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	l := logger.With().Ctx(ctx).Logger()
	f(&l)
	run(f)
}

// The result of a call of a func literal that returns its parameter is what
// that call passes (#174).

func badFuncLiteralReturnsParamPerCall(ctx context.Context, logger zerolog.Logger) {
	id := func(e *zerolog.Event) *zerolog.Event { return e.Str("k", "v") }
	a := id(logger.Info().Ctx(ctx))
	b := id(logger.Info())
	a.Msg("result of the call with ctx")    // OK - this call passes ctx
	b.Msg("result of the call without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

type iifeEventHolder struct{ e *zerolog.Event }

func badFuncLiteralReturnsParamInStructPerCall(ctx context.Context, logger zerolog.Logger) {
	id := func(e *zerolog.Event) *zerolog.Event {
		h := iifeEventHolder{e}
		return h.e
	}
	a := id(logger.Info().Ctx(ctx))
	b := id(logger.Info())
	a.Msg("through a struct, with ctx")    // OK - this call passes ctx
	b.Msg("through a struct, without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFuncLiteralReturnsParamInStructPointerPerCall(ctx context.Context, logger zerolog.Logger) {
	id := func(e *zerolog.Event) *zerolog.Event {
		h := &iifeEventHolder{e}
		return h.e.Str("k", "v")
	}
	a := id(logger.Info().Ctx(ctx))
	b := id(logger.Info())
	a.Msg("through a struct pointer, with ctx")    // OK - this call passes ctx
	b.Msg("through a struct pointer, without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFuncLiteralReturnsParamThroughTwoStructPointersPerCall(ctx context.Context, logger zerolog.Logger) {
	id := func(e *zerolog.Event) *zerolog.Event {
		h := &iifeEventHolder{e}
		h2 := &iifeEventHolder{h.e}
		return h2.e
	}
	a := id(logger.Info().Ctx(ctx))
	b := id(logger.Info())
	a.Msg("through two struct pointers, with ctx")    // OK - this call passes ctx
	b.Msg("through two struct pointers, without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFuncLiteralReturnsParamThroughTwoStructsPerCall(ctx context.Context, logger zerolog.Logger) {
	id := func(e *zerolog.Event) *zerolog.Event {
		h := iifeEventHolder{e}
		h2 := iifeEventHolder{h.e.Str("k", "v")}
		return h2.e
	}
	a := id(logger.Info().Ctx(ctx))
	b := id(logger.Info())
	a.Msg("through two structs, with ctx")    // OK - this call passes ctx
	b.Msg("through two structs, without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFuncLiteralReturnsParamStoredBackPerCall(ctx context.Context, logger zerolog.Logger, n int) {
	id := func(e *zerolog.Event) *zerolog.Event {
		h := &iifeEventHolder{e}
		for range n {
			h.e = h.e.Str("k", "v")
		}
		return h.e
	}
	a := id(logger.Info().Ctx(ctx))
	b := id(logger.Info())
	a.Msg("stored back in a loop, with ctx")    // OK - this call passes ctx
	b.Msg("stored back in a loop, without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFuncLiteralReturnsGlobalThroughStructPerCall(ctx context.Context, logger zerolog.Logger) {
	id := func(e *zerolog.Event) *zerolog.Event {
		h := &iifeEventHolder{e}
		if e == nil {
			h = &iifeEventHolder{iifeGlobalHolder.e}
		}
		return h.e
	}
	a := id(logger.Info().Ctx(ctx))
	a.Msg("a global on the other branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFuncLiteralReturnsCapturedFieldCopiedFromAnother(ctx context.Context, logger zerolog.Logger) {
	w := iifeEventHolder{logger.Info()}
	h := &iifeEventHolder{w.e}
	get := func(e *zerolog.Event) *zerolog.Event {
		_ = e
		return h.e
	}
	get(logger.Info().Ctx(ctx)).Msg("a captured field, not the argument") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFuncLiteralReturnsFieldOfPointerParamPerCall(ctx context.Context, logger zerolog.Logger) {
	get := func(h *iifeEventHolder) *zerolog.Event { return h.e }
	withCtx := iifeEventHolder{logger.Info().Ctx(ctx)}
	without := iifeEventHolder{logger.Info()}
	a := get(&withCtx)
	b := get(&without)
	a.Msg("field of a pointer param, with ctx")    // OK - this call passes ctx
	b.Msg("field of a pointer param, without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFuncLiteralReturnsFieldOfStructParamPerCall(ctx context.Context, logger zerolog.Logger) {
	get := func(h iifeEventHolder) *zerolog.Event { return h.e }
	a := get(iifeEventHolder{logger.Info().Ctx(ctx)})
	b := get(iifeEventHolder{logger.Info()})
	a.Msg("field of a struct param, with ctx")    // OK - this call passes ctx
	b.Msg("field of a struct param, without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFuncLiteralReturnsFieldOfLoadedParamPerCall(ctx context.Context, logger zerolog.Logger) {
	get := func(p *iifeEventHolder) *zerolog.Event {
		h := *p
		return h.e
	}
	withCtx := iifeEventHolder{logger.Info().Ctx(ctx)}
	without := iifeEventHolder{logger.Info()}
	a := get(&withCtx)
	b := get(&without)
	a.Msg("field of a struct loaded from a param, with ctx")    // OK - this call passes ctx
	b.Msg("field of a struct loaded from a param, without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

var iifeGlobalHolder iifeEventHolder

func badFuncLiteralReturnsGlobalField(ctx context.Context, logger zerolog.Logger) {
	get := func(e *zerolog.Event) *zerolog.Event { return iifeGlobalHolder.e }
	get(logger.Info().Ctx(ctx)).Msg("field of a global") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFuncLiteralReturnsFieldOfGlobalCopy(ctx context.Context, logger zerolog.Logger) {
	get := func(e *zerolog.Event) *zerolog.Event {
		h := iifeGlobalHolder
		return h.e
	}
	get(logger.Info().Ctx(ctx)).Msg("field of a copy of a global") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFuncLiteralReturnsStoredParamOnOnePath(ctx context.Context, logger zerolog.Logger, cond bool) {
	id := func(e *zerolog.Event) *zerolog.Event {
		h := iifeEventHolder{e}
		if cond {
			h.e = logger.Warn()
		}
		return h.e
	}
	a := id(logger.Info().Ctx(ctx))
	a.Msg("another event on one path") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodFuncLiteralReturnsParamWithCtx(ctx context.Context, logger zerolog.Logger) {
	set := func(e *zerolog.Event) *zerolog.Event { return e.Ctx(ctx) }
	set(logger.Info()).Msg("returns its param with ctx") // OK - the func literal sets ctx
}

func badIIFEEventParam(ctx context.Context, logger zerolog.Logger) {
	func(e *zerolog.Event) {
		e.Msg("iife param") // want `zerolog call chain missing .Ctx\(ctx\)`
	}(logger.Info())
}

func badFuncLiteralOneCallWithoutCtx(ctx context.Context, logger zerolog.Logger) {
	send := func(e *zerolog.Event) {
		e.Msg("one call without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	send(logger.Info().Ctx(ctx))
	send(logger.Warn())
}

// A func literal that is passed on may be called with anything.
func badFuncLiteralPassedOn(ctx context.Context, logger zerolog.Logger, run func(func(*zerolog.Event))) {
	send := func(e *zerolog.Event) {
		e.Msg("passed on") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	send(logger.Info().Ctx(ctx))
	run(send)
}

func badClosurePassedOn(ctx context.Context, logger zerolog.Logger, run func(func(*zerolog.Event))) {
	key := "k"
	send := func(e *zerolog.Event) {
		e.Str(key, "v").Msg("closure passed on") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	send(logger.Info().Ctx(ctx))
	run(send)
}

func badFuncLiteralCalledFromClosureWithoutCtx(ctx context.Context, logger zerolog.Logger) {
	send := func(e *zerolog.Event) {
		e.Msg("closure call without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	send(logger.Info().Ctx(ctx))
	func() { send(logger.Warn()) }()
}

func badFuncLiteralPassedOnFromClosure(ctx context.Context, logger zerolog.Logger, run func(func(*zerolog.Event))) {
	send := func(e *zerolog.Event) {
		e.Msg("passed on from closure") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	send(logger.Info().Ctx(ctx))
	func() { run(send) }()
}

// A call that may run another func too is still a call of the func literal.
func goodFuncLiteralReplacedInClosure(ctx context.Context, logger zerolog.Logger, other func(*zerolog.Event)) {
	send := func(e *zerolog.Event) {
		e.Msg("replaced in closure") // OK - every call that may run it passes ctx
	}
	send(logger.Info().Ctx(ctx))
	func() { send = other }()
	send(logger.Warn().Ctx(ctx))
}

func badFuncLiteralReplacedInClosure(ctx context.Context, logger zerolog.Logger, other func(*zerolog.Event)) {
	send := func(e *zerolog.Event) {
		e.Msg("replaced in closure") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	send(logger.Info().Ctx(ctx))
	func() { send = other }()
	send(logger.Warn())
}

func badFuncLiteralVariableAddressTaken(ctx context.Context, logger zerolog.Logger, keep func(*func(*zerolog.Event))) {
	send := func(e *zerolog.Event) {
		e.Msg("variable address taken") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	keep(&send)
	send(logger.Info().Ctx(ctx))
}

func badFuncLiteralReturned(ctx context.Context, logger zerolog.Logger) func(*zerolog.Event) {
	send := func(e *zerolog.Event) {
		e.Msg("returned") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	send(logger.Info().Ctx(ctx))
	return send
}

func badFuncLiteralNeverCalled(ctx context.Context, logger zerolog.Logger) {
	send := func(e *zerolog.Event) {
		e.Msg("never called") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	_ = send
}

func badDeclaredFuncParam(ctx context.Context, e *zerolog.Event) {
	e.Msg("declared func param") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== HELPER FUNCTIONS RETURNING EVENT =====

func badHelperReturnsEvent(ctx context.Context, logger zerolog.Logger) {
	e := createEvent(logger)
	e.Msg("from helper") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badHelperChainedImmediately(ctx context.Context, logger zerolog.Logger) {
	createEvent(logger).Str("key", "val").Msg("immediate chain") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// LIMITATION (false positive): An event or a Logger that a declared function
// returns is not traced into the function.
func limitationHelperWithCtx(ctx context.Context, logger zerolog.Logger) {
	e := createEventWithCtx(ctx, logger)
	e.Msg("helper with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func limitationHelperLoggerWithCtx(ctx context.Context, logger zerolog.Logger) {
	l := createLoggerWithCtx(ctx, logger)
	l.Info().Msg("Logger from a helper with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func createLoggerWithCtx(ctx context.Context, logger zerolog.Logger) zerolog.Logger {
	return logger.With().Ctx(ctx).Logger()
}

func createEvent(logger zerolog.Logger) *zerolog.Event {
	return logger.Info().Str("created", "helper")
}

func createEventWithCtx(ctx context.Context, logger zerolog.Logger) *zerolog.Event {
	return logger.Info().Ctx(ctx).Str("created", "helper")
}

// ===== MULTIPLE FUNCTION HOPS =====

func badMultipleHops(ctx context.Context, logger zerolog.Logger) {
	e := hop1(logger)
	e.Msg("multi hop") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func hop1(logger zerolog.Logger) *zerolog.Event {
	return hop2(logger)
}

func hop2(logger zerolog.Logger) *zerolog.Event {
	return hop3(logger)
}

func hop3(logger zerolog.Logger) *zerolog.Event {
	return logger.Info().Str("hop", "3")
}

// ===== VARIADIC HELPER (Returns Event) =====

func variadicHelper(events ...*zerolog.Event) *zerolog.Event {
	if len(events) > 0 {
		return events[0]
	}
	return nil
}

// Cross-function tracking: Analyzer sees through function calls where the terminator is outside.
// This works because SSA tracks the Event through Extract instruction.
func badVariadicHelper(ctx context.Context, logger zerolog.Logger) {
	e := variadicHelper(logger.Info())
	if e != nil {
		e.Msg("from variadic") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

// ===== IIFE WITH AN UNREACHABLE RETURN =====

// LIMITATION (false positive): IIFE with unreachable return
// SSA doesn't always eliminate unreachable code, so all return paths are checked.
// Even though the second return is unreachable, analyzer still reports.
func limitationIIFEUnreachableReturn(ctx context.Context, logger zerolog.Logger) {
	func() *zerolog.Event {
		if true {
			return logger.Info().Ctx(ctx)
		}
		return logger.Info() // unreachable, but SSA doesn't eliminate it
	}().Msg("iife unreachable") // want `zerolog call chain missing .Ctx\(ctx\)`
}
