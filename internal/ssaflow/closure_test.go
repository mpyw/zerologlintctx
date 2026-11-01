package ssaflow

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

const closureSrc = `package p

type T struct{}

func (*T) M() {}

func f() {
	x := 0
	read := func() int { return x }
	_ = read
	held := func() {}
	func() { _ = held }()
	held()
	var t T
	m := t.M
	m()
}
`

// closureFunc builds f of closureSrc in SSA.
func closureFunc(t *testing.T) *ssa.Function {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", closureSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg := types.NewPackage("p", "p")
	ssaPkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset, pkg, []*ast.File{f}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return ssaPkg.Func("f")
}

// closureMakers returns the MakeClosure instructions of fn, in order.
func closureMakers(fn *ssa.Function) []*ssa.MakeClosure {
	var makers []*ssa.MakeClosure
	for instr := range InstrsIn(fn) {
		if mc, ok := instr.(*ssa.MakeClosure); ok {
			makers = append(makers, mc)
		}
	}
	return makers
}

func TestClosures(t *testing.T) {
	fn := closureFunc(t)
	makers := closureMakers(fn)
	// read captures x, the closure of the IIFE captures held, and m is t.M bound
	if len(makers) != 3 {
		t.Fatalf("got %d MakeClosure, want 3", len(makers))
	}
	read, iife, bound := makers[0], makers[1], makers[2]
	c := NewClosures()

	if got := c.makersOf(fn); got != nil {
		t.Errorf("makersOf(f) = %v, want nil for a declared function", got)
	}
	readFn := read.Fn.(*ssa.Function)
	if got := c.makersOf(readFn); !slices.Equal(got, []*ssa.MakeClosure{read}) {
		t.Errorf("makersOf(read) = %v, want [%v]", got, read)
	}
	x := read.Bindings[0]
	if got := c.bindingOf(readFn.FreeVars[0]); got != x {
		t.Errorf("bindingOf(x in read) = %v, want %v", got, x)
	}
	if got := c.bindingOf(bound.Fn.(*ssa.Function).FreeVars[0]); got != nil {
		t.Errorf("bindingOf(the receiver of a method value) = %v, want nil", got)
	}

	alloc := x.(*ssa.Alloc)
	aliases := c.aliasesOf(alloc)
	if !aliases[alloc] || !aliases[readFn.FreeVars[0]] || len(aliases) != 2 {
		t.Errorf("aliasesOf(x) = %v, want x and the free variable of read", aliases)
	}
	if again := c.aliasesOf(alloc); len(again) != len(aliases) {
		t.Errorf("aliasesOf(x) again = %v, want %v", again, aliases)
	}

	held := iife.Bindings[0]
	values, ok := c.funcsHeldBy(held)
	if !ok || len(values) != 1 {
		t.Fatalf("funcsHeldBy(held) = %v, %v, want one func", values, ok)
	}
	if again, ok := c.funcsHeldBy(held); !ok || !slices.Equal(again, values) {
		t.Errorf("funcsHeldBy(held) again = %v, %v, want %v", again, ok, values)
	}
}
