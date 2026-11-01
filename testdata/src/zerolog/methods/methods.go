// Package methods covers method values and method expressions in the chain.
package methods

import (
	"context"

	"github.com/rs/zerolog"
)

// ===== METHOD VALUE =====

// Method values extract the method from the receiver, creating a MakeClosure.
// The analyzer traces through the closure bindings to find the receiver.
func badMethodValue(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	msg := e.Msg
	msg("method value") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodMethodValueWithCtx(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info().Ctx(ctx)
	msg := e.Msg
	msg("method value with ctx") // OK - ctx set via .Ctx(ctx)
}

// A method expression passes the receiver as the first argument (#79).
func badMethodExpression(ctx context.Context, logger zerolog.Logger) {
	msg := (*zerolog.Event).Msg
	msg(logger.Info(), "method expression") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodMethodExpressionWithCtx(ctx context.Context, logger zerolog.Logger) {
	msg := (*zerolog.Event).Msg
	msg(logger.Info().Ctx(ctx), "method expression with ctx") // OK - ctx set via .Ctx(ctx)
}

// ===== METHOD VALUES AND METHOD EXPRESSIONS IN THE CHAIN =====

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

// A method expression read from a map is not known statically. Its first
// argument is traced as what the event is made from, and that Logger is made
// from a Context with Ctx.
func goodInfoMethodExpressionFromMap(ctx context.Context, base zerolog.Logger, level string) {
	levels := map[string]func(*zerolog.Logger) *zerolog.Event{"info": (*zerolog.Logger).Info}
	l := base.With().Ctx(ctx).Logger()
	levels[level](&l).Msg("method expression from a map") // OK
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

// ===== METHOD VALUES AND METHOD EXPRESSIONS A CLOSURE CAPTURES =====

// A closure loads a method value from the variable it captures. The receiver
// is what the method value is bound to, read where the closure runs.

func goodInfoMethodValueInIIFE(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(ctx).Logger()
	info := l.Info
	func() { info().Msg("method value in an IIFE") }() // OK
}

func goodInfoMethodValueInClosureVar(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(ctx).Logger()
	info := l.Info
	f := func() { info().Msg("method value in a closure") } // OK
	f()
}

func goodInfoMethodValueInGo(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(ctx).Logger()
	info := l.Info
	go func() { info().Msg("method value in a goroutine") }() // OK
}

func goodInfoMethodValueInNestedIIFE(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(ctx).Logger()
	info := l.Info
	func() {
		func() { info().Msg("method value in a nested IIFE") }() // OK
	}()
}

func goodInfoMethodValueOfPointerParamInIIFE(ctx context.Context, base *zerolog.Logger) {
	*base = base.With().Ctx(ctx).Logger()
	info := base.Info
	func() { info().Msg("method value of a pointer parameter") }() // OK
}

func goodEventMethodValueInIIFE(ctx context.Context, base zerolog.Logger) {
	str := base.Info().Ctx(ctx).Str
	func() { str("k", "v").Msg("Str method value in an IIFE") }() // OK
}

func goodMsgMethodValueDeferred(ctx context.Context, base zerolog.Logger) {
	msg := base.Info().Ctx(ctx).Msg
	defer func() { msg("Msg method value deferred") }() // OK
}

func goodPrintMethodValueInIIFE(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(ctx).Logger()
	print := l.Print
	func() { print("Print method value in an IIFE") }() // OK
}

func goodMsgMethodValueCtxInPlaceBeforeIIFE(ctx context.Context, base zerolog.Logger) {
	e := base.Info()
	e.Ctx(ctx)
	msg := e.Msg
	func() { msg("Ctx in place before the IIFE") }() // OK
}

func goodMsgMethodValueCtxInPlaceInIIFE(ctx context.Context, base zerolog.Logger) {
	e := base.Info()
	msg := e.Msg
	func() {
		e.Ctx(ctx)
		msg("Ctx in place in the IIFE") // OK
	}()
}

func badMsgMethodValueFreshCtxInIIFE(ctx context.Context, base zerolog.Logger) {
	e := base.Info().Ctx(ctx)
	msg := e.Msg
	func() {
		e.Ctx(context.Background())
		msg("fresh Ctx in place in the IIFE") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

func badInfoMethodValueInIIFE(ctx context.Context, base zerolog.Logger) {
	info := base.Info
	func() { info().Msg("method value in an IIFE") }() // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badPrintMethodValueInIIFE(ctx context.Context, base zerolog.Logger) {
	print := base.Print
	func() { print("Print method value in an IIFE") }() // want `zerolog direct logging bypasses context`
}

// A method expression takes its receiver from the call in the closure.

func badMsgMethodExpressionInIIFE(ctx context.Context, base zerolog.Logger) {
	msg := (*zerolog.Event).Msg
	func() { msg(base.Info(), "method expression in an IIFE") }() // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodMsgMethodExpressionInIIFE(ctx context.Context, base zerolog.Logger) {
	msg := (*zerolog.Event).Msg
	func() { msg(base.Info().Ctx(ctx), "method expression in an IIFE") }() // OK
}

// A method expression of a method promoted through an embedded pointer, on a
// struct a closure captures.

type capturedEvent struct{ *zerolog.Event }

type capturedEventPtr struct{ *capturedEvent }

func goodPromotedMethodExpressionInIIFE(ctx context.Context, base zerolog.Logger) {
	h := capturedEventPtr{&capturedEvent{base.Info().Ctx(ctx)}}
	func() { capturedEventPtr.Msg(h, "promoted method expression in an IIFE") }() // OK
}

func goodPromotedPointerMethodExpressionInIIFE(ctx context.Context, base zerolog.Logger) {
	h := &capturedEventPtr{&capturedEvent{base.Info().Ctx(ctx)}}
	func() { (*capturedEventPtr).Msg(h, "promoted method expression on a pointer in an IIFE") }() // OK
}

func badPromotedPointerMethodExpressionInIIFE(ctx context.Context, base zerolog.Logger) {
	h := &capturedEventPtr{&capturedEvent{base.Info()}}
	func() { (*capturedEventPtr).Msg(h, "promoted method expression on a pointer in an IIFE") }() // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badPromotedMethodExpressionInIIFE(ctx context.Context, base zerolog.Logger) {
	h := capturedEventPtr{&capturedEvent{base.Info()}}
	func() { capturedEventPtr.Msg(h, "promoted method expression in an IIFE") }() // want `zerolog call chain missing .Ctx\(ctx\)`
}

// LIMITATION: a closure that loads a method value from a variable assigned
// more than once does not know which one it calls, so it is reported.
func limitationMethodValueAssignedTwiceInIIFE(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(ctx).Logger()
	info := l.Debug
	info = l.Info
	func() { info().Msg("method value assigned twice") }() // want `zerolog call chain missing .Ctx\(ctx\)`
}

// LIMITATION: a method value stored by another closure than the one that
// calls it is bound where its receiver is not read, so it is reported.
func limitationMethodValueStoredByAnotherClosure(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(ctx).Logger()
	var info func() *zerolog.Event
	func() { info = l.Info }()
	func() { info().Msg("method value stored by another closure") }() // want `zerolog call chain missing .Ctx\(ctx\)`
}
