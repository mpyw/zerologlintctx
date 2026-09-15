// aggregate.go walks a struct or array: the path from an address back to its
// root, and the copies a root was made from.

package ssa

import (
	"go/token"
	"iter"
	"slices"

	"golang.org/x/tools/go/ssa"
)

// aggregatePath decomposes an address into the root aggregate it derives from
// and the chain of field/element selections applied to it, outermost first:
//
//	&t0.inner.event  →  root t0, path [&t0.inner, &(t0.inner).event]
//
//declscope:package // store.go walks a path before resolving it
func aggregatePath(addr ssa.Value) (root ssa.Value, path []ssa.Value) {
	for {
		base := aggregateBase(addr)
		if base == nil {
			slices.Reverse(path)
			return addr, path
		}
		path = append(path, addr)
		addr = base
	}
}

// copySourcesOfAggregate yields the addresses whose whole-aggregate value is copied
// into root.
//
//declscope:package // store.go follows copies of a root
func copySourcesOfAggregate(fn *ssa.Function, root ssa.Value) iter.Seq[ssa.Value] {
	return func(yield func(ssa.Value) bool) {
		for instr := range instrsIn(fn) {
			store, ok := instr.(*ssa.Store)
			if !ok || !addressesMatch(store.Addr, root) {
				continue
			}
			if src := aggregateCopySource(store.Val); src != nil && !yield(src) {
				return
			}
		}
	}
}

// resolveAggregatePath returns the addresses reached by applying the same chain of
// selections to base that path applies to its own root.
//
//declscope:package // store.go resolves a path against a base
func resolveAggregatePath(fn *ssa.Function, base ssa.Value, path []ssa.Value) []ssa.Value {
	current := []ssa.Value{base}
	for _, step := range path {
		var next []ssa.Value
		for _, from := range current {
			for instr := range instrsIn(fn) {
				if elem, ok := instr.(ssa.Value); ok && selectsSameAggregateElement(elem, step, from) {
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

// selectsSameAggregateElement reports whether elem selects, from base, the same field or
// constant index that step selects from its own aggregate.
func selectsSameAggregateElement(elem, step, base ssa.Value) bool {
	switch s := step.(type) {
	case *ssa.FieldAddr:
		fa, ok := elem.(*ssa.FieldAddr)
		return ok && fa.X == base && fa.Field == s.Field
	case *ssa.IndexAddr:
		ia, ok := elem.(*ssa.IndexAddr)
		return ok && ia.X == base && constIndexesMatch(ia.Index, s.Index)
	}
	return false
}

// aggregateBase returns the aggregate that addr selects a field or element of,
// or nil if addr is not such a selection.
func aggregateBase(addr ssa.Value) ssa.Value {
	switch a := addr.(type) {
	case *ssa.FieldAddr:
		return a.X
	case *ssa.IndexAddr:
		return a.X
	}
	return nil
}

// aggregateCopySource returns the address a whole-aggregate value was loaded
// from (`t = *addr`), or nil for values produced any other way.
func aggregateCopySource(v ssa.Value) ssa.Value {
	if unop, ok := v.(*ssa.UnOp); ok && unop.Op == token.MUL {
		return unop.X
	}
	return nil
}
