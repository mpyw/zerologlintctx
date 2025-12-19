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

// ===== IGNORE COMMENTS =====

func goodIgnoredSameLine(ctx context.Context, log zerolog.Logger) {
	log.Info().Msg("ignored") //zerologlintctx:ignore
}

func goodIgnoredPreviousLine(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore
	log.Info().Msg("ignored")
}

func goodIgnoredWithSpace(ctx context.Context, log zerolog.Logger) {
	log.Info().Msg("ignored") // zerologlintctx:ignore
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
