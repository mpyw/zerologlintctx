// Stub package for testing
package zerolog

import (
	"context"
	"io"
	"time"
)

// Logger is the zerolog logger.
type Logger struct{}

// Event represents a zerolog log event.
type Event struct{}

// Context represents a zerolog context (returned by With()).
type Context struct{}

// Level represents a log level.
type Level int8

// Log levels.
const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
	PanicLevel
	NoLevel
	Disabled
	TraceLevel Level = -1
)

// DefaultContextLogger is the default logger used by Ctx when no logger is in context.
var DefaultContextLogger *Logger

// Ctx returns the Logger associated with the ctx.
func Ctx(ctx context.Context) *Logger {
	return &Logger{}
}

// New creates a new Logger.
func New(w io.Writer) Logger {
	return Logger{}
}

// Nop returns a no-op Logger.
func Nop() Logger {
	return Logger{}
}

// Logger methods
func (l Logger) Info() *Event            { return &Event{} }
func (l Logger) Debug() *Event           { return &Event{} }
func (l Logger) Warn() *Event            { return &Event{} }
func (l Logger) Error() *Event           { return &Event{} }
func (l Logger) Fatal() *Event           { return &Event{} }
func (l Logger) Panic() *Event           { return &Event{} }
func (l Logger) Trace() *Event           { return &Event{} }
func (l Logger) Log() *Event             { return &Event{} }
func (l Logger) WithLevel(level Level) *Event { return &Event{} }
func (l Logger) With() Context           { return Context{} }
func (l Logger) Level(lvl Level) Logger  { return l }
func (l Logger) Sample(s Sampler) Logger { return l }
func (l Logger) Hook(h Hook) Logger      { return l }
func (l Logger) Output(w io.Writer) Logger { return l }

// Sampler interface for sampling.
type Sampler interface {
	Sample(lvl Level) bool
}

// Hook interface for hooks.
type Hook interface {
	Run(e *Event, level Level, msg string)
}

// Context methods (for building loggers with preset fields)
func (c Context) Str(key, val string) Context              { return c }
func (c Context) Strs(key string, val []string) Context    { return c }
func (c Context) Int(key string, val int) Context          { return c }
func (c Context) Int64(key string, val int64) Context      { return c }
func (c Context) Uint(key string, val uint) Context        { return c }
func (c Context) Float64(key string, val float64) Context  { return c }
func (c Context) Bool(key string, val bool) Context        { return c }
func (c Context) Bytes(key string, val []byte) Context     { return c }
func (c Context) Hex(key string, val []byte) Context       { return c }
func (c Context) Time(key string, t time.Time) Context     { return c }
func (c Context) Dur(key string, d time.Duration) Context  { return c }
func (c Context) Interface(key string, i any) Context      { return c }
func (c Context) Err(err error) Context                    { return c }
func (c Context) Errs(key string, errs []error) Context    { return c }
func (c Context) AnErr(key string, err error) Context      { return c }
func (c Context) Stack() Context                           { return c }
func (c Context) Caller() Context                          { return c }
func (c Context) CallerWithSkipFrameCount(skip int) Context { return c }
func (c Context) IPAddr(key string, ip []byte) Context     { return c }
func (c Context) Timestamp() Context                       { return c }
func (c Context) Ctx(ctx context.Context) Context          { return c }
func (c Context) Logger() Logger                           { return Logger{} }

// Event methods
func (e *Event) Ctx(ctx context.Context) *Event       { return e }
func (e *Event) Str(key, val string) *Event           { return e }
func (e *Event) Strs(key string, val []string) *Event { return e }
func (e *Event) Int(key string, val int) *Event       { return e }
func (e *Event) Int64(key string, val int64) *Event   { return e }
func (e *Event) Uint(key string, val uint) *Event     { return e }
func (e *Event) Float64(key string, val float64) *Event { return e }
func (e *Event) Bool(key string, val bool) *Event     { return e }
func (e *Event) Bytes(key string, val []byte) *Event  { return e }
func (e *Event) Hex(key string, val []byte) *Event    { return e }
func (e *Event) Time(key string, t time.Time) *Event  { return e }
func (e *Event) Dur(key string, d time.Duration) *Event { return e }
func (e *Event) Interface(key string, i any) *Event   { return e }
func (e *Event) Err(err error) *Event                 { return e }
func (e *Event) Errs(key string, errs []error) *Event { return e }
func (e *Event) AnErr(key string, err error) *Event   { return e }
func (e *Event) Stack() *Event                        { return e }
func (e *Event) Caller() *Event                       { return e }
func (e *Event) CallerSkipFrame(skip int) *Event      { return e }
func (e *Event) IPAddr(key string, ip []byte) *Event  { return e }
func (e *Event) Timestamp() *Event                    { return e }
func (e *Event) Dict(key string, dict *Event) *Event  { return e }
func (e *Event) Array(key string, arr LogArrayMarshaler) *Event { return e }
func (e *Event) Object(key string, obj LogObjectMarshaler) *Event { return e }
func (e *Event) EmbedObject(obj LogObjectMarshaler) *Event { return e }
func (e *Event) Fields(fields any) *Event             { return e }
func (e *Event) Enabled() bool                        { return true }
func (e *Event) Discard() *Event                      { return e }
func (e *Event) Msg(msg string)                       {}
func (e *Event) Msgf(format string, v ...any)         {}
func (e *Event) MsgFunc(createMsg func() string)      {}
func (e *Event) Send()                                {}

// LogArrayMarshaler interface for array marshaling.
type LogArrayMarshaler interface {
	MarshalZerologArray(a *Array)
}

// LogObjectMarshaler interface for object marshaling.
type LogObjectMarshaler interface {
	MarshalZerologObject(e *Event)
}

// Array represents a zerolog array.
type Array struct{}

// Arr creates a new Array.
func Arr() *Array { return &Array{} }

// Array methods
func (a *Array) Str(val string) *Array        { return a }
func (a *Array) Int(val int) *Array           { return a }
func (a *Array) Bool(val bool) *Array         { return a }
func (a *Array) Interface(val any) *Array     { return a }
func (a *Array) Object(obj LogObjectMarshaler) *Array { return a }

// Dict creates a new Event for use with Event.Dict.
func Dict() *Event { return &Event{} }
