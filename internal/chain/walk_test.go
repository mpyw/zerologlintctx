package chain

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

// walkSrc has a branch, and a loop after it:
//
//	entry -> if.then, if.else -> if.done -> for.loop <-> for.body
//	                                         for.loop -> for.done (return)
const walkSrc = `package p

func f(c bool, n int) int {
	x := 0
	if c {
		x = 1
	} else {
		x = 2
	}
	for i := 0; i < n; i++ {
		x += i
	}
	return x
}
`

// walkFunc builds f of walkSrc in SSA, and returns its blocks by comment.
func walkFunc(t *testing.T) (*ssa.Function, map[string]*ssa.BasicBlock) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", walkSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg := types.NewPackage("p", "p")
	ssaPkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset, pkg, []*ast.File{f}, 0)
	if err != nil {
		t.Fatal(err)
	}
	fn := ssaPkg.Func("f")
	blocks := make(map[string]*ssa.BasicBlock)
	for _, b := range fn.Blocks {
		blocks[b.Comment] = b
	}
	for _, name := range []string{"entry", "if.then", "if.else", "if.done", "for.loop", "for.body", "for.done"} {
		if blocks[name] == nil {
			t.Fatalf("no block %q in f", name)
		}
	}
	return fn, blocks
}

// walkRecorder counts the instructions a walk visits, and the blocks it
// visits them in.
type walkRecorder struct {
	instrs map[ssa.Instruction]int
	blocks map[string]bool
}

func newWalkRecorder() *walkRecorder {
	return &walkRecorder{instrs: make(map[ssa.Instruction]int), blocks: make(map[string]bool)}
}

// visit records instr, and tells the path what step says for its block, or
// to go on.
func (r *walkRecorder) visit(step map[string]walkStep) walkVisit {
	return func(instr ssa.Instruction) walkStep {
		r.instrs[instr]++
		r.blocks[instr.Block().Comment] = true
		return step[instr.Block().Comment]
	}
}

// walked returns the names of the blocks visited, sorted.
func (r *walkRecorder) walked() []string {
	var names []string
	for name := range r.blocks {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func TestWalkBack(t *testing.T) {
	fn, blocks := walkFunc(t)
	positions := make(instrPositions)
	done := blocks["for.done"]
	ret := done.Instrs[len(done.Instrs)-1]
	from := []walkPoint{walkPointBefore(ret, positions)}
	// for.done holds only the return, which the walk starts before
	all := []string{"entry", "for.body", "for.loop", "if.done", "if.else", "if.then"}

	t.Run("every path, each instruction once", func(t *testing.T) {
		r := newWalkRecorder()
		entries := 0
		atEntry := func() bool { entries++; return true }
		if !walkBack(from, r.visit(nil), atEntry, nil) {
			t.Fatal("walkBack = false, want true")
		}
		for instr := range instrsIn(fn) {
			want := 1
			if instr == ret {
				want = 0 // The walk starts before it
			}
			if r.instrs[instr] != want {
				t.Errorf("%v in %s visited %d times, want %d", instr, instr.Block().Comment, r.instrs[instr], want)
			}
		}
		if entries != 1 {
			t.Errorf("atEntry called %d times, want 1", entries)
		}
	})

	t.Run("stop ends a path only", func(t *testing.T) {
		r := newWalkRecorder()
		entries := 0
		atEntry := func() bool { entries++; return true }
		if !walkBack(from, r.visit(map[string]walkStep{"if.then": walkStop}), atEntry, nil) {
			t.Fatal("walkBack = false, want true")
		}
		if entries != 1 {
			t.Errorf("atEntry called %d times, want 1: the path through if.else reaches it", entries)
		}
		r = newWalkRecorder()
		entries = 0
		walkBack(from, r.visit(map[string]walkStep{"if.done": walkStop}), atEntry, nil)
		if entries != 0 || r.blocks["if.then"] || r.blocks["entry"] {
			t.Errorf("stopped at if.done, walked %v with %d entries, want none before it", r.walked(), entries)
		}
	})

	t.Run("abort ends the walk", func(t *testing.T) {
		if walkBack(from, newWalkRecorder().visit(map[string]walkStep{"for.loop": walkAbort}), nil, nil) {
			t.Error("walkBack = true, want false")
		}
		if walkBack(from, newWalkRecorder().visit(nil), func() bool { return false }, nil) {
			t.Error("walkBack with atEntry false = true, want false")
		}
	})

	t.Run("through keeps to the blocks it accepts", func(t *testing.T) {
		r := newWalkRecorder()
		through := func(b *ssa.BasicBlock) bool { return b != blocks["if.else"] }
		walkBack(from, r.visit(nil), nil, through)
		want := slices.DeleteFunc(slices.Clone(all), func(s string) bool { return s == "if.else" })
		if got := r.walked(); !slices.Equal(got, want) {
			t.Errorf("walked %v, want %v", got, want)
		}
	})

	t.Run("a block once in each state", func(t *testing.T) {
		r := newWalkRecorder()
		entries := 0
		atEntry := func() bool { entries++; return true }
		// The state is the predecessor of if.done the path came through
		edge := func(b *ssa.BasicBlock, i int, s int) (int, bool) {
			if b == blocks["if.done"] {
				return i, true
			}
			return s, true
		}
		walkBackWith([]walkFrom[int]{{from[0], -1}}, r.visit(nil), atEntry, edge)
		if entries != 2 {
			t.Errorf("atEntry called %d times, want 2: once through each branch", entries)
		}
		if n := r.instrs[blocks["entry"].Instrs[0]]; n != 2 {
			t.Errorf("first instruction of entry visited %d times, want 2", n)
		}
	})

	t.Run("edge drops a path", func(t *testing.T) {
		r := newWalkRecorder()
		edge := func(b *ssa.BasicBlock, i int, s struct{}) (struct{}, bool) {
			return s, b.Preds[i] != blocks["if.then"]
		}
		walkBackWith([]walkFrom[struct{}]{{point: from[0]}}, r.visit(nil), nil, edge)
		if r.blocks["if.then"] || !r.blocks["if.else"] {
			t.Errorf("walked %v, want if.else but not if.then", r.walked())
		}
	})
}

func TestWalkFwd(t *testing.T) {
	fn, blocks := walkFunc(t)
	positions := make(instrPositions)
	first := blocks["entry"].Instrs[0]
	from := walkPointAfter(first, positions)

	t.Run("every path, each instruction once", func(t *testing.T) {
		r := newWalkRecorder()
		if !walkFwd(from, r.visit(nil)) {
			t.Fatal("walkFwd = false, want true")
		}
		for instr := range instrsIn(fn) {
			want := 1
			if instr == first {
				want = 0 // The walk starts after it
			}
			if r.instrs[instr] != want {
				t.Errorf("%v in %s visited %d times, want %d", instr, instr.Block().Comment, r.instrs[instr], want)
			}
		}
	})

	t.Run("stop ends a path only", func(t *testing.T) {
		r := newWalkRecorder()
		walkFwd(from, r.visit(map[string]walkStep{"if.then": walkStop}))
		if !r.blocks["for.done"] {
			t.Errorf("walked %v, want for.done through if.else", r.walked())
		}
		r = newWalkRecorder()
		walkFwd(from, r.visit(map[string]walkStep{"for.loop": walkStop}))
		if r.blocks["for.body"] || r.blocks["for.done"] {
			t.Errorf("walked %v, want nothing after for.loop", r.walked())
		}
	})

	t.Run("abort ends the walk", func(t *testing.T) {
		if walkFwd(from, newWalkRecorder().visit(map[string]walkStep{"for.done": walkAbort})) {
			t.Error("walkFwd = true, want false")
		}
	})
}

func TestWalkThrough(t *testing.T) {
	_, blocks := walkFunc(t)
	positions := make(instrPositions)
	through := walkThrough(blocks["if.then"].Instrs[0], positions)
	for name, b := range blocks {
		want := name != "entry" && name != "if.else"
		if got := through(b); got != want {
			t.Errorf("walkThrough(if.then)(%s) = %v, want %v", name, got, want)
		}
	}
}
