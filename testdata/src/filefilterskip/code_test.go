package filefilterskip

import (
	"context"

	"github.com/rs/zerolog"
)

// badNoCtxInTest is NOT reported when -test=false.
// This file should not be analyzed - no diagnostic expected.
func badNoCtxInTest(ctx context.Context, log zerolog.Logger) {
	log.Info().Msg("no context in test file - but skipped")
}
