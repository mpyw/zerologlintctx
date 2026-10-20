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
// So does a call to Logger.UpdateContext, which changes the Logger in place.
// contextUpdaters holds the funcs passed to those that every read sees:
//
//	l.UpdateContext(func(c zerolog.Context) zerolog.Context {
//	    return c.Ctx(ctx)            // contextUpdater
//	})
//	l.Info().Msg("msg")
//
// A pointer parameter gets updates and contextUpdaters only. Its plain
// stores are not values, since the caller's value is there on any path that
// skips them.
//
//declscope:shared // tracing.go asks this; the rest of the file is its working parts
func findAllStoredValues(addr ssa.Value) storedValues {
	var sv storedValues
	plain, updates := findStores(addr, make(map[ssa.Value]bool))
	_, isLocal := addr.(*ssa.Alloc)
	_, isParam := addr.(*ssa.Parameter)
	if !isParam {
		for _, store := range plain {
			sv.plain = append(sv.plain, store.Val)
		}
	}
	if !isLocal && !isParam {
		return sv
	}
	refs := *addr.Referrers() // An Alloc or a Parameter always has them
	for _, update := range updates {
		if storeUpdateSeenByEveryRead(addr, refs, update, plain, updates) {
			sv.updates = append(sv.updates, update.Val)
		}
	}
	for _, call := range storeContextUpdatesOf(addr, refs) {
		if fn := storeContextUpdater(call); fn != nil && storeUpdateSeenByEveryRead(addr, refs, call, plain, updates) {
			sv.contextUpdaters = append(sv.contextUpdaters, fn)
		}
	}
	return sv
}

// storedValues is what findAllStoredValues found written into an address.
//
//declscope:shared // tracing.go traces each part
type storedValues struct {
	// plain are the values of plain stores. Every one must have context.
	plain []ssa.Value
	// updates are the values of self-referential stores that every read sees.
	// One with context is enough.
	updates []ssa.Value
	// contextUpdaters are the funcs passed to Logger.UpdateContext calls that
	// every read sees. One that returns a Context with context on every path
	// is enough.
	contextUpdaters []*ssa.Function
}

// storeUpdateSeenByEveryRead reports whether update, which changes the value
// at addr, runs before every read of addr, and no plain store can run after
// it. refs are the referrers of addr. Loads that only feed a self-referential
// store are not reads: they are how the store takes the value before it.
func storeUpdateSeenByEveryRead(
	addr ssa.Value,
	refs []ssa.Instruction,
	update ssa.Instruction,
	plain, updates []*ssa.Store,
) bool {
	feeding := make(map[ssa.Instruction]bool)
	for _, u := range updates {
		recordLoad := func(v ssa.Value) bool {
			if unop, ok := v.(*ssa.UnOp); ok && unop.Op == token.MUL && unop.X == addr {
				feeding[unop] = true
			}
			return false
		}
		derivesFrom(u.Val, recordLoad, unOpOperand, make(map[ssa.Value]bool))
	}
	for _, ref := range refs {
		if ref == update || feeding[ref] {
			continue
		}
		if store, ok := ref.(*ssa.Store); ok && store.Addr == addr {
			if slices.Contains(plain, store) && instrReaches(update, store) {
				return false
			}
			continue
		}
		if !instrDominates(update, ref) {
			return false
		}
	}
	return true
}

// storeContextUpdatesOf returns the calls of Logger.UpdateContext on addr,
// among refs, its referrers.
func storeContextUpdatesOf(addr ssa.Value, refs []ssa.Instruction) []*ssa.Call {
	var calls []*ssa.Call
	for _, ref := range refs {
		call, ok := ref.(*ssa.Call)
		if !ok {
			continue
		}
		if c, ok := calleeOf(&call.Call); ok && c.recv == addr && c.updatesLoggerContext() {
			calls = append(calls, call)
		}
	}
	return calls
}

// storeContextUpdater returns the func that a Logger.UpdateContext call
// passes, or nil when it is not known statically.
func storeContextUpdater(call *ssa.Call) *ssa.Function {
	// The receiver comes first, so the func is the last argument.
	switch fn := call.Call.Args[len(call.Call.Args)-1].(type) {
	case *ssa.Function:
		return fn
	case *ssa.MakeClosure:
		f, _ := fn.Fn.(*ssa.Function)
		return f
	}
	return nil
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
