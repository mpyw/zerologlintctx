package zerolog

// Tests for With().Logger() patterns and MsgFunc terminator.
// These patterns build a derived logger with preset fields.

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ===== MsgFunc TERMINATOR =====

func badMsgFunc(ctx context.Context, logger zerolog.Logger) {
	logger.Info().MsgFunc(func() string { return "lazy" }) // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodMsgFuncWithCtx(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Ctx(ctx).MsgFunc(func() string { return "lazy" }) // OK
}

func badMsgFuncGlobal(ctx context.Context) {
	log.Info().MsgFunc(func() string { return "lazy global" }) // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodMsgFuncGlobalWithCtx(ctx context.Context) {
	log.Info().Ctx(ctx).MsgFunc(func() string { return "lazy global" }) // OK
}

// ===== With().Logger() PATTERNS =====

// Basic With().Logger() pattern - builds logger with preset fields
func badWithLogger(ctx context.Context, logger zerolog.Logger) {
	derived := logger.With().Str("service", "api").Logger()
	derived.Info().Msg("derived logger") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodWithLoggerEventCtx(ctx context.Context, logger zerolog.Logger) {
	derived := logger.With().Str("service", "api").Logger()
	derived.Info().Ctx(ctx).Msg("derived logger with ctx") // OK - ctx on event
}

func goodWithLoggerContextCtx(ctx context.Context, logger zerolog.Logger) {
	// Context.Ctx() sets the default context for the derived logger
	derived := logger.With().Str("service", "api").Ctx(ctx).Logger()
	derived.Info().Msg("derived logger") // OK - ctx was set during With()
}

// Chained With().Logger() directly calling log methods
func badWithLoggerInline(ctx context.Context, logger zerolog.Logger) {
	logger.With().Str("k", "v").Logger().Info().Msg("inline") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodWithLoggerInlineCtx(ctx context.Context, logger zerolog.Logger) {
	logger.With().Str("k", "v").Ctx(ctx).Logger().Info().Msg("inline with ctx") // OK
}

func goodWithLoggerInlineEventCtx(ctx context.Context, logger zerolog.Logger) {
	logger.With().Str("k", "v").Logger().Info().Ctx(ctx).Msg("inline event ctx") // OK
}

// Global logger With() pattern
func badGlobalWithLogger(ctx context.Context) {
	derived := log.With().Str("app", "test").Logger()
	derived.Info().Msg("global derived") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodGlobalWithLoggerCtx(ctx context.Context) {
	derived := log.With().Str("app", "test").Ctx(ctx).Logger()
	derived.Info().Msg("global derived with ctx") // OK
}

// ===== With().Logger() WITH VARIABLE ASSIGNMENTS =====

func badWithLoggerVariableChain(ctx context.Context, logger zerolog.Logger) {
	wc := logger.With()
	wc = wc.Str("key1", "val1")
	wc = wc.Str("key2", "val2")
	derived := wc.Logger()
	derived.Info().Msg("from variable chain") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodWithLoggerVariableChainCtx(ctx context.Context, logger zerolog.Logger) {
	wc := logger.With()
	wc = wc.Str("key1", "val1")
	wc = wc.Ctx(ctx) // ctx added during chain
	wc = wc.Str("key2", "val2")
	derived := wc.Logger()
	derived.Info().Msg("from variable chain with ctx") // OK
}

// ===== NESTED With().Logger() =====

func badNestedWithLogger(ctx context.Context, logger zerolog.Logger) {
	func() {
		derived := logger.With().Str("nested", "true").Logger()
		derived.Info().Msg("nested derived") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

func goodNestedWithLoggerCtx(ctx context.Context, logger zerolog.Logger) {
	func() {
		derived := logger.With().Ctx(ctx).Logger()
		derived.Info().Msg("nested derived with ctx") // OK
	}()
}

// ===== MULTIPLE With().Logger() CHAINS =====

func badMultipleWithLoggers(ctx context.Context, logger zerolog.Logger) {
	derived1 := logger.With().Str("id", "1").Logger()
	derived2 := derived1.With().Str("id", "2").Logger()
	derived2.Info().Msg("double derived") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodMultipleWithLoggersCtx(ctx context.Context, logger zerolog.Logger) {
	derived1 := logger.With().Ctx(ctx).Logger()
	derived2 := derived1.With().Str("id", "2").Logger()
	derived2.Info().Msg("double derived with ctx") // OK - ctx inherited
}

// ===== CONDITIONAL With() PATTERNS =====

func badConditionalWithLogger(ctx context.Context, logger zerolog.Logger, flag bool) {
	var derived zerolog.Logger
	if flag {
		derived = logger.With().Str("mode", "a").Logger()
	} else {
		derived = logger.With().Str("mode", "b").Logger()
	}
	derived.Info().Msg("conditional derived") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== With() ON zerolog.Ctx() RESULT =====

func goodZerologCtxWithLogger(ctx context.Context) {
	// zerolog.Ctx(ctx) already has context, With() preserves it
	logger := zerolog.Ctx(ctx)
	derived := logger.With().Str("extra", "field").Logger()
	derived.Info().Msg("from ctx derived") // OK - started from Ctx(ctx)
}

// ===== MsgFunc IN VARIOUS CONTEXTS =====

func badMsgFuncInLoop(ctx context.Context, logger zerolog.Logger) {
	for i := 0; i < 3; i++ {
		logger.Info().MsgFunc(func() string { return "loop" }) // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

func badMsgFuncWithVariable(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	e = e.Str("key", "val")
	e.MsgFunc(func() string { return "variable" }) // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodMsgFuncWithVariableCtx(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info().Ctx(ctx)
	e = e.Str("key", "val")
	e.MsgFunc(func() string { return "variable with ctx" }) // OK
}
