// Package ctxname covers which context is in scope and how it is named: several
// context parameters, a context named _, a shadowed context, a pointer to
// one, and a context type parameter.
package ctxname

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ===== MIDDLE LAYER INTRODUCES CTX (OUTER HAS NONE) =====

func evilMiddleLayerIntroducesCtx(logger zerolog.Logger) {
	func(ctx context.Context) {
		logger.Info().Ctx(ctx).Msg("middle layer") // OK
		func() {
			logger.Info().Msg("inner without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
		}()
	}(context.Background())
}

func evilMiddleLayerIntroducesCtxGood(logger zerolog.Logger) {
	func(ctx context.Context) {
		func() {
			logger.Info().Ctx(ctx).Msg("inner uses middle ctx") // OK
		}()
	}(context.Background())
}

// Three layers: outer no ctx, middle has ctx, inner has different ctx
func evilMiddleAndInnerBothHaveCtx(logger zerolog.Logger) {
	func(middleCtx context.Context) {
		logger.Info().Ctx(middleCtx).Msg("middle") // OK
		func(innerCtx context.Context) {
			logger.Info().Ctx(innerCtx).Msg("inner") // OK - uses innerCtx
		}(middleCtx)
	}(context.Background())
}

// ===== INTERLEAVED LAYERS (ctx -> no ctx -> ctx shadowing) =====

func evilInterleavedLayers(outerCtx context.Context, logger zerolog.Logger) {
	func() {
		func(middleCtx context.Context) {
			func() {
				logger.Info().Msg("interleaved") // want `zerolog call chain missing .Ctx\(middleCtx\)`
			}()
		}(outerCtx)
	}()
}

func evilInterleavedLayersGood(outerCtx context.Context, logger zerolog.Logger) {
	func() {
		func(middleCtx context.Context) {
			func() {
				logger.Info().Ctx(middleCtx).Msg("interleaved with ctx") // OK
			}()
		}(outerCtx)
	}()
}

// ===== MULTIPLE CONTEXT PARAMETERS =====

func evilMultipleCtxParams(ctx1 context.Context, ctx2 context.Context, logger zerolog.Logger) {
	logger.Info().Msg("multiple ctx") // want `zerolog call chain missing .Ctx\(ctx1\)`
}

func evilMultipleCtxParamsGood(ctx1 context.Context, ctx2 context.Context, logger zerolog.Logger) {
	logger.Info().Ctx(ctx1).Msg("uses ctx1") // OK
}

func evilMultipleCtxParamsNested(ctx1 context.Context, ctx2 context.Context, logger zerolog.Logger) {
	func() {
		logger.Info().Msg("nested multiple") // want `zerolog call chain missing .Ctx\(ctx1\)`
	}()
}

// ===== CONTEXT PARAMETER NAMED _ =====

// A context named _ cannot be passed on, so it counts as no context, as an
// unnamed one does (#80).
func evilBlankCtxParam(_ context.Context, logger zerolog.Logger) {
	logger.Info().Msg("blank ctx") // OK - no usable context
}

func evilUnnamedCtxParam(context.Context, zerolog.Logger) {
	log.Info().Msg("unnamed ctx") // OK - no usable context
}

// A blank context after a named one does not hide it.
func evilBlankThenNamedCtxParam(_ context.Context, ctx context.Context, logger zerolog.Logger) {
	logger.Info().Msg("named after blank") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A closure whose own context is named _ asks for the outer one.
func evilBlankCtxInClosure(ctx context.Context, logger zerolog.Logger) {
	func(_ context.Context) {
		logger.Info().Msg("blank ctx in closure") // want `zerolog call chain missing .Ctx\(ctx\)`
	}(ctx)
}

// ===== CONTEXT SHADOWED, OR BEHIND A POINTER =====

// Where the name of the context means something else, the context cannot be
// passed (#128).
func evilCtxShadowedByClosureParam(ctx context.Context, logger zerolog.Logger) {
	func(ctx string) {
		logger.Info().Str("s", ctx).Msg("shadowed by param") // OK - no usable context
	}("x")
}

func evilCtxShadowedByLocal(ctx context.Context, logger zerolog.Logger) {
	{
		ctx := 1
		logger.Info().Int("n", ctx).Msg("shadowed by local") // OK - no usable context
		logger.Printf("shadowed by local %d", ctx)          // OK - no usable context
	}
	logger.Info().Msg("after the shadow") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilCtxShadowedLaterInBlock(ctx context.Context, logger zerolog.Logger) {
	{
		logger.Info().Msg("before the shadow") // want `zerolog call chain missing .Ctx\(ctx\)`
		ctx := 1
		_ = ctx
	}
}

// A pointer to a context is passed by its value.
func evilCtxPointerParam(ctx *context.Context, logger zerolog.Logger) {
	logger.Info().Msg("pointer ctx") // want `zerolog call chain missing .Ctx\(\*ctx\)`
}

// A shadow of the other kind, a pointer for a value or a value for a pointer,
// cannot be passed as the name was given either (#159).
func evilCtxShadowedByPointer(ctx context.Context, logger zerolog.Logger) {
	{
		ctx := &ctx
		_ = ctx
		logger.Info().Msg("shadowed by a pointer") // OK - no context named ctx here
	}
}

func evilCtxPointerShadowedByValue(ctx *context.Context, logger zerolog.Logger) {
	{
		ctx := *ctx
		_ = ctx
		logger.Info().Msg("pointer shadowed by a value") // OK - no context named *ctx here
	}
}

var evilGlobalClosureShadowed = func(ctx context.Context) {
	func(ctx int) {
		log.Info().Int("n", ctx).Msg("global closure shadowed") // OK - no usable context
	}(1)
}

// ===== CONTEXT TYPE PARAMETER =====

// A type parameter whose constraint is, or embeds, context.Context is a
// context (#198).
func evilCtxTypeParam[C context.Context](ctx C, logger zerolog.Logger) {
	logger.Info().Msg("type param ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilCtxTypeParamWithCtx[C context.Context](ctx C, logger zerolog.Logger) {
	logger.Info().Ctx(ctx).Msg("type param ctx with ctx") // OK
}

func evilCtxTypeParamEmbedded[C interface {
	context.Context
	Name() string
}](ctx C, logger zerolog.Logger) {
	logger.Info().Msg("embedded type param ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

type evilCtxConstraint interface {
	context.Context
}

type evilNestedCtxConstraint interface {
	evilCtxConstraint
	Name() string
}

func evilCtxTypeParamNestedEmbedded[C evilNestedCtxConstraint](ctx C, logger zerolog.Logger) {
	logger.Info().Msg("nested embedded type param ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilCtxTypeParamPointer[C context.Context](ctx *C, logger zerolog.Logger) {
	logger.Info().Msg("pointer to type param ctx") // want `zerolog call chain missing .Ctx\(\*ctx\)`
}

func evilCtxTypeParamInClosure[C context.Context](ctx C, logger zerolog.Logger) {
	func() {
		logger.Info().Msg("type param ctx in closure") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

// A type parameter that may be another type is not a context.
func evilAnyTypeParam[C any](ctx C, logger zerolog.Logger) {
	logger.Info().Msg("any type param") // OK - not a context
}

func evilIntTypeParam[C interface {
	~int
	String() string
}](ctx C, logger zerolog.Logger) {
	logger.Info().Msg("int type param") // OK - not a context
}

func evilPointerCtxTypeParam[C *context.Context](ctx C, logger zerolog.Logger) {
	logger.Info().Msg("pointer constraint type param") // OK - not named as *ctx
}
