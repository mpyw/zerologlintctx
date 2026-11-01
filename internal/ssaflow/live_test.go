package ssaflow

import (
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"runtime"
	"slices"
	"testing"
	"time"
	"weak"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// liveSrc branches on constants, and on a parameter.
const liveSrc = `package p

const debug = false

func g() {}

func f(x int) int {
	if false {
		x = 1
		g()
	}
	return x
}

func h(c bool) (x int) {
	defer func() {
		if recover() != nil {
			x = 2
		}
	}()
	if true {
		x = 1
	}
	if c {
		panic(c)
	}
	return x
}

func k(c bool, x int) int {
	if c {
		x = 1
	}
	return x
}
`

// liveFuncs builds the functions of liveSrc in SSA.
func liveFuncs(t *testing.T) *ssa.Package {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", liveSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg := types.NewPackage("p", "p")
	ssaPkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset, pkg, []*ast.File{f}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return ssaPkg
}

// liveBlock returns the block of fn with the comment.
func liveBlock(t *testing.T, fn *ssa.Function, comment string) *ssa.BasicBlock {
	t.Helper()
	for _, b := range fn.Blocks {
		if b.Comment == comment {
			return b
		}
	}
	t.Fatalf("no block %q in %s", comment, fn.Name())
	return nil
}

// liveReturnPhi returns the Phi that the block of the return of fn starts
// with.
func liveReturnPhi(t *testing.T, fn *ssa.Function, comment string) *ssa.Phi {
	t.Helper()
	phi, ok := liveBlock(t, fn, comment).Instrs[0].(*ssa.Phi)
	if !ok {
		t.Fatalf("block %q of %s starts with no Phi", comment, fn.Name())
	}
	return phi
}

// TestLiveFlow checks the edges of `if false`, which never runs its branch.
func TestLiveFlow(t *testing.T) {
	fn := liveFuncs(t).Func("f")
	flow := LiveFlowOf(fn)
	entry, then, done := fn.Blocks[0], liveBlock(t, fn, "if.then"), liveBlock(t, fn, "if.done")

	if got := flow.Succs(entry); !slices.Equal(got, []*ssa.BasicBlock{done}) {
		t.Errorf("Succs(entry) = %v, want [if.done]", got)
	}
	// A block that cannot run takes every edge, as if it ran
	if got := flow.Succs(then); !slices.Equal(got, then.Succs) {
		t.Errorf("Succs(if.then) = %v, want %v", got, then.Succs)
	}
	for i, pred := range done.Preds {
		if want := pred == entry; flow.pred(done, i) != want {
			t.Errorf("pred(if.done, %s) = %v, want %v", pred.Comment, !want, want)
		}
	}
	if !flow.pred(then, 0) {
		t.Error("pred(if.then, entry) = false in a block that cannot run")
	}

	phi := liveReturnPhi(t, fn, "if.done")
	if got := LiveEdgeValues(phi); len(got) != 1 || got[0] != fn.Params[0] {
		t.Errorf("LiveEdgeValues = %v, want [x]", got)
	}
	for i := range LiveEdges(phi) {
		if done.Preds[i] != entry {
			t.Errorf("LiveEdges yields the edge from %s", done.Preds[i].Comment)
		}
		break
	}
}

// TestLiveFlowRecover checks that the recover block can run, though no
// block jumps to it.
func TestLiveFlowRecover(t *testing.T) {
	fn := liveFuncs(t).Func("h")
	if fn.Recover == nil {
		t.Fatal("h has no recover block")
	}
	flow := LiveFlowOf(fn)
	if flow.dead == nil || flow.dead[fn.Recover.Index] {
		t.Errorf("dead = %v, want the recover block %d to run", flow.dead, fn.Recover.Index)
	}
	if got := flow.Succs(fn.Recover); !slices.Equal(got, fn.Recover.Succs) {
		t.Errorf("Succs(recover) = %v, want %v", got, fn.Recover.Succs)
	}
}

// TestLiveFlowNoConstant checks that a function with no branch on a constant
// takes every edge, with no blocks marked.
func TestLiveFlowNoConstant(t *testing.T) {
	fn := liveFuncs(t).Func("k")
	flow := LiveFlowOf(fn)
	if flow.dead != nil {
		t.Errorf("dead = %v, want nil", flow.dead)
	}
	phi := liveReturnPhi(t, fn, "if.done")
	if got := LiveEdgeValues(phi); &got[0] != &phi.Edges[0] {
		t.Errorf("LiveEdgeValues = %v, want phi.Edges itself", got)
	}
}

// TestLiveFlowCache checks that the LiveFlow of a function is found once, and
// dropped once the function is collected.
func TestLiveFlowCache(t *testing.T) {
	key := liveCachedKey(t)
	for deadline := time.Now().Add(10 * time.Second); ; {
		runtime.GC()
		if _, ok := liveCache.Load(key); !ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the entry of a collected function is still in the cache")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// liveCachedKey asks for the LiveFlow of a function twice, and returns its
// key in the cache. The function is not held after it returns.
func liveCachedKey(t *testing.T) weak.Pointer[ssa.Function] {
	t.Helper()
	fn := liveFuncs(t).Func("f")
	first := LiveFlowOf(fn)
	if again := LiveFlowOf(fn); &again.dead[0] != &first.dead[0] {
		t.Error("LiveFlowOf found the flow of f again")
	}
	key := weak.Make(fn)
	if _, ok := liveCache.Load(key); !ok {
		t.Fatal("no entry for f in the cache")
	}
	return key
}

// TestLiveConstCond checks which comparisons of two constants are folded,
// and to what.
func TestLiveConstCond(t *testing.T) {
	boolT, intT := types.Typ[types.Bool], types.Typ[types.Int]
	floatT, complexT := types.Typ[types.Float64], types.Typ[types.Complex128]
	strT := types.Typ[types.String]
	c := func(v constant.Value, typ types.Type) *ssa.Const { return ssa.NewConst(v, typ) }
	one, two := c(constant.MakeInt64(1), intT), c(constant.MakeInt64(2), intT)
	half := c(constant.MakeFloat64(0.5), floatT)
	yes, no := c(constant.MakeBool(true), boolT), c(constant.MakeBool(false), boolT)
	a, b := c(constant.MakeString("a"), strT), c(constant.MakeString("b"), strT)
	i := c(constant.MakeImag(constant.MakeInt64(1)), complexT)
	nilPtr := c(nil, types.NewPointer(intT))

	tests := []struct {
		name   string
		cond   ssa.Value
		want   bool
		wantOK bool
	}{
		{"true", yes, true, true},
		{"not bool", one, false, false},
		{"nil", nilPtr, false, false},
		{"1 == 1", &ssa.BinOp{Op: token.EQL, X: one, Y: one}, true, true},
		{"1 != 1", &ssa.BinOp{Op: token.NEQ, X: one, Y: one}, false, true},
		{"1 < 2", &ssa.BinOp{Op: token.LSS, X: one, Y: two}, true, true},
		{"2 <= 1", &ssa.BinOp{Op: token.LEQ, X: two, Y: one}, false, true},
		{"2 > 0.5", &ssa.BinOp{Op: token.GTR, X: two, Y: half}, true, true},
		{"0.5 >= 1", &ssa.BinOp{Op: token.GEQ, X: half, Y: one}, false, true},
		{"true == false", &ssa.BinOp{Op: token.EQL, X: yes, Y: no}, false, true},
		{`"a" < "b"`, &ssa.BinOp{Op: token.LSS, X: a, Y: b}, true, true},
		{`"a" == "a"`, &ssa.BinOp{Op: token.EQL, X: a, Y: a}, true, true},
		{"1i == 1", &ssa.BinOp{Op: token.EQL, X: i, Y: one}, false, true},
		{"1i < 1", &ssa.BinOp{Op: token.LSS, X: i, Y: one}, false, false},
		{"true < false", &ssa.BinOp{Op: token.LSS, X: yes, Y: no}, false, false},
		{`"a" == 1`, &ssa.BinOp{Op: token.EQL, X: a, Y: one}, false, false},
		{"1 + 2", &ssa.BinOp{Op: token.ADD, X: one, Y: two}, false, false},
		{"nil == nil", &ssa.BinOp{Op: token.EQL, X: nilPtr, Y: nilPtr}, false, false},
		{"x == 1", &ssa.BinOp{Op: token.EQL, X: &ssa.Parameter{}, Y: one}, false, false},
		{"!true", &ssa.UnOp{Op: token.NOT, X: yes}, false, false},
	}
	for _, tt := range tests {
		got, ok := liveConstCond(tt.cond)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("liveConstCond(%s) = %v, %v, want %v, %v", tt.name, got, ok, tt.want, tt.wantOK)
		}
	}
}
