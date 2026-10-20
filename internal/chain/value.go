// value.go is the SSA plumbing the rest of the package is built on: unwrapping
// a value, walking back to what a value derives from, ordering instructions in
// the control flow, and iterating instructions. None of it knows about
// zerolog, or about tracing. Stores, aggregates and address comparison are in
// store.go, aggregate.go and match.go.
//
// It joins the core namespace because every other file of internal/chain
// stands on it. checker.go and tracing.go are the units built on top, and keep
// namespaces of their own.
//
//declscope:core
//declscope:shared

package chain

import (
	"iter"
	"slices"

	"golang.org/x/tools/go/ssa"
)

// unwrapInner extracts the inner value from SSA wrapper types.
func unwrapInner(v ssa.Value) ssa.Value {
	switch val := v.(type) {
	case *ssa.Extract:
		return val.Tuple
	case *ssa.MakeInterface:
		return val.X
	case *ssa.TypeAssert:
		return val.X
	case *ssa.FieldAddr:
		return val.X
	case *ssa.Field:
		return val.X
	case *ssa.IndexAddr:
		return val.X
	case *ssa.Index:
		return val.X
	case *ssa.Lookup:
		return val.X
	}
	return nil
}

// derivesFrom reports whether found holds for v, or for a value v derives
// from: the receiver of a call, an edge of a Phi, or what next returns for
// any other value (nil to stop). Values in seen are not followed, and each
// value followed is added to it, so cycles end.
func derivesFrom(
	v ssa.Value,
	found func(ssa.Value) bool,
	next func(ssa.Value) ssa.Value,
	seen map[ssa.Value]bool,
) bool {
	if found(v) {
		return true
	}
	if seen[v] {
		return false
	}
	seen[v] = true

	switch val := v.(type) {
	case *ssa.Call:
		if len(val.Call.Args) > 0 {
			return derivesFrom(val.Call.Args[0], found, next, seen)
		}
		return false
	case *ssa.Phi:
		return slices.ContainsFunc(val.Edges, func(edge ssa.Value) bool {
			return derivesFrom(edge, found, next, seen)
		})
	}

	if inner := next(v); inner != nil {
		return derivesFrom(inner, found, next, seen)
	}
	return false
}

// unOpOperand returns the operand of a unary operation, or nil for any other
// value. As the next step of derivesFrom, it follows loads.
func unOpOperand(v ssa.Value) ssa.Value {
	if unop, ok := v.(*ssa.UnOp); ok {
		return unop.X
	}
	return nil
}

// instrDominates reports whether a runs before b on every path to b.
func instrDominates(a, b ssa.Instruction) bool {
	if a.Block() != b.Block() {
		return a.Block().Dominates(b.Block())
	}
	return instrIndex(a) < instrIndex(b)
}

// instrReaches reports whether b can run after a.
func instrReaches(a, b ssa.Instruction) bool {
	if a.Block() == b.Block() && instrIndex(a) < instrIndex(b) {
		return true
	}
	seen := make(map[*ssa.BasicBlock]bool)
	queue := slices.Clone(a.Block().Succs)
	for len(queue) > 0 {
		block := queue[0]
		queue = queue[1:]
		if seen[block] {
			continue
		}
		seen[block] = true
		if block == b.Block() {
			return true
		}
		queue = append(queue, block.Succs...)
	}
	return false
}

// instrIndex returns the position of instr in its block.
//
//declscope:private
func instrIndex(instr ssa.Instruction) int {
	return slices.Index(instr.Block().Instrs, instr)
}

// isNilConst checks if a value is a nil constant.
func isNilConst(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	return ok && c.Value == nil
}

// parentFunc returns the function an SSA value belongs to.
func parentFunc(v ssa.Value) *ssa.Function {
	if instr, ok := v.(ssa.Instruction); ok {
		return instr.Parent()
	}
	return nil
}

// instrsIn yields every instruction of fn, in block order.
func instrsIn(fn *ssa.Function) iter.Seq[ssa.Instruction] {
	return func(yield func(ssa.Instruction) bool) {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if !yield(instr) {
					return
				}
			}
		}
	}
}
