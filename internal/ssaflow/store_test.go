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

// TestStorePickedThrough checks which edges of a Phi of pointers walkBefore
// takes for a picked variable, through edges that are Phis themselves.
func TestStorePickedThrough(t *testing.T) {
	leaf := ssa.NewConst(constant.MakeInt64(1), types.Typ[types.Int])
	other := ssa.NewConst(constant.MakeInt64(2), types.Typ[types.Int])
	inner := &ssa.Phi{Edges: []ssa.Value{other, leaf}}
	elsewhere := &ssa.Phi{Edges: []ssa.Value{other}}
	loop := &ssa.Phi{}
	loop.Edges = []ssa.Value{loop, elsewhere} // Leads back to itself, never to leaf
	outer := &ssa.Phi{Edges: []ssa.Value{inner, loop, leaf}}
	w := &storeWalk{picked: &storePick{phi: outer, leaf: leaf}}

	for _, tt := range []struct {
		name  string
		edge  ssa.Value
		need  *ssa.Phi
		takes bool
	}{
		{"the variable itself", leaf, nil, true},
		{"a Phi that holds it", inner, inner, true},
		{"a Phi that leads only back to itself and elsewhere", loop, nil, false},
		{"another value", other, nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			need, takes := w.pickedThrough(tt.edge)
			if need != tt.need || takes != tt.takes {
				t.Errorf("pickedThrough = (%p, %v), want (%p, %v)", need, takes, tt.need, tt.takes)
			}
		})
	}
}

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

// TestStorePickedStoredLater checks that a pointer a Phi picks, made in a
// loop after the Phi, is walked for only when no store goes through it.
func TestStorePickedStoredLater(t *testing.T) {
	const src = `package p

type L struct{ n int }

func mk() *L { return new(L) }

func storedThrough(a *L, l L, n int) {
	p := a
	for range n {
		_ = p.n
		q := mk()
		*q = l
		p = q
	}
}

func onlyMade(a *L, n int) {
	p := a
	for range n {
		_ = p.n
		p = mk()
	}
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
	positions := make(Positions)
	for name, want := range map[string]bool{"storedThrough": false, "onlyMade": true} {
		var pick *storePick
		for instr := range InstrsIn(pkg.Func(name)) {
			phi, isPhi := instr.(*ssa.Phi)
			if !isPhi {
				continue
			}
			for _, edge := range phi.Edges {
				if call, isCall := edge.(*ssa.Call); isCall && positions.reaches(phi, call) {
					pick = &storePick{phi: phi, leaf: call}
				}
			}
		}
		if pick == nil {
			t.Fatalf("%s: no Phi with a pointer made after it", name)
		}
		pointer, ok := pick.pointerOf(positions)
		if !ok {
			t.Fatalf("%s: pointerOf ok = false, want true", name)
		}
		at, _ := PlaceOf(pick.leaf)
		w := &storeWalk{at: at, positions: positions, picked: pick, pointer: pointer}
		if got := !w.pickedStoredLater(); got != want {
			t.Errorf("%s: walked for = %v, want %v", name, got, want)
		}
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
	if _, ok := storeTargetsReaching(stored, wrapper, nil, make(Positions)); ok {
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
// they are loaded, and that two loads of one, in a closure that captures it
// too, are one index value.
func TestStoreVariableOnce(t *testing.T) {
	const src = `package p

func use(*int) {}

func f(a int, s []int, cond bool) {
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
	for name, want := range map[string]bool{"a": true, "b": false, "c": false, "d": false, "e": false, "g": false} {
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
