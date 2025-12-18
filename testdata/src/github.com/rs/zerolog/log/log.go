// Stub package for testing - global logger
package log

import (
	"context"

	"github.com/rs/zerolog"
)

// Logger is the global logger.
var Logger zerolog.Logger

// Ctx returns the Logger associated with the ctx.
func Ctx(ctx context.Context) *zerolog.Logger {
	return zerolog.Ctx(ctx)
}

func Info() *zerolog.Event     { return Logger.Info() }
func Debug() *zerolog.Event    { return Logger.Debug() }
func Warn() *zerolog.Event     { return Logger.Warn() }
func Error() *zerolog.Event    { return Logger.Error() }
func Fatal() *zerolog.Event    { return Logger.Fatal() }
func Panic() *zerolog.Event    { return Logger.Panic() }
func Trace() *zerolog.Event    { return Logger.Trace() }
func With() zerolog.Context    { return Logger.With() }
