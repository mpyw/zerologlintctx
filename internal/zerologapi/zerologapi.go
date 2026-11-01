// Package zerologapi says what each function and method of zerolog does with
// the context of a log chain, and which values of package context are fresh.
// It is the one place that knows the zerolog API: the tracer and the checker
// in internal/chain ask Classify, and match no names of their own.
//
// Most of it comes from signatures. A method of Event that returns nothing
// ends a chain, and a method that returns another zerolog type hands the
// chain over to it. Where zerolog's source, not the signature, tells what a
// method does, a row of exceptions says so. Each row cites zerolog v1.35.1.
package zerologapi

import (
	"go/types"
	"strings"
)

// Type is a zerolog type that carries the context of a log chain.
type Type int

// The zerolog types.
const (
	// TypeNone is any other type, or no type at all.
	TypeNone Type = iota
	// TypeEvent is *zerolog.Event.
	TypeEvent
	// TypeLogger is zerolog.Logger, or a pointer to one.
	TypeLogger
	// TypeContext is zerolog.Context.
	TypeContext
)

// Kind is what a call of a zerolog function or method does in a log chain.
type Kind int

// The kinds.
const (
	// KindOther is a call that does nothing tracing follows, such as
	// zerolog.New or Logger.GetLevel, or a function outside zerolog.
	KindOther Kind = iota
	// KindSource sets the context: a Ctx. Its Source says which one.
	KindSource
	// KindHandoff returns another zerolog type, made from its receiver, such
	// as Logger.Info, Logger.With and Context.Logger.
	KindHandoff
	// KindChain returns a value of its receiver's type, such as Event.Str,
	// Context.Str and Logger.Level.
	KindChain
	// KindTerminator ends an Event chain: a method of Event that returns
	// nothing, such as Msg and Send.
	KindTerminator
	// KindDirectLog logs without an Event chain: Logger.Print, Printf,
	// Println and Write, and the Print functions of zerolog/log.
	KindDirectLog
	// KindInPlaceUpdate changes the Logger it runs on in place:
	// Logger.UpdateContext.
	KindInPlaceUpdate
)

// Sources is a set of the calls that give a value its context.
type Sources uint8

// The sources of context.
const (
	// SourceEvent is Event.Ctx(ctx), which sets the context of the event.
	SourceEvent Sources = 1 << iota
	// SourceContext is Context.Ctx(ctx), which sets the context of the
	// Logger the Context builds, and of the events of that Logger.
	SourceContext
	// SourceStored is zerolog.Ctx(ctx) or log.Ctx(ctx), which returns the
	// Logger stored in ctx. It does not attach ctx to the events of that
	// Logger, but the code took its Logger from the context on purpose.
	SourceStored

	// AllSources is every source.
	AllSources = SourceEvent | SourceContext | SourceStored
)

// Method is what Classify tells of a zerolog function or method.
type Method struct {
	// Kind is what a call of it does.
	Kind Kind
	// Recv is the type of its receiver, or TypeNone for a function.
	Recv Type
	// Result is the type of its only result, or TypeNone.
	Result Type
	// Keeps are the sources of the receiver's context that its result still
	// has. It is zero when the result carries none of it, as for a function.
	Keeps Sources
	// SameAsRecv reports whether the result is the receiver itself, not a
	// value made from it. Only a method of Event can be: the others have a
	// value receiver, and return a copy.
	SameAsRecv bool
	// Source is the context a call of a KindSource sets.
	Source Sources
}

// Package paths.
const (
	zerologPkgPath = "github.com/rs/zerolog"
	zerologLogPath = "github.com/rs/zerolog/log"
	contextPkgPath = "context"
)

// exception is a row of the exceptions: the method name on a receiver type.
// TypeNone stands for a function of zerolog or zerolog/log.
type exception struct {
	recv Type
	name string
}

// exceptions lists what zerolog's source, not the signature, tells. A Print
// method or function is found by its name in Classify, not here.
var exceptions = map[exception]Method{
	// func (e *Event) Ctx(ctx context.Context) *Event, event.go: sets e.ctx and
	// returns e.
	{TypeEvent, "Ctx"}: {
		Kind: KindSource, Recv: TypeEvent, Result: TypeEvent,
		Keeps: AllSources, SameAsRecv: true, Source: SourceEvent,
	},
	// func (c Context) Ctx(ctx context.Context) Context, context.go: sets c.l.ctx
	// and returns c.
	{TypeContext, "Ctx"}: {
		Kind: KindSource, Recv: TypeContext, Result: TypeContext,
		Keeps: AllSources, Source: SourceContext,
	},
	// func Ctx(ctx context.Context) *Logger, ctx.go, and log.Ctx, log/log.go:
	// the Logger stored in ctx.
	{TypeNone, "Ctx"}: {Kind: KindSource, Result: TypeLogger, Source: SourceStored},
	// func (e *Event) CreateDict() *Event, event.go: a new event for
	// Event.Dict, with the stack, hooks and context of e. Not e itself.
	{TypeEvent, "CreateDict"}: {
		Kind: KindChain, Recv: TypeEvent, Result: TypeEvent, Keeps: AllSources,
	},
	// func (c Context) CreateDict() *Event, context.go: a new event for
	// Context.Dict, with the stack, hooks and context of the Logger of c.
	// LIMITATION: tracing does not follow it back to c, so a dict that carries
	// a context from Context.Ctx is taken to have none.
	{TypeContext, "CreateDict"}: {Recv: TypeContext, Result: TypeEvent, Keeps: AllSources},
	// func (l Logger) Output(w io.Writer) Logger, log.go: a Logger built with
	// New(w), which keeps the level, sampler, stack, hooks and fields of l, but
	// not the context that Context.Ctx sets.
	{TypeLogger, "Output"}: {
		Kind: KindChain, Recv: TypeLogger, Result: TypeLogger, Keeps: SourceStored,
	},
	// func (l Logger) Write(p []byte) (n int, err error), log.go:
	// l.Log() ... Msg, so the event takes the context of l, as for Print. Its
	// two results hide it from the Print case of Classify.
	// LIMITATION: a use through io.Writer, such as fmt.Fprint(l, ...), is not
	// seen.
	{TypeLogger, "Write"}: {Kind: KindDirectLog, Recv: TypeLogger},
	// func (l *Logger) UpdateContext(update func(c Context) Context), log.go:
	// keeps the fields update writes to l. The context Context.Ctx sets in
	// them is dropped, so l keeps the context it had.
	{TypeLogger, "UpdateContext"}: {Kind: KindInPlaceUpdate, Recv: TypeLogger},
}

// Classify tells what fn, a declared function or method, does in a log chain.
// It is the zero Method, of KindOther, for a function outside zerolog.
func Classify(fn *types.Func) Method {
	sig := fn.Signature()
	m := Method{Recv: TypeNone, Result: resultOf(sig)}
	if recv := sig.Recv(); recv != nil {
		m.Recv = TypeOf(recv.Type())
		if m.Recv == TypeNone {
			return Method{} // A method of a type outside zerolog, or of Array
		}
	} else if !isZerologPkg(fn.Pkg()) {
		return Method{}
	}
	if row, ok := exceptions[exception{m.Recv, fn.Name()}]; ok {
		return row
	}
	void := sig.Results().Len() == 0
	switch {
	case void && strings.HasPrefix(fn.Name(), "Print") &&
		(m.Recv == TypeLogger || m.Recv == TypeNone && fn.Pkg().Path() == zerologLogPath):
		// func (l *Logger) Print(v ...interface{}), Printf and Println, log.go,
		// and Print and Printf of log/log.go: l.Debug() ... Msg, so the event
		// takes the context of l
		m.Kind = KindDirectLog
	case void && m.Recv == TypeEvent:
		m.Kind = KindTerminator
	case m.Recv == TypeNone || m.Result == TypeNone:
		// A function, such as zerolog.New or log.Info, which uses the global
		// Logger, or a method that returns another type, such as GetLevel
	case m.Result == m.Recv:
		m.Kind, m.Keeps = KindChain, AllSources
		// Every method of Event that returns an event returns e, and the
		// methods of Logger and Context have a value receiver
		_, m.SameAsRecv = sig.Recv().Type().(*types.Pointer)
	default:
		m.Kind, m.Keeps = KindHandoff, AllSources
	}
	return m
}

// TypeOf returns the zerolog type t is, through aliases and a pointer.
func TypeOf(t types.Type) Type {
	switch {
	case isNamedType(t, zerologPkgPath, "Event"):
		return TypeEvent
	case isNamedType(t, zerologPkgPath, "Logger"):
		return TypeLogger
	case isNamedType(t, zerologPkgPath, "Context"):
		return TypeContext
	}
	return TypeNone
}

// IsContextType reports whether t is context.Context, or a pointer to it.
// A type parameter counts when its constraint is, or embeds,
// context.Context, as for C in func f[C context.Context](ctx C).
func IsContextType(t types.Type) bool {
	if tp, ok := types.Unalias(unwrapPointer(t)).(*types.TypeParam); ok {
		return constrainsToContext(tp.Constraint())
	}
	return isNamedType(t, contextPkgPath, "Context")
}

// MakesFreshContext reports whether fn is context.Background or
// context.TODO, which return a context that carries nothing of the caller's.
func MakesFreshContext(fn *types.Func) bool {
	return fn.Pkg() != nil && fn.Pkg().Path() == contextPkgPath &&
		(fn.Name() == "Background" || fn.Name() == "TODO")
}

// resultOf returns the zerolog type of the only result of sig, or TypeNone.
func resultOf(sig *types.Signature) Type {
	if sig.Results().Len() != 1 {
		return TypeNone
	}
	return TypeOf(sig.Results().At(0).Type())
}

// isZerologPkg reports whether pkg is github.com/rs/zerolog or its global
// logger package github.com/rs/zerolog/log.
func isZerologPkg(pkg *types.Package) bool {
	return pkg != nil && (pkg.Path() == zerologPkgPath || pkg.Path() == zerologLogPath)
}

// constrainsToContext reports whether the constraint c is context.Context, or
// an interface that embeds it at any depth. A pointer, as in
// func f[C *context.Context](ctx C), does not count: ctx is not a context.
func constrainsToContext(c types.Type) bool {
	if _, isPtr := types.Unalias(c).(*types.Pointer); isPtr {
		return false
	}
	if isNamedType(c, contextPkgPath, "Context") {
		return true
	}
	iface, ok := c.Underlying().(*types.Interface)
	if !ok {
		return false
	}
	for e := range iface.EmbeddedTypes() {
		if constrainsToContext(e) {
			return true
		}
	}
	return false
}

// unwrapPointer returns the element type if t is a pointer, otherwise returns t.
// Aliases are resolved first, so `type E = *zerolog.Event` unwraps to zerolog.Event.
func unwrapPointer(t types.Type) types.Type {
	if ptr, ok := types.Unalias(t).(*types.Pointer); ok {
		return ptr.Elem()
	}
	return t
}

// isNamedType reports whether t, or what it points to, is the named type
// typeName of the package pkgPath. Aliases are resolved: go/types represents
// every alias declaration as a *types.Alias node, so a bare *types.Named
// assertion would miss `type E = zerolog.Event`.
func isNamedType(t types.Type, pkgPath, typeName string) bool {
	named, ok := types.Unalias(unwrapPointer(t)).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == pkgPath && obj.Name() == typeName
}
