// live.go tells which paths of the control flow of a function can run. A
// branch on a constant, such as `if true`, or `if debug` for `const debug =
// true`, takes one successor only. go/ssa still keeps the edge to the other
// successor, and the blocks that only that edge leads to:
//
//	if true:untyped bool goto 1 else 2   // the edge to 2 is never taken
//
// Every walk of the control flow, and every look at the edges of a Phi, in
// this package and in internal/chain, takes only the edges LiveFlow tells
// can run. Only an If on an *ssa.Const counts, so a path that can run is
// never dropped.

package ssaflow

import (
	"go/constant"
	"iter"

	"golang.org/x/tools/go/ssa"
)

// LiveFlow tells which blocks of a function can run, and which edges between
// them control can take.
//
// Inside a block that cannot run, every edge is taken, as if the block ran.
// A walk that starts there, or a Phi there, then sees what it saw before
// branches on constants were told apart. A walk that starts in a block that
// can run stays in such blocks.
type LiveFlow struct {
	// dead marks, by the Index of each block, the blocks that no path from
	// the entry or the recover block reaches. It is nil when the function
	// has no branch on a constant.
	dead []bool
}

// LiveFlowOf returns the LiveFlow of fn.
func LiveFlowOf(fn *ssa.Function) LiveFlow {
	if !hasLiveConstBranch(fn) {
		return LiveFlow{}
	}
	dead := make([]bool, len(fn.Blocks))
	for i := range dead {
		dead[i] = true
	}
	var queue []*ssa.BasicBlock
	for _, root := range []*ssa.BasicBlock{fn.Blocks[0], fn.Recover} {
		if root != nil {
			dead[root.Index] = false
			queue = append(queue, root)
		}
	}
	flow := LiveFlow{dead: dead}
	for len(queue) > 0 {
		b := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, succ := range flow.Succs(b) {
			if dead[succ.Index] {
				dead[succ.Index] = false
				queue = append(queue, succ)
			}
		}
	}
	return flow
}

// hasLiveConstBranch reports whether a block of fn ends with a branch on a
// constant.
func hasLiveConstBranch(fn *ssa.Function) bool {
	for _, b := range fn.Blocks {
		if _, ok := liveConstSucc(b); ok {
			return true
		}
	}
	return false
}

// liveConstSucc returns the index in b.Succs of the successor that b takes,
// when b ends with an If on a constant. ok is false for any other block.
func liveConstSucc(b *ssa.BasicBlock) (_ int, ok bool) {
	branch, isIf := b.Instrs[len(b.Instrs)-1].(*ssa.If)
	if !isIf {
		return 0, false
	}
	c, isConst := branch.Cond.(*ssa.Const)
	if !isConst || c.Value == nil || c.Value.Kind() != constant.Bool {
		return 0, false
	}
	if constant.BoolVal(c.Value) {
		return 0, true
	}
	return 1, true
}

// Succs returns the successors of b that control can go to from b. Each is
// in b.Succs.
func (f LiveFlow) Succs(b *ssa.BasicBlock) []*ssa.BasicBlock {
	if f.dead == nil || f.dead[b.Index] {
		return b.Succs
	}
	if k, ok := liveConstSucc(b); ok {
		return b.Succs[k : k+1]
	}
	return b.Succs
}

// pred reports whether control can come into b from b.Preds[i]: the
// predecessor can run, and the edge is not the one a branch on a constant
// never takes.
//
//declscope:shared // walk.go and match.go walk back only into such predecessors
func (f LiveFlow) pred(b *ssa.BasicBlock, i int) bool {
	if f.dead == nil || f.dead[b.Index] {
		return true
	}
	pred := b.Preds[i]
	if f.dead[pred.Index] {
		return false
	}
	k, ok := liveConstSucc(pred)
	return !ok || pred.Succs[k] == b
}

// edges yields the index and the value of each edge of phi that control can
// take into its block (see LiveFlow.pred). phi is in the function of f.
func (f LiveFlow) edges(phi *ssa.Phi) iter.Seq2[int, ssa.Value] {
	return func(yield func(int, ssa.Value) bool) {
		for i, edge := range phi.Edges {
			if f.pred(phi.Block(), i) && !yield(i, edge) {
				return
			}
		}
	}
}

// LiveEdges is LiveFlow.edges for the function of phi.
func LiveEdges(phi *ssa.Phi) iter.Seq2[int, ssa.Value] {
	return liveFlowAt(phi).edges(phi)
}

// liveFlowAt returns the LiveFlow of the function of phi. A Phi in no block,
// as a test builds one by hand, takes every edge.
func liveFlowAt(phi *ssa.Phi) LiveFlow {
	if phi.Block() == nil {
		return LiveFlow{}
	}
	return LiveFlowOf(phi.Parent())
}

// LiveEdgeValues returns the values of the edges of phi that control can
// take into its block, in order. It is phi.Edges itself when every edge
// can be taken.
func LiveEdgeValues(phi *ssa.Phi) []ssa.Value {
	flow := liveFlowAt(phi)
	if flow.dead == nil || flow.dead[phi.Block().Index] {
		return phi.Edges
	}
	var values []ssa.Value
	for _, edge := range flow.edges(phi) {
		values = append(values, edge)
	}
	return values
}

// liveFlows keeps the LiveFlow of each function a walk goes through, found
// the first time a block of the function is asked about.
//
//declscope:shared // walk.go keeps one per walk back
type liveFlows map[*ssa.Function]LiveFlow

// of returns the LiveFlow of the function of b.
//
//declscope:shared // walk.go asks it at the top of each block
func (m liveFlows) of(b *ssa.BasicBlock) LiveFlow {
	fn := b.Parent()
	flow, ok := m[fn]
	if !ok {
		flow = LiveFlowOf(fn)
		m[fn] = flow
	}
	return flow
}
