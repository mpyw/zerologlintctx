// Package zerolog contains test fixtures for the zerolog context propagation checker.
// This file covers Logger transformation patterns - Level(), Output(), With(), embedded
// loggers, type aliases, goroutine usage, and interface-based logger providers.
// See basic.go for simple cases, evil.go for general edge cases,
// and evil_ssa.go for SSA-specific limitations.
package zerolog

import (
	"context"
	"io"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// =============================================================================
// LOGGER TRANSFORMATION TRACKING
// =============================================================================

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

// =============================================================================
// zerolog.Ctx() VARIATIONS
// =============================================================================

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

// =============================================================================
// log.Ctx() VARIATIONS (global package)
// =============================================================================

func goodLogCtxDirect(ctx context.Context) {
	log.Ctx(ctx).Info().Msg("log.Ctx direct") // OK
}

func goodLogCtxWithWith(ctx context.Context) {
	l := log.Ctx(ctx).With().Str("pkg", "log").Logger()
	l.Info().Msg("log.Ctx with with") // OK
}

// =============================================================================
// COMPLEX With().Logger() CHAINS
// =============================================================================

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

// =============================================================================
// GOROUTINE USAGE
// =============================================================================

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
		// LIMITATION: FreeVar tracking through MakeClosure not working in test stubs
		// In production, this would be OK since derived has ctx
		derived.Info().Msg("goroutine derived with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

// =============================================================================
// REASSIGNMENT PATTERNS
// =============================================================================

func badLoggerReassignment(ctx context.Context, logger zerolog.Logger) {
	l := logger
	l = l.Level(zerolog.InfoLevel)
	l.Info().Msg("reassigned logger") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventReassignmentLoop(ctx context.Context, logger zerolog.Logger, fields []string) {
	e := logger.Info()
	for _, f := range fields {
		e = e.Str(f, f)
	}
	e.Msg("loop reassigned") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventReassignmentLoopWithCtx(ctx context.Context, logger zerolog.Logger, fields []string) {
	e := logger.Info().Ctx(ctx)
	for _, f := range fields {
		e = e.Str(f, f)
	}
	e.Msg("loop reassigned with ctx") // OK
}

// =============================================================================
// INTERFACE PATTERNS
// =============================================================================

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

// =============================================================================
// EMBEDDED STRUCT
// =============================================================================

type Service struct {
	zerolog.Logger
}

func badEmbeddedLogger(ctx context.Context, svc *Service) {
	svc.Info().Msg("embedded") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEmbeddedLoggerWithCtx(ctx context.Context, svc *Service) {
	svc.Info().Ctx(ctx).Msg("embedded with ctx") // OK
}

func badEmbeddedLoggerDerived(ctx context.Context, svc *Service) {
	l := svc.With().Str("svc", "test").Logger()
	l.Info().Msg("embedded derived") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEmbeddedLoggerDerivedCtx(ctx context.Context, svc *Service) {
	l := svc.With().Ctx(ctx).Logger()
	l.Info().Msg("embedded derived with ctx") // OK
}

// =============================================================================
// TYPE ALIAS
// =============================================================================

type MyLogger = zerolog.Logger
type MyEvent = zerolog.Event

func badTypeAlias(ctx context.Context, logger MyLogger) {
	logger.Info().Msg("type alias") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodTypeAliasWithCtx(ctx context.Context, logger MyLogger) {
	logger.Info().Ctx(ctx).Msg("type alias with ctx") // OK
}

// =============================================================================
// io.Writer PATTERNS
// =============================================================================

func badWriterLogger(ctx context.Context, w io.Writer) {
	l := zerolog.New(w)
	l.Info().Msg("writer logger") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodWriterLoggerWithCtx(ctx context.Context, w io.Writer) {
	l := zerolog.New(w)
	l.Info().Ctx(ctx).Msg("writer logger with ctx") // OK
}

// =============================================================================
// MULTIPLE LOGGERS IN SAME FUNCTION
// =============================================================================

func badMultipleLoggers(ctx context.Context, l1, l2 zerolog.Logger) {
	l1.Info().Msg("logger 1") // want `zerolog call chain missing .Ctx\(ctx\)`
	l2.Warn().Msg("logger 2") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badMixedLoggers(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Msg("param logger")        // want `zerolog call chain missing .Ctx\(ctx\)`
	log.Info().Msg("global logger")          // want `zerolog call chain missing .Ctx\(ctx\)`
	zerolog.New(os.Stdout).Info().Msg("new") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodMixedLoggersWithCtx(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Ctx(ctx).Msg("param logger with ctx") // OK
	log.Ctx(ctx).Info().Msg("global ctx logger")        // OK
	zerolog.Ctx(ctx).Info().Msg("zerolog ctx")          // OK
}

// =============================================================================
// EARLY RETURN PATTERNS
// =============================================================================

func badEarlyReturn(ctx context.Context, logger zerolog.Logger, shouldReturn bool) {
	e := logger.Info()
	if shouldReturn {
		e.Msg("early return") // want `zerolog call chain missing .Ctx\(ctx\)`
		return
	}
	e.Str("continued", "true").Msg("continued") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEarlyReturnWithCtx(ctx context.Context, logger zerolog.Logger, shouldReturn bool) {
	e := logger.Info().Ctx(ctx)
	if shouldReturn {
		e.Msg("early return with ctx") // OK
		return
	}
	e.Str("continued", "true").Msg("continued with ctx") // OK
}

// =============================================================================
// SHADOWING
// =============================================================================

func badShadowedLogger(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Msg("outer") // want `zerolog call chain missing .Ctx\(ctx\)`
	{
		logger := zerolog.Nop()
		logger.Info().Msg("shadowed") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	logger.Info().Msg("outer again") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodShadowedLoggerWithCtx(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Ctx(ctx).Msg("outer") // OK
	{
		logger := zerolog.Ctx(ctx)
		logger.Info().Msg("shadowed with ctx") // OK
	}
	logger.Info().Ctx(ctx).Msg("outer again") // OK
}

// =============================================================================
// LABELED STATEMENTS
// =============================================================================

func badLabeledLoop(ctx context.Context, logger zerolog.Logger) {
outer:
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if j == 1 {
				logger.Info().Int("i", i).Int("j", j).Msg("break outer") // want `zerolog call chain missing .Ctx\(ctx\)`
				break outer
			}
		}
	}
}

// =============================================================================
// nil LOGGER
// =============================================================================

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
