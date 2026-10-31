package chain

import (
	"go/constant"
	"go/types"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// memoGraph traces the nodes of a graph through a traceMemo. A leaf has a
// verdict of its own, and any other node has every node it points to as a
// source.
type memoGraph struct {
	memo   traceMemo
	edges  map[string][]string
	leaves map[string]verdict
	values map[string]ssa.Value
	calls  map[string]int // How many times each node was traced, not taken from the memo
}

func newMemoGraph(edges map[string][]string, leaves map[string]verdict) *memoGraph {
	return &memoGraph{
		memo:   newTraceMemo(),
		edges:  edges,
		leaves: leaves,
		values: make(map[string]ssa.Value),
		calls:  make(map[string]int),
	}
}

// value returns the SSA value that stands for the node name.
func (g *memoGraph) value(name string) ssa.Value {
	if v, ok := g.values[name]; ok {
		return v
	}
	v := ssa.NewConst(constant.MakeString(name), types.Typ[types.String])
	g.values[name] = v
	return v
}

func (g *memoGraph) trace(name string) verdict {
	return g.memo.trace(memoKey{v: g.value(name)}, true, func() verdict {
		g.calls[name]++
		if v, ok := g.leaves[name]; ok {
			return v
		}
		return all(verdictsOf(g.edges[name], g.trace))
	})
}

func TestTraceMemoInProgressIsNeutral(t *testing.T) {
	t.Parallel()
	// P is an update of itself in a loop, after an event with context
	g := newMemoGraph(map[string][]string{"P": {"ctx", "P"}}, map[string]verdict{"ctx": yes})
	if got := g.trace("P"); got != yes {
		t.Fatalf("trace(P) = %v, want yes", got)
	}
	// It depends on itself only, so it is kept for good
	if got := g.trace("P"); got != yes || g.calls["P"] != 1 {
		t.Errorf("trace(P) again = %v after %d traces, want yes after 1", got, g.calls["P"])
	}
	if len(g.memo.frames) != 0 {
		t.Errorf("frames %v left after the trace", g.memo.frames)
	}
}

func TestTraceMemoCycleOnly(t *testing.T) {
	t.Parallel()
	// A value whose only source leads back to it adds no source
	g := newMemoGraph(map[string][]string{"P": {"Q"}, "Q": {"P"}}, nil)
	if got := g.trace("P"); got != neutral {
		t.Errorf("trace(P) = %v, want neutral", got)
	}
}

func TestTraceMemoProvisional(t *testing.T) {
	t.Parallel()
	// P = Phi(Q, warn), Q = Phi(P, ctx). Q is yes while P is taken as
	// neutral, but its own verdict is no: it reaches warn through P.
	g := newMemoGraph(
		map[string][]string{"P": {"Q", "warn"}, "Q": {"P", "ctx"}},
		map[string]verdict{"warn": no, "ctx": yes},
	)
	if got := g.trace("P"); got != no {
		t.Fatalf("trace(P) = %v, want no", got)
	}
	if got := g.trace("Q"); got != no {
		t.Errorf("trace(Q) = %v, want no: the yes found inside P does not hold once P is done", got)
	}
	if g.calls["Q"] != 2 {
		t.Errorf("Q traced %d times, want 2", g.calls["Q"])
	}
}

func TestTraceMemoProvisionalReusedInsideFrame(t *testing.T) {
	t.Parallel()
	// H = Phi(A, B), A and B both lead to C, and C leads back to H. C is
	// traced once while H is in progress, and again once H is done.
	g := newMemoGraph(
		map[string][]string{"H": {"A", "B"}, "A": {"C"}, "B": {"C"}, "C": {"H", "ctx"}},
		map[string]verdict{"ctx": yes},
	)
	if got := g.trace("H"); got != yes {
		t.Fatalf("trace(H) = %v, want yes", got)
	}
	if g.calls["C"] != 1 {
		t.Errorf("C traced %d times inside H, want 1", g.calls["C"])
	}
	if got := g.trace("H"); got != yes || g.calls["H"] != 1 {
		t.Errorf("trace(H) again = %v after %d traces, want yes after 1", got, g.calls["H"])
	}
	if got := g.trace("C"); got != yes || g.calls["C"] != 2 {
		t.Errorf("trace(C) = %v after %d traces, want yes after 2", got, g.calls["C"])
	}
}

func TestTraceMemoNotKept(t *testing.T) {
	t.Parallel()
	m := newTraceMemo()
	key := memoKey{v: ssa.NewConst(constant.MakeBool(true), types.Typ[types.Bool]), param: true}
	n := 0
	trace := func() verdict {
		n++
		return yes
	}
	m.trace(key, false, trace)
	m.trace(key, false, trace)
	if n != 2 {
		t.Errorf("traced %d times, want 2: a verdict not kept is traced again", n)
	}
}
