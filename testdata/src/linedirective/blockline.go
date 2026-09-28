package linedirective

import (
	"context"

	"github.com/rs/zerolog/log"
)

// ===== SHOULD NOT REPORT =====

// The /*line*/ directive moves the adjusted line of the call to 50.
// The ignore directive on the line above must still apply. Do not gofmt this
// file: gofmt moves the call to its own line.
func goodIgnoredAboveBlockLineDirective(ctx context.Context) {
	//zerologlintctx:ignore
	/*line blockline.go:50*/ log.Info().Msg("ignored")
}
