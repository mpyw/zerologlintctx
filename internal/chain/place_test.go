package chain

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

// placeStores returns the address of each store in fn, in order.
func placeStores(fn *ssa.Function) []ssa.Value {
	var addrs []ssa.Value
	for instr := range instrsIn(fn) {
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
	inB1, _ := placeOf(addrs[0])
	inB2, tracked := placeOf(addrs[1])
	if !tracked {
		t.Error("a local variable is not tracked")
	}
	if _, isAlloc := inB2.root.(*ssa.Alloc); !isAlloc || len(inB2.path) != 2 {
		t.Fatalf("o.in.b: root %T, %d selections, want an Alloc and 2", inB2.root, len(inB2.path))
	}
	if inB2.path[0].field != 1 || inB2.path[1].field != 1 {
		t.Errorf("o.in.b selects fields %d and %d, want 1 and 1", inB2.path[0].field, inB2.path[1].field)
	}
	if addrs[0] == addrs[1] || !inB1.same(inB2) || inB1.key() != inB2.key() {
		t.Error("two accesses of o.in.b are not the same place")
	}

	arr1, _ := placeOf(addrs[2])
	arrU1, _ := placeOf(addrs[3])
	if !arr1.same(arrU1) {
		t.Error("o.arr[1] and o.arr[uint8(1)] do not select the same")
	}
	if arr1.key() == arrU1.key() {
		t.Error("the key of a constant element does not spell its type")
	}

	arrI1, _ := placeOf(addrs[4])
	arrI2, _ := placeOf(addrs[5])
	if arrI1.same(arrI1) || arrI1.same(arrI2) || arrI1.same(arr1) {
		t.Error("an element with a variable index is the same as another selection")
	}
	if arrI1.key() != arrI2.key() {
		t.Error("two elements at the same variable index have different keys")
	}
	if !arrI1.prefix(1).prefixOf(arrI2) || arrI1.prefixOf(arrI2) {
		t.Error("o.arr is not a prefix of o.arr[i].a, or o.arr[i].a is")
	}
	if inB2.prefixOf(arr1) || !inB2.prefix(0).prefixOf(arr1) {
		t.Error("o.in.b is a prefix of o.arr[1].a, or o is not")
	}

	param, tracked := placeOf(addrs[6])
	if _, isParam := param.root.(*ssa.Parameter); !isParam || !tracked {
		t.Errorf("p.in.a: root %T, tracked %v, want a tracked Parameter", param.root, tracked)
	}
	if param.same(inB2) || param.key() == inB2.key() {
		t.Error("places with different roots are the same")
	}
	if g, tracked := placeOf(addrs[7]); tracked {
		t.Errorf("global.in.a, root %T, is tracked", g.root)
	}

	if !addressesMatch(addrs[0], addrs[1]) || addressesMatch(addrs[2], addrs[4]) || addressesMatch(addrs[4], addrs[5]) {
		t.Error("addressesMatch does not follow sel.same")
	}
}

func TestPlaceWith(t *testing.T) {
	fn := placeFunc(t)
	at, _ := placeOf(placeStores(fn)[0])
	base := at.prefix(1)
	ext := base.with(sel{field: 1})
	ext.path[0] = sel{field: 3}
	if base.path[0].field != 1 {
		t.Error("with shares the path of the place it extends")
	}
	if !at.prefix(1).with(sel{field: 1}).same(at) {
		t.Error("o.in with .b is not o.in.b")
	}
}

func TestValueSelAndLoad(t *testing.T) {
	fn := placeFunc(t)
	var fields, indexes int
	for instr := range instrsIn(fn) {
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
			if _, isLoad := asLoad(v); isLoad {
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
	inB, _ := placeOf(addrs[0])
	arr1, _ := placeOf(addrs[2])
	arrI, _ := placeOf(addrs[4])
	param, _ := placeOf(addrs[6])
	for _, tt := range []struct {
		name string
		p, q place
		want bool
	}{
		{"the same place", inB, inB, true},
		{"o and o.in.b", inB.prefix(0), inB, true},
		{"o.in.b and o", inB, inB.prefix(0), true},
		{"o.in.b and o.arr[1].a", inB, arr1, false},
		{"o.arr[i].a and o.arr[1].a", arrI, arr1, true},
		{"o.arr[1] and o.arr[i]", arr1.prefix(2), arrI.prefix(2), true},
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
