// store.go finds what was written into an address. findAllStoredValues is the
// entry; it leans on aggregate.go to follow copies and on match.go to decide
// whether two addresses are the same.

package ssa

import (
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
// Self-referential stores (where the value loads from the same address) are skipped:
//
//	e := logger.Info().Ctx(ctx)
//	ptr := &e
//	for i := 0; i < 3; i++ {
//	    *ptr = (*ptr).Str("k", "v")  // self-referential: skipped
//	}
//	(*ptr).Msg("msg")  // only traces initial store, finds ctx
//
//declscope:package // tracing.go asks this; the rest of the file is its working parts
func findAllStoredValues(addr ssa.Value) []ssa.Value {
	return findStoredValues(addr, make(map[ssa.Value]bool))
}

// findStoredValues implements findAllStoredValues, carrying the set of
// addresses already resolved so that following aggregate copies terminates.
func findStoredValues(addr ssa.Value, visited map[ssa.Value]bool) []ssa.Value {
	if visited[addr] {
		return nil
	}
	visited[addr] = true

	fn := parentFunc(addr)
	if fn == nil {
		return nil
	}

	var storedValues []ssa.Value
	for instr := range instrsIn(fn) {
		store, ok := instr.(*ssa.Store)
		if !ok || !addressesMatch(store.Addr, addr) {
			continue
		}
		// Skip self-referential stores where the value loads from the same address.
		// These just transform the existing value (e.g., *ptr = (*ptr).Str(...))
		// and would cause infinite recursion during tracing.
		if valueLoadsFromMatchingAddress(store.Val, addr) {
			continue
		}
		storedValues = append(storedValues, store.Val)
	}
	if len(storedValues) > 0 {
		return storedValues
	}

	return findStoresThroughAggregateCopy(fn, addr, visited)
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
func findStoresThroughAggregateCopy(fn *ssa.Function, addr ssa.Value, visited map[ssa.Value]bool) []ssa.Value {
	root, path := aggregatePath(addr)
	if len(path) == 0 {
		return nil
	}

	var storedValues []ssa.Value
	for src := range copySourcesOfAggregate(fn, root) {
		for _, equivalent := range resolveAggregatePath(fn, src, path) {
			storedValues = append(storedValues, findStoredValues(equivalent, visited)...)
		}
	}
	return storedValues
}
