// value.go is the SSA plumbing the rest of the package is built on: unwrapping
// a value, walking back to what a value derives from, ordering instructions in
// the control flow, and iterating instructions. None of it knows about
// zerolog, or about tracing. The model of an address is in place.go, and
// stores and address comparison are in store.go and match.go.
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
	case *ssa.MakeInterface:
		return val.X
	case *ssa.ChangeType:
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
// from: the receiver of a call (see calleeReceiver), an edge of a Phi, or what next returns for
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
		if recv := calleeReceiver(&val.Call); recv != nil {
			return derivesFrom(recv, found, next, seen)
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

// instrPositions remembers the position of each instruction in its block.
// A block is indexed once, the first time one of its instructions is asked.
type instrPositions map[ssa.Instruction]int

// of returns the position of instr in its block.
func (p instrPositions) of(instr ssa.Instruction) int {
	if i, ok := p[instr]; ok {
		return i
	}
	for i, in := range instr.Block().Instrs {
		p[in] = i
	}
	return p[instr]
}

// reaches reports whether b can run after a.
func (p instrPositions) reaches(a, b ssa.Instruction) bool {
	return p.after(a)(b)
}

// after returns a test of whether an instruction can run after a. It finds
// the blocks that follow a once, for every instruction it is asked about.
func (p instrPositions) after(a ssa.Instruction) func(ssa.Instruction) bool {
	following := p.following(a)
	return func(b ssa.Instruction) bool {
		return following[b.Block()] || a.Block() == b.Block() && p.of(a) < p.of(b)
	}
}

// following returns the blocks that can run after the block of a: its
// successors, theirs, and so on. The block of a is among them only in a loop.
func (p instrPositions) following(a ssa.Instruction) map[*ssa.BasicBlock]bool {
	following := make(map[*ssa.BasicBlock]bool)
	queue := slices.Clone(a.Block().Succs)
	for len(queue) > 0 {
		block := queue[0]
		queue = queue[1:]
		if following[block] {
			continue
		}
		following[block] = true
		queue = append(queue, block.Succs...)
	}
	return following
}

// isNilConst checks if a value is a nil constant.
func isNilConst(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	return ok && c.Value == nil
}

// parentFunc returns the function an SSA value belongs to, for an
// instruction or a parameter.
func parentFunc(v ssa.Value) *ssa.Function {
	switch v := v.(type) {
	case ssa.Instruction:
		return v.Parent()
	case *ssa.Parameter:
		return v.Parent()
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

// selectedFields returns the Field instructions that select fields from the
// struct value v, one after another: each value of the last field the
// function selects from v, or v itself when fields is empty.
func selectedFields(v ssa.Value, fields []int) []ssa.Value {
	values := []ssa.Value{v}
	for _, index := range fields {
		var next []ssa.Value
		for _, w := range values {
			for _, ref := range *w.Referrers() {
				if f, ok := ref.(*ssa.Field); ok && f.X == w && f.Field == index {
					next = append(next, f)
				}
			}
		}
		values = next
	}
	return values
}
