// match.go decides whether two SSA values name the same address. It is the
// bottom of this package and calls nothing above it.

package chain

import (
	"go/token"
	"maps"
	"slices"

	"golang.org/x/tools/go/ssa"
)

// valueLoadsFromMatchingAddress checks if a value (or its receiver chain) loads from the given address.
// This is used to detect self-referential stores like: *ptr = (*ptr).Str(...)
//
//declscope:shared // store.go asks this of a load
func valueLoadsFromMatchingAddress(v ssa.Value, addr ssa.Value) bool {
	isLoadFromAddr := func(v ssa.Value) bool {
		unop, ok := v.(*ssa.UnOp)
		return ok && unop.Op == token.MUL && addressesMatch(unop.X, addr)
	}
	return derivesFrom(v, isLoadFromAddr, unOpOperand, make(map[ssa.Value]bool))
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
//declscope:shared // store.go and aggregate.go both compare addresses
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

	// A pointer is loaded again for each use of it, as a captured receiver
	// is, or read again from a struct value, as h.m is for a value h. Two
	// loads of the same address that the same stores to it reach, or two
	// fields of the same value, are taken for the same pointer. A store
	// through any of them may then be what a read through another sees,
	// which is why every store must have context.
	//
	//	t1 = *t0 ; t2 = &t1.log ; *t2 = v   // write
	//	t8 = *t0 ; t9 = &t8.log ; ... = *t9 // read, t8 != t1
	//
	// A store of another pointer between the two loads makes the second one
	// another pointer:
	//
	//	*t0 = a ; t1 = *t0 ; t2 = &t1.log ; *t2 = v
	//	*t0 = b ; t8 = *t0 ; t9 = &t8.log ; ... = *t9 // reads b.log
	l1, ok1 := a.(*ssa.UnOp)
	l2, ok2 := b.(*ssa.UnOp)
	if ok1 && ok2 {
		return l1.Op == token.MUL && l2.Op == token.MUL && addressesMatch(l1.X, l2.X) &&
			maps.Equal(storesBeforeLoad(l1), storesBeforeLoad(l2))
	}

	f1, ok1 := a.(*ssa.Field)
	f2, ok2 := b.(*ssa.Field)
	if ok1 && ok2 {
		return f1.Field == f2.Field && addressesMatch(f1.X, f2.X)
	}

	return false
}

// storesBeforeLoad returns the instructions after which the value that load
// reads is at its address, on some path to load: the stores to the address,
// or to an aggregate that holds it, and the declaration of the local variable
// it is in. A path that leads back to the entry of the function, or to its
// recover block, is the nil key. Stores through another reference are not
// seen, as storesReaching does not see them.
func storesBeforeLoad(load *ssa.UnOp) map[ssa.Instruction]bool {
	found := make(map[ssa.Instruction]bool)
	b := load.Block()
	type step struct {
		block *ssa.BasicBlock
		from  int // Index of the last instruction of block to look at
	}
	queue := []step{{b, slices.Index(b.Instrs, ssa.Instruction(load)) - 1}}
	seen := make(map[*ssa.BasicBlock]bool)
	for len(queue) > 0 {
		s := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		stopped := false
		for i := s.from; i >= 0 && !stopped; i-- {
			if instr := s.block.Instrs[i]; overwritesLoaded(instr, load.X) {
				found[instr] = true
				stopped = true
			}
		}
		if stopped {
			continue
		}
		if len(s.block.Preds) == 0 {
			found[nil] = true
			continue
		}
		for _, pred := range s.block.Preds {
			if !seen[pred] {
				seen[pred] = true
				queue = append(queue, step{pred, len(pred.Instrs) - 1})
			}
		}
	}
	return found
}

// overwritesLoaded reports whether instr puts another value at addr: a store
// to addr or to an aggregate that holds it, or the declaration of the local
// variable addr is in.
func overwritesLoaded(instr ssa.Instruction, addr ssa.Value) bool {
	store, isStore := instr.(*ssa.Store)
	for x := addr; x != nil; {
		if isStore && addressesMatch(store.Addr, x) {
			return true
		}
		if alloc, isAlloc := x.(*ssa.Alloc); isAlloc && instr == alloc {
			return true
		}
		switch sel := x.(type) {
		case *ssa.FieldAddr:
			x = sel.X
		case *ssa.IndexAddr:
			x = sel.X
		default:
			x = nil
		}
	}
	return false
}

// constIndexesMatch reports whether two index operands are equal constants.
// Non-constant indexes never match, since they may denote different elements.
//
//declscope:shared // aggregate.go compares two index selections
func constIndexesMatch(a, b ssa.Value) bool {
	c1, ok1 := a.(*ssa.Const)
	c2, ok2 := b.(*ssa.Const)
	return ok1 && ok2 && c1.Value == c2.Value
}
