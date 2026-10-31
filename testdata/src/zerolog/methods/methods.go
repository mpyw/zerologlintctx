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
