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

// valueSrc updates t in a loop through a func value, so the Phi of the loop
// leads back to itself through a call whose callee is not known. It also
// returns a map element, an element of an array value, and the result of a
// func literal called on the spot.
const valueSrc = `package p

type T struct{ x int }

func arr() [2]*T { return [2]*T{} }

func f(c bool, g func(*T) *T, m map[string]*T) (*T, *T, *T, *T) {
	t := &T{}
	for i := 0; i < 3; i++ {
		if c {
			t = g(t)
		}
	}
	return t, m["k"], arr()[1], func() *T { return nil }()
}
`

// valueFunc builds f of valueSrc in SSA, and returns the values it returns.
func valueFunc(t *testing.T) (*ssa.Function, []ssa.Value) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", valueSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg := types.NewPackage("p", "p")
	ssaPkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset, pkg, []*ast.File{f}, 0)
	if err != nil {
		t.Fatal(err)
	}
	fn := ssaPkg.Func("f")
	for instr := range InstrsIn(fn) {
		if ret, ok := instr.(*ssa.Return); ok {
			return fn, ret.Results
		}
	}
	t.Fatal("no return in f")
	return nil, nil
}

func TestDerivesFromThroughPhiAndDynamicCall(t *testing.T) {
	t.Parallel()
	_, results := valueFunc(t)
	phi, ok := results[0].(*ssa.Phi)
	if !ok {
		t.Fatalf("t is returned as %T, want a Phi", results[0])
	}
	isAlloc := func(v ssa.Value) bool {
		_, ok := v.(*ssa.Alloc)
		return ok
	}
	if !derivesFrom(phi, isAlloc, UnwrapInner, make(map[ssa.Value]bool)) {
		t.Error("the Phi does not derive from the allocation of t")
	}
	never := func(ssa.Value) bool { return false }
	if derivesFrom(phi, never, UnwrapInner, make(map[ssa.Value]bool)) {
		t.Error("derivesFrom found a value that nothing matches")
	}
}

func TestCalleeReceiver(t *testing.T) {
	t.Parallel()
	fn, results := valueFunc(t)
	var dynamic *ssa.Call
	for instr := range InstrsIn(fn) {
		if call, ok := instr.(*ssa.Call); ok && call.Call.StaticCallee() == nil {
			dynamic = call
		}
	}
	if dynamic == nil {
		t.Fatal("no call of g in f")
	}
	if recv := CalleeReceiver(&dynamic.Call); recv != dynamic.Call.Args[0] {
		t.Errorf("CalleeReceiver(%v) = %v, want its first argument", dynamic, recv)
	}
	literal, ok := results[3].(*ssa.Call)
	if !ok {
		t.Fatalf("the func literal's result is returned as %T, want a Call", results[3])
	}
	if recv := CalleeReceiver(&literal.Call); recv != nil {
		t.Errorf("CalleeReceiver(%v) = %v, want nil for a func literal", literal, recv)
	}
}

func TestUnwrapInnerElements(t *testing.T) {
	t.Parallel()
	fn, results := valueFunc(t)
	lookup, ok := results[1].(*ssa.Lookup)
	if !ok {
		t.Fatalf("m[k] is returned as %T, want a Lookup", results[1])
	}
	if got := UnwrapInner(lookup); got != fn.Params[2] {
		t.Errorf("unwrapInner(m[k]) = %v, want m", got)
	}
	index, ok := results[2].(*ssa.Index)
	if !ok {
		t.Fatalf("arr()[1] is returned as %T, want an Index", results[2])
	}
	if got := UnwrapInner(index); got != index.X {
		t.Errorf("unwrapInner(arr()[1]) = %v, want arr()", got)
	}
}
