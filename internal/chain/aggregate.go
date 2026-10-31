// aggregate.go follows the copies of a struct or array: the addresses a root
// was copied from whole, and the same selections from each of them.

package chain

import (
	"iter"

	"golang.org/x/tools/go/ssa"
)

// copySourcesOfAggregate yields the addresses whose whole-aggregate value is copied
// into root.
//
//declscope:shared // store.go follows copies of a root
func copySourcesOfAggregate(fn *ssa.Function, root ssa.Value) iter.Seq[ssa.Value] {
	return func(yield func(ssa.Value) bool) {
		for instr := range instrsIn(fn) {
			store, ok := instr.(*ssa.Store)
			if !ok || !addressesMatch(store.Addr, root) {
				continue
			}
			// A whole-aggregate value loaded from an address (`t = *addr`)
			if load, isLoad := asLoad(store.Val); isLoad && !yield(load.X) {
				return
			}
		}
	}
}

// resolveAggregatePath returns the addresses reached by applying path, the
// selections of a place, to base.
//
//declscope:shared // store.go resolves a path against a base
func resolveAggregatePath(fn *ssa.Function, base ssa.Value, path []sel) []ssa.Value {
	current := []ssa.Value{base}
	for _, step := range path {
		var next []ssa.Value
		for _, from := range current {
			for instr := range instrsIn(fn) {
				elem, ok := instr.(ssa.Value)
				if !ok {
					continue
				}
				if x, s, ok := addrSel(elem); ok && x == from && s.same(step) {
					next = append(next, elem)
				}
			}
		}
		if len(next) == 0 {
			return nil
		}
		current = next
	}
	return current
}
