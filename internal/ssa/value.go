// value.go is the SSA plumbing the rest of the package is built on: following
// a value back to where it was stored, walking an aggregate, comparing two
// addresses. None of it knows about zerolog, or about tracing.
//
// It joins the core namespace because it is what internal/ssa is named for.
// checker.go and tracing.go are the units built on top, and keep namespaces of
// their own.
//
//declscope:core
//declscope:package

package ssa

import (
	"iter"

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
