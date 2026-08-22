package zerolog

import (
	"context"

	"github.com/rs/zerolog"
)

// Go 1.27 language features, exercised so the analyzer keeps working on code
// that uses them.

// ===== PROMOTED FIELD KEYS IN COMPOSITE LITERALS (Go 1.27) =====

type innerEventHolder struct {
	event *zerolog.Event
}

type outerEventHolder struct {
	innerEventHolder
	label string
}

func badPromotedFieldKey(ctx context.Context, logger zerolog.Logger) {
	// `event` is promoted from the embedded innerEventHolder (Go 1.27).
	h := outerEventHolder{event: logger.Info(), label: "x"}
	h.event.Msg("promoted key") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodPromotedFieldKey(ctx context.Context, logger zerolog.Logger) {
	h := outerEventHolder{event: logger.Info().Ctx(ctx), label: "x"}
	h.event.Msg("promoted key with ctx") // OK
}

// ===== GENERIC METHODS (Go 1.27) =====

type genericLogHolder[T any] struct {
	value T
}

func (g genericLogHolder[T]) LogWith[F any](ctx context.Context, logger zerolog.Logger, f func(T) F) {
	logger.Info().Msg("generic method") // want `zerolog call chain missing .Ctx\(ctx\)`
	_ = f(g.value)
}

func (g genericLogHolder[T]) LogWithCtx[F any](ctx context.Context, logger zerolog.Logger, f func(T) F) {
	logger.Info().Ctx(ctx).Msg("generic method with ctx") // OK
	_ = f(g.value)
}

func useGenericMethods(ctx context.Context, logger zerolog.Logger) {
	g := genericLogHolder[int]{value: 1}
	g.LogWith(ctx, logger, func(i int) string { return "" })
	g.LogWithCtx(ctx, logger, func(i int) string { return "" })
}
