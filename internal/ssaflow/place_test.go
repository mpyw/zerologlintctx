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

const placeSrc = `package p

type inner struct{ a, b int }

type outer struct {
	x     int
	in    inner
	arr   [4]inner
}

var global outer

func f(i int, p *outer) {
	var o outer
	o.in.b = 1
	o.in.b = 2
	o.arr[1].a = 3
	o.arr[uint8(1)].a = 4
	o.arr[i].a = 5
	o.arr[i].a = 6
	p.in.a = 7
	global.in.a = 8
	_ = o.in.b
	_ = mk().arr[2].b
}

func mk() outer { return outer{} }
`

// placeFunc builds f of placeSrc in SSA.
func placeFunc(t *testing.T) *ssa.Function {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", placeSrc, 0)
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

// samePlace reports whether p and q have the same root, and select the same
// as prefixOf compares them, so that an element at a variable index is not
// the same as any.
func samePlace(p, q Place) bool {
	return len(p.path) == len(q.path) && p.prefixOf(q)
}

// placeStores returns the address of each store in fn, in order.
func placeStores(fn *ssa.Function) []ssa.Value {
	var addrs []ssa.Value
	for instr := range InstrsIn(fn) {
		if store, ok := instr.(*ssa.Store); ok {
			addrs = append(addrs, store.Addr)
		}
	}
	return addrs
}

func TestPlaceOf(t *testing.T) {
	fn := placeFunc(t)
	addrs := placeStores(fn)
	// o.in.b = 1, o.in.b = 2, o.arr[1].a, o.arr[uint8(1)].a, o.arr[i].a ×2,
	// p.in.a, global.in.a
	if len(addrs) != 8 {
		t.Fatalf("got %d stores, want 8", len(addrs))
	}
	inB1, _ := PlaceOf(addrs[0])
	inB2, tracked := PlaceOf(addrs[1])
	if !tracked {
		t.Error("a local variable is not tracked")
	}
	if _, isAlloc := inB2.root.(*ssa.Alloc); !isAlloc || len(inB2.path) != 2 {
		t.Fatalf("o.in.b: root %T, %d selections, want an Alloc and 2", inB2.root, len(inB2.path))
	}
	if inB2.path[0].field != 1 || inB2.path[1].field != 1 {
		t.Errorf("o.in.b selects fields %d and %d, want 1 and 1", inB2.path[0].field, inB2.path[1].field)
	}
	if addrs[0] == addrs[1] || !samePlace(inB1, inB2) || inB1.Key() != inB2.Key() {
		t.Error("two accesses of o.in.b are not the same place")
	}

	arr1, _ := PlaceOf(addrs[2])
	arrU1, _ := PlaceOf(addrs[3])
	if !samePlace(arr1, arrU1) {
		t.Error("o.arr[1] and o.arr[uint8(1)] do not select the same")
	}
	if arr1.Key() == arrU1.Key() {
		t.Error("the key of a constant element does not spell its type")
	}

	arrI1, _ := PlaceOf(addrs[4])
	arrI2, _ := PlaceOf(addrs[5])
	if samePlace(arrI1, arrI1) || samePlace(arrI1, arrI2) || samePlace(arrI1, arr1) {
		t.Error("an element with a variable index is the same as another selection")
	}
	if arrI1.Key() != arrI2.Key() {
		t.Error("two elements at the same variable index have different keys")
	}
	if !arrI1.Variable() || arr1.Variable() || inB2.Variable() {
		t.Error("Variable does not tell o.arr[i].a from o.arr[1].a and o.in.b")
	}
	if !placePrefix(arrI1, 1).prefixOf(arrI2) || arrI1.prefixOf(arrI2) {
		t.Error("o.arr is not a prefix of o.arr[i].a, or o.arr[i].a is")
	}
	if inB2.prefixOf(arr1) || !placePrefix(inB2, 0).prefixOf(arr1) {
		t.Error("o.in.b is a prefix of o.arr[1].a, or o is not")
	}

	param, tracked := PlaceOf(addrs[6])
	if _, isParam := param.root.(*ssa.Parameter); !isParam || !tracked {
		t.Errorf("p.in.a: root %T, tracked %v, want a tracked Parameter", param.root, tracked)
	}
	if samePlace(param, inB2) || param.Key() == inB2.Key() {
		t.Error("places with different roots are the same")
	}
	if g, tracked := PlaceOf(addrs[7]); tracked {
		t.Errorf("global.in.a, root %T, is tracked", g.root)
	}

	if !addressesMatch(addrs[0], addrs[1]) || addressesMatch(addrs[2], addrs[4]) || addressesMatch(addrs[4], addrs[5]) {
		t.Error("addressesMatch does not follow sel.same")
	}
}

func TestPlaceWith(t *testing.T) {
	fn := placeFunc(t)
	at, _ := PlaceOf(placeStores(fn)[0])
	base := placePrefix(at, 1)
	ext := base.with(sel{field: 1})
	ext.path[0] = sel{field: 3}
	if base.path[0].field != 1 {
		t.Error("with shares the path of the place it extends")
	}
	if !samePlace(placePrefix(at, 1).with(sel{field: 1}), at) {
		t.Error("o.in with .b is not o.in.b")
	}
}

func TestPlaceFields(t *testing.T) {
	fn := placeFunc(t)
	addrs := placeStores(fn)
	inB, _ := PlaceOf(addrs[0]) // o.in.b
	if !samePlace(placePrefix(inB, 0).WithFields([]int{1, 1}), inB) {
		t.Error("o with .in.b is not o.in.b")
	}
}

func TestValueSelAndLoad(t *testing.T) {
	fn := placeFunc(t)
	var fields, indexes int
	for instr := range InstrsIn(fn) {
		v, ok := instr.(ssa.Value)
		if !ok {
			continue
		}
		if from, s, ok := valueSel(v); ok {
			if from == nil {
				t.Errorf("%v selects from nothing", v)
			}
			switch v.(type) {
			case *ssa.Field:
				fields++
			case *ssa.Index:
				indexes++
				if s.index == nil || s.variable() {
					t.Errorf("%v: mk().arr[2] has no constant index", v)
				}
			}
		}
		if _, _, ok := addrSel(v); ok {
			if _, isLoad := AsLoad(v); isLoad {
				t.Errorf("%v is a selection and a load", v)
			}
		}
	}
	// mk().arr[2].b
	if fields != 2 || indexes != 1 {
		t.Errorf("got %d fields and %d elements of values, want 2 and 1", fields, indexes)
	}
}

func TestPlaceOverlaps(t *testing.T) {
	addrs := placeStores(placeFunc(t))
	inB, _ := PlaceOf(addrs[0])
	arr1, _ := PlaceOf(addrs[2])
	arrI, _ := PlaceOf(addrs[4])
	param, _ := PlaceOf(addrs[6])
	for _, tt := range []struct {
		name string
		p, q Place
		want bool
	}{
		{"the same place", inB, inB, true},
		{"o and o.in.b", placePrefix(inB, 0), inB, true},
		{"o.in.b and o", inB, placePrefix(inB, 0), true},
		{"o.in.b and o.arr[1].a", inB, arr1, false},
		{"o.arr[i].a and o.arr[1].a", arrI, arr1, true},
		{"o.arr[1] and o.arr[i]", placePrefix(arr1, 2), placePrefix(arrI, 2), true},
		{"o.in.b and o.arr[i].a", inB, arrI, false},
		{"different roots", inB, param, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.overlaps(tt.q); got != tt.want {
				t.Errorf("overlaps = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlaceCovers(t *testing.T) {
	addrs := placeStores(placeFunc(t))
	inB, _ := PlaceOf(addrs[0])
	arr1, _ := PlaceOf(addrs[2])
	arrI1, _ := PlaceOf(addrs[4])
	arrI2, _ := PlaceOf(addrs[5])
	param, _ := PlaceOf(addrs[6])
	once := func(a, b ssa.Value) bool { return a == b }
	again := func(a, b ssa.Value) bool { return false }
	for _, tt := range []struct {
		name string
		p, q Place
		once func(a, b ssa.Value) bool
		want placeCover
	}{
		{"the same place", inB, inB, once, coverMust},
		{"o and o.in.b", placePrefix(inB, 0), inB, once, coverMust},
		{"o.in.b and o, which it selects from", inB, placePrefix(inB, 0), once, coverNone},
		{"o.in.b and o.arr[1].a", inB, arr1, once, coverNone},
		{"o.arr[i].a and o.arr[i].a, i once", arrI1, arrI2, once, coverMust},
		{"o.arr[i].a and o.arr[i].a, i again", arrI1, arrI2, again, coverMay},
		{"o.arr[i] and o.arr[i].a", placePrefix(arrI1, 2), arrI2, once, coverMust},
		{"o.arr[1].a and o.arr[i].a", arr1, arrI1, once, coverMay},
		{"o.arr[i].a and o.arr[1].a", arrI1, arr1, once, coverMay},
		{"different roots", inB, param, once, coverNone},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.covers(tt.q, tt.once); got != tt.want {
				t.Errorf("covers = %v, want %v", got, tt.want)
			}
		})
	}
}

// placePrefix returns the place that the first n selections of p select.
func placePrefix(p Place, n int) Place {
	return Place{root: p.root, path: p.path[:n]}
}

const placeLoadedSrc = `package p

type inner struct{}

func (*inner) use() {}

type pair struct{ *inner }

func g(c bool, n int, p, q *pair, mk func() pair) {
	(*p).use()
	w := *p
	if c {
		w = *q
	}
	w.use()
	for range n {
		if c {
			w = *q
		}
	}
	w.use()
	var z pair
	if c {
		z = *p
	}
	z.use()
	v := mk()
	if c {
		v = *p
	}
	v.use()
}
`

func TestLoadedSelection(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", placeLoadedSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	ssaPkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset, types.NewPackage("p", "p"), []*ast.File{f}, 0)
	if err != nil {
		t.Fatal(err)
	}
	fn := ssaPkg.Func("g")
	var args []ssa.Value
	for instr := range InstrsIn(fn) {
		if call, ok := instr.(*ssa.Call); ok && call.Call.StaticCallee() != nil && call.Call.StaticCallee().Name() == "use" {
			args = append(args, call.Call.Args[0])
		}
	}
	if len(args) != 5 {
		t.Fatalf("calls of use = %d, want 5", len(args))
	}
	p, q := fn.Params[2], fn.Params[3]
	tests := []struct {
		name  string
		roots []ssa.Value // The root of each read, nil when ok is false
		ok    bool
	}{
		{"a field of a load", []ssa.Value{p}, true},
		{"a field of a Phi of loads", []ssa.Value{p, q}, true},
		{"a Phi that leads back to itself", []ssa.Value{p, q}, true},
		{"a Phi with the zero value", []ssa.Value{p}, true},
		{"a Phi with a call", nil, false},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reads, ok := LoadedSelection(args[i])
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			roots := make(map[ssa.Value]bool)
			for _, rd := range reads {
				if len(rd.At.path) != 1 {
					t.Errorf("path of %v = %v, want one field", rd.At.root, rd.At.path)
				}
				roots[rd.At.root] = true
			}
			if len(roots) != len(tt.roots) {
				t.Fatalf("roots = %v, want %v", roots, tt.roots)
			}
			for _, r := range tt.roots {
				if !roots[r] {
					t.Errorf("no read from %v", r)
				}
			}
		})
	}
	if _, ok := LoadedSelection(p); ok {
		t.Error("ok for a value that selects nothing")
	}
}

// TestLoadedSelectionThrough checks the edges a read is taken through, and
// the bound on the reads found.
func TestLoadedSelectionThrough(t *testing.T) {
	addr := ssa.NewConst(nil, types.NewPointer(types.NewStruct(nil, nil)))
	load := func() *ssa.UnOp { return &ssa.UnOp{Op: token.MUL, X: addr} }
	a, b := load(), load()
	inner := &ssa.Phi{Edges: []ssa.Value{a, b}}
	outer := &ssa.Phi{Edges: []ssa.Value{inner}}
	inner.Edges = append(inner.Edges, outer) // Back to a Phi on the way
	reads, ok := LoadedSelection(&ssa.Field{X: outer})
	if !ok || len(reads) != 2 {
		t.Fatalf("LoadedSelection = %d reads, %v, want 2 reads", len(reads), ok)
	}
	want := []LoadedEdge{{outer, 0}, {inner, 1}}
	if got := reads[1].Through; reads[1].Load != b || !slices.Equal(got, want) {
		t.Errorf("second read = %v through %v, want %v through %v", reads[1].Load, got, b, want)
	}

	many := &ssa.Phi{}
	for range loadedReadsMax + 1 {
		many.Edges = append(many.Edges, load())
	}
	if _, ok := LoadedSelection(&ssa.Field{X: many}); ok {
		t.Errorf("LoadedSelection of %d loads: ok", len(many.Edges))
	}
}
