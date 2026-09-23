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

// A space after "//" makes it a malformed directive, which suppresses nothing.
func badIgnoreWithSpaceSameLine(ctx context.Context, log zerolog.Logger) {
	log.Info().Msg("not ignored") // zerologlintctx:ignore // want `zerolog call chain missing .Ctx\(ctx\)` `malformed zerologlintctx directive: write //zerologlintctx:ignore`
}

func goodIgnoredWithReason(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore - intentionally not passing context
	log.Info().Msg("ignored")
}

func badIgnoreWithSpacePreviousLine(ctx context.Context, log zerolog.Logger) {
	// zerologlintctx:ignore // want `malformed zerologlintctx directive: write //zerologlintctx:ignore`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A directive name that only starts with "ignore" is not the ignore directive.
func badIgnoreLookalikeName(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignored
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A tool name that only starts with "zerologlintctx" is not this tool.
func badIgnoreLookalikeTool(ctx context.Context, log zerolog.Logger) {
	//zerologlintctxx:ignore
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== MALFORMED DIRECTIVES =====

func badMalformedTabAfterSlashes(ctx context.Context, log zerolog.Logger) {
	//	zerologlintctx:ignore // want `malformed zerologlintctx directive: write //zerologlintctx:ignore`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badMalformedSpaceAfterColon(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx: ignore // want `malformed zerologlintctx directive: write //zerologlintctx:ignore`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badMalformedBlockComment(ctx context.Context, log zerolog.Logger) {
	/*zerologlintctx:ignore*/ // want `malformed zerologlintctx directive: write //zerologlintctx:ignore`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badMalformedSpacedBlockComment(ctx context.Context, log zerolog.Logger) {
	/* zerologlintctx:ignore */ // want `malformed zerologlintctx directive: write //zerologlintctx:ignore`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// An uppercase name is not a valid directive name, so no rewrite is suggested.
func badMalformedUppercaseName(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:Ignore // want `^malformed zerologlintctx directive$`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A comment with no directive name gets no suggestion either.
func badMalformedNoName(ctx context.Context, log zerolog.Logger) {
	/* zerologlintctx: */ // want `^malformed zerologlintctx directive$`
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
