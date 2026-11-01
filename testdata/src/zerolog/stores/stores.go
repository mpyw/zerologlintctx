// Package stores covers which stores to a variable in memory reach a read:
// self-referential stores, overwritten stores, and values passed through a
// helper in a loop.
package stores

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ===== SELF-REFERENTIAL STORE THAT SETS CTX =====

// A store whose value is built from the address it is stored to, as
// l = l.With().Ctx(ctx).Logger() is once l has its address taken, sets the
// context itself. Real zerolog takes the address for every Logger.Info call.
func goodSelfReferentialStoreSetsCtx(ctx context.Context, logger zerolog.Logger) {
	l := logger
	p := &l
	*p = p.With().Ctx(ctx).Logger()
	p.Info().Msg("self-referential store with ctx") // OK - ctx set via With().Ctx(ctx)
}

func goodSelfReferentialEventStoreSetsCtx(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	p := &e
	*p = (*p).Ctx(ctx)
	(*p).Msg("self-referential event store with ctx") // OK - ctx set via .Ctx(ctx)
}

func badSelfReferentialStoreWithoutCtx(ctx context.Context, logger zerolog.Logger) {
	l := logger
	p := &l
	*p = p.With().Str("k", "v").Logger()
	p.Info().Msg("self-referential store without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// The store that sets ctx runs only on one path, so the read is reported.
func badSelfReferentialStoreInBranch(ctx context.Context, logger zerolog.Logger, cond bool) {
	l := logger
	p := &l
	if cond {
		*p = p.With().Ctx(ctx).Logger()
	}
	p.Info().Msg("self-referential store in branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A plain store after the one that sets ctx replaces the value.
func badPlainStoreAfterSelfReferentialStore(ctx context.Context, logger zerolog.Logger) {
	l := logger
	p := &l
	*p = p.With().Ctx(ctx).Logger()
	*p = logger
	p.Info().Msg("overwritten after ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// Updates on either side of the one that sets ctx keep it.
func goodSelfReferentialStoresAroundCtx(ctx context.Context, logger zerolog.Logger) {
	l := logger
	p := &l
	*p = p.With().Str("a", "1").Logger()
	*p = p.With().Ctx(ctx).Logger()
	*p = p.With().Str("b", "2").Logger()
	p.Info().Msg("updates around ctx") // OK - ctx set via With().Ctx(ctx)
}

// Only the stores that reach a read count (#102). An update on every path
// is enough, and a store that a later one overwrites does not reach the read.
func goodUpdateOnBothBranches(ctx context.Context, logger zerolog.Logger, cond bool) {
	l := logger
	if cond {
		l = l.With().Ctx(ctx).Logger()
	} else {
		l = l.With().Ctx(ctx).Str("a", "b").Logger()
	}
	l.Info().Msg("updated on both branches") // OK - ctx set on every path
}

func goodUpdateOnEveryCase(ctx context.Context, logger zerolog.Logger, n int) {
	l := logger
	switch n {
	case 1:
		l = l.With().Ctx(ctx).Logger()
	case 2:
		l = logger.With().Ctx(ctx).Logger()
	default:
		l = l.With().Str("k", "v").Ctx(ctx).Logger()
	}
	l.Info().Msg("updated on every case") // OK - ctx set on every path
}

func goodOverwrittenStore(ctx context.Context, logger zerolog.Logger) {
	l := logger
	l = logger.With().Ctx(ctx).Logger()
	l.Info().Msg("first store overwritten") // OK - only the second store reaches
}

func goodUpdateInLoopKeepsCtx(ctx context.Context, logger zerolog.Logger, xs []int) {
	l := logger.With().Ctx(ctx).Logger()
	for range xs {
		l = l.With().Int("i", 1).Logger()
		l.Info().Msg("in loop") // OK - ctx set before the loop
	}
	l.Info().Msg("after loop") // OK - ctx set before the loop
}

// A helper may return any value, so a loop that passes the value through one
// does not keep the context it had before (#114).

func badEventThroughHelperInLoop(ctx context.Context, logger zerolog.Logger, xs []int) {
	e := logger.Info().Ctx(ctx)
	for range xs {
		e = replaceEvent(e)
	}
	e.Msg("through helper in loop") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badLoggerThroughHelperInLoop(ctx context.Context, logger zerolog.Logger, xs []int) {
	l := logger.With().Ctx(ctx).Logger()
	for range xs {
		l = replaceLogger(l)
	}
	l.Info().Msg("logger through helper in loop") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventThroughMethodValueInLoop(ctx context.Context, logger zerolog.Logger, xs []int) {
	e := logger.Info().Ctx(ctx)
	for range xs {
		str := e.Str
		e = str("k", "v")
	}
	e.Msg("through method value in loop") // OK - the method keeps the event
}

func goodEventThroughFuncValueInLoop(ctx context.Context, logger zerolog.Logger, xs []int, wrap func(*zerolog.Event) *zerolog.Event) {
	e := logger.Info().Ctx(ctx)
	for range xs {
		e = wrap(e)
	}
	e.Msg("through func value in loop") // OK - a call not known statically is taken to keep its first argument
}

func badEventReplacedByIIFEInLoop(ctx context.Context, logger zerolog.Logger, xs []int) {
	e := logger.Info().Ctx(ctx)
	for range xs {
		e = func() *zerolog.Event { return logger.Warn() }()
	}
	e.Msg("replaced by iife in loop") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func replaceEvent(*zerolog.Event) *zerolog.Event { return log.Info() }

func replaceLogger(zerolog.Logger) zerolog.Logger { return zerolog.Nop() }

// ===== STORES THAT REACH A READ =====

func goodStoreAfterClosureIsMade(ctx context.Context, logger zerolog.Logger) {
	var l zerolog.Logger
	f := func() { l.Info().Msg("store after closure") } // OK - the only store has ctx
	l = logger.With().Ctx(ctx).Logger()
	f()
}

func badUpdateOnOneCase(ctx context.Context, logger zerolog.Logger, n int) {
	l := logger
	switch n {
	case 1:
		l = l.With().Ctx(ctx).Logger()
	case 2:
		l = l.With().Str("a", "b").Logger()
	}
	l.Info().Msg("not every case") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badReadBeforeUpdateInLoop(ctx context.Context, logger zerolog.Logger, xs []int) {
	l := logger
	for range xs {
		l.Info().Msg("first iteration has none") // want `zerolog call chain missing .Ctx\(ctx\)`
		l = l.With().Ctx(ctx).Logger()
	}
}

func badPointerParamUpdatedInBranch(ctx context.Context, l *zerolog.Logger, cond bool) {
	if cond {
		*l = l.With().Ctx(ctx).Logger()
	}
	l.Info().Msg("caller's value on the other path") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodPointerParamStoredWithCtx(ctx context.Context, l *zerolog.Logger, base zerolog.Logger) {
	*l = base.With().Ctx(ctx).Logger()
	l.Info().Msg("stored over the caller's value") // OK - the store reaches every read
}

// ===== STORES UNDER A BRANCH ON A CONSTANT =====

// A store in a branch that never runs does not reach a read, and a store in
// one that always runs covers the path past it (#266).

const (
	storesDebug   = true
	storesVerbose = false
)

func goodStoreUnderTrueConstant(ctx context.Context, logger zerolog.Logger) {
	l := logger
	p := &l
	if storesDebug {
		*p = logger.With().Ctx(ctx).Logger()
	}
	p.Info().Msg("stored under a true constant") // OK - the store always runs
}

func badStoreUnderFalseConstant(ctx context.Context, logger zerolog.Logger) {
	l := logger
	p := &l
	if storesVerbose {
		*p = logger.With().Ctx(ctx).Logger()
	}
	p.Info().Msg("stored under a false constant") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodStoreInClosureUnderTrueConstant(ctx context.Context, logger zerolog.Logger) {
	l := logger
	if storesDebug {
		l = logger.With().Ctx(ctx).Logger()
	}
	func() {
		l.Info().Msg("captured after a store under a true constant") // OK - the store always runs
	}()
}

func badStoreUnderCondition(ctx context.Context, logger zerolog.Logger, cond bool) {
	l := logger
	p := &l
	if cond {
		*p = logger.With().Ctx(ctx).Logger()
	}
	p.Info().Msg("stored under a condition") // want `zerolog call chain missing .Ctx\(ctx\)`
}
