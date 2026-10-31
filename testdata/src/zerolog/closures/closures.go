// Package closures covers closures, nested func literals and goroutines, and the
// variables they capture.
package closures

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ===== DEEP NESTING AND CLOSURES =====

func evilDeepNestedClosures(ctx context.Context, logger zerolog.Logger) {
	func() {
		func() {
			func() {
				logger.Info().Msg("deep nested") // want `zerolog call chain missing .Ctx\(ctx\)`
			}()
		}()
	}()
}

func evilDeepNestedClosuresWithCtx(ctx context.Context, logger zerolog.Logger) {
	func() {
		func() {
			func() {
				logger.Info().Ctx(ctx).Msg("deep nested with ctx") // OK
			}()
		}()
	}()
}

func evilClosureCapturingLogger(ctx context.Context, logger zerolog.Logger) {
	doSomething := func() {
		logger.Info().Msg("captured logger") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	doSomething()
}

func evilClosureCapturingDerivedLogger(ctx context.Context, logger zerolog.Logger) {
	derived := logger.With().Str("captured", "true").Logger()
	doSomething := func() {
		derived.Info().Msg("captured derived") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	doSomething()
}

func evilClosureCapturingDerivedLoggerWithCtx(ctx context.Context, logger zerolog.Logger) {
	derived := logger.With().Ctx(ctx).Logger()
	doSomething := func() {
		derived.Info().Msg("captured derived with ctx") // OK - ctx set via With().Ctx(ctx)
	}
	doSomething()
}

// ===== ADVANCED NESTED PATTERNS (SHADOWING, ARGUMENT PASSING) =====

// Shadowing - inner ctx shadows outer
func evilShadowingInnerHasCtx(outerCtx context.Context, logger zerolog.Logger) {
	innerFunc := func(ctx context.Context) {
		logger.Info().Ctx(ctx).Msg("uses inner ctx") // OK - uses inner ctx
	}
	innerFunc(outerCtx)
}

func evilShadowingInnerIgnoresCtx(outerCtx context.Context, logger zerolog.Logger) {
	innerFunc := func(ctx context.Context) {
		logger.Info().Msg("ignores inner ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	innerFunc(outerCtx)
}

// Two levels of shadowing
func evilShadowingTwoLevels(ctx1 context.Context, logger zerolog.Logger) {
	func(ctx2 context.Context) {
		func(ctx3 context.Context) {
			logger.Info().Ctx(ctx3).Msg("uses ctx3") // OK
		}(ctx2)
	}(ctx1)
}

func evilShadowingTwoLevelsBad(ctx1 context.Context, logger zerolog.Logger) {
	func(ctx2 context.Context) {
		func(ctx3 context.Context) {
			logger.Info().Msg("ignores ctx3") // want `zerolog call chain missing .Ctx\(ctx3\)`
		}(ctx2)
	}(ctx1)
}

// ===== METHOD WITH CLOSURE ON STRUCT =====

type LoggerHolder struct {
	Log zerolog.Logger
}

func (h *LoggerHolder) MethodWithClosureBad(ctx context.Context) {
	func() {
		h.Log.Info().Msg("method closure") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

func (h *LoggerHolder) MethodWithClosureGood(ctx context.Context) {
	func() {
		h.Log.Info().Ctx(ctx).Msg("method closure with ctx") // OK
	}()
}

// ===== CLOSURES IN PACKAGE-LEVEL VARIABLES =====

// A closure in a package-level variable belongs to the package's init, and is
// checked as one inside a function is (#81).
var evilGlobalClosure = func(ctx context.Context) {
	log.Info().Msg("global closure") // want `zerolog call chain missing .Ctx\(ctx\)`
}

var evilGlobalClosureWithCtx = func(ctx context.Context) {
	log.Info().Ctx(ctx).Msg("global closure with ctx") // OK
}

var evilGlobalClosureWithoutCtx = func() {
	log.Info().Msg("global closure without ctx") // OK - no context in scope
}

var evilGlobalNestedClosure = func(ctx context.Context) func() {
	return func() {
		log.Info().Msg("nested in global closure") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

var evilGlobalClosures = map[string]func(context.Context){
	"a": func(ctx context.Context) {
		log.Info().Msg("closure in global map") // want `zerolog call chain missing .Ctx\(ctx\)`
	},
}

// ===== CLOSURE CAPTURING AND DEFERRED USE =====

func badClosureCapture(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	fn := func() {
		e.Msg("captured") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	fn()
}

func badClosureCaptureDeep(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	fn := func() {
		inner := func() {
			e.Msg("deep capture") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		inner()
	}
	fn()
}

func goodClosureCaptureWithCtx(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info().Ctx(ctx)
	fn := func() {
		e.Msg("captured with ctx") // OK - ctx set via .Ctx(ctx)
	}
	fn()
}

// ===== TRIPLE NESTED CLOSURES =====

func badTripleNestedClosure(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	func() {
		func() {
			func() {
				e.Msg("triple nested") // want `zerolog call chain missing .Ctx\(ctx\)`
			}()
		}()
	}()
}

func goodTripleNestedClosureWithCtx(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info().Ctx(ctx)
	func() {
		func() {
			func() {
				e.Msg("triple nested with ctx") // OK - ctx set via .Ctx(ctx)
			}()
		}()
	}()
}

// ===== CLOSURE THAT MODIFIES CAPTURED VAR =====

func badClosureModifiesCaptured(ctx context.Context, logger zerolog.Logger) {
	var e *zerolog.Event
	f := func() {
		e = logger.Info()
	}
	f()
	e.Msg("modified by closure") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// LIMITATION (false positive): Closure-modified captured variable.
// Can't trace through closure that writes to captured var even though ctx is present.
func limitationClosureModifiesCapturedWithCtx(ctx context.Context, logger zerolog.Logger) {
	var e *zerolog.Event
	f := func() {
		e = logger.Info().Ctx(ctx)
	}
	f()
	e.Msg("modified by closure with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== FUNCTION LITERAL AS ARGUMENT =====

func badFuncLiteralArg(ctx context.Context, logger zerolog.Logger) {
	doSomething(func() {
		logger.Info().Msg("in func literal arg") // want `zerolog call chain missing .Ctx\(ctx\)`
	})
}

func goodFuncLiteralArgWithCtx(ctx context.Context, logger zerolog.Logger) {
	doSomething(func() {
		logger.Info().Ctx(ctx).Msg("in func literal arg with ctx") // OK
	})
}

func doSomething(f func()) {
	f()
}

// ===== CLOSURE IN LOOP WITH VARYING CTX =====

// Test case: Closure created in loop with varying ctx
// Each iteration should be checked independently
func badClosureInLoopVaryingCtx(ctx context.Context, logger zerolog.Logger) {
	for i := 0; i < 2; i++ {
		e := logger.Info()
		if i == 1 {
			e = e.Ctx(ctx) // only second iteration has ctx
		}
		f := func() {
			e.Msg("in loop closure") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		f()
	}
}

// Test case: Multiple MakeClosure for same closure function
// If created in different branches with different bindings, all must have ctx
func badMultipleMakeClosureBranches(ctx context.Context, logger zerolog.Logger, cond bool) {
	var f func()
	if cond {
		e := logger.Info().Ctx(ctx)
		f = func() { e.Msg("branch 1") }
	} else {
		e := logger.Info() // no ctx
		f = func() { e.Msg("branch 2") } // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	f()
}

// ===== CLOSURES AND THE STORES THAT REACH THEM =====

// A closure reads what its captured variable can hold when it runs, not
// what the variable held at a read before the closure was made (#136).

func goodDeferredClosureAfterEarlierRead(ctx context.Context, logger zerolog.Logger) {
	l := logger.With().Str("req", "x").Logger()
	l.Debug().Ctx(ctx).Msg("start")
	l = l.With().Ctx(ctx).Logger()
	defer func() {
		l.Info().Msg("deferred after an earlier read") // OK - l has ctx when the closure runs
	}()
}

func goodIIFEAfterEarlierRead(ctx context.Context, logger zerolog.Logger) {
	l := logger
	l.Debug().Ctx(ctx).Msg("start")
	l = l.With().Ctx(ctx).Logger()
	func() {
		l.Info().Msg("iife after an earlier read") // OK - l has ctx when the closure runs
	}()
}

func goodGoClosureEventAfterEarlierUse(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	_ = e.Str("k", "v")
	e = logger.Info().Ctx(ctx)
	go func() {
		e.Msg("go closure after an earlier use") // OK - e has ctx when the closure runs
	}()
}

func goodLoggerPointerPickedOnBranch(ctx context.Context, logger zerolog.Logger, cond bool) {
	a := logger.With().Ctx(ctx).Logger()
	b := logger.With().Ctx(ctx).Str("k", "v").Logger()
	_ = a.With()
	p := &a
	if cond {
		p = &b
	}
	p.Info().Msg("logger pointer picked on a branch") // OK - both loggers have ctx
}

// A closure that is only called runs at each call, and reads what reaches it
// there, not the stores after (#173).

func goodIIFEBeforeEventReplaced(ctx context.Context, logger zerolog.Logger, base zerolog.Logger) {
	lc := logger.With().Ctx(ctx).Logger()
	e := lc.Info()
	func() {
		e.Msg("iife before the event is replaced") // OK - e has ctx at the call
	}()
	e = base.Info()
	_ = e
}

func goodIIFEInLoopBeforeLoggerReplaced(ctx context.Context, logger zerolog.Logger, n int) {
	l := logger.With().Ctx(ctx).Logger()
	for range n {
		func() {
			l.Info().Msg("iife in a loop") // OK - l has ctx at every call
		}()
	}
	l = logger
	_ = l
}

func badIIFEInLoopLoggerReplacedInLoop(ctx context.Context, logger zerolog.Logger, n int) {
	l := logger.With().Ctx(ctx).Logger()
	for range n {
		func() {
			l.Info().Msg("iife in a loop, replaced in the loop") // want `zerolog call chain missing .Ctx\(ctx\)`
		}()
		l = logger
	}
}

func badClosureStoreAfterIsMade(ctx context.Context, logger zerolog.Logger) {
	l := logger.With().Ctx(ctx).Logger()
	f := func() {
		l.Info().Msg("store after the closure is made") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	l = logger
	f()
}

// A nil stored to an event in memory logs nothing, and is skipped as a nil
// edge of a Phi is (#158).

func goodNilStoredToCapturedEvent(ctx context.Context, logger zerolog.Logger, cond bool) {
	e := logger.Info().Ctx(ctx)
	func() { _ = e }()
	if cond {
		e = nil
	}
	e.Msg("nil stored to a captured event") // OK - the other store has ctx
}

func badOnlyNilStoredToCapturedEvent(ctx context.Context, logger zerolog.Logger) {
	var e *zerolog.Event
	func() { _ = e }()
	e = logger.Info()
	if e != nil {
		e = nil
	}
	e.Msg("nil or no ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A field or constant element of a captured variable is what is at the same
// selections of the variable bound where the closure is made (#153).

type capturedHolder struct {
	l zerolog.Logger
	e *zerolog.Event
}

func goodCapturedField(ctx context.Context, logger zerolog.Logger) {
	var h capturedHolder
	h.l = logger.With().Ctx(ctx).Logger()
	func() {
		h.l.Info().Msg("captured field") // OK - h.l has ctx
	}()
}

func goodCapturedEventField(ctx context.Context, logger zerolog.Logger) {
	h := capturedHolder{e: logger.Info().Ctx(ctx)}
	func() {
		h.e.Msg("captured event field") // OK - h.e has ctx
	}()
}

func goodDeferredCapturedArrayElement(ctx context.Context, logger zerolog.Logger) {
	var arr [2]zerolog.Logger
	arr[1] = logger.With().Ctx(ctx).Logger()
	defer func() {
		arr[1].Info().Msg("deferred captured array element") // OK - arr[1] has ctx
	}()
}

func badCapturedField(ctx context.Context, logger zerolog.Logger) {
	h := capturedHolder{l: logger.With().Ctx(ctx).Logger()}
	f := func() {
		h.l.Info().Msg("captured field") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	h.l = logger
	f()
}

func badCapturedFieldOther(ctx context.Context, logger zerolog.Logger) {
	var h capturedHolder
	h.l = logger
	func() {
		h.l.Info().Msg("captured field without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

// ===== GOROUTINE USAGE =====

func badGoroutineLog(ctx context.Context, logger zerolog.Logger) {
	go func() {
		logger.Info().Msg("goroutine log") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

func goodGoroutineLogWithCtx(ctx context.Context, logger zerolog.Logger) {
	go func() { // OK - ctx is used via .Ctx(ctx)
		logger.Info().Ctx(ctx).Msg("goroutine log with ctx") // OK - zerolog ctx is set
	}()
}

func badGoroutineWithDerivedLogger(ctx context.Context, logger zerolog.Logger) {
	derived := logger.With().Str("goroutine", "true").Logger()
	go func() {
		derived.Info().Msg("goroutine derived") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

func goodGoroutineWithDerivedLoggerCtx(ctx context.Context, logger zerolog.Logger) {
	derived := logger.With().Ctx(ctx).Logger()
	go func() {
		derived.Info().Msg("goroutine derived with ctx") // OK - ctx set via With().Ctx(ctx)
	}()
}
