// Package logger covers Logger transformations: With().Logger() chains, Level,
// Output, zerolog.New, zerolog.Nop, zerolog.Ctx and log.Ctx.
package logger

import (
	"context"
	"io"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ===== LOGGER TRANSFORMATION CHAINS =====

func evilLoggerLevel(ctx context.Context, logger zerolog.Logger) {
	debugLogger := logger.Level(zerolog.DebugLevel)
	debugLogger.Debug().Msg("debug level") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilLoggerLevelWithCtx(ctx context.Context, logger zerolog.Logger) {
	debugLogger := logger.Level(zerolog.DebugLevel)
	debugLogger.Debug().Ctx(ctx).Msg("debug level with ctx") // OK
}

func evilLoggerOutput(ctx context.Context, logger zerolog.Logger) {
	fileLogger := logger.Output(os.Stderr)
	fileLogger.Info().Msg("file logger") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilLoggerOutputWithCtx(ctx context.Context, logger zerolog.Logger) {
	fileLogger := logger.Output(os.Stderr)
	fileLogger.Info().Ctx(ctx).Msg("file logger with ctx") // OK
}

func evilLoggerChainedTransforms(ctx context.Context, logger zerolog.Logger) {
	transformed := logger.Level(zerolog.InfoLevel).Output(os.Stdout)
	transformed.Info().Msg("chained transforms") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== zerolog.New() AND zerolog.Nop() =====

func evilZerologNew(ctx context.Context) {
	logger := zerolog.New(os.Stdout)
	logger.Info().Msg("new logger") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilZerologNewWithCtx(ctx context.Context) {
	logger := zerolog.New(os.Stdout)
	logger.Info().Ctx(ctx).Msg("new logger with ctx") // OK
}

func evilZerologNewWithTimestamp(ctx context.Context) {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	logger.Info().Msg("new with timestamp") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilZerologNewWithTimestampAndCtx(ctx context.Context) {
	logger := zerolog.New(os.Stdout).With().Timestamp().Ctx(ctx).Logger()
	logger.Info().Msg("new with timestamp and ctx") // OK
}

func evilZerologNop(ctx context.Context) {
	logger := zerolog.Nop()
	logger.Info().Msg("nop logger") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== COMPLEX With() CHAINS =====

func evilLongWithChain(ctx context.Context, logger zerolog.Logger) {
	derived := logger.With().
		Str("service", "api").
		Str("version", "v1").
		Int("port", 8080).
		Bool("debug", true).
		Str("env", "prod").
		Logger()
	derived.Info().Msg("long chain") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilLongWithChainCtxInMiddle(ctx context.Context, logger zerolog.Logger) {
	derived := logger.With().
		Str("service", "api").
		Str("version", "v1").
		Ctx(ctx). // Ctx in the middle
		Int("port", 8080).
		Bool("debug", true).
		Logger()
	derived.Info().Msg("long chain with ctx") // OK
}

func evilWithChainMultipleVariables(ctx context.Context, logger zerolog.Logger) {
	wc1 := logger.With()
	wc2 := wc1.Str("a", "1")
	wc3 := wc2.Str("b", "2")
	wc4 := wc3.Str("c", "3")
	derived := wc4.Logger()
	derived.Info().Msg("multi var chain") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilWithChainMultipleVariablesCtxAtEnd(ctx context.Context, logger zerolog.Logger) {
	wc1 := logger.With()
	wc2 := wc1.Str("a", "1")
	wc3 := wc2.Str("b", "2")
	wc4 := wc3.Ctx(ctx)
	derived := wc4.Logger()
	derived.Info().Msg("multi var chain with ctx") // OK
}

// ===== TRIPLE With().Logger() CHAINS =====

func evilTripleWithLogger(ctx context.Context, logger zerolog.Logger) {
	d1 := logger.With().Str("level", "1").Logger()
	d2 := d1.With().Str("level", "2").Logger()
	d3 := d2.With().Str("level", "3").Logger()
	d3.Info().Msg("triple derived") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilTripleWithLoggerCtxAtFirst(ctx context.Context, logger zerolog.Logger) {
	d1 := logger.With().Ctx(ctx).Str("level", "1").Logger()
	d2 := d1.With().Str("level", "2").Logger()
	d3 := d2.With().Str("level", "3").Logger()
	d3.Info().Msg("triple derived ctx at first") // OK - ctx inherited
}

func evilTripleWithLoggerCtxAtSecond(ctx context.Context, logger zerolog.Logger) {
	d1 := logger.With().Str("level", "1").Logger()
	d2 := d1.With().Ctx(ctx).Str("level", "2").Logger()
	d3 := d2.With().Str("level", "3").Logger()
	d3.Info().Msg("triple derived ctx at second") // OK - ctx inherited
}

func evilTripleWithLoggerCtxAtThird(ctx context.Context, logger zerolog.Logger) {
	d1 := logger.With().Str("level", "1").Logger()
	d2 := d1.With().Str("level", "2").Logger()
	d3 := d2.With().Ctx(ctx).Str("level", "3").Logger()
	d3.Info().Msg("triple derived ctx at third") // OK
}

// ===== MIXING zerolog.Ctx() WITH TRANSFORMATIONS =====

func evilZerologCtxWithLevel(ctx context.Context) {
	logger := zerolog.Ctx(ctx).Level(zerolog.DebugLevel)
	logger.Debug().Msg("ctx with level") // OK - started from Ctx(ctx)
}

func evilZerologCtxWithOutput(ctx context.Context) {
	logger := zerolog.Ctx(ctx).Output(os.Stderr)
	logger.Info().Msg("ctx with output") // OK - started from Ctx(ctx)
}

func evilZerologCtxWithWith(ctx context.Context) {
	derived := zerolog.Ctx(ctx).With().Str("extra", "field").Logger()
	derived.Info().Msg("ctx with with") // OK - started from Ctx(ctx)
}

func evilZerologCtxTripleWith(ctx context.Context) {
	d1 := zerolog.Ctx(ctx).With().Str("l", "1").Logger()
	d2 := d1.With().Str("l", "2").Logger()
	d3 := d2.With().Str("l", "3").Logger()
	d3.Info().Msg("ctx triple with") // OK - started from Ctx(ctx)
}

// ===== LOGGER FROM CONTEXT =====

func goodLoggerFromContext(ctx context.Context) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Msg("from context") // OK - logger came from ctx
}

func badLoggerFromContextThenNew(ctx context.Context, logger zerolog.Logger) {
	_ = zerolog.Ctx(ctx)                    // Get but don't use
	logger.Info().Msg("ignored ctx logger") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== LOGGER TRANSFORMATION TRACKING =====

// Logger.Level() should not affect ctx tracking
func badLoggerLevel(ctx context.Context, logger zerolog.Logger) {
	l := logger.Level(zerolog.DebugLevel)
	l.Debug().Msg("leveled") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodLoggerLevelWithCtx(ctx context.Context, logger zerolog.Logger) {
	l := logger.Level(zerolog.DebugLevel)
	l.Debug().Ctx(ctx).Msg("leveled with ctx") // OK
}

// Logger.Output() should not affect ctx tracking
func badLoggerOutput(ctx context.Context, logger zerolog.Logger) {
	l := logger.Output(os.Stderr)
	l.Info().Msg("output changed") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodLoggerOutputWithCtx(ctx context.Context, logger zerolog.Logger) {
	l := logger.Output(os.Stderr)
	l.Info().Ctx(ctx).Msg("output changed with ctx") // OK
}

// Chained transformations
func badChainedTransformations(ctx context.Context, logger zerolog.Logger) {
	l := logger.Level(zerolog.WarnLevel).Output(os.Stdout)
	l.Warn().Msg("multi transform") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// Logger.Output builds its result with zerolog.New, and copies the level,
// sampler, stack, hooks and fields, but not the context that Context.Ctx sets
// (#193). Level, Sample and Hook keep it.
func badCtxLoggerThenOutput(ctx context.Context, base zerolog.Logger, w io.Writer) {
	l := base.With().Ctx(ctx).Logger().Output(w)
	l.Info().Msg("ctx dropped by Output") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badCtxLoggerThenOutputThenWith(ctx context.Context, base zerolog.Logger, w io.Writer) {
	l := base.With().Ctx(ctx).Logger().Level(zerolog.InfoLevel).Output(w).With().Str("k", "v").Logger()
	l.Info().Msg("ctx dropped by Output, before With") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badCtxLoggerVariableThenOutput(ctx context.Context, base zerolog.Logger, w io.Writer) {
	l := base.With().Ctx(ctx).Logger()
	l = l.Output(w)
	l.Info().Msg("ctx dropped by Output, on a variable") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodCtxLoggerAfterOutput(ctx context.Context, base zerolog.Logger, w io.Writer) {
	l := base.Output(w).With().Ctx(ctx).Logger()
	l.Info().Msg("ctx set after Output") // OK
}

func goodCtxLoggerThenLevelSampleHook(ctx context.Context, base zerolog.Logger, s zerolog.Sampler, h zerolog.Hook) {
	l := base.With().Ctx(ctx).Logger().Level(zerolog.InfoLevel).Sample(s).Hook(h)
	l.Info().Msg("Level, Sample and Hook keep ctx") // OK
}

func goodCtxEventOnOutputLogger(ctx context.Context, base zerolog.Logger, w io.Writer) {
	l := base.With().Ctx(ctx).Logger().Output(w)
	l.Info().Ctx(ctx).Msg("ctx on the event") // OK
}

// A logger taken from the context counts as a deliberate use of it, whatever
// is done to it after, so it still counts past Output.
func goodZerologCtxThenOutput(ctx context.Context, w io.Writer) {
	l := zerolog.Ctx(ctx).Output(w)
	l.Info().Msg("logger from ctx, then Output") // OK
}

func goodLogCtxThenWithThenOutput(ctx context.Context, w io.Writer) {
	l := log.Ctx(ctx).With().Str("k", "v").Logger().Output(w)
	l.Info().Msg("logger from ctx, then With and Output") // OK
}

// ===== zerolog.Ctx() VARIATIONS =====

func goodZerologCtxDirect(ctx context.Context) {
	zerolog.Ctx(ctx).Info().Msg("direct ctx") // OK - zerolog.Ctx returns ctx-aware logger
}

func goodZerologCtxChained(ctx context.Context) {
	zerolog.Ctx(ctx).Info().Str("key", "val").Int("num", 42).Msg("chained") // OK
}

func goodZerologCtxWithWith(ctx context.Context) {
	l := zerolog.Ctx(ctx).With().Str("base", "field").Logger()
	l.Info().Msg("ctx with with") // OK - ctx inherited through With()
}

func goodZerologCtxMultipleWith(ctx context.Context) {
	l1 := zerolog.Ctx(ctx).With().Str("l", "1").Logger()
	l2 := l1.With().Str("l", "2").Logger()
	l3 := l2.With().Str("l", "3").Logger()
	l3.Info().Msg("triple with from ctx") // OK
}

func goodZerologCtxWithLevel(ctx context.Context) {
	l := zerolog.Ctx(ctx).Level(zerolog.DebugLevel)
	l.Debug().Msg("ctx with level") // OK
}

// ===== log.Ctx() VARIATIONS (global package) =====

func goodLogCtxDirect(ctx context.Context) {
	log.Ctx(ctx).Info().Msg("log.Ctx direct") // OK
}

func goodLogCtxWithWith(ctx context.Context) {
	l := log.Ctx(ctx).With().Str("pkg", "log").Logger()
	l.Info().Msg("log.Ctx with with") // OK
}

// ===== log.Ctx(ctx).Err() PATTERNS (issue #8) =====

func goodLogCtxErr(ctx context.Context, err error) {
	log.Ctx(ctx).Err(err).Msg("log.Ctx with Err") // OK - ctx from log.Ctx
}

func goodLogCtxErrMsgf(ctx context.Context, err error) {
	log.Ctx(ctx).Err(err).Msgf("failed: %v", err) // OK - ctx from log.Ctx
}

func goodZerologCtxErr(ctx context.Context, err error) {
	zerolog.Ctx(ctx).Err(err).Msg("zerolog.Ctx with Err") // OK - ctx from zerolog.Ctx
}

func goodLogCtxErrStoredEvent(ctx context.Context, err error) {
	zl := log.Ctx(ctx)
	var event *zerolog.Event
	if err != nil {
		event = zl.Err(err)
	} else {
		event = zl.Debug()
	}
	event.Msg("conditional event from log.Ctx") // OK - ctx from log.Ctx
}

func goodLogCtxDerivedLoggerThenErr(ctx context.Context, err error) {
	zl := log.Ctx(ctx).With().Str("key", "val").Logger()
	zl.Err(err).Msg("derived logger Err") // OK - ctx inherited through With()
}

func goodZerologCtxWithLevelMethod(ctx context.Context) {
	zerolog.Ctx(ctx).WithLevel(zerolog.DebugLevel).Msg("WithLevel from Ctx") // OK - ctx from zerolog.Ctx
}

func goodLogCtxWithLevelMethod(ctx context.Context) {
	log.Ctx(ctx).WithLevel(zerolog.InfoLevel).Str("key", "val").Msg("WithLevel from log.Ctx") // OK
}

// ===== COMPLEX With().Logger() CHAINS =====

func badWithLoggerFromNew(ctx context.Context) {
	l := zerolog.New(os.Stdout).With().Timestamp().Logger()
	l.Info().Msg("new with timestamp") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodWithLoggerFromNewWithCtx(ctx context.Context) {
	l := zerolog.New(os.Stdout).With().Timestamp().Ctx(ctx).Logger()
	l.Info().Msg("new with timestamp and ctx") // OK
}

func badWithLoggerMultipleVariables(ctx context.Context, logger zerolog.Logger) {
	wc := logger.With()
	wc = wc.Str("a", "1")
	wc = wc.Str("b", "2")
	l := wc.Logger()
	l.Info().Msg("multi var with") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodWithLoggerMultipleVariablesCtx(ctx context.Context, logger zerolog.Logger) {
	wc := logger.With()
	wc = wc.Str("a", "1")
	wc = wc.Ctx(ctx)
	wc = wc.Str("b", "2")
	l := wc.Logger()
	l.Info().Msg("multi var with ctx") // OK
}

// ===== INTERFACE PATTERNS =====

type LoggerProvider interface {
	GetLogger() zerolog.Logger
}

type myProvider struct {
	logger zerolog.Logger
}

func (p *myProvider) GetLogger() zerolog.Logger {
	return p.logger
}

func badLoggerFromInterface(ctx context.Context, provider LoggerProvider) {
	l := provider.GetLogger()
	l.Info().Msg("from interface") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodLoggerFromInterfaceWithCtx(ctx context.Context, provider LoggerProvider) {
	l := provider.GetLogger()
	l.Info().Ctx(ctx).Msg("from interface with ctx") // OK
}

// ===== MULTIPLE LOGGERS IN SAME FUNCTION =====

func badMultipleLoggers(ctx context.Context, l1, l2 zerolog.Logger) {
	l1.Info().Msg("logger 1") // want `zerolog call chain missing .Ctx\(ctx\)`
	l2.Warn().Msg("logger 2") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badMixedLoggers(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Msg("param logger") // want `zerolog call chain missing .Ctx\(ctx\)`
	log.Info().Msg("global logger")   // want `zerolog call chain missing .Ctx\(ctx\)`
	newLogger := zerolog.New(os.Stdout)
	newLogger.Info().Msg("new") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodMixedLoggersWithCtx(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Ctx(ctx).Msg("param logger with ctx") // OK
	log.Ctx(ctx).Info().Msg("global ctx logger")        // OK
	zerolog.Ctx(ctx).Info().Msg("zerolog ctx")          // OK
}

// ===== nil LOGGER =====

func badNilLoggerCheck(ctx context.Context, logger *zerolog.Logger) {
	if logger != nil {
		logger.Info().Msg("not nil") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

func goodNilLoggerCheckWithCtx(ctx context.Context, logger *zerolog.Logger) {
	if logger != nil {
		logger.Info().Ctx(ctx).Msg("not nil with ctx") // OK
	}
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

// With().Logger() used right away. Logger.Info has a pointer receiver, so
// the Logger() result must be held in a variable first.
func badWithLoggerInline(ctx context.Context, logger zerolog.Logger) {
	l := logger.With().Str("k", "v").Logger()
	l.Info().Msg("inline") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodWithLoggerInlineCtx(ctx context.Context, logger zerolog.Logger) {
	l := logger.With().Str("k", "v").Ctx(ctx).Logger()
	l.Info().Msg("inline with ctx") // OK
}

func goodWithLoggerInlineEventCtx(ctx context.Context, logger zerolog.Logger) {
	l := logger.With().Str("k", "v").Logger()
	l.Info().Ctx(ctx).Msg("inline event ctx") // OK
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

// ===== Context.CreateDict =====

// LIMITATION (false positive): Context.CreateDict is not followed back to the
// Context, so a dict that carries the context from Context.Ctx is taken to
// have none.
func limitationContextCreateDictWithCtx(ctx context.Context, logger zerolog.Logger) {
	d := logger.With().Ctx(ctx).CreateDict()
	d.Msg("dict of a context with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}
