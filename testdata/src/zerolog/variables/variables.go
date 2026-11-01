// Package variables covers events and loggers held in local variables: reassignment,
// shadowing, interfaces, named and multiple results.
package variables

import (
	"context"

	"github.com/rs/zerolog"
)

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

func badNilEvent(ctx context.Context) {
	var e *zerolog.Event
	e.Msg("nil event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== REASSIGNMENT OF SAME VARIABLE =====

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

// ===== METHOD CHAINING WITH INTERMEDIATE VARIABLES =====

func badPingPongVariables(ctx context.Context, logger zerolog.Logger) {
	a := logger.Info()
	b := a.Str("key1", "val1")
	c := b.Str("key2", "val2")
	d := c.Int("num", 42)
	d.Msg("ping pong") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badPingPongWithReassignment(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	e = e.Str("key1", "val1")
	e = e.Str("key2", "val2")
	e = e.Int("num", 42)
	e.Msg("reassigned chain") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodPingPongWithCtxInMiddle(ctx context.Context, logger zerolog.Logger) {
	a := logger.Info()
	b := a.Str("key1", "val1")
	c := b.Ctx(ctx) // ctx added in middle
	d := c.Int("num", 42)
	d.Msg("ctx in middle") // OK
}

// ===== TYPE ASSERTION CHAOS =====

func badEventViaInterface(ctx context.Context, logger zerolog.Logger) {
	var i interface{} = logger.Info()
	e := i.(*zerolog.Event)
	e.Msg("via interface") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventViaInterfaceWithCtx(ctx context.Context, logger zerolog.Logger) {
	var i interface{} = logger.Info().Ctx(ctx)
	e := i.(*zerolog.Event)
	e.Msg("via interface with ctx") // OK - TypeAssert tracking
}

// LIMITATION (false negative): a value used through an interface it is
// assigned to is not followed. The terminator is a call of the interface's
// method, not of zerolog's.
func limitationEventThroughInterfaceMethod(ctx context.Context, logger zerolog.Logger) {
	var w interface{ Msg(string) } = logger.Info()
	w.Msg("through an interface method") // LIMITATION: should report but doesn't
}

// ===== NAMED RETURN VALUES =====

func badNamedReturn(ctx context.Context, logger zerolog.Logger) {
	e := getNamedEvent(logger)
	e.Msg("named return") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func getNamedEvent(logger zerolog.Logger) (event *zerolog.Event) {
	event = logger.Info()
	return
}

// ===== INTERFACE ASSIGNMENT =====

type eventGetter interface {
	GetEvent() *zerolog.Event
}

type eventGetterImpl struct {
	e *zerolog.Event
}

func (h *eventGetterImpl) GetEvent() *zerolog.Event {
	return h.e
}

// Interface method call: Still reports because the terminator is in a function with context.
func badInterfaceMethod(ctx context.Context, logger zerolog.Logger) {
	var h eventGetter = &eventGetterImpl{e: logger.Info()}
	e := h.GetEvent()
	e.Msg("from interface") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== NAMED RETURN =====

func namedReturn(logger zerolog.Logger) (e *zerolog.Event) {
	e = logger.Info()
	return
}

// Named return: Still reports because the terminator is in a function with context.
func badNamedReturnHelper(ctx context.Context, logger zerolog.Logger) {
	e := namedReturn(logger)
	e.Msg("from named return") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== BLANK IDENTIFIER =====

func badBlankIdentifier(ctx context.Context, logger zerolog.Logger) {
	_ = logger.Info().Ctx(ctx)
	logger.Warn().Msg("not the one with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== REASSIGNMENT SHADOWS =====

func badReassignmentShadows(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info().Ctx(ctx)
	_ = e // use e
	e = logger.Warn()
	e.Msg("shadowed event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== MULTI-VALUE RETURN =====

func multiReturn(logger zerolog.Logger) (*zerolog.Event, error) {
	return logger.Info(), nil
}

// Multi-value return: Still reports because the terminator is in a function with context.
func badMultiReturn(ctx context.Context, logger zerolog.Logger) {
	e, _ := multiReturn(logger)
	e.Msg("from multi return") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== DYNAMIC DISPATCH (INTERFACE) =====

// Test case: Interface method call (no static callee)
// The analyzer should trace through the receiver
type EventProvider interface {
	GetEvent() *zerolog.Event
}

type eventProviderImpl struct {
	e *zerolog.Event
}

func (p *eventProviderImpl) GetEvent() *zerolog.Event {
	return p.e
}

func badDynamicDispatch(ctx context.Context, logger zerolog.Logger) {
	var provider EventProvider = &eventProviderImpl{e: logger.Info()}
	e := provider.GetEvent()
	e.Msg("dynamic dispatch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== REASSIGNMENT PATTERNS =====

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

// ===== SHADOWING =====

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
