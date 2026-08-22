package zerolog

import (
	"context"

	"github.com/rs/zerolog"
)

// go/types always represents an alias declaration as a *types.Alias node, so
// zerolog and context types reached through an alias must still be recognized.

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
