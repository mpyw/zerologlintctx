// Package directlog covers direct logging (Print, Printf, Println, Write),
// which bypasses the Event chain.
package directlog

import (
	"context"
	"io"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ===== DIRECT LOGGING (bypasses Event chain) =====

func badLoggerPrint(ctx context.Context, logger *zerolog.Logger) {
	logger.Print("direct print") // want `zerolog direct logging bypasses context`
}

func badLoggerPrintf(ctx context.Context, logger *zerolog.Logger) {
	logger.Printf("direct %s", "printf") // want `zerolog direct logging bypasses context`
}

func badLoggerPrintln(ctx context.Context, logger *zerolog.Logger) {
	logger.Println("direct println") // want `zerolog direct logging bypasses context`
}

func badNewLoggerPrint(ctx context.Context) {
	l := zerolog.New(nil)
	l.Print("zerolog.New print") // want `zerolog direct logging bypasses context`
}

func badNopLoggerPrint(ctx context.Context) {
	l := zerolog.Nop()
	l.Print("zerolog.Nop print") // want `zerolog direct logging bypasses context`
}

func badGlobalPrint(ctx context.Context) {
	log.Print("global print") // want `zerolog direct logging bypasses context`
}

func badGlobalPrintf(ctx context.Context) {
	log.Printf("global %s", "printf") // want `zerolog direct logging bypasses context`
}

// Direct logging run by go or defer is reported as the plain call is (#78).
func badDeferredLoggerPrint(ctx context.Context, logger zerolog.Logger) {
	defer logger.Print("deferred print") // want `zerolog direct logging bypasses context`
}

func badGoGlobalPrint(ctx context.Context) {
	go log.Print("go print") // want `zerolog direct logging bypasses context`
}

// Direct logging through a method value is reported as the plain call is (#79).
func badLoggerPrintfMethodValue(ctx context.Context, logger zerolog.Logger) {
	p := logger.Printf
	p("bound %d", 1) // want `zerolog direct logging bypasses context`
}

func badLoggerPrintMethodExpression(ctx context.Context, logger *zerolog.Logger) {
	p := (*zerolog.Logger).Print
	p(logger, "method expression") // want `zerolog direct logging bypasses context`
}

// Logger.Print builds its event from the Logger, as Logger.Info does, so a
// Logger that carries the context passes it on (#196).
func goodCtxLoggerPrint(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(ctx).Logger()
	l.Print("ctx logger print")          // OK
	l.Printf("ctx logger %s", "printf")  // OK
	l.Println("ctx logger println")      // OK
	defer l.Print("deferred ctx logger") // OK
}

func goodCtxLoggerPrintMethodValue(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(ctx).Logger()
	p := l.Printf
	p("bound %d", 1) // OK
}

func goodLogCtxPrint(ctx context.Context) {
	log.Ctx(ctx).Print("logger from ctx") // OK - the logger stored in ctx
}

func goodCtxLoggerPrintReassigned(ctx context.Context, l zerolog.Logger) {
	l = l.With().Ctx(ctx).Logger()
	l.Print("reassigned with ctx") // OK
}

func badCtxLoggerPrintOnOneBranch(ctx context.Context, l zerolog.Logger, cond bool) {
	if cond {
		l = l.With().Ctx(ctx).Logger()
	}
	l.Print("ctx on one branch") // want `zerolog direct logging bypasses context`
}

func badLoggerWithoutCtxPrint(ctx context.Context, base zerolog.Logger) {
	l := base.With().Str("k", "v").Logger()
	l.Print("no ctx") // want `zerolog direct logging bypasses context`
}

// Logger.Write logs with l.Log() ... Msg, so it is direct logging, as Print
// is (#235).
func badLoggerWrite(ctx context.Context, logger zerolog.Logger) {
	_, _ = logger.Write([]byte("direct write\n")) // want `zerolog direct logging bypasses context`
}

func badLoggerPointerWrite(ctx context.Context, logger *zerolog.Logger) {
	logger.Write([]byte("direct write\n")) // want `zerolog direct logging bypasses context`
}

func badLoggerWriteMethodValue(ctx context.Context, logger zerolog.Logger) {
	w := logger.Write
	w([]byte("bound write\n")) // want `zerolog direct logging bypasses context`
}

func goodCtxLoggerWrite(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(ctx).Logger()
	_, _ = l.Write([]byte("ctx logger write\n")) // OK
}

func goodCtxLoggerWriteMethodValue(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(ctx).Logger()
	w := l.Write
	w([]byte("bound write\n")) // OK
}

func goodLogCtxWrite(ctx context.Context) {
	log.Ctx(ctx).Write([]byte("logger from ctx\n")) // OK - the logger stored in ctx
}

// LIMITATION: a use of the Logger through io.Writer is not followed.
func limitationLoggerAsWriter(ctx context.Context, logger zerolog.Logger) {
	var w io.Writer = logger
	w.Write([]byte("through io.Writer\n")) // not reported
}

// The global Logger of log.Print is not traced, so it is reported even when
// it was given the context.
func badGlobalPrintAfterCtx(ctx context.Context) {
	log.Logger = log.Logger.With().Ctx(ctx).Logger()
	log.Print("global with ctx") // want `zerolog direct logging bypasses context`
}

// These should NOT be reported (void but not logging)
func goodSetGlobalLevel(ctx context.Context) {
	zerolog.SetGlobalLevel(zerolog.DebugLevel) // OK - not a logging call
}

func goodDisableSampling(ctx context.Context) {
	zerolog.DisableSampling(true) // OK - not a logging call
}

func goodUpdateContext(ctx context.Context, logger *zerolog.Logger) {
	logger.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("key", "value")
	}) // OK - configuration, not logging
}
