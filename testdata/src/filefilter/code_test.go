package filefilter

import (
	"context"

	"github.com/rs/zerolog"
)

// badNoCtxInTest is reported when -test=true (default).
func badNoCtxInTest(ctx context.Context, log zerolog.Logger) {
	log.Info().Msg("no context in test file") // want `zerolog call chain missing .Ctx\(ctx\)`
}
