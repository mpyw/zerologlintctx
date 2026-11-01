package zerologapi_test

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/mpyw/zerologlintctx/internal/zerologapi"
)

// The signatures below are copied from zerolog v1.35.1: event.go, context.go,
// log.go, ctx.go, array.go and log/log.go.
const zerologSrc = `package zerolog

import (
	"context"
	"io"
)

type Level int8
type Sampler interface{}
type Hook interface{}
type LogArrayMarshaler interface{}

type Logger struct{}
type Event struct{}
type Context struct{ l Logger }
type Array struct{}

func New(w io.Writer) Logger                  { return Logger{} }
func Nop() Logger                             { return Logger{} }
func Ctx(ctx context.Context) *Logger         { return nil }
func Dict() *Event                            { return nil }
func Arr() *Array                             { return nil }

func (l Logger) Output(w io.Writer) Logger    { return l }
func (l Logger) With() Context                { return Context{l} }
func (l *Logger) UpdateContext(update func(c Context) Context) {}
func (l Logger) Level(lvl Level) Logger       { return l }
func (l Logger) GetLevel() Level              { return 0 }
func (l Logger) Sample(s Sampler) Logger      { return l }
func (l Logger) Hook(hooks ...Hook) Logger    { return l }
func (l *Logger) Info() *Event                { return nil }
func (l *Logger) Err(err error) *Event        { return nil }
func (l *Logger) WithLevel(level Level) *Event { return nil }
func (l *Logger) Print(v ...interface{})      {}
func (l *Logger) Printf(format string, v ...interface{}) {}
func (l *Logger) Println(v ...interface{})    {}
func (l Logger) Write(p []byte) (n int, err error) { return 0, nil }
func (l Logger) WithContext(ctx context.Context) context.Context { return ctx }

func (c Context) Logger() Logger              { return c.l }
func (c Context) Str(key, val string) Context { return c }
func (c Context) Ctx(ctx context.Context) Context { return c }
func (c Context) CreateDict() *Event          { return nil }
func (c Context) CreateArray() *Array         { return nil }

func (e *Event) Str(key, val string) *Event   { return e }
func (e *Event) Ctx(ctx context.Context) *Event { return e }
func (e *Event) CreateDict() *Event           { return nil }
func (e *Event) Dict(key string, dict *Event) *Event { return e }
func (e *Event) CreateArray() *Array          { return nil }
func (e *Event) Enabled() bool                { return true }
func (e *Event) GetCtx() context.Context      { return nil }
func (e *Event) Msg(msg string)               {}
func (e *Event) Msgf(format string, v ...interface{}) {}
func (e *Event) MsgFunc(createMsg func() string) {}
func (e *Event) Send()                        {}

func (a *Array) Str(val string) *Array        { return a }
`

const zerologLogSrc = `package log

import (
	"context"
	"io"

	"github.com/rs/zerolog"
)

func Output(w io.Writer) zerolog.Logger         { return zerolog.Logger{} }
func With() zerolog.Context                     { return zerolog.Context{} }
func Info() *zerolog.Event                      { return nil }
func Print(v ...interface{})                    {}
func Printf(format string, v ...interface{})    {}
func Ctx(ctx context.Context) *zerolog.Logger   { return nil }
`

// otherSrc is a package outside zerolog, with names zerolog uses.
const otherSrc = `package other

import (
	"context"

	"github.com/rs/zerolog"
)

type Wrapper struct{ zerolog.Logger }
type DefinedLogger zerolog.Logger
type AliasEvent = zerolog.Event
type AliasEventPtr = *zerolog.Event

func (w Wrapper) Ctx(ctx context.Context) Wrapper { return w }
func Print(v ...interface{})                      {}
func Ctx(ctx context.Context) *zerolog.Logger     { return nil }

type Ctxer interface{ context.Context }
type CtxPtr interface{ *context.Context }

func Param[C context.Context, E Ctxer, P CtxPtr, S any, U ~int](c C, e E, p P, s S, u U) {}
`

// newPackages type-checks the fake zerolog, zerolog/log and a package outside
// them, against the real context and io.
func newPackages(t *testing.T) map[string]*types.Package {
	t.Helper()
	fset := token.NewFileSet()
	pkgs := map[string]*types.Package{}
	std := importer.Default()
	imp := importerFunc(func(path string) (*types.Package, error) {
		if p, ok := pkgs[path]; ok {
			return p, nil
		}
		return std.Import(path)
	})
	for _, src := range []struct{ path, code string }{
		{"github.com/rs/zerolog", zerologSrc},
		{"github.com/rs/zerolog/log", zerologLogSrc},
		{"example.com/other", otherSrc},
	} {
		f, err := parser.ParseFile(fset, src.path+".go", src.code, 0)
		if err != nil {
			t.Fatal(err)
		}
		conf := types.Config{Importer: imp}
		p, err := conf.Check(src.path, fset, []*ast.File{f}, nil)
		if err != nil {
			t.Fatal(err)
		}
		pkgs[src.path] = p
	}
	return pkgs
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// lookupFunc finds the function name, or the method name of the type recv,
// in pkg.
func lookupFunc(t *testing.T, pkg *types.Package, recv, name string) *types.Func {
	t.Helper()
	if recv == "" {
		fn, ok := pkg.Scope().Lookup(name).(*types.Func)
		if !ok {
			t.Fatalf("no func %s in %s", name, pkg.Path())
		}
		return fn
	}
	obj := pkg.Scope().Lookup(recv)
	if obj == nil {
		t.Fatalf("no type %s in %s", recv, pkg.Path())
	}
	m, _, _ := types.LookupFieldOrMethod(types.NewPointer(obj.Type()), false, pkg, name)
	fn, ok := m.(*types.Func)
	if !ok {
		t.Fatalf("no method %s.%s in %s", recv, name, pkg.Path())
	}
	return fn
}

func TestClassify(t *testing.T) {
	pkgs := newPackages(t)
	const (
		zl    = "github.com/rs/zerolog"
		log   = "github.com/rs/zerolog/log"
		other = "example.com/other"
	)
	const (
		none = zerologapi.TypeNone
		ev   = zerologapi.TypeEvent
		lg   = zerologapi.TypeLogger
		cx   = zerologapi.TypeContext
		all  = zerologapi.AllSources
	)
	tests := []struct {
		pkg, recv, name string
		want            zerologapi.Method
	}{
		// Sources
		{zl, "Event", "Ctx", zerologapi.Method{
			Kind: zerologapi.KindSource, Recv: ev, Result: ev, Keeps: all, SameAsRecv: true, Source: zerologapi.SourceEvent,
		}},
		{zl, "Context", "Ctx", zerologapi.Method{
			Kind: zerologapi.KindSource, Recv: cx, Result: cx, Keeps: all, Source: zerologapi.SourceContext,
		}},
		{zl, "", "Ctx", zerologapi.Method{Kind: zerologapi.KindSource, Result: lg, Source: zerologapi.SourceStored}},
		{log, "", "Ctx", zerologapi.Method{Kind: zerologapi.KindSource, Result: lg, Source: zerologapi.SourceStored}},

		// Handoffs
		{zl, "Logger", "Info", zerologapi.Method{Kind: zerologapi.KindHandoff, Recv: lg, Result: ev, Keeps: all}},
		{zl, "Logger", "Err", zerologapi.Method{Kind: zerologapi.KindHandoff, Recv: lg, Result: ev, Keeps: all}},
		{zl, "Logger", "WithLevel", zerologapi.Method{Kind: zerologapi.KindHandoff, Recv: lg, Result: ev, Keeps: all}},
		{zl, "Logger", "With", zerologapi.Method{Kind: zerologapi.KindHandoff, Recv: lg, Result: cx, Keeps: all}},
		{zl, "Context", "Logger", zerologapi.Method{Kind: zerologapi.KindHandoff, Recv: cx, Result: lg, Keeps: all}},

		// Chains
		{zl, "Event", "Str", zerologapi.Method{Kind: zerologapi.KindChain, Recv: ev, Result: ev, Keeps: all, SameAsRecv: true}},
		{zl, "Event", "Dict", zerologapi.Method{Kind: zerologapi.KindChain, Recv: ev, Result: ev, Keeps: all, SameAsRecv: true}},
		{zl, "Event", "CreateDict", zerologapi.Method{Kind: zerologapi.KindChain, Recv: ev, Result: ev, Keeps: all}},
		{zl, "Context", "Str", zerologapi.Method{Kind: zerologapi.KindChain, Recv: cx, Result: cx, Keeps: all}},
		{zl, "Logger", "Level", zerologapi.Method{Kind: zerologapi.KindChain, Recv: lg, Result: lg, Keeps: all}},
		{zl, "Logger", "Sample", zerologapi.Method{Kind: zerologapi.KindChain, Recv: lg, Result: lg, Keeps: all}},
		{zl, "Logger", "Hook", zerologapi.Method{Kind: zerologapi.KindChain, Recv: lg, Result: lg, Keeps: all}},
		{zl, "Logger", "Output", zerologapi.Method{
			Kind: zerologapi.KindChain, Recv: lg, Result: lg, Keeps: zerologapi.SourceStored,
		}},

		// Terminators
		{zl, "Event", "Msg", zerologapi.Method{Kind: zerologapi.KindTerminator, Recv: ev}},
		{zl, "Event", "Msgf", zerologapi.Method{Kind: zerologapi.KindTerminator, Recv: ev}},
		{zl, "Event", "MsgFunc", zerologapi.Method{Kind: zerologapi.KindTerminator, Recv: ev}},
		{zl, "Event", "Send", zerologapi.Method{Kind: zerologapi.KindTerminator, Recv: ev}},

		// Direct logging
		{zl, "Logger", "Print", zerologapi.Method{Kind: zerologapi.KindDirectLog, Recv: lg}},
		{zl, "Logger", "Printf", zerologapi.Method{Kind: zerologapi.KindDirectLog, Recv: lg}},
		{zl, "Logger", "Println", zerologapi.Method{Kind: zerologapi.KindDirectLog, Recv: lg}},
		{zl, "Logger", "Write", zerologapi.Method{Kind: zerologapi.KindDirectLog, Recv: lg}},
		{log, "", "Print", zerologapi.Method{Kind: zerologapi.KindDirectLog}},
		{log, "", "Printf", zerologapi.Method{Kind: zerologapi.KindDirectLog}},

		// In-place update
		{zl, "Logger", "UpdateContext", zerologapi.Method{Kind: zerologapi.KindInPlaceUpdate, Recv: lg}},

		// Other zerolog calls, which tracing does not follow
		{zl, "Context", "CreateDict", zerologapi.Method{Recv: cx, Result: ev, Keeps: all}},
		{zl, "Context", "CreateArray", zerologapi.Method{Recv: cx}},
		{zl, "Event", "CreateArray", zerologapi.Method{Recv: ev}},
		{zl, "Event", "Enabled", zerologapi.Method{Recv: ev}},
		{zl, "Event", "GetCtx", zerologapi.Method{Recv: ev}},
		{zl, "Logger", "GetLevel", zerologapi.Method{Recv: lg}},
		{zl, "Logger", "WithContext", zerologapi.Method{Recv: lg}},
		{zl, "Array", "Str", zerologapi.Method{}},
		{zl, "", "New", zerologapi.Method{Result: lg}},
		{zl, "", "Nop", zerologapi.Method{Result: lg}},
		{zl, "", "Dict", zerologapi.Method{Result: ev}},
		{zl, "", "Arr", zerologapi.Method{}},
		{log, "", "Info", zerologapi.Method{Result: ev}},
		{log, "", "With", zerologapi.Method{Result: cx}},
		{log, "", "Output", zerologapi.Method{Result: lg}},

		// Outside zerolog, under names zerolog uses
		{other, "Wrapper", "Ctx", zerologapi.Method{}},
		{other, "Wrapper", "Info", zerologapi.Method{Kind: zerologapi.KindHandoff, Recv: lg, Result: ev, Keeps: all}}, // Promoted
		{other, "", "Print", zerologapi.Method{}},
		{other, "", "Ctx", zerologapi.Method{}},
	}
	for _, tt := range tests {
		name := fmt.Sprintf("%s.%s.%s", tt.pkg, tt.recv, tt.name)
		t.Run(name, func(t *testing.T) {
			fn := lookupFunc(t, pkgs[tt.pkg], tt.recv, tt.name)
			if got := zerologapi.Classify(fn); got != tt.want {
				t.Errorf("Classify() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTypeOf(t *testing.T) {
	pkgs := newPackages(t)
	zl, other := pkgs["github.com/rs/zerolog"].Scope(), pkgs["example.com/other"].Scope()
	event := zl.Lookup("Event").Type()
	tests := []struct {
		name string
		typ  types.Type
		want zerologapi.Type
	}{
		{"Event", event, zerologapi.TypeEvent},
		{"*Event", types.NewPointer(event), zerologapi.TypeEvent},
		{"Logger", zl.Lookup("Logger").Type(), zerologapi.TypeLogger},
		{"*Logger", types.NewPointer(zl.Lookup("Logger").Type()), zerologapi.TypeLogger},
		{"Context", zl.Lookup("Context").Type(), zerologapi.TypeContext},
		{"Array", zl.Lookup("Array").Type(), zerologapi.TypeNone},
		{"alias of Event", other.Lookup("AliasEvent").Type(), zerologapi.TypeEvent},
		{"alias of *Event", other.Lookup("AliasEventPtr").Type(), zerologapi.TypeEvent},
		{"defined Logger", other.Lookup("DefinedLogger").Type(), zerologapi.TypeNone},
		{"struct embedding Logger", other.Lookup("Wrapper").Type(), zerologapi.TypeNone},
		{"int", types.Typ[types.Int], zerologapi.TypeNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := zerologapi.TypeOf(tt.typ); got != tt.want {
				t.Errorf("TypeOf() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsContextType(t *testing.T) {
	pkgs := newPackages(t)
	params := lookupFunc(t, pkgs["example.com/other"], "", "Param").Signature().Params()
	ctxType := params.At(0).Type().(*types.TypeParam).Constraint()
	tests := []struct {
		name string
		typ  types.Type
		want bool
	}{
		{"context.Context", ctxType, true},
		{"*context.Context", types.NewPointer(ctxType), true},
		{"C context.Context", params.At(0).Type(), true},
		{"E interface{ context.Context }", params.At(1).Type(), true},
		{"P interface{ *context.Context }", params.At(2).Type(), false},
		{"S any", params.At(3).Type(), false},
		{"U ~int", params.At(4).Type(), false},
		{"string", types.Typ[types.String], false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := zerologapi.IsContextType(tt.typ); got != tt.want {
				t.Errorf("IsContextType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMakesFreshContext(t *testing.T) {
	pkgs := newPackages(t)
	var ctxPkg *types.Package
	for _, imp := range pkgs["example.com/other"].Imports() {
		if imp.Path() == "context" {
			ctxPkg = imp
		}
	}
	tests := []struct {
		name string
		want bool
	}{
		{"Background", true},
		{"TODO", true},
		{"WithoutCancel", false},
		{"WithValue", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := lookupFunc(t, ctxPkg, "", tt.name)
			if got := zerologapi.MakesFreshContext(fn); got != tt.want {
				t.Errorf("MakesFreshContext() = %v, want %v", got, tt.want)
			}
		})
	}
	t.Run("outside context", func(t *testing.T) {
		if zerologapi.MakesFreshContext(lookupFunc(t, pkgs["github.com/rs/zerolog"], "", "Nop")) {
			t.Error("MakesFreshContext(zerolog.Nop) = true, want false")
		}
	})
}
