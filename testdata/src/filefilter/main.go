// Package filefilter tests file filtering functionality.
// Tests that:
// - Generated files are always skipped (see generated.go)
// - Test files are analyzed by default (see code_test.go)
package filefilter

import (
	"context"

	"github.com/rs/zerolog"
)

// badNoCtx should be reported in regular files.
func badNoCtx(ctx context.Context, log zerolog.Logger) {
	log.Info().Msg("no context") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// goodWithCtx properly uses context.
func goodWithCtx(ctx context.Context, log zerolog.Logger) {
	log.Info().Ctx(ctx).Msg("with context")
}
