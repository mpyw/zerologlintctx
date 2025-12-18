// Package zerolog contains test fixtures for the zerolog context propagation checker.
// This file covers evil/adversarial edge cases - unusual but valid code patterns like
// deep nesting, closures, conditionals, loops, and various field types.
// See basic.go for simple cases, evil_ssa.go for SSA-specific limitations,
// and evil_logger.go for Logger transformation patterns.
package zerolog

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// =============================================================================
// EVIL: DEEP NESTING AND CLOSURES
// =============================================================================

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
		// NOTE: In test stubs, SSA optimizes away the receiver for method calls
		// on zero-value structs with empty implementations. In production code
		// with real zerolog, FreeVar tracing through MakeClosure bindings works.
		derived.Info().Msg("captured derived with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	doSomething()
}

// =============================================================================
// EVIL: LOGGER TRANSFORMATION CHAINS
// =============================================================================

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

// =============================================================================
// EVIL: zerolog.New() AND zerolog.Nop()
// =============================================================================

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

// =============================================================================
// EVIL: COMPLEX With() CHAINS
// =============================================================================

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

// =============================================================================
// EVIL: TRIPLE With().Logger() CHAINS
// =============================================================================

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

// =============================================================================
// EVIL: CONDITIONAL AND BRANCHING
// =============================================================================

func evilConditionalLogLevel(ctx context.Context, logger zerolog.Logger, isDebug bool) {
	var event *zerolog.Event
	if isDebug {
		event = logger.Debug()
	} else {
		event = logger.Info()
	}
	event.Msg("conditional level") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilConditionalLogLevelWithCtx(ctx context.Context, logger zerolog.Logger, isDebug bool) {
	var event *zerolog.Event
	if isDebug {
		event = logger.Debug().Ctx(ctx)
	} else {
		event = logger.Info().Ctx(ctx)
	}
	event.Msg("conditional level with ctx") // OK
}

func evilConditionalWithLogger(ctx context.Context, logger zerolog.Logger, addExtra bool) {
	var derived zerolog.Logger
	if addExtra {
		derived = logger.With().Str("extra", "yes").Logger()
	} else {
		derived = logger.With().Logger()
	}
	derived.Info().Msg("conditional with") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilConditionalWithLoggerPartialCtx(ctx context.Context, logger zerolog.Logger, addExtra bool) {
	var derived zerolog.Logger
	if addExtra {
		derived = logger.With().Ctx(ctx).Str("extra", "yes").Logger() // has ctx
	} else {
		derived = logger.With().Logger() // NO ctx!
	}
	// All branches must have ctx - partial coverage is flagged
	derived.Info().Msg("conditional partial ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilConditionalWithLoggerBothCtx(ctx context.Context, logger zerolog.Logger, addExtra bool) {
	var derived zerolog.Logger
	if addExtra {
		derived = logger.With().Ctx(ctx).Str("extra", "yes").Logger()
	} else {
		derived = logger.With().Ctx(ctx).Logger()
	}
	derived.Info().Msg("conditional both ctx") // OK
}

// =============================================================================
// EVIL: SWITCH STATEMENTS
// =============================================================================

func evilSwitchLogger(ctx context.Context, logger zerolog.Logger, level string) {
	var event *zerolog.Event
	switch level {
	case "debug":
		event = logger.Debug()
	case "info":
		event = logger.Info()
	case "warn":
		event = logger.Warn()
	default:
		event = logger.Error()
	}
	event.Msg("switch level") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilSwitchLoggerWithCtx(ctx context.Context, logger zerolog.Logger, level string) {
	var event *zerolog.Event
	switch level {
	case "debug":
		event = logger.Debug().Ctx(ctx)
	case "info":
		event = logger.Info().Ctx(ctx)
	case "warn":
		event = logger.Warn().Ctx(ctx)
	default:
		event = logger.Error().Ctx(ctx)
	}
	event.Msg("switch level with ctx") // OK
}

// =============================================================================
// EVIL: LOOPS
// =============================================================================

func evilLoopWithDifferentLoggers(ctx context.Context, loggers []zerolog.Logger) {
	for _, logger := range loggers {
		logger.Info().Msg("loop logger") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

func evilLoopWithDifferentLoggersCtx(ctx context.Context, loggers []zerolog.Logger) {
	for _, logger := range loggers {
		logger.Info().Ctx(ctx).Msg("loop logger with ctx") // OK
	}
}

func evilLoopBuildingEvent(ctx context.Context, logger zerolog.Logger, fields map[string]string) {
	event := logger.Info()
	for k, v := range fields {
		event = event.Str(k, v)
	}
	event.Msg("loop built event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilLoopBuildingEventWithCtx(ctx context.Context, logger zerolog.Logger, fields map[string]string) {
	event := logger.Info().Ctx(ctx)
	for k, v := range fields {
		event = event.Str(k, v)
	}
	event.Msg("loop built event with ctx") // OK
}

func evilLoopBuildingContext(ctx context.Context, logger zerolog.Logger, fields map[string]string) {
	wc := logger.With()
	for k, v := range fields {
		wc = wc.Str(k, v)
	}
	derived := wc.Logger()
	derived.Info().Msg("loop built context") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilLoopBuildingContextWithCtx(ctx context.Context, logger zerolog.Logger, fields map[string]string) {
	wc := logger.With().Ctx(ctx)
	for k, v := range fields {
		wc = wc.Str(k, v)
	}
	derived := wc.Logger()
	derived.Info().Msg("loop built context with ctx") // OK
}

// =============================================================================
// EVIL: EVENT METHODS THAT RETURN *Event
// =============================================================================

func evilEventDict(ctx context.Context, logger zerolog.Logger) {
	logger.Info().
		Dict("user", zerolog.Dict().
			Str("name", "alice").
			Int("age", 30)).
		Msg("with dict") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilEventDictWithCtx(ctx context.Context, logger zerolog.Logger) {
	logger.Info().
		Ctx(ctx).
		Dict("user", zerolog.Dict().
			Str("name", "alice").
			Int("age", 30)).
		Msg("with dict and ctx") // OK
}

func evilEventTimestamp(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Timestamp().Str("key", "val").Msg("with timestamp") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilEventTimestampWithCtx(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Ctx(ctx).Timestamp().Str("key", "val").Msg("with timestamp and ctx") // OK
}

func evilEventStack(ctx context.Context, logger zerolog.Logger) {
	logger.Error().Stack().Err(errors.New("test")).Msg("with stack") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilEventStackWithCtx(ctx context.Context, logger zerolog.Logger) {
	logger.Error().Ctx(ctx).Stack().Err(errors.New("test")).Msg("with stack and ctx") // OK
}

func evilEventCaller(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Caller().Msg("with caller") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilEventCallerWithCtx(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Ctx(ctx).Caller().Msg("with caller and ctx") // OK
}

// =============================================================================
// EVIL: ALL TERMINATORS
// =============================================================================

func evilAllTerminatorsMsg(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Msg("msg") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilAllTerminatorsMsgf(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Msgf("msgf %s", "test") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilAllTerminatorsMsgFunc(ctx context.Context, logger zerolog.Logger) {
	logger.Info().MsgFunc(func() string { return "msgfunc" }) // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilAllTerminatorsSend(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Str("key", "val").Send() // want `zerolog call chain missing .Ctx\(ctx\)`
}

// =============================================================================
// EVIL: ALL LOG LEVELS
// =============================================================================

func evilAllLevelsTrace(ctx context.Context, logger zerolog.Logger) {
	logger.Trace().Msg("trace") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilAllLevelsDebug(ctx context.Context, logger zerolog.Logger) {
	logger.Debug().Msg("debug") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilAllLevelsInfo(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Msg("info") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilAllLevelsWarn(ctx context.Context, logger zerolog.Logger) {
	logger.Warn().Msg("warn") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilAllLevelsError(ctx context.Context, logger zerolog.Logger) {
	logger.Error().Msg("error") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilAllLevelsFatal(ctx context.Context, logger zerolog.Logger) {
	logger.Fatal().Msg("fatal") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilAllLevelsPanic(ctx context.Context, logger zerolog.Logger) {
	logger.Panic().Msg("panic") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilAllLevelsLog(ctx context.Context, logger zerolog.Logger) {
	logger.Log().Msg("log") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilAllLevelsWithLevel(ctx context.Context, logger zerolog.Logger) {
	logger.WithLevel(zerolog.InfoLevel).Msg("withlevel") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// =============================================================================
// EVIL: GLOBAL LOGGER VARIATIONS
// =============================================================================

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

// =============================================================================
// EVIL: MIXING zerolog.Ctx() WITH TRANSFORMATIONS
// =============================================================================

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

// =============================================================================
// EVIL: POINTER VS VALUE RECEIVER CONFUSION
// =============================================================================

func evilPointerLogger(ctx context.Context, logger *zerolog.Logger) {
	logger.Info().Msg("pointer logger") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilPointerLoggerWithCtx(ctx context.Context, logger *zerolog.Logger) {
	logger.Info().Ctx(ctx).Msg("pointer logger with ctx") // OK
}

func evilPointerLoggerWith(ctx context.Context, logger *zerolog.Logger) {
	derived := logger.With().Str("ptr", "logger").Logger()
	derived.Info().Msg("pointer logger with") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilPointerLoggerWithCtx2(ctx context.Context, logger *zerolog.Logger) {
	derived := logger.With().Ctx(ctx).Logger()
	derived.Info().Msg("pointer logger with ctx") // OK
}

// =============================================================================
// EVIL: TIME AND DURATION FIELDS
// =============================================================================

func evilTimeFields(ctx context.Context, logger zerolog.Logger) {
	logger.Info().
		Time("timestamp", time.Now()).
		Dur("duration", time.Second).
		Msg("with time fields") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilTimeFieldsWithCtx(ctx context.Context, logger zerolog.Logger) {
	logger.Info().
		Ctx(ctx).
		Time("timestamp", time.Now()).
		Dur("duration", time.Second).
		Msg("with time fields and ctx") // OK
}

// =============================================================================
// EVIL: ERROR FIELDS
// =============================================================================

func evilMultipleErrors(ctx context.Context, logger zerolog.Logger) {
	err1 := errors.New("error1")
	err2 := errors.New("error2")
	logger.Error().
		Err(err1).
		AnErr("secondary", err2).
		Errs("all", []error{err1, err2}).
		Msg("multiple errors") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilMultipleErrorsWithCtx(ctx context.Context, logger zerolog.Logger) {
	err1 := errors.New("error1")
	err2 := errors.New("error2")
	logger.Error().
		Ctx(ctx).
		Err(err1).
		AnErr("secondary", err2).
		Errs("all", []error{err1, err2}).
		Msg("multiple errors with ctx") // OK
}

// =============================================================================
// EVIL: BYTES AND HEX
// =============================================================================

func evilBytesAndHex(ctx context.Context, logger zerolog.Logger) {
	data := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	logger.Info().
		Bytes("raw", data).
		Hex("hex", data).
		Msg("bytes and hex") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilBytesAndHexWithCtx(ctx context.Context, logger zerolog.Logger) {
	data := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	logger.Info().
		Ctx(ctx).
		Bytes("raw", data).
		Hex("hex", data).
		Msg("bytes and hex with ctx") // OK
}

// =============================================================================
// EVIL: IMMEDIATE INVOCATION OF CLOSURE
// =============================================================================

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

// =============================================================================
// EVIL: FUNCTION RETURNING LOGGER
// =============================================================================

func getLogger(base zerolog.Logger) zerolog.Logger {
	return base.With().Str("from", "getLogger").Logger()
}

func evilFunctionReturningLogger(ctx context.Context, logger zerolog.Logger) {
	derived := getLogger(logger)
	derived.Info().Msg("from function") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// Note: This is a known limitation - we can't trace across function boundaries
// unless the function itself adds .Ctx(ctx)

// =============================================================================
// EVIL: EVENT Enabled() CHECK
// =============================================================================

func evilEnabledCheck(ctx context.Context, logger zerolog.Logger) {
	event := logger.Debug()
	if event.Enabled() {
		event.Str("debug", "enabled").Msg("conditional debug") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

func evilEnabledCheckWithCtx(ctx context.Context, logger zerolog.Logger) {
	event := logger.Debug().Ctx(ctx)
	if event.Enabled() {
		event.Str("debug", "enabled").Msg("conditional debug with ctx") // OK
	}
}

// =============================================================================
// EVIL: EVENT Discard()
// =============================================================================

func evilDiscardedEvent(ctx context.Context, logger zerolog.Logger) {
	event := logger.Info()
	if false {
		event = event.Discard()
	}
	event.Msg("maybe discarded") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilDiscardedEventWithCtx(ctx context.Context, logger zerolog.Logger) {
	event := logger.Info().Ctx(ctx)
	if false {
		event = event.Discard()
	}
	event.Msg("maybe discarded with ctx") // OK
}

// =============================================================================
// EVIL: MULTIPLE LOG STATEMENTS IN ONE FUNCTION
// =============================================================================

func evilMultipleLogStatements(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Msg("first")                          // want `zerolog call chain missing .Ctx\(ctx\)`
	logger.Debug().Str("key", "val").Msg("second")      // want `zerolog call chain missing .Ctx\(ctx\)`
	logger.Warn().Int("count", 42).Send()               // want `zerolog call chain missing .Ctx\(ctx\)`
	logger.Error().Err(errors.New("oops")).Msg("third") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilMultipleLogStatementsPartialCtx(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Ctx(ctx).Msg("first")                      // OK
	logger.Debug().Str("key", "val").Msg("second")           // want `zerolog call chain missing .Ctx\(ctx\)`
	logger.Warn().Ctx(ctx).Int("count", 42).Send()           // OK
	logger.Error().Err(errors.New("oops")).Msg("third")      // want `zerolog call chain missing .Ctx\(ctx\)`
}

// =============================================================================
// EVIL: INTERFACE FIELD io.Writer
// =============================================================================

func evilWithWriter(ctx context.Context, w io.Writer) {
	logger := zerolog.New(w)
	logger.Info().Msg("with writer") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilWithWriterAndCtx(ctx context.Context, w io.Writer) {
	logger := zerolog.New(w)
	logger.Info().Ctx(ctx).Msg("with writer and ctx") // OK
}

// =============================================================================
// EVIL: Context CHAIN METHODS (Stack, Caller, Timestamp, etc.)
// =============================================================================

func evilContextChainStack(ctx context.Context, logger zerolog.Logger) {
	derived := logger.With().Stack().Str("with", "stack").Logger()
	derived.Info().Msg("context with stack") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilContextChainStackWithCtx(ctx context.Context, logger zerolog.Logger) {
	derived := logger.With().Stack().Ctx(ctx).Logger()
	derived.Info().Msg("context with stack and ctx") // OK
}

func evilContextChainCaller(ctx context.Context, logger zerolog.Logger) {
	derived := logger.With().Caller().Logger()
	derived.Info().Msg("context with caller") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilContextChainCallerWithCtx(ctx context.Context, logger zerolog.Logger) {
	derived := logger.With().Caller().Ctx(ctx).Logger()
	derived.Info().Msg("context with caller and ctx") // OK
}

func evilContextChainTimestamp(ctx context.Context, logger zerolog.Logger) {
	derived := logger.With().Timestamp().Logger()
	derived.Info().Msg("context with timestamp") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilContextChainTimestampWithCtx(ctx context.Context, logger zerolog.Logger) {
	derived := logger.With().Timestamp().Ctx(ctx).Logger()
	derived.Info().Msg("context with timestamp and ctx") // OK
}

// =============================================================================
// EVIL: REASSIGNMENT OF SAME VARIABLE
// =============================================================================

func evilReassignEvent(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	e = e.Str("a", "1")
	e = e.Str("b", "2")
	e = e.Str("c", "3")
	e.Msg("reassigned event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilReassignEventWithCtx(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info().Ctx(ctx)
	e = e.Str("a", "1")
	e = e.Str("b", "2")
	e = e.Str("c", "3")
	e.Msg("reassigned event with ctx") // OK
}

func evilReassignLogger(ctx context.Context, logger zerolog.Logger) {
	l := logger
	l = l.With().Str("a", "1").Logger()
	l = l.With().Str("b", "2").Logger()
	l.Info().Msg("reassigned logger") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilReassignLoggerWithCtx(ctx context.Context, logger zerolog.Logger) {
	l := logger
	l = l.With().Ctx(ctx).Logger()
	l = l.With().Str("a", "1").Logger()
	l.Info().Msg("reassigned logger with ctx") // OK
}

// =============================================================================
// EVIL: ADVANCED NESTED PATTERNS (SHADOWING, ARGUMENT PASSING)
// =============================================================================

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

// =============================================================================
// EVIL: DEFERRED CLOSURES
// =============================================================================

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

// =============================================================================
// EVIL: MIDDLE LAYER INTRODUCES CTX (OUTER HAS NONE)
// =============================================================================

func evilMiddleLayerIntroducesCtx(logger zerolog.Logger) {
	func(ctx context.Context) {
		logger.Info().Ctx(ctx).Msg("middle layer") // OK
		func() {
			logger.Info().Msg("inner without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
		}()
	}(context.Background())
}

func evilMiddleLayerIntroducesCtxGood(logger zerolog.Logger) {
	func(ctx context.Context) {
		func() {
			logger.Info().Ctx(ctx).Msg("inner uses middle ctx") // OK
		}()
	}(context.Background())
}

// Three layers: outer no ctx, middle has ctx, inner has different ctx
func evilMiddleAndInnerBothHaveCtx(logger zerolog.Logger) {
	func(middleCtx context.Context) {
		logger.Info().Ctx(middleCtx).Msg("middle") // OK
		func(innerCtx context.Context) {
			logger.Info().Ctx(innerCtx).Msg("inner") // OK - uses innerCtx
		}(middleCtx)
	}(context.Background())
}

// =============================================================================
// EVIL: INTERLEAVED LAYERS (ctx -> no ctx -> ctx shadowing)
// =============================================================================

func evilInterleavedLayers(outerCtx context.Context, logger zerolog.Logger) {
	func() {
		func(middleCtx context.Context) {
			func() {
				logger.Info().Msg("interleaved") // want `zerolog call chain missing .Ctx\(middleCtx\)`
			}()
		}(outerCtx)
	}()
}

func evilInterleavedLayersGood(outerCtx context.Context, logger zerolog.Logger) {
	func() {
		func(middleCtx context.Context) {
			func() {
				logger.Info().Ctx(middleCtx).Msg("interleaved with ctx") // OK
			}()
		}(outerCtx)
	}()
}

// =============================================================================
// EVIL: METHOD WITH CLOSURE ON STRUCT
// =============================================================================

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

// =============================================================================
// EVIL: MULTIPLE CONTEXT PARAMETERS
// =============================================================================

func evilMultipleCtxParams(ctx1 context.Context, ctx2 context.Context, logger zerolog.Logger) {
	logger.Info().Msg("multiple ctx") // want `zerolog call chain missing .Ctx\(ctx1\)`
}

func evilMultipleCtxParamsGood(ctx1 context.Context, ctx2 context.Context, logger zerolog.Logger) {
	logger.Info().Ctx(ctx1).Msg("uses ctx1") // OK
}

func evilMultipleCtxParamsNested(ctx1 context.Context, ctx2 context.Context, logger zerolog.Logger) {
	func() {
		logger.Info().Msg("nested multiple") // want `zerolog call chain missing .Ctx\(ctx1\)`
	}()
}
