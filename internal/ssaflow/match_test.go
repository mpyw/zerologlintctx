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

const matchSrc = `package p

type holder struct{ e *int }

var gh holder

var gp *holder

var ga [2]holder

var gs []holder

func use(*int) {}

func mk() *holder { return nil }

func mka() [1]*holder { return [1]*holder{} }

var ch chan *holder

func f(pp **holder, i int) {
	use((*pp).e)
	use((*pp).e)
	h := mk()
	use(h.e)
	use(mk().e)
	use(gh.e)
	use(gp.e)
	use(ga[i].e)
	use(func() holder { return gh }().e)
	use(gs[i].e)
	use((<-ch).e)
	use(mka()[0].e)
}
`

// matchArgs returns the argument of each call of use in f of matchSrc.
func matchArgs(t *testing.T) []ssa.Value {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", matchSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset, types.NewPackage("p", "p"), []*ast.File{f}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var args []ssa.Value
	for instr := range InstrsIn(pkg.Func("f")) {
		if call, ok := instr.(*ssa.Call); ok && call.Call.StaticCallee() == pkg.Func("use") {
			args = append(args, call.Call.Args[0])
		}
	}
	if len(args) != 11 {
		t.Fatalf("calls of use = %d, want 11", len(args))
	}
	return args
}

// matchAddr returns the address arg, a load, reads.
func matchAddr(t *testing.T, arg ssa.Value) ssa.Value {
	t.Helper()
	load, ok := AsLoad(arg)
	if !ok {
		t.Fatalf("%v is not a load", arg)
	}
	return load.X
}

func TestMatchedPlacesKeyOf(t *testing.T) {
	args := matchArgs(t)
	m := NewMatchedPlaces()
	keys := make([]PlaceKey, 4)
	for i, arg := range args[:4] {
		at, _ := PlaceOf(matchAddr(t, arg))
		key, ok := m.KeyOf(at)
		if !ok {
			t.Fatalf("KeyOf(%v): not ok", at.root)
		}
		keys[i] = key
	}
	if keys[0] != keys[1] {
		t.Error("two reads of (*pp).e have two keys")
	}
	if keys[2] == keys[3] || keys[0] == keys[2] {
		t.Error("reads through two pointers have the same key")
	}
	for _, arg := range args[4:7] {
		at, _ := PlaceOf(matchAddr(t, arg))
		if _, ok := m.KeyOf(at); ok {
			t.Errorf("KeyOf(%v) of a global: ok", at.root)
		}
	}
}

func TestMatchFromGlobal(t *testing.T) {
	args := matchArgs(t)
	for i, want := range []bool{false, false, false, false, true, true, true} {
		if addr := matchAddr(t, args[i]); matchFromGlobal(addr) != want {
			t.Errorf("matchFromGlobal(%v) = %v, want %v", addr, !want, want)
		}
	}
	if matchFromGlobal(args[7]) {
		t.Errorf("matchFromGlobal(%v) of a field of a call: true", args[7])
	}
	if !matchFromGlobal(matchAddr(t, args[8])) {
		t.Errorf("matchFromGlobal(%v) of an element of a global slice: false", args[8])
	}
	for _, arg := range args[9:] {
		if addr := matchAddr(t, arg); matchFromGlobal(addr) {
			t.Errorf("matchFromGlobal(%v) of a receive or an element of a call: true", addr)
		}
	}
}
