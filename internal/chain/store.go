// store.go finds what was written into an address. findAllStoredValues is the
// entry; it leans on aggregate.go to follow copies and on match.go to decide
// whether two addresses are the same.

package chain

import (
	"go/token"
	"slices"

	"golang.org/x/tools/go/ssa"
)

// findAllStoredValues finds all values that were stored at the given address.
// Multiple stores can occur in different control flow paths (e.g., if/else branches).
// All stored values must be checked for context to handle cases like:
//
//	e := logger.Info().Ctx(ctx)
//	ptr := &e
//	if cond {
//	    *ptr = logger.Warn()  // no ctx in this branch!
//	}
//	(*ptr).Msg("msg")  // should report: one branch lacks ctx
//
// Self-referential stores, whose value loads from the same address, are
// updates: each transforms the value already there. They are skipped as
// values, since they are built from the other stores and would cause infinite
// recursion during tracing:
//
//	e := logger.Info().Ctx(ctx)
//	ptr := &e
//	for i := 0; i < 3; i++ {
//	    *ptr = (*ptr).Str("k", "v")  // update: skipped as a value
//	}
//	(*ptr).Msg("msg")  // only traces initial store, finds ctx
//
// An update may set the context itself, though. updates holds those that
// every read of a local variable sees, which tracing may take on their own:
//
//	l := logger
//	l = l.With().Ctx(ctx).Logger()   // update seen by every read below
//	l.Info().Msg("msg")              // Info takes &l with real zerolog
//
//declscope:shared // tracing.go asks this; the rest of the file is its working parts
func findAllStoredValues(addr ssa.Value) (values, updates []ssa.Value) {
	plain, updateStores := findStores(addr, make(map[ssa.Value]bool))
	for _, store := range plain {
		values = append(values, store.Val)
	}
	alloc, ok := addr.(*ssa.Alloc)
	if !ok {
		return values, nil
	}
	for _, update := range updateStores {
		if storeUpdateSeenByEveryRead(alloc, update, plain, updateStores) {
			updates = append(updates, update.Val)
		}
	}
	return values, updates
}

// storeUpdateSeenByEveryRead reports whether update, a self-referential store
// to alloc, runs before every read of alloc, and no plain store can run after
// it. Loads that only feed an update are not reads: they are how the update
// takes the value before it.
func storeUpdateSeenByEveryRead(alloc *ssa.Alloc, update *ssa.Store, plain, updates []*ssa.Store) bool {
	refs := alloc.Referrers()
	if refs == nil {
		return false
	}
	feeding := make(map[ssa.Instruction]bool)
	for _, u := range updates {
		recordLoad := func(v ssa.Value) bool {
			if unop, ok := v.(*ssa.UnOp); ok && unop.Op == token.MUL && unop.X == alloc {
				feeding[unop] = true
			}
			return false
		}
		derivesFrom(u.Val, recordLoad, unOpOperand, make(map[ssa.Value]bool))
	}
	for _, ref := range *refs {
		if store, ok := ref.(*ssa.Store); ok && store.Addr == alloc {
			if slices.Contains(plain, store) && instrReaches(update, store) {
				return false
			}
			continue
		}
		if feeding[ref] {
			continue
		}
		if !instrDominates(update, ref) {
			return false
		}
	}
	return true
}

// findStores finds the stores to addr, as plain stores and self-referential
// updates, carrying the set of addresses already resolved so that following
// aggregate copies terminates.
func findStores(addr ssa.Value, visited map[ssa.Value]bool) (plain, updates []*ssa.Store) {
	if visited[addr] {
		return nil, nil
	}
	visited[addr] = true

	fn := parentFunc(addr)
	if fn == nil {
		return nil, nil
	}

	for instr := range instrsIn(fn) {
		store, ok := instr.(*ssa.Store)
		if !ok || !addressesMatch(store.Addr, addr) {
			continue
		}
		if valueLoadsFromMatchingAddress(store.Val, addr) {
			updates = append(updates, store)
			continue
		}
		plain = append(plain, store)
	}
	if len(plain) > 0 {
		return plain, updates
	}

	return findStoresThroughAggregateCopy(fn, addr, visited), updates
}

// findStoresThroughAggregateCopy resolves stores hidden behind a whole-aggregate
// copy. A composite literal is built in a temporary that is then copied into the
// destination in one go, so the per-element stores never mention the destination:
//
//	h := eventHolder{event: logger.Info().Ctx(ctx)}
//	h.event.Msg("msg")
//
// becomes
//
//	t0 = local eventHolder (h)
//	t1 = local eventHolder (complit)
//	t2 = &t1.event
//	*t2 = t4                 // field initialized on the temporary
//	t5 = *t1
//	*t0 = t5                 // whole struct copied into h
//	t6 = &t0.event           // ← the address we are asked about
//
// Searching for stores to t6 finds nothing, so follow the copy back to the
// temporary and resolve the same selection path there instead. The path is
// resolved element by element, so embedded structs nest arbitrarily deep.
func findStoresThroughAggregateCopy(fn *ssa.Function, addr ssa.Value, visited map[ssa.Value]bool) []*ssa.Store {
	root, path := aggregatePath(addr)
	if len(path) == 0 {
		return nil
	}

	var plain []*ssa.Store
	for src := range copySourcesOfAggregate(fn, root) {
		for _, equivalent := range resolveAggregatePath(fn, src, path) {
			found, _ := findStores(equivalent, visited)
			plain = append(plain, found...)
		}
	}
	return plain
}
