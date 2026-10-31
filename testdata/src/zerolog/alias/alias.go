// Package alias covers zerolog and context types reached through an alias or a
// defined type. go/types always represents an alias declaration as a
// *types.Alias node, so these types must still be recognized.
package alias

import (
	"context"

	"github.com/rs/zerolog"
)

// ===== TYPE ALIAS =====

type MyLogger = zerolog.Logger
type MyEvent = zerolog.Event

func badTypeAlias(ctx context.Context, logger MyLogger) {
	logger.Info().Msg("type alias") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodTypeAliasWithCtx(ctx context.Context, logger MyLogger) {
	logger.Info().Ctx(ctx).Msg("type alias with ctx") // OK
}

// ===== CONVERSIONS THROUGH A DEFINED TYPE =====

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

// ===== ALIASED context.Context PARAMETER =====

type ctxAlias = context.Context

func badAliasedContextParam(ctx ctxAlias, logger zerolog.Logger) {
	logger.Info().Msg("aliased ctx param") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodAliasedContextParam(ctx ctxAlias, logger zerolog.Logger) {
	logger.Info().Ctx(ctx).Msg("aliased ctx param with ctx") // OK
}

// ===== ALIASED zerolog TYPES AS IIFE RETURN TYPES =====

type eventAlias = *zerolog.Event

func badAliasedEventFromIIFE(ctx context.Context, logger zerolog.Logger) {
	e := func() eventAlias { return logger.Info() }()
	e.Msg("aliased event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodAliasedEventFromIIFE(ctx context.Context, logger zerolog.Logger) {
	e := func() eventAlias { return logger.Info().Ctx(ctx) }()
	e.Msg("aliased event with ctx") // OK
}

type loggerPtrAlias = *zerolog.Logger

func badAliasedLoggerFromIIFE(ctx context.Context, logger zerolog.Logger) {
	l := func() loggerPtrAlias { return &logger }()
	l.Info().Msg("aliased logger") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodAliasedLoggerFromIIFE(ctx context.Context, logger zerolog.Logger) {
	l := func() loggerPtrAlias { return zerolog.Ctx(ctx) }()
	l.Info().Msg("aliased logger with ctx") // OK
}
