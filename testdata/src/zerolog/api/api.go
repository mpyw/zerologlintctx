// Package api covers the zerolog Event API: levels, field methods, terminators,
// MsgFunc, Enabled and Discard.
package api

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ===== EVENT METHODS THAT RETURN *Event =====

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

// ===== ALL TERMINATORS =====

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

// ===== ALL LOG LEVELS =====

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

// ===== POINTER VS VALUE RECEIVER CONFUSION =====

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

// ===== TIME AND DURATION FIELDS =====

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

// ===== ERROR FIELDS =====

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

// ===== BYTES AND HEX =====

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

// ===== EVENT Enabled() CHECK =====

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

// ===== EVENT Discard() =====

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

// ===== INTERFACE FIELD io.Writer =====

func evilWithWriter(ctx context.Context, w io.Writer) {
	logger := zerolog.New(w)
	logger.Info().Msg("with writer") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilWithWriterAndCtx(ctx context.Context, w io.Writer) {
	logger := zerolog.New(w)
	logger.Info().Ctx(ctx).Msg("with writer and ctx") // OK
}

// ===== Context CHAIN METHODS (Stack, Caller, Timestamp, etc.) =====

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

// ===== VARIADIC SHENANIGANS =====

func badVariadicEvent(ctx context.Context, logger zerolog.Logger) {
	events := []*zerolog.Event{logger.Info(), logger.Warn()}
	processEvents(events...)
}

func processEvents(events ...*zerolog.Event) {
	for _, e := range events {
		e.Msg("variadic") // Can't track ctx here - no context in scope
	}
}

// ===== MsgFunc TERMINATOR =====

func badMsgFuncEdge(ctx context.Context, logger zerolog.Logger) {
	logger.Info().MsgFunc(func() string { return "msgfunc" }) // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodMsgFuncEdgeWithCtx(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Ctx(ctx).MsgFunc(func() string { return "msgfunc" }) // OK
}

// ===== DISCARD EVENT =====

func badDiscardEvent(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	_ = e.Str("key", "value") // Discard result
	// But e itself doesn't have the Str call applied (it's immutable-ish but returns new)
	e.Msg("discarded chain") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== LONG CHAIN WITH MIXED METHODS =====

// Test case: Long chain with mixed Event methods
func badLongMixedChain(ctx context.Context, logger zerolog.Logger) {
	logger.Info().
		Str("a", "1").
		Int("b", 2).
		Bool("c", true).
		Float64("d", 3.14).
		Msg("long mixed chain") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodLongMixedChainWithCtx(ctx context.Context, logger zerolog.Logger) {
	logger.Info().
		Ctx(ctx).
		Str("a", "1").
		Int("b", 2).
		Bool("c", true).
		Float64("d", 3.14).
		Msg("long mixed chain with ctx") // OK
}

// ===== io.Writer PATTERNS =====

func badWriterLogger(ctx context.Context, w io.Writer) {
	l := zerolog.New(w)
	l.Info().Msg("writer logger") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodWriterLoggerWithCtx(ctx context.Context, w io.Writer) {
	l := zerolog.New(w)
	l.Info().Ctx(ctx).Msg("writer logger with ctx") // OK
}

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
