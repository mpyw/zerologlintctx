// walk.go walks the paths of the control flow of a function, block by block:
// backwards to a point with walkBack and walkBackWith, and forwards from one
// with WalkFwd. The callers decide what each instruction means to a path;
// the walk only takes each block once and in the same order.
//
// store.go walks back from a read to the stores that reach it, and
// internal/chain walks to and from an Event.Ctx call. Every declaration is
// shared with the files of the package.
//
//declscope:shared

package ssaflow

import (
	"golang.org/x/tools/go/ssa"
)

// WalkPoint is a point in a function: before the instruction at index of
// block, or at its end when index is the number of its instructions.
type WalkPoint struct {
	block *ssa.BasicBlock
	index int
}

// WalkPointBefore returns the point right before instr.
func WalkPointBefore(instr ssa.Instruction, positions Positions) WalkPoint {
	return WalkPoint{block: instr.Block(), index: positions.of(instr)}
}

// WalkPointAfter returns the point right after instr.
func WalkPointAfter(instr ssa.Instruction, positions Positions) WalkPoint {
	return WalkPoint{block: instr.Block(), index: positions.of(instr) + 1}
}

// WalkStep is what a path does at an instruction a walk looks at.
type WalkStep int

const (
	// WalkOn goes on past the instruction.
	WalkOn WalkStep = iota
	// WalkStop ends the path at the instruction. The other paths go on.
	WalkStop
	// WalkAbort ends the whole walk, which returns false.
	WalkAbort
)

// WalkFrom is a point a walk starts from, with the state of its path there.
type WalkFrom[S comparable] struct {
	Point WalkPoint
	State S
}

// walkVisit looks at an instruction on a path, and tells what the path does
// there.
//
//declscope:private
type walkVisit func(ssa.Instruction) WalkStep

// walkEdge tells, for a path that reaches the top of b in state s, whether it
// goes on into the i-th predecessor of b, and in which state at its end.
//
//declscope:private
type walkEdge[S comparable] func(b *ssa.BasicBlock, i int, s S) (S, bool)

// walkBack walks every path that leads to a point of from, backwards. On each
// path it shows visit the instructions before the point, latest first, then
// those of each predecessor, and so on, until visit stops the path.
//
// A path that reaches the top of a block with no predecessors, the entry of
// the function or its recover block, calls atEntry, which tells whether the
// walk goes on; nil goes on. through, when not nil, keeps the walk to the
// predecessors it accepts.
//
// Each block is walked into once from its end, even when it is also where a
// path starts. walkBack returns false when visit or atEntry ended the walk.
func walkBack(
	from []WalkPoint,
	visit walkVisit,
	atEntry func() bool,
	through func(*ssa.BasicBlock) bool,
) bool {
	starts := make([]WalkFrom[struct{}], len(from))
	for i, p := range from {
		starts[i].Point = p
	}
	var edge walkEdge[struct{}]
	if through != nil {
		edge = func(b *ssa.BasicBlock, i int, s struct{}) (struct{}, bool) {
			return s, through(b.Preds[i])
		}
	}
	return walkBackWith(starts, visit, atEntry, edge)
}

// walkBackWith is walkBack with a state on each path, which edge, when not
// nil, carries into each predecessor, or drops a path with. A block is walked
// into once in each state. With a nil edge, every predecessor is taken in the
// same state.
//
//declscope:private
func walkBackWith[S comparable](
	from []WalkFrom[S],
	visit walkVisit,
	atEntry func() bool,
	edge walkEdge[S],
) bool {
	return WalkBackIn(from, func(instr ssa.Instruction, _ *S) WalkStep { return visit(instr) }, atEntry, edge)
}

// WalkBackIn is walkBackWith with visit shown the state of the path too,
// which it may change: the path goes on past the instruction in the state
// visit leaves.
func WalkBackIn[S comparable](
	from []WalkFrom[S],
	visit func(ssa.Instruction, *S) WalkStep,
	atEntry func() bool,
	edge walkEdge[S],
) bool {
	var entered func(S) bool
	if atEntry != nil {
		entered = func(S) bool { return atEntry() }
	}
	return walkBackStates(from, visit, entered, edge)
}

// walkBackStates is WalkBackIn with atEntry shown the state of the path that
// gets to the entry.
func walkBackStates[S comparable](
	from []WalkFrom[S],
	visit func(ssa.Instruction, *S) WalkStep,
	atEntry func(S) bool,
	edge walkEdge[S],
) bool {
	type visited struct {
		block *ssa.BasicBlock
		state S
	}
	queue := make([]WalkFrom[S], len(from))
	copy(queue, from)
	seen := make(map[visited]bool)
	for len(queue) > 0 {
		s := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		b := s.Point.block
		step := WalkOn
		for i := s.Point.index - 1; i >= 0 && step == WalkOn; i-- {
			step = visit(b.Instrs[i], &s.State)
		}
		if step == WalkAbort {
			return false
		}
		if step == WalkStop {
			continue
		}
		if len(b.Preds) == 0 {
			if atEntry != nil && !atEntry(s.State) {
				return false
			}
			continue
		}
		for i, pred := range b.Preds {
			state := s.State
			if edge != nil {
				var takes bool
				if state, takes = edge(b, i, state); !takes {
					continue
				}
			}
			if v := (visited{pred, state}); !seen[v] {
				seen[v] = true
				queue = append(queue, WalkFrom[S]{WalkPoint{pred, len(pred.Instrs)}, state})
			}
		}
	}
	return true
}

// WalkFwd walks every path that leads on from the point from. On each path it
// shows visit the instructions from the point, earliest first, then those of
// each successor, and so on, until visit stops the path. A path ends with the
// block that has no successors, at a return or a panic.
//
// Each block is walked into once from its top, even when it is also where the
// walk starts. WalkFwd returns false when visit ended the walk.
func WalkFwd(from WalkPoint, visit walkVisit) bool {
	queue := []WalkPoint{from}
	seen := make(map[*ssa.BasicBlock]bool)
	for len(queue) > 0 {
		p := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		step := WalkOn
		for i := p.index; i < len(p.block.Instrs) && step == WalkOn; i++ {
			step = visit(p.block.Instrs[i])
		}
		if step == WalkAbort {
			return false
		}
		if step == WalkStop {
			continue
		}
		for _, succ := range p.block.Succs {
			if !seen[succ] {
				seen[succ] = true
				queue = append(queue, WalkPoint{succ, 0})
			}
		}
	}
	return true
}

// WalkReachable returns a test of whether a path from the point from reaches
// a point without running avoid on the way. Both points and avoid are in one
// function. The test finds the blocks such a path enters once, the first time
// it is asked about a point that is not straight down the block of from.
// work, when not nil, counts those blocks.
func WalkReachable(from WalkPoint, avoid ssa.Instruction, positions Positions, work *int) func(WalkPoint) bool {
	ab, ai := avoid.Block(), positions.of(avoid)
	// avoidsAt reports whether a path from index i of b to its end does not
	// run avoid
	avoidsAt := func(b *ssa.BasicBlock, i int) bool { return b != ab || ai < i }
	var entered map[*ssa.BasicBlock]bool
	enter := func() {
		entered = make(map[*ssa.BasicBlock]bool)
		var queue []*ssa.BasicBlock
		if avoidsAt(from.block, from.index) {
			queue = append(queue, from.block.Succs...)
		}
		for len(queue) > 0 {
			b := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			if entered[b] {
				continue
			}
			entered[b] = true
			if work != nil {
				*work++
			}
			if avoidsAt(b, 0) {
				queue = append(queue, b.Succs...)
			}
		}
	}
	return func(to WalkPoint) bool {
		if to.block == from.block && from.index <= to.index && (ab != to.block || ai < from.index || to.index <= ai) {
			return true // Straight down the block
		}
		if entered == nil {
			enter()
		}
		// A path enters the block of to at its top, and runs the
		// instructions before to
		return entered[to.block] && (to.block != ab || to.index <= ai)
	}
}

// walkThrough returns the blocks on a path through instr: its own block, and
// those that can run after it. A walk back from a point after instr that
// keeps to them, and stops at instr, takes only the paths that pass instr.
func walkThrough(instr ssa.Instruction, positions Positions) func(*ssa.BasicBlock) bool {
	blocks := positions.following(instr)
	blocks[instr.Block()] = true
	return func(b *ssa.BasicBlock) bool { return blocks[b] }
}
