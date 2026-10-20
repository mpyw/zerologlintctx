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
// log.Ctx(ctx).Err() PATTERNS (issue #8)
// =============================================================================

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
		derived.Info().Msg("goroutine derived with ctx") // OK - ctx set via With().Ctx(ctx)
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

// =============================================================================
// DIRECT LOGGING (bypasses Event chain)
// =============================================================================

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

// =============================================================================
// UPDATECONTEXT (changes the Logger in place)
// =============================================================================

// UpdateContext keeps only the fields its func writes. What Context.Ctx sets
// in the func is dropped, so the Logger has no context after it (#152).
func badUpdateContextCtxDropped(ctx context.Context, l zerolog.Logger) {
	l.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Ctx(ctx)
	})
	l.Info().Msg("after update") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badUpdateContextCtxDroppedOnPointer(ctx context.Context, l *zerolog.Logger) {
	l.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("k", "v").Ctx(ctx)
	})
	l.Info().Msg("after update on pointer") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badUpdateContextCtxDroppedThenWith(ctx context.Context, l zerolog.Logger) {
	l.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Ctx(ctx)
	})
	derived := l.With().Str("k", "v").Logger()
	derived.Info().Msg("derived after update") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodUpdateContextKeepsCtx(ctx context.Context, l zerolog.Logger) {
	l = l.With().Ctx(ctx).Logger()
	l.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("k", "v")
	})
	l.Info().Msg("ctx before update") // OK - UpdateContext keeps the ctx the Logger had
}

func badUpdateContextWithoutCtx(ctx context.Context, l zerolog.Logger) {
	l.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("k", "v")
	})
	l.Info().Msg("after update without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badLogBeforeUpdateContext(ctx context.Context, l zerolog.Logger) {
	l.Info().Msg("before update") // want `zerolog call chain missing .Ctx\(ctx\)`
	l.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Ctx(ctx)
	})
}

func badUpdateContextInBranch(ctx context.Context, l zerolog.Logger, cond bool) {
	if cond {
		l.UpdateContext(func(c zerolog.Context) zerolog.Context {
			return c.Ctx(ctx)
		})
	}
	l.Info().Msg("update on one path") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badUpdateContextOnOneReturn(ctx context.Context, l zerolog.Logger, cond bool) {
	l.UpdateContext(func(c zerolog.Context) zerolog.Context {
		if cond {
			return c
		}
		return c.Ctx(ctx)
	})
	l.Info().Msg("ctx on one return") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badUpdateContextThenReplaced(ctx context.Context, l zerolog.Logger, other zerolog.Logger) {
	l.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Ctx(ctx)
	})
	l = other
	l.Info().Msg("replaced after update") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badPointerParamWithoutUpdate(ctx context.Context, l *zerolog.Logger) {
	l.Info().Msg("pointer param") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func addServiceKey(c zerolog.Context) zerolog.Context {
	return c.Str("service", "api")
}

func badUpdateContextWithDeclaredFunc(ctx context.Context, l zerolog.Logger) {
	l.UpdateContext(addServiceKey)
	l.Info().Msg("declared func without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// The func is not known statically, so it cannot count.
func badUpdateContextWithFuncValue(ctx context.Context, l zerolog.Logger, update func(zerolog.Context) zerolog.Context) {
	l.UpdateContext(update)
	l.Info().Msg("func value") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// =============================================================================
// LOGGER IN A FIELD OR ELEMENT
// =============================================================================

// Logger.Info has a pointer receiver, so its receiver is the address of the
// field or element itself, not a load from it (#97).

type fieldLoggerHolder struct {
	log zerolog.Logger
}

type embeddedLoggerHolder struct {
	zerolog.Logger
}

func goodLoggerInFieldLiteral(ctx context.Context, l zerolog.Logger) {
	s := fieldLoggerHolder{log: l.With().Ctx(ctx).Logger()}
	s.log.Info().Msg("field literal") // OK
}

func goodLoggerInPointerFieldLiteral(ctx context.Context, l zerolog.Logger) {
	s := &fieldLoggerHolder{log: l.With().Ctx(ctx).Logger()}
	s.log.Info().Msg("pointer field literal") // OK
}

func goodLoggerAssignedToField(ctx context.Context, l zerolog.Logger) {
	var s fieldLoggerHolder
	s.log = l.With().Ctx(ctx).Logger()
	s.log.Info().Msg("assigned field") // OK
}

func goodLoggerInArrayElement(ctx context.Context, l zerolog.Logger) {
	var arr [1]zerolog.Logger
	arr[0] = l.With().Ctx(ctx).Logger()
	arr[0].Info().Msg("array element") // OK
}

func goodLoggerInEmbeddedField(ctx context.Context, l zerolog.Logger) {
	e := embeddedLoggerHolder{l.With().Ctx(ctx).Logger()}
	e.Info().Msg("embedded field") // OK
}

func badLoggerInFieldLiteral(ctx context.Context, l zerolog.Logger) {
	s := fieldLoggerHolder{log: l}
	s.log.Info().Msg("field literal") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badLoggerAssignedToFieldInBranch(ctx context.Context, l zerolog.Logger, cond bool) {
	s := fieldLoggerHolder{log: l}
	if cond {
		s.log = l.With().Ctx(ctx).Logger()
	}
	s.log.Info().Msg("one branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badLoggerInArrayElement(ctx context.Context, l zerolog.Logger) {
	var arr [2]zerolog.Logger
	arr[0] = l.With().Ctx(ctx).Logger()
	arr[1] = l
	arr[1].Info().Msg("other element") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badLoggerInEmbeddedField(ctx context.Context, l zerolog.Logger) {
	e := embeddedLoggerHolder{l}
	e.Info().Msg("embedded field") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A field takes updates too, as a local variable does (#98).

func (s *fieldLoggerHolder) goodFieldUpdatedWithCtx(ctx context.Context) {
	s.log = s.log.With().Ctx(ctx).Logger()
	s.log.Info().Msg("field updated") // OK
}

func (s *fieldLoggerHolder) badFieldUpdateContextCtxDropped(ctx context.Context) {
	s.log.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Ctx(ctx)
	})
	s.log.Info().Msg("field UpdateContext") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodLocalFieldUpdatedWithCtx(ctx context.Context, l zerolog.Logger) {
	s := fieldLoggerHolder{log: l}
	s.log = s.log.With().Ctx(ctx).Logger()
	s.log.Info().Msg("local field updated") // OK
}

func (s *fieldLoggerHolder) badFieldUpdatedWithoutCtx(ctx context.Context) {
	s.log = s.log.With().Str("k", "v").Logger()
	s.log.Info().Msg("field updated") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func (s *fieldLoggerHolder) badFieldReadBeforeUpdate(ctx context.Context) {
	s.log.Info().Msg("before update") // want `zerolog call chain missing .Ctx\(ctx\)`
	s.log = s.log.With().Ctx(ctx).Logger()
}

func (s *fieldLoggerHolder) badFieldUpdatedInBranch(ctx context.Context, cond bool) {
	if cond {
		s.log = s.log.With().Ctx(ctx).Logger()
	}
	s.log.Info().Msg("one branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

var globalLoggerHolder fieldLoggerHolder

// The field of a global can change anywhere, so its updates do not count.
func badGlobalFieldUpdated(ctx context.Context) {
	globalLoggerHolder.log = globalLoggerHolder.log.With().Ctx(ctx).Logger()
	globalLoggerHolder.log.Info().Msg("global field") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A closure that stores to a variable it captures, and then reads it, reads
// what it stored, as a load of a captured event does.
func goodClosureStoresCapturedLogger(ctx context.Context, l zerolog.Logger) {
	var captured zerolog.Logger
	func() {
		captured = l.With().Ctx(ctx).Logger()
		captured.Info().Msg("stored in closure") // OK
	}()
	_ = captured
}

// A store in the closure on another path does not reach the read, which
// sees the value the closure was made with.
func goodClosureStoreOnOtherPath(ctx context.Context, l zerolog.Logger, cond bool) {
	captured := l.With().Ctx(ctx).Logger()
	func() {
		if cond {
			captured = l
			return
		}
		captured.Info().Msg("value the closure was made with") // OK
	}()
}

func badClosureStoreWithoutCtx(ctx context.Context, l zerolog.Logger, cond bool) {
	captured := l.With().Ctx(ctx).Logger()
	func() {
		if cond {
			captured = l
		}
		captured.Info().Msg("stored on one path") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

func newFieldLoggerHolderPtr() *fieldLoggerHolder {
	return &fieldLoggerHolder{}
}

// The holder a call returns may be shared, but the stores in this function
// are what it holds when nothing else is known.
func goodFieldOfReturnedHolderStoredWithCtx(ctx context.Context, l zerolog.Logger) {
	s := newFieldLoggerHolderPtr()
	s.log = l.With().Ctx(ctx).Logger()
	s.log.Info().Msg("field of returned holder") // OK
}

func goodReturnedHolderStoredWithCtx(ctx context.Context, l zerolog.Logger) {
	s := newFieldLoggerHolderPtr()
	*s = fieldLoggerHolder{log: l.With().Ctx(ctx).Logger()}
	s.log.Info().Msg("returned holder") // OK
}

func newFieldLoggerHolder(l zerolog.Logger) fieldLoggerHolder {
	return fieldLoggerHolder{log: l}
}

func badFieldOfZeroedHolder(ctx context.Context, l zerolog.Logger) {
	s := fieldLoggerHolder{log: l.With().Ctx(ctx).Logger()}
	s = fieldLoggerHolder{}
	s.log.Info().Msg("zeroed") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFieldOfHolderFromCall(ctx context.Context, l zerolog.Logger) {
	s := fieldLoggerHolder{log: l.With().Ctx(ctx).Logger()}
	s = newFieldLoggerHolder(l)
	s.log.Info().Msg("from call") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFieldOfHolderCopiedFromGlobal(ctx context.Context) {
	s := globalLoggerHolder
	s.log.Info().Msg("copied from global") // want `zerolog call chain missing .Ctx\(ctx\)`
}

type twoLoggerHolder struct {
	a, b zerolog.Logger
}

// The field a Phi picks is not known, so each field takes every store that
// reaches any read of it.
func goodEitherFieldWithCtx(ctx context.Context, l zerolog.Logger, cond bool) {
	s := twoLoggerHolder{a: l.With().Ctx(ctx).Logger(), b: l.With().Ctx(ctx).Logger()}
	p := &s.a
	if cond {
		p = &s.b
	}
	p.Info().Msg("either field") // OK
}

func badEitherFieldOneWithoutCtx(ctx context.Context, l zerolog.Logger, cond bool) {
	s := twoLoggerHolder{a: l.With().Ctx(ctx).Logger(), b: l}
	p := &s.a
	if cond {
		p = &s.b
	}
	p.Info().Msg("either field") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEitherPointerParamUpdated(ctx context.Context, l1, l2 *zerolog.Logger, cond bool) {
	*l1 = l1.With().Ctx(ctx).Logger()
	*l2 = l2.With().Ctx(ctx).Logger()
	l1.Info().Msg("after update") // OK
	p := l1
	if cond {
		p = l2
	}
	p.Info().Msg("either pointer") // OK
}

func badFieldPointerCapturedThenReplaced(ctx context.Context, l zerolog.Logger) {
	s := fieldLoggerHolder{log: l.With().Ctx(ctx).Logger()}
	p := &s.log
	f := func() { p.Info().Msg("captured field") } // want `zerolog call chain missing .Ctx\(ctx\)`
	s = newFieldLoggerHolder(l)
	f()
}

var (
	globalEvent        *zerolog.Event
	globalEventHolderA eventHolder
	globalEventHolderB eventHolder
)

func badGlobalEvent(ctx context.Context) {
	globalEvent.Msg("global event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// Copies between two globals lead back to each other, and end there.
func badGlobalHoldersCopiedBothWays(ctx context.Context) {
	globalEventHolderA = globalEventHolderB
	globalEventHolderB = globalEventHolderA
	globalEventHolderB.event.Msg("b") // want `zerolog call chain missing .Ctx\(ctx\)`
	globalEventHolderA.event.Msg("a") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// =============================================================================
// METHOD VALUES AND METHOD EXPRESSIONS IN THE CHAIN
// =============================================================================

// The receiver of a method value is what it is bound to, and that of a
// method expression is its first argument (#101).

func goodInfoMethodValue(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(ctx).Logger()
	info := l.Info
	info().Msg("method value") // OK
}

func goodInfoMethodExpression(ctx context.Context, base zerolog.Logger) {
	info := (*zerolog.Logger).Info
	l := base.With().Ctx(ctx).Logger()
	info(&l).Msg("method expression") // OK
}

func goodWithMethodValue(ctx context.Context, base zerolog.Logger) {
	with := base.With
	l := with().Ctx(ctx).Logger()
	l.Info().Msg("With method value") // OK
}

func goodEventMethodValue(ctx context.Context, base zerolog.Logger) {
	str := base.Info().Ctx(ctx).Str
	str("k", "v").Msg("Str method value") // OK
}

func badInfoMethodValue(ctx context.Context, base zerolog.Logger) {
	info := base.Info
	info().Msg("method value") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badInfoMethodExpression(ctx context.Context, base zerolog.Logger) {
	info := (*zerolog.Logger).Info
	info(&base).Msg("method expression") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventMethodValue(ctx context.Context, base zerolog.Logger) {
	str := base.Info().Str
	str("k", "v").Msg("Str method value") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// =============================================================================
// CONVERSIONS THROUGH A DEFINED TYPE
// =============================================================================

// A conversion between types with the same underlying type keeps the value
// (#104).

type definedLogger zerolog.Logger

func goodLoggerThroughDefinedType(ctx context.Context, base zerolog.Logger) {
	d := definedLogger(base.With().Ctx(ctx).Logger())
	l := zerolog.Logger(d)
	l.Info().Msg("through defined type") // OK
}

func badLoggerThroughDefinedType(ctx context.Context, base zerolog.Logger) {
	d := definedLogger(base)
	l := zerolog.Logger(d)
	l.Info().Msg("through defined type") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A func value is not known statically, so its first argument is traced as
// if it were the receiver.
func goodEventThroughFuncValue(ctx context.Context, base zerolog.Logger, decorate func(*zerolog.Event) *zerolog.Event) {
	decorate(base.Info().Ctx(ctx)).Msg("func value") // OK
}

func badEventThroughFuncValue(ctx context.Context, base zerolog.Logger, decorate func(*zerolog.Event) *zerolog.Event) {
	decorate(base.Info()).Msg("func value") // want `zerolog call chain missing .Ctx\(ctx\)`
}
