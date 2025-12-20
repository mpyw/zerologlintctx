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

func Info() *zerolog.Event                         { return Logger.Info() }
func Debug() *zerolog.Event                        { return Logger.Debug() }
func Warn() *zerolog.Event                         { return Logger.Warn() }
func Error() *zerolog.Event                        { return Logger.Error() }
func Fatal() *zerolog.Event                        { return Logger.Fatal() }
func Panic() *zerolog.Event                        { return Logger.Panic() }
func Trace() *zerolog.Event                        { return Logger.Trace() }
func Log() *zerolog.Event                          { return Logger.Log() }
func Err(err error) *zerolog.Event                 { return Logger.Err(err) }
func WithLevel(level zerolog.Level) *zerolog.Event { return Logger.WithLevel(level) }
func With() zerolog.Context                        { return Logger.With() }

// Direct logging functions (bypass Event chain)
func Print(v ...any)                 { Logger.Print(v...) }
func Printf(format string, v ...any) { Logger.Printf(format, v...) }
