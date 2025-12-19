// Package filefilterskip tests file filtering with -test=false.
// Tests that:
// - Generated files are always skipped (see generated.go)
// - Test files are skipped when -test=false (see code_test.go)
package filefilterskip

import (
	"context"

	"github.com/rs/zerolog"
)

// badNoCtx should be reported in regular files even with -test=false.
func badNoCtx(ctx context.Context, log zerolog.Logger) {
	log.Info().Msg("no context") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// goodWithCtx properly uses context.
func goodWithCtx(ctx context.Context, log zerolog.Logger) {
	log.Info().Ctx(ctx).Msg("with context")
}
