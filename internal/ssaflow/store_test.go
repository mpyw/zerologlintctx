package ssaflow

import (
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// TestStorePointerOf checks where storePointerOf finds a pointer made: by
// the load of the struct value a field of it is selected from, through the
// fields of fields too.
func TestStorePointerOf(t *testing.T) {
	from := ssa.NewConst(constant.MakeInt64(1), types.Typ[types.Int])
	load := &ssa.UnOp{Op: token.MUL, X: from}
	field := &ssa.Field{X: &ssa.Field{X: load, Field: 1}}
	p := storePointerOf(field)
	if p.def != load || p.from != from || p.global {
		t.Errorf("storePointerOf = (%v, %v, %v), want (%v, %v, false)", p.def, p.from, p.global, load, from)
	}
}

// TestStoreGuardNeed checks what a path needs when a value must be the leaf:
// nothing for the leaf, the value for a Phi or a load, the struct value
// loaded for a field of it, and no path for another value.
func TestStoreGuardNeed(t *testing.T) {
	const src = `package p

type L struct{ n int }

func (*L) m() {}

type H struct{ *L }

func f(a, b *L, h *H, c bool) {
	p := a
	if c {
		p = b
	}
	_ = p.n
	(*h).m()
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
	a, b := fn.Params[0], fn.Params[1]
	var phi *ssa.Phi
	var field *ssa.Field
	for instr := range InstrsIn(fn) {
		switch v := instr.(type) {
		case *ssa.Phi:
			phi = v
		case *ssa.Field:
			field = v
		}
	}
	if phi == nil || field == nil {
		t.Fatalf("no Phi or no field of a struct value: %v, %v", phi, field)
	}
	load, _ := AsLoad(field.X)
	g := newStoreGuard(phi, a, newStorePointers(make(Positions)))
	for _, tt := range []struct {
		name string
		v    ssa.Value
		ok   bool
		kind storeGuardKind
		root ssa.Value
		path int
	}{
		{"the leaf", a, true, storeGuardResolved, nil, 0},
		{"a Phi", phi, true, storeGuardValue, phi, 0},
		{"a field of a loaded struct", field, true, storeGuardValue, load, 1},
		{"another value", b, false, storeGuardResolved, nil, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var st storePath
			ok := g.need(&st, PlaceAt(tt.v))
			if ok != tt.ok || st.kind != tt.kind {
				t.Fatalf("need = %v, kind %v; want %v, kind %v", ok, st.kind, tt.ok, tt.kind)
			}
			if at := g.places[st.at]; tt.root != nil && (at.root != tt.root || len(at.path) != tt.path) {
				t.Errorf("needs %v with %d selections, want %v with %d", at.root, len(at.path), tt.root, tt.path)
			}
		})
	}
}

const storeSrc = `package p

type L struct{ n int }

func (l *L) Read() int { return l.n }

func (l *L) Keep() {}

func f() {
	var l L
	l.Keep()
	_ = l.Read()
}
`

// storeModel is a Model whose method Keep leaves its receiver.
type storeModel struct{}

func (storeModel) IsHandle(types.Type) bool { return false }

func (storeModel) LeavesReceiver(fn *types.Func) bool { return fn.Name() == "Keep" }

// TestStoreReadsOfLeavesReceiver checks that a call of a method the model
// says leaves its receiver is not a read of the receiver.
func TestStoreReadsOfLeavesReceiver(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", storeSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg := types.NewPackage("p", "p")
	ssaPkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset, pkg, []*ast.File{file}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var alloc *ssa.Alloc
	for instr := range InstrsIn(ssaPkg.Func("f")) {
		if a, ok := instr.(*ssa.Alloc); ok {
			alloc = a
		}
	}
	if alloc == nil {
		t.Fatal("no Alloc of l in f")
	}
	at, _ := PlaceOf(alloc)
	reads, _ := storeReadsOf(alloc, at, storeModel{})
	var names []string
	for _, r := range reads {
		if call, ok := r.(*ssa.Call); ok {
			names = append(names, call.Call.StaticCallee().Name())
		}
	}
	if len(names) != 1 || names[0] != "Read" {
		t.Errorf("storeReadsOf calls = %v, want [Read]", names)
	}
}

// TestStoreTargetsReachingWrapper checks that a pointer read from the free
// variable of a wrapper, a method value made where it cannot be seen, is not
// followed: the wrapper has no MakeClosure in a function it is declared in.
func TestStoreTargetsReachingWrapper(t *testing.T) {
	const src = `package p

type T struct{}

func (*T) m() {}

func f(x *T) func() { return x.m }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset, types.NewPackage("p", "p"), []*ast.File{f}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var wrapper *ssa.Function
	for instr := range InstrsIn(pkg.Func("f")) {
		if mc, ok := instr.(*ssa.MakeClosure); ok {
			wrapper = mc.Fn.(*ssa.Function)
		}
	}
	if wrapper == nil || len(wrapper.FreeVars) != 1 {
		t.Fatalf("no method value wrapper with one free variable: %v", wrapper)
	}
	if makers := storeMakersOf(wrapper); makers != nil {
		t.Errorf("storeMakersOf(%v) = %v, want nil", wrapper, makers)
	}
	stored := storeReaching{captured: []Place{PlaceAt(wrapper.FreeVars[0])}}
	if _, ok := storeTargetsReaching(stored, wrapper, nil, newStorePointers(make(Positions))); ok {
		t.Error("storeTargetsReaching of a free variable of a wrapper: ok")
	}
}

// TestStoreViaTo checks the closures that lead from a nested func literal
// out to the function of a value: none for a value of its own, and the
// default for a value in no function, or in a function it is not in.
func TestStoreViaTo(t *testing.T) {
	const src = `package p

func f() {
	x := 0
	func() {
		func() { x++ }()
	}()
}

func g(n int) int { return n + 1 }
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
	f := pkg.Func("f")
	mid, inner := f.AnonFuncs[0], f.AnonFuncs[0].AnonFuncs[0]
	outerMC, innerMC := storeMakersOf(mid)[0], storeMakersOf(inner)[0]
	x := outerMC.Bindings[0]
	g := pkg.Func("g").Blocks[0]
	ret := g.Instrs[len(g.Instrs)-1].(*ssa.Return)
	def := []*ssa.MakeClosure{outerMC}

	for _, tt := range []struct {
		name string
		v    ssa.Value
		want []*ssa.MakeClosure
	}{
		{"a variable of the outer function", x, []*ssa.MakeClosure{innerMC, outerMC}},
		{"a free variable of its own", inner.FreeVars[0], nil},
		{"a constant", ssa.NewConst(constant.MakeInt64(1), types.Typ[types.Int]), def},
		{"a value of a function it is not in", ret.Results[0], def},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := storeViaTo(inner, tt.v, def); !slices.Equal(got, tt.want) {
				t.Errorf("storeViaTo(%v) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}

// TestStoreVariableOnce checks which local variables hold one value wherever
// they are loaded, whole or by a field or element, and that two loads of one, in a closure that captures it
// too, are one index value.
func TestStoreVariableOnce(t *testing.T) {
	const src = `package p

func use(*int) {}

func f(a int, s []int, cond bool, arr [2]int, brr [2]int) {
	_ = s[a]
	func() { _ = s[a] }()
	b := 0
	b++
	func() { _ = b }()
	c := 0
	use(&c)
	func() { _ = c }()
	d := 0
	func() { d = 1 }()
	_ = d
	for range s {
		e := 0
		func() { _ = e }()
	}
	var g int
	if cond {
		g = 1
	}
	func() { _ = g }()
	func() { _ = arr[0] }()
	func() { brr[1] = 1 }()
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
	allocs := make(map[string]*ssa.Alloc)
	for instr := range InstrsIn(fn) {
		if a, ok := instr.(*ssa.Alloc); ok {
			allocs[a.Comment] = a
		}
	}
	w := &storeWalk{positions: make(Positions)}
	for name, want := range map[string]bool{"a": true, "arr": true, "brr": false, "b": false, "c": false, "d": false, "e": false, "g": false} {
		if got := w.storeVariableOnce(allocs[name]); got != want {
			t.Errorf("storeVariableOnce(%s) = %v, want %v", name, got, want)
		}
	}

	// The load of a in f, and the one in the closure that captures it
	var outer, inner ssa.Value
	for instr := range InstrsIn(fn) {
		if load, ok := AsLoad(storeValueOf(instr)); ok && load.X == allocs["a"] {
			outer = load
		}
	}
	for instr := range InstrsIn(fn.AnonFuncs[0]) {
		if load, ok := AsLoad(storeValueOf(instr)); ok {
			if _, isFreeVar := load.X.(*ssa.FreeVar); isFreeVar && load.Type() == types.Typ[types.Int] {
				inner = load
			}
		}
	}
	if outer == nil || inner == nil {
		t.Fatalf("loads of a: %v in f, %v in the closure", outer, inner)
	}
	if !w.indexOnce(outer, inner) {
		t.Error("indexOnce(two loads of a) = false, want true")
	}
	if w.indexOnce(outer, allocs["a"]) {
		t.Error("indexOnce(a load of a and its address) = true, want false")
	}
}

// storeValueOf returns instr as a value, or nil when it is not one.
func storeValueOf(instr ssa.Instruction) ssa.Value {
	v, _ := instr.(ssa.Value)
	return v
}

// TestStoreTypeHolds checks which values may hold a pointer, not through
// another pointer: the pointer, a struct or array of one, and
// unsafe.Pointer, but not a pointer to it or another type.
func TestStoreTypeHolds(t *testing.T) {
	ptr := types.NewPointer(types.Typ[types.Int])
	box := types.NewStruct([]*types.Var{types.NewField(token.NoPos, nil, "p", ptr, false)}, nil)
	tests := []struct {
		name string
		t    types.Type
		want bool
	}{
		{"the pointer", ptr, true},
		{"a struct of one", box, true},
		{"an array of structs of one", types.NewArray(box, 2), true},
		{"unsafe.Pointer", types.Typ[types.UnsafePointer], true},
		{"a pointer to a struct of one", types.NewPointer(box), false},
		{"another basic type", types.Typ[types.Int], false},
		{"a struct of another type", types.NewStruct([]*types.Var{types.NewField(token.NoPos, nil, "n", types.Typ[types.Int], false)}, nil), false},
	}
	for _, tt := range tests {
		if got := storeTypeHolds(tt.t, ptr); got != tt.want {
			t.Errorf("storeTypeHolds(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
