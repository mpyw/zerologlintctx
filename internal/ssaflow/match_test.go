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
		if key, ok := m.KeyOf(at); ok || key != at.Key() {
			t.Errorf("KeyOf(%v) of a global = %v, %v, want %v, false", at.root, key, ok, at.Key())
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

// TestMatchedPlacesKeyAtRun checks that a place whose root is not a load of a
// free variable keeps its key.
func TestMatchedPlacesKeyAtRun(t *testing.T) {
	const src = `package p

func f(x *int) {
	func() { _ = *x }()
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset, types.NewPackage("p", "p"), []*ast.File{file}, 0)
	if err != nil {
		t.Fatal(err)
	}
	fn := pkg.Func("f")
	var mc *ssa.MakeClosure
	var call *ssa.Call
	for instr := range InstrsIn(fn) {
		switch in := instr.(type) {
		case *ssa.MakeClosure:
			mc = in
		case *ssa.Call:
			call = in
		}
	}
	if mc == nil || call == nil {
		t.Fatal("no IIFE in f")
	}
	key := PlaceAt(fn.Params[0]).Key()
	if got, ok := NewMatchedPlaces().KeyAtRun(key, mc, call); ok || got != key {
		t.Errorf("KeyAtRun(x) = %v, %v, want %v, false", got, ok, key)
	}
}

// TestMatchedPlacesKeyAtRunNotCaptured checks that a place read through a
// pointer that is not loaded from a variable the func literal captures, or
// that another func literal reads, keeps its key.
func TestMatchedPlacesKeyAtRunNotCaptured(t *testing.T) {
	const src = `package p

func f(x **int) {
	_ = **x
	func() { _ = **x }()
	func() { _ = **x }()
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset, types.NewPackage("p", "p"), []*ast.File{file}, 0)
	if err != nil {
		t.Fatal(err)
	}
	fn := pkg.Func("f")
	firstLoad := func(f *ssa.Function) *ssa.UnOp {
		for instr := range InstrsIn(f) {
			if v, isValue := instr.(ssa.Value); isValue {
				if load, ok := AsLoad(v); ok {
					return load
				}
			}
		}
		t.Fatalf("no load in %s", f)
		return nil
	}
	var mc *ssa.MakeClosure
	var call *ssa.Call
	for instr := range InstrsIn(fn) {
		switch in := instr.(type) {
		case *ssa.MakeClosure:
			if mc == nil {
				mc = in
			}
		case *ssa.Call:
			if call == nil {
				call = in
			}
		}
	}
	if mc == nil || call == nil || len(fn.AnonFuncs) != 2 {
		t.Fatal("no two IIFEs in f")
	}
	for _, load := range []*ssa.UnOp{firstLoad(fn), firstLoad(fn.AnonFuncs[1])} {
		key := PlaceAt(load).Key()
		if got, ok := NewMatchedPlaces().KeyAtRun(key, mc, call); ok || got != key {
			t.Errorf("KeyAtRun(%v) = %v, %v, want %v, false", load, got, ok, key)
		}
	}
}

// TestMatchedPlacesSameAtRuns checks which places that func literals nested in
// f$1 read are the same place as f$1 runs them.
func TestMatchedPlacesSameAtRuns(t *testing.T) {
	const src = `package p

type holder struct{ e, f *int }

func f(h, g *holder, hs [1]*holder) {
	func() {
		func() { _ = h.e }()
		func() { _ = h.e }()
		func() { _ = g.e }()
		func() { _ = h.f }()
		func() { func() { _ = h.e }() }()
		func(pk **holder) { _ = (*pk).e }(&h)
		func() { _ = hs[0].e }()
		func() { _ = hs[0].e }()
	}()
	func() { _ = h.e }()
	func() {
		func() { _ = h.e }()
		func() { _ = h.e }()
		h = g
	}()
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset, types.NewPackage("p", "p"), []*ast.File{file}, 0)
	if err != nil {
		t.Fatal(err)
	}
	fn := pkg.Func("f")
	// keyIn returns the key of the field the func literal at path reads,
	// through the anonymous functions of fn, outermost first.
	keyIn := func(path ...int) PlaceKey {
		lit := fn
		for _, i := range path {
			lit = lit.AnonFuncs[i]
		}
		for instr := range InstrsIn(lit) {
			if fa, ok := instr.(*ssa.FieldAddr); ok {
				at, _ := PlaceOf(fa)
				return at.Key()
			}
		}
		t.Fatalf("no field read in %s", lit)
		return PlaceKey{}
	}
	outer, stored := fn.AnonFuncs[0], fn.AnonFuncs[2]
	m := NewMatchedPlaces()
	for _, tc := range []struct {
		name string
		a, b PlaceKey
		fn   *ssa.Function
		want bool
	}{
		{"h.e on two func literals", keyIn(0, 0), keyIn(0, 1), outer, true},
		{"h.e two func literals down", keyIn(0, 0), keyIn(0, 4, 0), outer, true},
		{"h.e and g.e", keyIn(0, 0), keyIn(0, 2), outer, false},
		{"h.e and h.f", keyIn(0, 0), keyIn(0, 3), outer, false},
		{"h.e and itself", keyIn(0, 0), keyIn(0, 0), outer, true},
		{"h.e and (*pk).e for a parameter pk", keyIn(0, 0), keyIn(0, 5), outer, false},
		{"hs[0].e on two func literals", keyIn(0, 6), keyIn(0, 7), outer, true},
		{"h.e out of the func literal", keyIn(0, 0), keyIn(1), outer, false},
		{"h.e where h is stored to", keyIn(2, 0), keyIn(2, 1), stored, false},
		{"the parameters themselves", PlaceAt(fn.Params[0]).Key(), PlaceAt(fn.Params[1]).Key(), fn, false},
	} {
		if got := m.SameAtRuns(tc.a, tc.b, tc.fn); got != tc.want {
			t.Errorf("%s: SameAtRuns = %v, want %v", tc.name, got, tc.want)
		}
	}
}
