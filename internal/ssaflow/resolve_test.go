package ssaflow

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

const resolveSrc = `package p

type T struct{}

func (*T) M() {}

func f() {
	var t T
	m := t.M
	m()
}
`

// TestResolveFreeVarOfWrapper checks that the free variable of a method
// value, a wrapper made where no MakeClosure of a func literal is, resolves
// to one unknown source.
func TestResolveFreeVarOfWrapper(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", resolveSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg := types.NewPackage("p", "p")
	ssaPkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset, pkg, []*ast.File{file}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var bound *ssa.Function
	for instr := range InstrsIn(ssaPkg.Func("f")) {
		if mc, ok := instr.(*ssa.MakeClosure); ok {
			bound = mc.Fn.(*ssa.Function)
		}
	}
	if bound == nil || len(bound.FreeVars) != 1 {
		t.Fatalf("got method value wrapper %v, want one with a free variable", bound)
	}
	r := Resolver{Positions: make(Positions), Closures: NewClosures()}
	var got []Resolved
	for src := range r.ResolveFreeVar(bound.FreeVars[0], nil) {
		got = append(got, src)
	}
	if len(got) != 1 || got[0].V != nil {
		t.Errorf("resolveFreeVar = %v, want one unknown source", got)
	}
}
