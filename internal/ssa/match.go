// match.go decides whether two SSA values name the same address. It is the
// bottom of this package and calls nothing above it.

package ssa

import (
	"go/token"

	"golang.org/x/tools/go/ssa"
)

// valueLoadsFromMatchingAddress checks if a value (or its receiver chain) loads from the given address.
// This is used to detect self-referential stores like: *ptr = (*ptr).Str(...)
//
//declscope:package // store.go asks this of a load
func valueLoadsFromMatchingAddress(v ssa.Value, addr ssa.Value) bool {
	switch val := v.(type) {
	case *ssa.UnOp:
		// Check if this is a dereference of the address
		if val.Op == token.MUL && addressesMatch(val.X, addr) {
			return true
		}
		return valueLoadsFromMatchingAddress(val.X, addr)
	case *ssa.Call:
		// Check receiver (first argument for method calls)
		if len(val.Call.Args) > 0 {
			return valueLoadsFromMatchingAddress(val.Call.Args[0], addr)
		}
	case *ssa.Phi:
		// Check all edges
		for _, edge := range val.Edges {
			if valueLoadsFromMatchingAddress(edge, addr) {
				return true
			}
		}
	}
	return false
}

// addressesMatch checks if two addresses refer to the same memory location.
//
// Selections are compared structurally rather than by identity, because SSA
// emits a fresh FieldAddr/IndexAddr for every access. Without that, the two
// halves of a nested access would never line up:
//
//	t1 = &t0.inner ; t2 = &t1.event ; *t2 = v   // write
//	t3 = &t0.inner ; t4 = &t3.event ; ... = *t4 // read, t3 != t1
//
//declscope:package // store.go and aggregate.go both compare addresses
func addressesMatch(a, b ssa.Value) bool {
	if a == b {
		return true
	}

	fa1, ok1 := a.(*ssa.FieldAddr)
	fa2, ok2 := b.(*ssa.FieldAddr)
	if ok1 && ok2 {
		return fa1.Field == fa2.Field && addressesMatch(fa1.X, fa2.X)
	}

	ia1, ok1 := a.(*ssa.IndexAddr)
	ia2, ok2 := b.(*ssa.IndexAddr)
	if ok1 && ok2 {
		return constIndexesMatch(ia1.Index, ia2.Index) && addressesMatch(ia1.X, ia2.X)
	}

	return false
}

// constIndexesMatch reports whether two index operands are equal constants.
// Non-constant indexes never match, since they may denote different elements.
//
//declscope:package // aggregate.go compares two index selections
func constIndexesMatch(a, b ssa.Value) bool {
	c1, ok1 := a.(*ssa.Const)
	c2, ok2 := b.(*ssa.Const)
	return ok1 && ok2 && c1.Value == c2.Value
}
