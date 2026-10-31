// Package basic covers simple good and bad cases, and the global logger.
package basic

import (
	"context"
	"errors"

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

// ===== zerolog.Ctx() PATTERNS =====

// These test cases verify that zerolog.Ctx(ctx) is recognized as context-aware.

func goodZerologCtx(ctx context.Context) {
	// zerolog.Ctx(ctx) already uses context, so .Ctx() is not needed
	zerolog.Ctx(ctx).Info().Msg("using zerolog.Ctx") // OK - ctx already used
}

func goodZerologCtxWithFields(ctx context.Context) {
	zerolog.Ctx(ctx).Info().Str("key", "value").Msg("with fields") // OK
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

// ===== GLOBAL LOGGER VARIATIONS =====

func evilGlobalAllLevels(ctx context.Context) {
	log.Trace().Msg("global trace") // want `zerolog call chain missing .Ctx\(ctx\)`
	log.Debug().Msg("global debug") // want `zerolog call chain missing .Ctx\(ctx\)`
	log.Info().Msg("global info")   // want `zerolog call chain missing .Ctx\(ctx\)`
	log.Warn().Msg("global warn")   // want `zerolog call chain missing .Ctx\(ctx\)`
	log.Error().Msg("global error") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilGlobalWithChain(ctx context.Context) {
	derived := log.With().Str("global", "derived").Logger()
	derived.Info().Msg("global derived") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilGlobalWithChainCtx(ctx context.Context) {
	derived := log.With().Ctx(ctx).Str("global", "derived").Logger()
	derived.Info().Msg("global derived with ctx") // OK
}

// ===== MULTIPLE LOG STATEMENTS IN ONE FUNCTION =====

func evilMultipleLogStatements(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Msg("first")                          // want `zerolog call chain missing .Ctx\(ctx\)`
	logger.Debug().Str("key", "val").Msg("second")      // want `zerolog call chain missing .Ctx\(ctx\)`
	logger.Warn().Int("count", 42).Send()               // want `zerolog call chain missing .Ctx\(ctx\)`
	logger.Error().Err(errors.New("oops")).Msg("third") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilMultipleLogStatementsPartialCtx(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Ctx(ctx).Msg("first")                 // OK
	logger.Debug().Str("key", "val").Msg("second")      // want `zerolog call chain missing .Ctx\(ctx\)`
	logger.Warn().Ctx(ctx).Int("count", 42).Send()      // OK
	logger.Error().Err(errors.New("oops")).Msg("third") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== GLOBAL LOGGER EDGE CASES =====

func badGlobalIIFE(ctx context.Context) {
	func() *zerolog.Event {
		return log.Info()
	}().Msg("global iife") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badGlobalConditional(ctx context.Context, flag bool) {
	var e *zerolog.Event
	if flag {
		e = log.Info()
	} else {
		e = log.Logger.Warn()
	}
	e.Msg("global conditional") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodGlobalCtxImmediateChain(ctx context.Context) {
	log.Ctx(ctx).Info().Str("key", "val").Msg("global ctx chain") // OK
}

// ===== MULTIPLE TERMINATORS IN SAME SCOPE =====

func badMultipleTerminators(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	e.Msg("first") // want `zerolog call chain missing .Ctx\(ctx\)`

	e2 := logger.Warn()
	e2.Msgf("second: %s", "test") // want `zerolog call chain missing .Ctx\(ctx\)`

	e3 := logger.Error()
	e3.Send() // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== EARLY RETURN PATTERN =====

func badEarlyReturnPartial(ctx context.Context, logger zerolog.Logger, ok bool) {
	if !ok {
		logger.Error().Msg("early return") // want `zerolog call chain missing .Ctx\(ctx\)`
		return
	}
	logger.Info().Ctx(ctx).Msg("normal path") // OK
}

// ===== MULTIPLE EVENTS IN SAME LINE =====

func badMultipleEventsOneLine(ctx context.Context, logger zerolog.Logger, cond bool) {
	if cond {
		logger.Info().Msg("a") // want `zerolog call chain missing .Ctx\(ctx\)`
	} else {
		logger.Warn().Msg("b") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}
