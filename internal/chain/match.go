// match.go decides whether two SSA values name the same address. It is the
// bottom of this package and calls nothing above it.

package chain

import (
	"go/token"
	"maps"
	"slices"

	"golang.org/x/tools/go/ssa"
)

// addressesMatch checks if two addresses refer to the same memory location.
//
// Selections are compared structurally rather than by identity, because SSA
// emits a fresh FieldAddr/IndexAddr for every access. Without that, the two
// halves of a nested access would never line up:
//
//	t1 = &t0.inner ; t2 = &t1.event ; *t2 = v   // write
//	t3 = &t0.inner ; t4 = &t3.event ; ... = *t4 // read, t3 != t1
//
// Two selections match as sel.same says, so an element whose index is not a
// constant matches only through the same instruction, which a == b catches.
//
//declscope:shared // store.go tells whether two roots are the same pointer
func addressesMatch(a, b ssa.Value) bool {
	if a == b {
		return true
	}

	if xa, sa, ok := addrSel(a); ok {
		xb, sb, ok := addrSel(b)
		return ok && sa.same(sb) && addressesMatch(xa, xb)
	}

	// A pointer is loaded again for each use of it, as a captured receiver
	// is, or read again from a struct value, as h.m is for a value h. Two
	// loads of the same address that the same stores to it reach, or two
	// fields of the same value, are taken for the same pointer. A store
	// through any of them may then be what a read through another sees (see
	// storesReachingPointer).
	//
	//	t1 = *t0 ; t2 = &t1.log ; *t2 = v   // write
	//	t8 = *t0 ; t9 = &t8.log ; ... = *t9 // read, t8 != t1
	//
	// A store of another pointer between the two loads makes the second one
	// another pointer:
	//
	//	*t0 = a ; t1 = *t0 ; t2 = &t1.log ; *t2 = v
	//	*t0 = b ; t8 = *t0 ; t9 = &t8.log ; ... = *t9 // reads b.log
	if l1, ok := a.(*ssa.UnOp); ok {
		l2, ok := b.(*ssa.UnOp)
		return ok && l1.Op == token.MUL && l2.Op == token.MUL && addressesMatch(l1.X, l2.X) &&
			maps.Equal(matchReachingStores(l1), matchReachingStores(l2))
	}

	// An element of an array value is not matched, only a field of a struct
	// value
	f1, ok1 := a.(*ssa.Field)
	f2, ok2 := b.(*ssa.Field)
	return ok1 && ok2 && f1.Field == f2.Field && addressesMatch(f1.X, f2.X)
}

// matchReachingStores returns the instructions after which the value that load
// reads is at its address, on some path to load: the stores to the address,
// or to an aggregate that holds it, and the declaration of the local variable
// it is in. A path that leads back to the entry of the function, or to its
// recover block, is the nil key. Stores through another reference are not
// seen, as storesReaching does not see them.
func matchReachingStores(load *ssa.UnOp) map[ssa.Instruction]bool {
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
			if instr := s.block.Instrs[i]; matchOverwrites(instr, load.X) {
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

// matchOverwrites reports whether instr puts another value at addr: a store
// to addr or to an aggregate that holds it, or the declaration of the local
// variable addr is in.
//
//declscope:shared // store.go stops a walk for a pointer where another one is stored
func matchOverwrites(instr ssa.Instruction, addr ssa.Value) bool {
	store, isStore := instr.(*ssa.Store)
	for x, ok := addr, true; ok; x, _, ok = addrSel(x) {
		if isStore && addressesMatch(store.Addr, x) {
			return true
		}
		if alloc, isAlloc := x.(*ssa.Alloc); isAlloc && instr == alloc {
			return true
		}
	}
	return false
}
