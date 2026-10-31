// Package deferred covers deferred terminators and deferred closures, which run at
// the returns after the defer statement.
package deferred

import (
	"context"

	"github.com/rs/zerolog"
)

// ===== DEFERRED TERMINATORS AND Ctx AFTER THE DEFER =====

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

// ===== DEFERRED CLOSURES =====

func evilDeferredClosure(ctx context.Context, logger zerolog.Logger) {
	defer func() {
		logger.Info().Msg("deferred") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

func evilDeferredClosureWithCtx(ctx context.Context, logger zerolog.Logger) {
	defer func() {
		logger.Info().Ctx(ctx).Msg("deferred with ctx") // OK
	}()
}

func evilDeferredNested(ctx context.Context, logger zerolog.Logger) {
	defer func() {
		defer func() {
			logger.Info().Msg("nested defer") // want `zerolog call chain missing .Ctx\(ctx\)`
		}()
	}()
}

func evilDeferredNestedWithCtx(ctx context.Context, logger zerolog.Logger) {
	defer func() {
		defer func() {
			logger.Info().Ctx(ctx).Msg("nested defer with ctx") // OK
		}()
	}()
}

// ===== DEFERRED LOGGING =====

func badDeferredLog(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	defer e.Msg("deferred") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodDeferredLogWithCtx(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info().Ctx(ctx)
	defer e.Msg("deferred with ctx") // OK
}

// Test case: Deferred bound method call
// The bound method (method value) should also be checked for deferred calls
func badDeferredBoundMethod(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	msg := e.Msg
	defer msg("deferred bound method") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodDeferredBoundMethodWithCtx(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info().Ctx(ctx)
	msg := e.Msg
	defer msg("deferred bound method with ctx") // OK
}

// A chain run by a go statement is checked as a deferred one is (#78).
func badGoStatementLog(ctx context.Context, logger zerolog.Logger) {
	go logger.Info().Msg("go") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodGoStatementLogWithCtx(ctx context.Context, logger zerolog.Logger) {
	go logger.Info().Ctx(ctx).Msg("go with ctx") // OK
}

func badGoStatementBoundMethod(ctx context.Context, logger zerolog.Logger) {
	msg := logger.Info().Msg
	go msg("go bound method") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== PANIC RECOVERY =====

func badPanicRecoveryEvent(ctx context.Context, logger zerolog.Logger) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error().Interface("panic", r).Msg("recovered") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
	}()
	panic("test")
}

func goodPanicRecoveryWithCtx(ctx context.Context, logger zerolog.Logger) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error().Ctx(ctx).Interface("panic", r).Msg("recovered with ctx") // OK
		}
	}()
	panic("test")
}

func goodPanicRecoveryEvent(ctx context.Context, logger zerolog.Logger) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error().Ctx(ctx).Interface("panic", r).Msg("recovered with ctx") // OK
		}
	}()
	panic("test")
}

// ===== DEFER WITH CLOSURE =====

func badDeferWithClosure(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	defer func() {
		e.Msg("defer closure") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

// ===== DEFER WITH NAMED RETURN =====

func badDeferNamedReturn(ctx context.Context, logger zerolog.Logger) (err error) {
	defer func() {
		if err != nil {
			logger.Error().Err(err).Msg("failed") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
	}()
	return nil
}

func goodDeferNamedReturnWithCtx(ctx context.Context, logger zerolog.Logger) (err error) {
	defer func() {
		if err != nil {
			logger.Error().Ctx(ctx).Err(err).Msg("failed with ctx") // OK
		}
	}()
	return nil
}

// ===== DEFERRED CLOSURES AND THE STORES THAT REACH THEM =====

// A closure that is only deferred runs at the returns after the defer
// statement, so only the stores that reach them can be what it reads (#123).

func goodDeferredClosureCtxAfterDefer(ctx context.Context, logger zerolog.Logger) {
	l := logger
	defer func() {
		l.Info().Msg("deferred closure, ctx after defer") // OK - l has ctx at the return
	}()
	l = l.With().Ctx(ctx).Logger()
}

func goodDeferredClosureInBranch(ctx context.Context, logger zerolog.Logger, cond bool) {
	if cond {
		l := logger.With().Ctx(ctx).Logger()
		defer func() {
			l.Info().Msg("deferred closure in a branch") // OK - l has ctx wherever the closure runs (#135)
		}()
	}
}

func goodDeferredClosureInLoop(ctx context.Context, logger zerolog.Logger, n int) {
	for range n {
		l := logger.With().Ctx(ctx).Logger()
		defer func() {
			l.Info().Msg("deferred closure in a loop") // OK - each l has ctx (#135)
		}()
	}
}

// A variable declared in a loop is another one on each iteration, so a store
// on a later one is not to the variable a closure deferred earlier captures
// (#156).
func badDeferredClosureCtxOnLaterIteration(ctx context.Context, logger zerolog.Logger) {
	for i := 0; ; i++ {
		l := logger
		if i == 0 {
			defer func() {
				l.Info().Msg("deferred closure, ctx on a later iteration") // want `zerolog call chain missing .Ctx\(ctx\)`
			}()
			continue
		}
		l = logger.With().Ctx(ctx).Logger()
		return
	}
}

// On a path that skips a defer statement, nothing is deferred, and what is
// stored there does not count.

func goodDeferredClosureInBranchUpdatedAfter(ctx context.Context, logger zerolog.Logger, cond bool) {
	l := logger
	if cond {
		defer func() {
			l.Info().Msg("deferred in a branch, updated after") // OK - l has ctx at every return the defer reaches
		}()
		l = l.With().Ctx(ctx).Logger()
	}
}

func badDeferredClosureInBranchUpdatedOnOnePath(ctx context.Context, logger zerolog.Logger, a, b bool) {
	l := logger
	if a {
		defer func() {
			l.Info().Msg("deferred in a branch, updated on one path") // want `zerolog call chain missing .Ctx\(ctx\)`
		}()
		if b {
			l = l.With().Ctx(ctx).Logger()
		}
	}
}

func goodDeferredPointerArgInBranchUpdatedAfter(ctx context.Context, logger zerolog.Logger, cond bool) {
	l := logger
	if cond {
		defer func(p *zerolog.Logger) {
			p.Info().Msg("deferred pointer in a branch, updated after") // OK - l has ctx at every return the defer reaches
		}(&l)
		l = l.With().Ctx(ctx).Logger()
	}
}

func badDeferredClosureInBranch(ctx context.Context, logger zerolog.Logger, cond bool) {
	if cond {
		l := logger
		defer func() {
			l.Info().Msg("deferred closure in a branch, no ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
		}()
	}
}

func badDeferredClosureCtxBeforeOneReturn(ctx context.Context, logger zerolog.Logger, cond bool) {
	l := logger
	defer func() {
		l.Info().Msg("deferred closure, ctx before one return") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
	if cond {
		return
	}
	l = l.With().Ctx(ctx).Logger()
}

func badDeferredClosureCtxLost(ctx context.Context, logger zerolog.Logger) {
	l := logger.With().Ctx(ctx).Logger()
	defer func() {
		l.Info().Msg("deferred closure, ctx lost") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
	l = logger
}

func badClosureDeferredAndCalled(ctx context.Context, logger zerolog.Logger) {
	l := logger
	f := func() {
		l.Info().Msg("closure deferred and called") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	defer f()
	f()
	l = l.With().Ctx(ctx).Logger()
}
