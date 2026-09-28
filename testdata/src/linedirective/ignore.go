// Package linedirective tests that //line directives do not move ignore
// directives or file filters away from the code they belong to.
package linedirective

import (
	"context"

	"github.com/rs/zerolog/log"
)

// ===== SHOULD NOT REPORT =====

// The function is below a //line directive, so its adjusted file name is
// ignore.tmpl. The ignore directive must still apply.
//
//line ignore.tmpl:1
func goodIgnoredBelowLineDirective(ctx context.Context) {
	log.Info().Msg("ignored") //zerologlintctx:ignore
}

// ===== SHOULD REPORT =====

func badBelowLineDirective(ctx context.Context) {
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badUnusedIgnoreBelowLineDirective(ctx context.Context) {
	//zerologlintctx:ignore // want `unused zerologlintctx:ignore directive`
	log.Info().Ctx(ctx).Msg("with context")
}
