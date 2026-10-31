// Package freshctx covers fresh contexts (context.Background, context.TODO) given
// in place of the context in scope.
package freshctx

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ===== FRESH CONTEXTS =====

// A fresh context.Background() or TODO() carries nothing of the caller's, so
// a Ctx call given one does not count while a context is in scope (#197).

func badEventCtxBackground(ctx context.Context, log zerolog.Logger) {
	log.Info().Ctx(context.Background()).Msg("Background") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badZerologCtxTODO(ctx context.Context) {
	zerolog.Ctx(context.TODO()).Info().Msg("TODO") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badLogCtxBackground(ctx context.Context) {
	log.Ctx(context.Background()).Info().Msg("Background") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badContextCtxBackground(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(context.Background()).Logger()
	l.Info().Msg("Background on the logger") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxBackgroundVariable(ctx context.Context, log zerolog.Logger, debug bool) {
	bg := context.Background()
	if debug {
		bg = context.TODO()
	}
	log.Info().Ctx(bg).Msg("Background in a variable") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxBackgroundInMemory(ctx context.Context, log zerolog.Logger) {
	bg := context.Background()
	_ = &bg
	log.Info().Ctx(bg).Msg("Background in memory") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxBackgroundInPlace(ctx context.Context, log zerolog.Logger) {
	e := log.Info()
	e.Ctx(context.Background())
	e.Msg("Background in place") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A fresh context replaces the one the chain had.
func badEventCtxBackgroundOverCtxLogger(ctx context.Context, base zerolog.Logger) {
	l := base.With().Ctx(ctx).Logger()
	l.Info().Ctx(context.Background()).Msg("Background replaces ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxBackgroundUpdatedInLoop(ctx context.Context, log zerolog.Logger, n int, debug bool) {
	bg := context.Background()
	for range n {
		if debug {
			bg = context.TODO()
		}
		log.Info().Ctx(bg).Msg("Background updated in a loop") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

type freshContextHolder struct{ ctx context.Context }

func badEventCtxBackgroundInField(ctx context.Context, log zerolog.Logger) {
	var h freshContextHolder
	h.ctx = context.Background()
	log.Info().Ctx(h.ctx).Msg("Background in a field") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxBackgroundStoredThroughPointer(ctx context.Context, log zerolog.Logger, pc *context.Context) {
	*pc = context.Background()
	log.Info().Ctx(*pc).Msg("Background stored through a pointer") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventCtxThroughPointer(ctx context.Context, log zerolog.Logger, pc *context.Context) {
	log.Info().Ctx(*pc).Msg("through a pointer") // OK - the caller's value is not known
}

func goodEventCtxBackgroundUpdatedByClosure(ctx context.Context, log zerolog.Logger) {
	bg := context.Background()
	func() {
		bg = ctx
		log.Info().Ctx(bg).Msg("updated by a closure") // OK - ctx is stored
	}()
	func() {
		log.Info().Ctx(bg).Msg("captured, updated elsewhere") // OK - not known to be fresh
	}()
}

func goodEventCtxFromChannel(ctx context.Context, log zerolog.Logger, ch chan context.Context) {
	log.Info().Ctx(<-ch).Msg("from a channel") // OK - not known to be fresh
}

var globalBackground = context.Background()

// A global can be changed anywhere, so it is not taken as fresh.
func goodEventCtxGlobalBackground(ctx context.Context, log zerolog.Logger) {
	log.Info().Ctx(globalBackground).Msg("global") // OK - not known to be fresh
}

func goodEventCtxFromFuncValue(ctx context.Context, log zerolog.Logger, mk func() context.Context) {
	log.Info().Ctx(mk()).Msg("func value") // OK - not known to be fresh
}

func freshContextHelper(ctx context.Context) context.Context { return ctx }

func goodEventCtxFromHelper(ctx context.Context, log zerolog.Logger) {
	log.Info().Ctx(freshContextHelper(ctx)).Msg("from a helper") // OK - not Background itself
}

func goodEventCtxDerivedFromBackground(ctx context.Context, log zerolog.Logger) {
	derived := context.WithoutCancel(context.Background())
	log.Info().Ctx(derived).Msg("derived context") // OK - not Background itself
}

func goodEventCtxBackgroundOrCtx(ctx context.Context, log zerolog.Logger, detached bool) {
	c := ctx
	if detached {
		c = context.Background()
	}
	log.Info().Ctx(c).Msg("ctx on one path") // OK - ctx on one path
}

func goodEventCtxBackgroundWithoutCtxInScope(log zerolog.Logger) {
	log.Info().Ctx(context.Background()).Msg("no ctx in scope") // OK - no ctx to pass
}

// LIMITATION (false negative): A fresh context given to Ctx in place, with
// its result unused, does not count, but it does not replace the context the
// event had either.
func limitationEventCtxBackgroundInPlaceOverCtx(ctx context.Context, log zerolog.Logger) {
	e := log.Info().Ctx(ctx)
	e.Ctx(context.Background())
	e.Msg("Background in place over ctx") // LIMITATION: should report but doesn't
}

// LIMITATION(#214) (false negative): A variable a closure captures is not
// followed back to what is stored in it, so it is not taken as fresh.
func limitationEventCtxBackgroundCaptured(ctx context.Context, log zerolog.Logger) {
	bg := context.Background()
	func() {
		log.Info().Ctx(bg).Msg("Background captured") // LIMITATION(#214): should report but doesn't
	}()
}
