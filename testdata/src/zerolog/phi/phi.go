// Package phi covers control flow that merges values: branches, switches, loops,
// goto, select, and pointers picked on a branch.
package phi

import (
	"context"

	"github.com/rs/zerolog"
)

// ===== CONDITIONAL AND BRANCHING =====

func evilConditionalLogLevel(ctx context.Context, logger zerolog.Logger, isDebug bool) {
	var event *zerolog.Event
	if isDebug {
		event = logger.Debug()
	} else {
		event = logger.Info()
	}
	event.Msg("conditional level") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilConditionalLogLevelWithCtx(ctx context.Context, logger zerolog.Logger, isDebug bool) {
	var event *zerolog.Event
	if isDebug {
		event = logger.Debug().Ctx(ctx)
	} else {
		event = logger.Info().Ctx(ctx)
	}
	event.Msg("conditional level with ctx") // OK
}

func evilConditionalWithLogger(ctx context.Context, logger zerolog.Logger, addExtra bool) {
	var derived zerolog.Logger
	if addExtra {
		derived = logger.With().Str("extra", "yes").Logger()
	} else {
		derived = logger.With().Logger()
	}
	derived.Info().Msg("conditional with") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilConditionalWithLoggerPartialCtx(ctx context.Context, logger zerolog.Logger, addExtra bool) {
	var derived zerolog.Logger
	if addExtra {
		derived = logger.With().Ctx(ctx).Str("extra", "yes").Logger() // has ctx
	} else {
		derived = logger.With().Logger() // NO ctx!
	}
	// All branches must have ctx - partial coverage is flagged
	derived.Info().Msg("conditional partial ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilConditionalWithLoggerBothCtx(ctx context.Context, logger zerolog.Logger, addExtra bool) {
	var derived zerolog.Logger
	if addExtra {
		derived = logger.With().Ctx(ctx).Str("extra", "yes").Logger()
	} else {
		derived = logger.With().Ctx(ctx).Logger()
	}
	derived.Info().Msg("conditional both ctx") // OK
}

// ===== SWITCH STATEMENTS =====

func evilSwitchLogger(ctx context.Context, logger zerolog.Logger, level string) {
	var event *zerolog.Event
	switch level {
	case "debug":
		event = logger.Debug()
	case "info":
		event = logger.Info()
	case "warn":
		event = logger.Warn()
	default:
		event = logger.Error()
	}
	event.Msg("switch level") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilSwitchLoggerWithCtx(ctx context.Context, logger zerolog.Logger, level string) {
	var event *zerolog.Event
	switch level {
	case "debug":
		event = logger.Debug().Ctx(ctx)
	case "info":
		event = logger.Info().Ctx(ctx)
	case "warn":
		event = logger.Warn().Ctx(ctx)
	default:
		event = logger.Error().Ctx(ctx)
	}
	event.Msg("switch level with ctx") // OK
}

// ===== LOOPS =====

func evilLoopWithDifferentLoggers(ctx context.Context, loggers []zerolog.Logger) {
	for _, logger := range loggers {
		logger.Info().Msg("loop logger") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

func evilLoopWithDifferentLoggersCtx(ctx context.Context, loggers []zerolog.Logger) {
	for _, logger := range loggers {
		logger.Info().Ctx(ctx).Msg("loop logger with ctx") // OK
	}
}

func evilLoopBuildingEvent(ctx context.Context, logger zerolog.Logger, fields map[string]string) {
	event := logger.Info()
	for k, v := range fields {
		event = event.Str(k, v)
	}
	event.Msg("loop built event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilLoopBuildingEventWithCtx(ctx context.Context, logger zerolog.Logger, fields map[string]string) {
	event := logger.Info().Ctx(ctx)
	for k, v := range fields {
		event = event.Str(k, v)
	}
	event.Msg("loop built event with ctx") // OK
}

func evilLoopBuildingContext(ctx context.Context, logger zerolog.Logger, fields map[string]string) {
	wc := logger.With()
	for k, v := range fields {
		wc = wc.Str(k, v)
	}
	derived := wc.Logger()
	derived.Info().Msg("loop built context") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func evilLoopBuildingContextWithCtx(ctx context.Context, logger zerolog.Logger, fields map[string]string) {
	wc := logger.With().Ctx(ctx)
	for k, v := range fields {
		wc = wc.Str(k, v)
	}
	derived := wc.Logger()
	derived.Info().Msg("loop built context with ctx") // OK
}

// An event made on a branch in a loop, and updated after the branch, reaches
// the update through the loop's Phi as well. The edge that leads back is
// neutral, and the event made on the branch still counts.

func evilLoopBranchThenUpdate(ctx context.Context, logger zerolog.Logger, n int, cond bool) {
	e := logger.Info().Ctx(ctx)
	for i := 0; i < n; i++ {
		if cond {
			e = logger.Warn()
		}
		e = e.Str("k", "v")
	}
	e.Msg("loop branch then update") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodLoopBranchThenUpdateWithCtx(ctx context.Context, logger zerolog.Logger, n int, cond bool) {
	e := logger.Info().Ctx(ctx)
	for i := 0; i < n; i++ {
		if cond {
			e = logger.Warn().Ctx(ctx)
		}
		e = e.Str("k", "v")
	}
	e.Msg("loop branch then update with ctx") // OK
}

// ===== CONDITIONAL EVENT CREATION (PHI NODES) =====

func badConditionalEvent(ctx context.Context, logger zerolog.Logger, flag bool) {
	var e *zerolog.Event
	if flag {
		e = logger.Info()
	} else {
		e = logger.Warn()
	}
	e.Msg("conditional") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badConditionalOneBranchHasCtx(ctx context.Context, logger zerolog.Logger, flag bool) {
	// ALL branches must have ctx - partial coverage is now detected
	var e *zerolog.Event
	if flag {
		e = logger.Info().Ctx(ctx) // One branch has ctx
	} else {
		e = logger.Warn() // Other branch doesn't
	}
	e.Msg("partial conditional") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodConditionalBothBranchesHaveCtx(ctx context.Context, logger zerolog.Logger, flag bool) {
	var e *zerolog.Event
	if flag {
		e = logger.Info().Ctx(ctx)
	} else {
		e = logger.Warn().Ctx(ctx)
	}
	e.Msg("both branches have ctx") // OK
}

func badTernaryLikeConditional(ctx context.Context, logger zerolog.Logger, flag bool) {
	e := func() *zerolog.Event {
		if flag {
			return logger.Info()
		}
		return logger.Error()
	}()
	e.Msg("ternary-like") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== LOOP-CREATED EVENTS =====

func badEventInLoop(ctx context.Context, logger zerolog.Logger) {
	for i := 0; i < 3; i++ {
		e := logger.Info().Int("i", i)
		e.Msg("loop iteration") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

// A branch in a loop merges in a Phi that leads back to the loop's own, and
// only its edge that leads back is skipped (#151).

func badEventReplacedOnBranchInLoop(ctx context.Context, logger zerolog.Logger, cond bool) {
	e := logger.Info().Ctx(ctx)
	for i := 0; i < 2; i++ {
		if cond {
			e = logger.Warn()
		}
	}
	e.Msg("replaced on a branch in a loop") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventUpdatedOnBranchInLoop(ctx context.Context, logger zerolog.Logger, cond bool) {
	e := logger.Info().Ctx(ctx)
	for i := 0; i < 2; i++ {
		if cond {
			e = e.Int("i", i)
		}
	}
	e.Msg("updated on a branch in a loop") // OK - every update keeps ctx
}

func goodEventReplacedWithCtxOnBranchInLoop(ctx context.Context, logger zerolog.Logger, a, b bool) {
	e := logger.Info().Ctx(ctx)
	for i := 0; i < 2; i++ {
		if a {
			e = logger.Warn().Ctx(ctx)
		}
		if b {
			e = e.Int("i", i)
		}
	}
	e.Msg("replaced with ctx on a branch in a loop") // OK - every value has ctx
}

func goodEventInLoopWithCtx(ctx context.Context, logger zerolog.Logger) {
	for i := 0; i < 3; i++ {
		e := logger.Info().Ctx(ctx).Int("i", i)
		e.Msg("loop iteration with ctx") // OK
	}
}

// ===== SWITCH STATEMENT =====

func badSwitchEvent(ctx context.Context, logger zerolog.Logger, level int) {
	var e *zerolog.Event
	switch level {
	case 0:
		e = logger.Debug()
	case 1:
		e = logger.Info()
	case 2:
		e = logger.Warn()
	default:
		e = logger.Error()
	}
	e.Msg("from switch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodSwitchEventAllCtx(ctx context.Context, logger zerolog.Logger, level int) {
	var e *zerolog.Event
	switch level {
	case 0:
		e = logger.Debug().Ctx(ctx)
	case 1:
		e = logger.Info().Ctx(ctx)
	case 2:
		e = logger.Warn().Ctx(ctx)
	default:
		e = logger.Error().Ctx(ctx)
	}
	e.Msg("from switch with ctx") // OK
}

// ===== SELECT STATEMENT =====

func badSelectEvent(ctx context.Context, logger zerolog.Logger, ch chan int) {
	var e *zerolog.Event
	select {
	case <-ch:
		e = logger.Info()
	default:
		e = logger.Warn()
	}
	e.Msg("from select") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== NESTED PHI NODES =====

func badNestedPhiNodes(ctx context.Context, logger zerolog.Logger, a, b bool) {
	var e *zerolog.Event
	if a {
		if b {
			e = logger.Debug()
		} else {
			e = logger.Info()
		}
	} else {
		if b {
			e = logger.Warn()
		} else {
			e = logger.Error()
		}
	}
	e.Msg("nested phi") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badNestedPhiPartialCtx(ctx context.Context, logger zerolog.Logger, a, b bool) {
	var e *zerolog.Event
	if a {
		if b {
			e = logger.Debug().Ctx(ctx)
		} else {
			e = logger.Info().Ctx(ctx)
		}
	} else {
		if b {
			e = logger.Warn() // Missing ctx in this branch!
		} else {
			e = logger.Error().Ctx(ctx)
		}
	}
	e.Msg("nested phi partial") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodNestedPhiAllCtx(ctx context.Context, logger zerolog.Logger, a, b bool) {
	var e *zerolog.Event
	if a {
		if b {
			e = logger.Debug().Ctx(ctx)
		} else {
			e = logger.Info().Ctx(ctx)
		}
	} else {
		if b {
			e = logger.Warn().Ctx(ctx)
		} else {
			e = logger.Error().Ctx(ctx)
		}
	}
	e.Msg("nested phi all ctx") // OK
}

// ===== SHORT-CIRCUIT EVALUATION =====

func badShortCircuitAnd(ctx context.Context, logger zerolog.Logger, flag bool) {
	var e *zerolog.Event
	if flag && func() bool {
		e = logger.Info()
		return true
	}() {
		e.Msg("short circuit and") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

func badShortCircuitOr(ctx context.Context, logger zerolog.Logger, flag bool) {
	var e *zerolog.Event
	if flag || func() bool {
		e = logger.Info()
		return false
	}() {
		if e != nil {
			e.Msg("short circuit or") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
	}
}

// ===== FALLTHROUGH IN SWITCH =====

func badFallthroughSwitch(ctx context.Context, logger zerolog.Logger, level int) {
	var e *zerolog.Event
	switch level {
	case 0:
		e = logger.Debug()
		fallthrough
	case 1:
		if e == nil {
			e = logger.Info()
		}
		e.Msg("fallthrough") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

// ===== GOTO STATEMENT =====

func badGotoStatement(ctx context.Context, logger zerolog.Logger, flag bool) {
	var e *zerolog.Event
	if flag {
		e = logger.Info()
		goto log
	}
	e = logger.Warn()
log:
	e.Msg("after goto") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== TYPE SWITCH =====

func badTypeSwitch(ctx context.Context, logger zerolog.Logger) {
	var i interface{} = logger.Info()
	switch e := i.(type) {
	case *zerolog.Event:
		e.Msg("from type switch") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

func goodTypeSwitchWithCtx(ctx context.Context, logger zerolog.Logger) {
	var i interface{} = logger.Info().Ctx(ctx)
	switch e := i.(type) {
	case *zerolog.Event:
		e.Msg("from type switch with ctx") // OK
	}
}

// ===== NIL CHECK BEFORE USE =====

func badNilCheckBeforeUse(ctx context.Context, logger zerolog.Logger) {
	var e *zerolog.Event
	if true {
		e = logger.Info()
	}
	if e != nil {
		e.Msg("nil checked") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

// Phi node with nil edge from uninitialized variable.
// The nil edge is skipped because nil pointers can't have methods called on them.
func goodNilCheckWithCtx(ctx context.Context, logger zerolog.Logger) {
	var e *zerolog.Event
	if true {
		e = logger.Info().Ctx(ctx)
	}
	if e != nil {
		e.Msg("nil checked with ctx") // OK - nil edge skipped, ctx edge has context
	}
}

// Phi node with nil + multiple non-nil edges.
// All non-nil edges must have context.
func badNilCheckMixedEdges(ctx context.Context, logger zerolog.Logger, cond1, cond2 bool) {
	var e *zerolog.Event
	if cond1 {
		e = logger.Info().Ctx(ctx) // has ctx
	} else if cond2 {
		e = logger.Info() // no ctx
	}
	if e != nil {
		e.Msg("mixed edges") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

// Phi node where all non-nil edges have context.
func goodNilCheckAllEdgesWithCtx(ctx context.Context, logger zerolog.Logger, cond1, cond2 bool) {
	var e *zerolog.Event
	if cond1 {
		e = logger.Info().Ctx(ctx)
	} else if cond2 {
		e = logger.Debug().Ctx(ctx)
	}
	if e != nil {
		e.Msg("all edges with ctx") // OK - nil skipped, all non-nil have ctx
	}
}

// ===== CONDITIONAL EXPRESSION (TERNARY-LIKE) =====

func selectEvent(cond bool, a, b *zerolog.Event) *zerolog.Event {
	if cond {
		return a
	}
	return b
}

func badConditionalSelect(ctx context.Context, logger zerolog.Logger) {
	e := selectEvent(true, logger.Info(), logger.Warn())
	e.Msg("conditional select") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== POINTER-BASED LOOP =====

// Test case: Loop modifying event through pointer
// Tests edgeLeadsToImpl handling of UnOp
func badPointerBasedLoop(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	ptr := &e
	for i := 0; i < 3; i++ {
		*ptr = (*ptr).Str("key", "val")
	}
	(*ptr).Msg("pointer loop") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// Test case: Pointer-based loop with ctx - properly traced
// Self-referential stores (*ptr = (*ptr).Str(...)) are skipped as they
// just transform the existing value without changing ctx status.
func goodPointerBasedLoopWithCtx(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info().Ctx(ctx)
	ptr := &e
	for i := 0; i < 3; i++ {
		*ptr = (*ptr).Str("key", "val")
	}
	(*ptr).Msg("pointer loop with ctx") // OK - ctx set in initial store
}

// ===== POINTER WITH CONDITIONAL STORE =====

// Test case: Multiple stores to same pointer address - partial ctx
// If only one branch has ctx, should report
func badPointerConditionalStorePartialCtx(ctx context.Context, logger zerolog.Logger, cond bool) {
	e := logger.Info() // no ctx
	ptr := &e
	if cond {
		*ptr = logger.Warn().Ctx(ctx) // has ctx
	}
	(*ptr).Msg("ptr conditional partial") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// Test case: Both stores have ctx - should be OK
func goodPointerConditionalStoreAllCtx(ctx context.Context, logger zerolog.Logger, cond bool) {
	e := logger.Info().Ctx(ctx) // has ctx
	ptr := &e
	if cond {
		*ptr = logger.Warn().Ctx(ctx) // has ctx
	}
	(*ptr).Msg("ptr conditional all ctx") // OK - all paths have ctx
}

// Test case: Initial has ctx, conditional does not
func badPointerConditionalStoreInitialCtx(ctx context.Context, logger zerolog.Logger, cond bool) {
	e := logger.Info().Ctx(ctx) // has ctx
	ptr := &e
	if cond {
		*ptr = logger.Warn() // no ctx
	}
	(*ptr).Msg("ptr initial ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== PHI WITH POINTER ACCESS =====

// Test case: Phi where all stores are via pointer - both have ctx
func goodPhiAllViaPointerWithCtx(ctx context.Context, logger zerolog.Logger, cond bool) {
	var e *zerolog.Event
	ptr := &e
	if cond {
		*ptr = logger.Info().Ctx(ctx)
	} else {
		*ptr = logger.Warn().Ctx(ctx)
	}
	(*ptr).Msg("phi all via pointer") // OK - all paths have ctx
}

// Test case: Mixed access - direct and via pointer
func goodPhiMixedAccessWithCtx(ctx context.Context, logger zerolog.Logger, cond1, cond2 bool) {
	var e *zerolog.Event
	if cond1 {
		e = logger.Info().Ctx(ctx) // direct assignment
	} else {
		ptr := &e
		if cond2 {
			*ptr = logger.Warn().Ctx(ctx) // via pointer
		}
	}
	if e != nil {
		e.Msg("mixed access with ctx") // OK - all non-nil paths have ctx
	}
}

// Test case: Nested pointer with loop - all paths have ctx
func goodNestedPointerLoopWithCtx(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info().Ctx(ctx)
	ptr := &e
	for i := 0; i < 2; i++ {
		*ptr = (*ptr).Str("iter", "val")
	}
	ptr2 := &ptr
	(**ptr2).Msg("nested pointer loop") // OK - ctx propagates through
}

// ===== FIELD THROUGH A STRUCT POINTER PICKED BY A PHI =====

// A field read through a struct pointer that a Phi picks has what each struct
// the Phi may point to holds there (#192).

type phiLoggerHolder struct{ l zerolog.Logger }

type phiEventHolder struct{ e *zerolog.Event }

func goodLoggerFieldThroughPointerPickedOnBranch(ctx context.Context, cond bool) {
	h := &phiLoggerHolder{l: zerolog.Ctx(ctx).With().Logger()}
	if cond {
		h = &phiLoggerHolder{l: zerolog.Ctx(ctx).With().Str("k", "v").Logger()}
	}
	h.l.Info().Msg("both holders carry ctx") // OK - both loggers have ctx
}

func goodEventFieldThroughPointerPickedOnBranch(ctx context.Context, logger zerolog.Logger, cond bool) {
	h := &phiEventHolder{e: logger.Info().Ctx(ctx)}
	if cond {
		h = &phiEventHolder{e: logger.Warn().Ctx(ctx)}
	}
	h.e.Msg("both events carry ctx") // OK - both events have ctx
}

func goodEventFieldThroughPointerSetAfterPick(ctx context.Context, logger zerolog.Logger, cond bool) {
	h := &phiEventHolder{e: logger.Info()}
	if cond {
		h = &phiEventHolder{e: logger.Warn()}
	}
	h.e = logger.Error().Ctx(ctx)
	h.e.Msg("set through the pointer after the pick") // OK - the event stored last has ctx
}

func goodLoggerFieldThroughPointerToVariablePickedOnBranch(ctx context.Context, logger zerolog.Logger, cond bool) {
	a := phiLoggerHolder{l: logger.With().Ctx(ctx).Logger()}
	b := phiLoggerHolder{l: logger.With().Ctx(ctx).Str("k", "v").Logger()}
	p := &a
	if cond {
		p = &b
	}
	p.l.Info().Msg("pointer to a variable picked on a branch") // OK - both loggers have ctx
}

func goodEventFieldThroughPointerPickedOnNestedBranches(ctx context.Context, logger zerolog.Logger, a, b bool) {
	h := &phiEventHolder{e: logger.Info().Ctx(ctx)}
	if a {
		h = &phiEventHolder{e: logger.Warn().Ctx(ctx)}
	} else if b {
		h = &phiEventHolder{e: logger.Error().Ctx(ctx)}
	}
	h.e.Msg("every holder carries ctx") // OK - every event has ctx
}

func badEventFieldThroughPointerPickedOnNestedBranches(ctx context.Context, logger zerolog.Logger, a, b bool) {
	h := &phiEventHolder{e: logger.Info().Ctx(ctx)}
	if a {
		h = &phiEventHolder{e: logger.Warn().Ctx(ctx)}
	} else if b {
		h = &phiEventHolder{e: logger.Error()}
	}
	h.e.Msg("one nested holder lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventFieldThroughPointerPickedWithNil(ctx context.Context, logger zerolog.Logger, cond bool) {
	var h *phiEventHolder
	if cond {
		h = &phiEventHolder{e: logger.Info()}
	}
	h.e.Msg("the only holder lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badLoggerFieldThroughPointerPickedOnBranch(ctx context.Context, logger zerolog.Logger, cond bool) {
	h := &phiLoggerHolder{l: logger.With().Ctx(ctx).Logger()}
	if cond {
		h = &phiLoggerHolder{l: logger}
	}
	h.l.Info().Msg("one holder lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventFieldThroughPointerPickedOnBranch(ctx context.Context, logger zerolog.Logger, cond bool) {
	h := &phiEventHolder{e: logger.Info().Ctx(ctx)}
	if cond {
		h = &phiEventHolder{e: logger.Warn()}
	}
	h.e.Msg("one event lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventFieldThroughPointerToVariableChangedAfterPick(ctx context.Context, logger zerolog.Logger, cond bool) {
	a := phiEventHolder{e: logger.Info().Ctx(ctx)}
	b := phiEventHolder{e: logger.Warn().Ctx(ctx)}
	p := &a
	if cond {
		p = &b
	}
	b.e = logger.Error()
	p.e.Msg("one variable changed after the pick") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventFieldThroughPointerPickedFromParam(ctx context.Context, logger zerolog.Logger, cond bool, q *phiEventHolder) {
	h := &phiEventHolder{e: logger.Info().Ctx(ctx)}
	if cond {
		h = q
	}
	h.e.Msg("one pointer is the caller's") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventFieldThroughPointerPickedInLoop(ctx context.Context, logger zerolog.Logger, n int) {
	h := &phiEventHolder{e: logger.Info().Ctx(ctx)}
	for range n {
		h.e.Msg("one holder of the loop lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
		h = &phiEventHolder{e: logger.Warn()}
	}
}

func goodEventFieldThroughPointerPickedInLoop(ctx context.Context, logger zerolog.Logger, n int) {
	h := &phiEventHolder{e: logger.Info().Ctx(ctx)}
	for range n {
		h.e.Msg("every holder of the loop has ctx") // OK - both events have ctx
		h = &phiEventHolder{e: logger.Warn().Ctx(ctx)}
	}
}

// A path back through a Phi of pointers reads the place the edge it takes
// points to. The caller's event at q.e is carried only by the path through q,
// and a Ctx call on q.e counts for it there (#313).

func goodEventFieldThroughPointerPickedFromParamCtxOnBoth(ctx context.Context, logger zerolog.Logger, cond bool, q *phiEventHolder) {
	h := &phiEventHolder{e: logger.Info().Ctx(ctx)}
	if cond {
		q.e.Ctx(ctx)
		h = q
	}
	h.e.Msg("both pointers have ctx") // OK
}

func goodEventFieldThroughPointerPickedFromParamCtxBeforePick(ctx context.Context, logger zerolog.Logger, cond bool, q *phiEventHolder) {
	q.e.Ctx(ctx)
	h := &phiEventHolder{e: logger.Info().Ctx(ctx)}
	if cond {
		h = q
	}
	h.e.Msg("ctx on the caller's event before the pick") // OK
}

func badEventFieldThroughPointerPickedFromParamCtxOnOther(ctx context.Context, logger zerolog.Logger, cond bool, q, r *phiEventHolder) {
	h := &phiEventHolder{e: logger.Info().Ctx(ctx)}
	if cond {
		r.e.Ctx(ctx)
		h = q
	}
	h.e.Msg("ctx on another caller's event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventFieldThroughPointerPickedFromParamCtxOnHolderPath(ctx context.Context, logger zerolog.Logger, cond bool, q *phiEventHolder) {
	h := &phiEventHolder{e: logger.Info()}
	if cond {
		h = q
	} else {
		q.e.Ctx(ctx)
		h.e.Ctx(ctx)
	}
	h.e.Msg("ctx on the caller's event only where it is not picked") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventFieldThroughPointerPickedFromParamStoredOrCtx(ctx context.Context, logger zerolog.Logger, stored, cond bool, q *phiEventHolder) {
	if stored {
		q.e = logger.Warn().Ctx(ctx)
	} else {
		q.e.Ctx(ctx)
	}
	h := &phiEventHolder{e: logger.Info().Ctx(ctx)}
	if cond {
		h = q
	}
	h.e.Msg("an event with ctx stored at q.e, or ctx on the caller's") // OK
}

func badEventFieldThroughPointerPickedFromParamStoredOnOnePath(ctx context.Context, logger zerolog.Logger, stored, cond bool, q *phiEventHolder) {
	if stored {
		q.e = logger.Warn().Ctx(ctx)
	}
	h := &phiEventHolder{e: logger.Info().Ctx(ctx)}
	if cond {
		h = q
	}
	h.e.Msg("the caller's event at q.e when nothing is stored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventFieldThroughPointerPickedWithNilFromParamCtx(ctx context.Context, cond bool, q *phiEventHolder) {
	var h *phiEventHolder
	if cond {
		q.e.Ctx(ctx)
		h = q
	}
	h.e.Msg("the only holder is the caller's, with ctx") // OK - a nil holder has no event
}

func badEventFieldThroughPointerPickedWithNilFromParam(ctx context.Context, cond, other bool, q *phiEventHolder) {
	var h *phiEventHolder
	if cond {
		h = q
	}
	if other {
		q.e.Ctx(ctx)
	}
	h.e.Msg("the only holder is the caller's, with ctx on one path") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func phiNewEventHolder(e *zerolog.Event) *phiEventHolder {
	return &phiEventHolder{e: e}
}

func badEventFieldThroughPointerPickedFromCallAndParamCtx(ctx context.Context, logger zerolog.Logger, cond bool, q *phiEventHolder) {
	h := phiNewEventHolder(logger.Info())
	if cond {
		q.e.Ctx(ctx)
		h = q
	}
	h.e.Msg("the holder a call returns lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventFieldThroughPointerPickedFromCallAndParamCtxAfter(ctx context.Context, logger zerolog.Logger, cond bool, q *phiEventHolder) {
	h := phiNewEventHolder(logger.Info())
	if cond {
		h = q
	}
	h.e.Ctx(ctx)
	h.e.Msg("ctx on the holder picked") // OK
}

// The path through q does not meet the store into the struct literal the
// other event is read from.
func badEventFieldThroughPointerPickedFromParamStructLiteral(ctx context.Context, logger zerolog.Logger, cond bool, q *phiEventHolder) {
	h := &phiEventHolder{e: (&phiEventHolder{logger.Info()}).e.Ctx(ctx)}
	if cond {
		h = q
	}
	h.e.Msg("one pointer is the caller's") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventFieldThroughPointerPickedFromParamStructLiteralCtx(ctx context.Context, logger zerolog.Logger, cond bool, q *phiEventHolder) {
	h := &phiEventHolder{e: (&phiEventHolder{logger.Info()}).e.Ctx(ctx)}
	if cond {
		q.e.Ctx(ctx)
		h = q
	}
	h.e.Msg("both pointers have ctx, one from a struct literal") // OK
}

// ===== AN EVENT READ FROM MEMORY, PICKED WITH ANOTHER =====

// A path that takes the other edge of a Phi carries the event of that edge,
// so a store to where the first event is read from does not end it (#302).

func badEventFromStructLiteralPickedWithParam(ctx context.Context, logger zerolog.Logger, cond bool, e2 *zerolog.Event) {
	e := (&phiEventHolder{logger.Info()}).e.Ctx(ctx)
	if cond {
		e = e2
	}
	e.Msg("one edge is the caller's event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventFromStructLiteralOfVariablePickedWithParam(ctx context.Context, logger zerolog.Logger, cond bool, e2 *zerolog.Event) {
	x := logger.Info()
	e := (&phiEventHolder{x}).e.Ctx(ctx)
	if cond {
		e = e2
	}
	e.Msg("one edge is the caller's event, the other from a variable") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventFromNestedStructLiteralsPickedWithParam(ctx context.Context, logger zerolog.Logger, cond bool, e2 *zerolog.Event) {
	e := (&phiEventHolder{(&phiEventHolder{logger.Info()}).e}).e.Ctx(ctx)
	if cond {
		e = e2
	}
	e.Msg("one edge is the caller's event, the other from nested struct literals") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventFromStructLiteralPickedWithParamCtx(ctx context.Context, logger zerolog.Logger, cond bool, e2 *zerolog.Event) {
	e := (&phiEventHolder{logger.Info()}).e.Ctx(ctx)
	if cond {
		e2.Ctx(ctx)
		e = e2
	}
	e.Msg("both edges have ctx") // OK
}

func goodEventFromStructLiteralPickedWithStructLiteral(ctx context.Context, logger zerolog.Logger, cond bool) {
	e := (&phiEventHolder{logger.Info()}).e.Ctx(ctx)
	if cond {
		e = (&phiEventHolder{logger.Warn()}).e.Ctx(ctx)
	}
	e.Msg("both edges from struct literals have ctx") // OK
}

func badEventFromStructLiteralInPlacePickedWithParam(ctx context.Context, logger zerolog.Logger, cond bool, e2 *zerolog.Event) {
	b := &phiEventHolder{logger.Info()}
	e := b.e
	if cond {
		e = e2
	}
	b.e.Ctx(ctx)
	e.Msg("ctx in place on the struct literal's event only") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventFromStructLiteralInPlacePickedWithParamCtx(ctx context.Context, logger zerolog.Logger, cond bool, e2 *zerolog.Event) {
	b := &phiEventHolder{logger.Info()}
	e := b.e
	if cond {
		e = e2
	}
	b.e.Ctx(ctx)
	e2.Ctx(ctx)
	e.Msg("ctx in place on both events") // OK
}

func goodEventFromStructLiteralPickedWithParamInLoopCtx(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	e := (&phiEventHolder{logger.Info()}).e.Ctx(ctx)
	for range n {
		e.Msg("the caller's event with ctx on the later iterations") // OK
		e = e2.Ctx(ctx)
	}
}

func goodEventFromStructLiteralPickedWithParamFreshOnOther(ctx context.Context, logger zerolog.Logger, cond bool, e2 *zerolog.Event) {
	e := (&phiEventHolder{logger.Info().Ctx(ctx)}).e
	if cond {
		e = e2.Ctx(ctx)
	}
	logger.Warn().Ctx(context.Background()).Msg("a fresh context on another event") // want `zerolog call chain missing .Ctx\(ctx\)`
	e.Msg("both edges have ctx of their own") // OK
}

// The same holds for a receiver that reads the Phi back from memory: the read
// sees only the Phi, so a path takes each edge of it (#325).

func badEventFromStructLiteralPickedWithParamReadBack(ctx context.Context, logger zerolog.Logger, cond bool, e2 *zerolog.Event) {
	e := (&phiEventHolder{logger.Info()}).e.Ctx(ctx)
	if cond {
		e = e2
	}
	(&phiEventHolder{e}).e.Msg("one edge is the caller's event, read back") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventFromStructLiteralPickedWithParamReadBackFromVariable(ctx context.Context, logger zerolog.Logger, cond bool, e2 *zerolog.Event) {
	e := (&phiEventHolder{logger.Info()}).e.Ctx(ctx)
	if cond {
		e = e2
	}
	h := &phiEventHolder{e}
	h.e.Msg("one edge is the caller's event, read back from a variable") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventFromStructLiteralPickedWithParamReadBackTwice(ctx context.Context, logger zerolog.Logger, cond bool, e2 *zerolog.Event) {
	e := (&phiEventHolder{logger.Info()}).e.Ctx(ctx)
	if cond {
		e = e2
	}
	(&phiEventHolder{(&phiEventHolder{e}).e}).e.Msg("one edge is the caller's event, read back twice") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventCtxInPlaceOnStructLiteralPickedWithParamReadBack(ctx context.Context, logger zerolog.Logger, cond bool, e2 *zerolog.Event) {
	x := (&phiEventHolder{logger.Info()}).e
	x.Ctx(ctx)
	e := x
	if cond {
		e = e2
	}
	(&phiEventHolder{e}).e.Msg("ctx in place on one edge, read back") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventFromStructLiteralPickedWithParamCtxReadBack(ctx context.Context, logger zerolog.Logger, cond bool, e2 *zerolog.Event) {
	e := (&phiEventHolder{logger.Info()}).e.Ctx(ctx)
	if cond {
		e2.Ctx(ctx)
		e = e2
	}
	(&phiEventHolder{e}).e.Msg("both edges have ctx, read back") // OK
}

func goodEventFromStructLiteralPickedWithStructLiteralReadBack(ctx context.Context, logger zerolog.Logger, cond bool) {
	e := (&phiEventHolder{logger.Info()}).e.Ctx(ctx)
	if cond {
		e = (&phiEventHolder{logger.Warn()}).e.Ctx(ctx)
	}
	(&phiEventHolder{e}).e.Msg("both edges from struct literals have ctx, read back") // OK
}

func badEventFromStructLiteralPickedWithParamReadBackInLoop(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	e := (&phiEventHolder{logger.Info()}).e.Ctx(ctx)
	for range n {
		(&phiEventHolder{e}).e.Msg("the caller's event on the later iterations, read back") // want `zerolog call chain missing .Ctx\(ctx\)`
		e = e2
	}
}

func goodEventFromStructLiteralPickedWithParamReadBackInLoopCtx(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	e := (&phiEventHolder{logger.Info()}).e.Ctx(ctx)
	for range n {
		(&phiEventHolder{e}).e.Msg("the caller's event with ctx on the later iterations, read back") // OK
		e = e2.Ctx(ctx)
	}
}

// A read before the store in a loop sees the Phi of the iteration before,
// whose edges a path takes as it goes back around the loop.

func badEventPickedWithParamReadBeforeStoreInLoop(ctx context.Context, logger zerolog.Logger, n int, cond bool, e2 *zerolog.Event) {
	e := logger.Info().Ctx(ctx)
	h := &phiEventHolder{nil}
	for range n {
		if h.e != nil {
			h.e.Msg("the event of the iteration before, read back") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		h.e = e
		if cond {
			e = e2
		}
	}
}

func badEventFromStructLiteralPickedWithParamReadBeforeStoreInLoop(ctx context.Context, logger zerolog.Logger, n int, cond bool, e2 *zerolog.Event) {
	e := (&phiEventHolder{logger.Info()}).e.Ctx(ctx)
	h := &phiEventHolder{nil}
	for range n {
		if h.e != nil {
			h.e.Msg("the event of the iteration before, read back from a struct literal") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		h.e = e
		if cond {
			e = e2
		}
	}
}

func goodEventFromStructLiteralPickedWithParamReadBeforeStoreInLoopCtx(ctx context.Context, logger zerolog.Logger, n int, cond bool, e2 *zerolog.Event) {
	e := (&phiEventHolder{logger.Info()}).e.Ctx(ctx)
	h := &phiEventHolder{nil}
	for range n {
		if h.e != nil {
			h.e.Msg("the event of the iteration before, with ctx, read back") // OK
		}
		h.e = e
		if cond {
			e = e2.Ctx(ctx)
		}
	}
}

// ===== AN EVENT REPLACED IN A LOOP AFTER ITS TERMINATOR =====

// The Phi at the top of the loop merges the event made before it with the one
// stored at the end of an iteration. A path back around the loop carries that
// one on, up to where it is made or given, as the caller's event is (#314).

func badLoopReplacedWithParamAfterTerminator(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	e := logger.Info().Ctx(ctx)
	for range n {
		e.Msg("the caller's event on the later iterations") // want `zerolog call chain missing .Ctx\(ctx\)`
		e = e2
	}
}

func goodLoopReplacedWithParamCtxBeforeLoop(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	e := logger.Info().Ctx(ctx)
	e2.Ctx(ctx)
	for range n {
		e.Msg("ctx in place on the caller's event before the loop") // OK
		e = e2
	}
}

func goodLoopReplacedWithParamCtxInLoop(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	e := logger.Info().Ctx(ctx)
	for range n {
		e.Msg("ctx in place on the caller's event before it is stored") // OK
		e2.Ctx(ctx)
		e = e2
	}
}

func badLoopReplacedWithParamOnBranch(ctx context.Context, logger zerolog.Logger, n int, cond bool, e2 *zerolog.Event) {
	e := logger.Info().Ctx(ctx)
	for range n {
		e.Msg("the caller's event after a branch stores it") // want `zerolog call chain missing .Ctx\(ctx\)`
		if cond {
			e = e2
		}
	}
}

func goodLoopReplacedWithParamOnBranchCtx(ctx context.Context, logger zerolog.Logger, n int, cond bool, e2 *zerolog.Event) {
	e := logger.Info().Ctx(ctx)
	for range n {
		e.Msg("ctx in place on the caller's event on the branch") // OK
		if cond {
			e2.Ctx(ctx)
			e = e2
		}
	}
}

func badLoopReplacedWithEventWithoutCtx(ctx context.Context, logger zerolog.Logger, n int) {
	e := logger.Info().Ctx(ctx)
	for range n {
		e.Msg("an event without ctx on the later iterations") // want `zerolog call chain missing .Ctx\(ctx\)`
		e = logger.Warn()
	}
}

// ===== AN EVENT KEPT IN A FIELD FOR THE NEXT ITERATION =====

// A field read before its store in a loop holds nil on the first iteration,
// and the event stored on the iteration before on the later ones. A Ctx call
// on that event after the store counts, as it does for a variable (#333).

func goodLoopFieldKeptCtxAfterStore(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	e := logger.Info()
	b := &phiEventHolder{nil}
	for range n {
		if b.e != nil {
			b.e.Msg("the event of the iteration before, ctx after the store") // OK
		}
		b.e = e
		e.Ctx(ctx)
		e = e2.Ctx(ctx)
	}
}

func goodLoopVariableKeptCtxAfterStore(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	e := logger.Info()
	var last *zerolog.Event
	for range n {
		if last != nil {
			last.Msg("the event of the iteration before, ctx after the store") // OK
		}
		last = e
		e.Ctx(ctx)
		e = e2.Ctx(ctx)
	}
}

func goodLoopFieldOfZeroValueKeptCtxAfterStore(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	e := logger.Info()
	var b phiEventHolder
	for range n {
		if b.e != nil {
			b.e.Msg("nil on the first iteration, the zero value") // OK
		}
		b.e = e
		e.Ctx(ctx)
		e = e2.Ctx(ctx)
	}
}

func goodLoopFieldKeptCtxBeforeStore(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	e := logger.Info()
	b := &phiEventHolder{nil}
	for range n {
		if b.e != nil {
			b.e.Msg("the event of the iteration before, ctx before the store") // OK
		}
		e.Ctx(ctx)
		b.e = e
		e = e2.Ctx(ctx)
	}
}

func goodLoopFieldKeptMadeInLoopCtxAfterStore(ctx context.Context, logger zerolog.Logger, n int) {
	b := &phiEventHolder{nil}
	for range n {
		if b.e != nil {
			b.e.Msg("the run before, ctx after the store") // OK
		}
		e := logger.Info()
		b.e = e
		e.Ctx(ctx)
	}
}

func badLoopFieldKeptCtxOnBranch(ctx context.Context, logger zerolog.Logger, n int, cond bool, e2 *zerolog.Event) {
	e := logger.Info()
	b := &phiEventHolder{nil}
	for range n {
		if b.e != nil {
			b.e.Msg("ctx after the store on a branch only") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		b.e = e
		if cond {
			e.Ctx(ctx)
		}
		e = e2.Ctx(ctx)
	}
}

func badLoopFieldKeptCtxBeforeStoreOnSomeIterations(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	e := logger.Info()
	b := &phiEventHolder{nil}
	for i := range n {
		if b.e != nil {
			b.e.Msg("ctx before the store on even iterations only") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		if i%2 == 0 {
			e.Ctx(ctx)
		}
		b.e = e
		e = e2.Ctx(ctx)
	}
}

func badLoopFieldKeptWithoutCtx(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	e := logger.Info()
	b := &phiEventHolder{nil}
	for range n {
		if b.e != nil {
			b.e.Msg("the first event, stored without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		b.e = e
		e = e2.Ctx(ctx)
	}
}

func badLoopFieldKeptReplacedWithoutCtx(ctx context.Context, logger zerolog.Logger, n int) {
	e := logger.Info()
	b := &phiEventHolder{nil}
	for range n {
		if b.e != nil {
			b.e.Msg("an event without ctx stored on each iteration") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		b.e = e
		e = logger.Warn()
	}
}

func badLoopFieldKeptCtxOnNextEventOnly(ctx context.Context, logger zerolog.Logger, n int) {
	e := logger.Info()
	b := &phiEventHolder{nil}
	for range n {
		if b.e != nil {
			b.e.Msg("ctx on the event stored next, not this one") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		b.e = e
		e = logger.Warn()
		e.Ctx(ctx)
	}
}

func badLoopFieldKeptMadeInLoopWithoutCtx(ctx context.Context, logger zerolog.Logger, n int) {
	b := &phiEventHolder{nil}
	for range n {
		if b.e != nil {
			b.e.Msg("the run before, stored without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		b.e = logger.Info()
	}
}

func badFieldOfZeroValueStored(ctx context.Context, logger zerolog.Logger) {
	b := &phiEventHolder{}
	b.e = logger.Info()
	b.e.Msg("stored over the zero value, without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFieldStoredThenNilOnBranch(ctx context.Context, logger zerolog.Logger, cond bool) {
	b := &phiEventHolder{nil}
	b.e = logger.Info()
	if cond {
		b.e = nil
	}
	b.e.Msg("the event without ctx on the other path") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A field that a call can write to may hold another event than nil, so nil
// stored there before does not end a path.

func phiFillHolder(b *phiEventHolder, logger zerolog.Logger) {
	b.e = logger.Warn()
}

func phiFillEvent(e **zerolog.Event, logger zerolog.Logger) {
	*e = logger.Warn()
}

func badLoopFieldKeptAfterCall(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	e := logger.Info()
	b := &phiEventHolder{nil}
	phiFillHolder(b, logger)
	for range n {
		b.e.Msg("the call's event on the first iteration") // want `zerolog call chain missing .Ctx\(ctx\)`
		b.e = e
		e.Ctx(ctx)
		e = e2.Ctx(ctx)
	}
}

func badLoopFieldKeptAfterCallWithField(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	e := logger.Info()
	b := &phiEventHolder{nil}
	phiFillEvent(&b.e, logger)
	for range n {
		b.e.Msg("the call's event on the first iteration") // want `zerolog call chain missing .Ctx\(ctx\)`
		b.e = e
		e.Ctx(ctx)
		e = e2.Ctx(ctx)
	}
}

func badLoopFieldOfParamKeptAfterCall(ctx context.Context, logger zerolog.Logger, n int, b *phiEventHolder, e2 *zerolog.Event) {
	e := logger.Info()
	b.e = nil
	phiFillHolder(b, logger)
	for range n {
		b.e.Msg("the call's event on the first iteration") // want `zerolog call chain missing .Ctx\(ctx\)`
		b.e = e
		e.Ctx(ctx)
		e = e2.Ctx(ctx)
	}
}

// The holder declared before the event: a path that reaches where the event
// is made, without passing its store to the field, does not carry it there
// (#338).

func goodLoopFieldKeptCtxAfterStoreHolderFirst(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	b := &phiEventHolder{nil}
	e := logger.Info()
	for range n {
		if b.e != nil {
			b.e.Msg("the holder declared first, ctx after the store") // OK
		}
		b.e = e
		e.Ctx(ctx)
		e = e2.Ctx(ctx)
	}
}

func goodLoopFieldKeptCtxBeforeStoreHolderFirst(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	b := &phiEventHolder{nil}
	e := logger.Info()
	for range n {
		if b.e != nil {
			b.e.Msg("the holder declared first, ctx before the store") // OK
		}
		e.Ctx(ctx)
		b.e = e
		e = e2.Ctx(ctx)
	}
}

func badLoopFieldKeptWithoutCtxHolderFirst(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	b := &phiEventHolder{nil}
	e := logger.Info()
	for range n {
		if b.e != nil {
			b.e.Msg("the holder declared first, stored without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		b.e = e
		e = e2.Ctx(ctx)
	}
}

func badFieldStoredOnBranchHolderFirst(ctx context.Context, logger zerolog.Logger, cond bool) {
	b := &phiEventHolder{nil}
	e := logger.Info()
	if cond {
		b.e = e
	}
	if b.e != nil {
		b.e.Msg("the holder declared first, stored on a branch without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

// A store to where a value stored in the field is read from counts only once
// the path passes the store to the field. A variable captured by a func
// literal is such a place (#337).

func goodLoopCapturedKeptCtxAfterStore(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	e := logger.Info()
	_ = func() { _ = e }
	b := &phiEventHolder{nil}
	for range n {
		if b.e != nil {
			b.e.Msg("a captured event, ctx after the store") // OK
		}
		b.e = e
		e.Ctx(ctx)
		e = e2.Ctx(ctx)
	}
}

func badLoopCapturedKeptWithoutCtx(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	e := logger.Info()
	_ = func() { _ = e }
	b := &phiEventHolder{nil}
	for range n {
		if b.e != nil {
			b.e.Msg("a captured event, stored without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		b.e = e
		e = e2.Ctx(ctx)
	}
}

func badLoopCapturedKeptWithoutCtxNoCheck(ctx context.Context, logger zerolog.Logger, n int, e2 *zerolog.Event) {
	e := logger.Info()
	_ = func() { _ = e }
	b := &phiEventHolder{nil}
	for range n {
		b.e.Msg("a captured event, stored without ctx, no nil check") // want `zerolog call chain missing .Ctx\(ctx\)`
		b.e = e
		e = e2.Ctx(ctx)
	}
}

// ===== AN EVENT READ BACK FROM A NEW HOLDER IN A LOOP =====

// A holder made on each iteration holds nil when it is made, but what a path
// carries there is the value stored into it, which it reads back, not the
// value read from the holder of the iteration before (#337).

func goodLoopEventReadBackFromBox(ctx context.Context, logger zerolog.Logger, n int, cond bool) {
	e := logger.Info().Ctx(ctx)
	for range n {
		if cond {
			e = logger.Warn().Ctx(ctx)
		}
		e = (&phiEventHolder{e}).e
	}
	e.Msg("every event read back has ctx") // OK
}

func badLoopEventReadBackFromBox(ctx context.Context, logger zerolog.Logger, n int, cond bool) {
	e := logger.Info().Ctx(ctx)
	for range n {
		if cond {
			e = logger.Warn()
		}
		e = (&phiEventHolder{e}).e
	}
	e.Msg("the event on a branch, read back, lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badLoopEventReadBackFromBoxWithField(ctx context.Context, logger zerolog.Logger, n int, cond bool) {
	e := logger.Info().Ctx(ctx)
	for range n {
		if cond {
			e = logger.Warn()
		}
		e = (&phiEventHolder{e}).e.Str("k", "v")
	}
	e.Msg("the event on a branch, read back with a field, lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badLoopEventReadBackFromBoxStoredLater(ctx context.Context, logger zerolog.Logger, n int, cond bool) {
	e := logger.Info().Ctx(ctx)
	for range n {
		if cond {
			e = logger.Warn()
		}
		b := &phiEventHolder{}
		b.e = e
		e = b.e
	}
	e.Msg("the event on a branch, stored over the zero value, lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== POINTER TO A VARIABLE DECLARED IN A BRANCH =====

// A Phi of pointers that may point to a variable declared in a branch or a
// loop takes it only from the edges that run its declaration. On the other
// edges, the variable holds nothing that the Phi passes on (#232).

func goodLoggerPointerToVariableDeclaredInBranch(ctx context.Context, base zerolog.Logger, cond bool) {
	p := zerolog.Ctx(ctx)
	if cond {
		l := base.With().Ctx(ctx).Logger()
		p = &l
	}
	p.Info().Msg("pointer to a variable declared in a branch") // OK - both loggers have ctx
}

func goodLoggerPointerToOneOfTwoVariablesDeclaredInBranch(ctx context.Context, base zerolog.Logger, cond bool) {
	a := base.With().Ctx(ctx).Logger()
	p := &a
	if cond {
		b := base.With().Ctx(ctx).Logger()
		p = &b
	}
	p.Info().Msg("one of two variables declared in a branch") // OK - both loggers have ctx
}

// Read in a func literal, the pointer is a captured variable. Each value it
// can hold is walked along the paths on which the variable holds it, out of
// the closure to where it runs: l from the store p = &l only, and the logger
// of ctx from the other stores.
func goodLoggerPointerToVariableSetBeforePickInIIFE(ctx context.Context, base zerolog.Logger, a, b bool) {
	p := zerolog.Ctx(ctx)
	if a {
		l := base
		if b {
			l = base.With().Ctx(ctx).Logger()
			p = &l
		}
	}
	func() {
		p.Info().Msg("l lacks ctx only on the edge that does not pick it, in an IIFE") // OK - p points to l only after the store with ctx
	}()
}

func goodLoggerPointerToVariableSetBeforePickInDeferred(ctx context.Context, base zerolog.Logger, a, b bool) {
	p := zerolog.Ctx(ctx)
	if a {
		l := base
		if b {
			l = base.With().Ctx(ctx).Logger()
			p = &l
		}
	}
	defer func() {
		p.Info().Msg("l lacks ctx only on the edge that does not pick it, deferred") // OK - p points to l only after the store with ctx
	}()
}

func badLoggerPointerToVariableSetBeforePickInIIFE(ctx context.Context, base zerolog.Logger, a, b bool) {
	p := zerolog.Ctx(ctx)
	if a {
		l := base.With().Ctx(ctx).Logger()
		if b {
			l = base.With().Logger()
			p = &l
		}
	}
	func() {
		p.Info().Msg("l lacks ctx on the edge that picks it, in an IIFE") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

func badLoggerPointerToVariableSetBeforePickInDeferred(ctx context.Context, base zerolog.Logger, a, b bool) {
	p := zerolog.Ctx(ctx)
	if a {
		l := base.With().Ctx(ctx).Logger()
		if b {
			l = base.With().Logger()
			p = &l
		}
	}
	defer func() {
		p.Info().Msg("l lacks ctx on the edge that picks it, deferred") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

// A store to l after the pick reaches the read on the paths that pick l.
func goodLoggerPointerToVariableSetAfterPickInIIFE(ctx context.Context, base zerolog.Logger, cond bool) {
	p := zerolog.Ctx(ctx)
	l := base
	if cond {
		p = &l
	}
	l = base.With().Ctx(ctx).Logger()
	func() {
		p.Info().Msg("l set with ctx after the pick, in an IIFE") // OK - the store after the pick has ctx
	}()
}

func badLoggerPointerToVariableSetAfterPickInIIFE(ctx context.Context, base zerolog.Logger, cond bool) {
	p := zerolog.Ctx(ctx)
	l := base.With().Ctx(ctx).Logger()
	if cond {
		p = &l
	}
	l = base
	func() {
		p.Info().Msg("l set without ctx after the pick, in an IIFE") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

// A goroutine may run after a store to the variable it reads the pointer
// from, so a value stored there after the go statement counts too.
func goodLoggerPointerSetAfterGoroutineStarts(ctx context.Context, base zerolog.Logger) {
	p := zerolog.Ctx(ctx)
	l := base.With().Ctx(ctx).Logger()
	go func() {
		p.Info().Msg("p set after the goroutine starts") // OK - both loggers have ctx
	}()
	p = &l
}

func badLoggerPointerSetAfterGoroutineStarts(ctx context.Context, base zerolog.Logger) {
	p := zerolog.Ctx(ctx)
	l := base
	go func() {
		p.Info().Msg("p set after the goroutine starts, without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
	p = &l
}

// A closure deferred on one branch reads the pointer at the return, on the
// paths through its defer statement only.
func goodLoggerPointerPickedReadInClosureDeferredOnBranch(ctx context.Context, base zerolog.Logger, cond, other bool) {
	p := zerolog.Ctx(ctx)
	if other {
		l := base.With().Ctx(ctx).Logger()
		p = &l
	}
	if cond {
		defer func() {
			p.Info().Msg("picked, read in a closure deferred on a branch") // OK - both loggers have ctx
		}()
	}
}

func badLoggerPointerPickedReadInClosureDeferredOnBranch(ctx context.Context, base zerolog.Logger, cond, other bool) {
	p := zerolog.Ctx(ctx)
	if other {
		l := base
		p = &l
	}
	if cond {
		defer func() {
			p.Info().Msg("picked, read in a closure deferred on a branch, one without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
		}()
	}
}

// A deferred call of a method on the pointer takes the pointer at its defer
// statement, and reads what it points to at the return: a store through the
// pointer after the statement is what it reads (#363).
func goodLoggerPointerPickedDeferredStoredAfter(ctx context.Context, base zerolog.Logger, other bool) {
	a := base
	b := base
	p := &a
	if other {
		p = &b
	}
	defer p.Print("picked, deferred, stored through after") // OK - the store after the defer has ctx
	*p = base.With().Ctx(ctx).Logger()
}

func goodLoggerPointerOrCtxPickedDeferredStoredAfter(ctx context.Context, base zerolog.Logger, other bool) {
	l := base
	p := zerolog.Ctx(ctx)
	if other {
		p = &l
	}
	defer p.Print("picked with the logger of ctx, deferred, stored through after") // OK - the store after the defer has ctx
	*p = base.With().Ctx(ctx).Logger()
}

func goodLoggerPointerToVariableDeclaredInBranchDeferredStoredAfter(ctx context.Context, base zerolog.Logger, other bool) {
	p := zerolog.Ctx(ctx)
	if other {
		l := base
		p = &l
	}
	defer p.Print("picked, declared in a branch, deferred, stored through after") // OK - the store after the defer has ctx
	*p = base.With().Ctx(ctx).Logger()
}

func goodLoggerPointerParamOrVariablePickedDeferredStoredAfter(ctx context.Context, l1 *zerolog.Logger, base zerolog.Logger, other bool) {
	l := base
	p := l1
	if other {
		p = &l
	}
	defer p.Print("pointer parameter or variable, deferred, stored through after") // OK - the store after the defer has ctx
	*p = base.With().Ctx(ctx).Logger()
}

func goodLoggerPointerPickedInLoopDeferredStoredAfter(ctx context.Context, base zerolog.Logger, n int) {
	a := base
	b := base
	p := &a
	for i := range n {
		if i == 1 {
			p = &b
		}
	}
	defer p.Print("picked in a loop, deferred after it, stored through after") // OK - the store after the defer has ctx
	*p = base.With().Ctx(ctx).Logger()
}

func goodLoggerPointerFromCallDeferredStoredAfter(ctx context.Context, base zerolog.Logger) {
	p := newDeferredLoggerPtr()
	defer p.Print("from a call, deferred, stored through after") // OK - the store after the defer has ctx
	*p = base.With().Ctx(ctx).Logger()
}

func badLoggerPointerPickedDeferredStoredAfterOnBranch(ctx context.Context, base zerolog.Logger, other, cond bool) {
	a := base
	b := base
	p := &a
	if other {
		p = &b
	}
	defer p.Print("picked, deferred, stored through after on a branch") // want `zerolog direct logging bypasses context; use Event chain with .Ctx\(ctx\)`
	if cond {
		*p = base.With().Ctx(ctx).Logger()
	}
}

func badLoggerPointerPickedDeferredStoredWithoutCtxAfter(ctx context.Context, base zerolog.Logger, other bool) {
	p := zerolog.Ctx(ctx)
	if other {
		l := base.With().Ctx(ctx).Logger()
		p = &l
	}
	defer p.Print("picked, deferred, stored through without ctx after") // want `zerolog direct logging bypasses context; use Event chain with .Ctx\(ctx\)`
	*p = base
}

// Each iteration makes a pointer of its own, and the store after the
// defer statement is through the pointer of the last one.
func badLoggerPointerMadeInLoopDeferredStoredAfter(ctx context.Context, base zerolog.Logger, n int) {
	for i := range n {
		p := newDeferredLoggerPtr()
		if i == 0 {
			defer p.Print("made in a loop, deferred on the first iteration") // want `zerolog direct logging bypasses context; use Event chain with .Ctx\(ctx\)`
			continue
		}
		*p = base.With().Ctx(ctx).Logger()
	}
}

func newDeferredLoggerPtr() *zerolog.Logger { return new(zerolog.Logger) }

// LIMITATION (false positive): each iteration makes a pointer of its own.
// The pointer is made again after the defer statement. So the walk goes
// back from the statement, as for a go statement, and every store after it
// counts too. What the pointer points to before the statement is still
// seen, although the store after it, on the same iteration, has ctx.
func limitationLoggerPointerMadeInLoopDeferredStoredAfter(ctx context.Context, base zerolog.Logger, n int) {
	for range n {
		p := newDeferredLoggerPtr()
		defer p.Print("made in a loop, deferred, stored through after") // want `zerolog direct logging bypasses context; use Event chain with .Ctx\(ctx\)`
		*p = base.With().Ctx(ctx).Logger()
	}
}

// A Phi of pointers put in a struct field is the pointer read back from the
// field: a store through the Phi is a store through it (#367).
type phiLoggerPtrBox struct{ p *zerolog.Logger }

func goodLoggerPointerPickedInFieldStoredThrough(ctx context.Context, base zerolog.Logger, other bool) {
	a := base
	b := base
	p := &a
	if other {
		p = &b
	}
	h := phiLoggerPtrBox{p}
	*p = base.With().Ctx(ctx).Logger()
	h.p.Print("picked, put in a field, stored through") // OK - the store through p has ctx
}

func badLoggerPointerPickedInFieldStoredThroughWithoutCtx(ctx context.Context, base zerolog.Logger, other bool) {
	a := base.With().Ctx(ctx).Logger()
	b := base.With().Ctx(ctx).Logger()
	p := &a
	if other {
		p = &b
	}
	h := phiLoggerPtrBox{p}
	*p = base
	h.p.Print("picked, put in a field, stored through without ctx") // want `zerolog direct logging bypasses context; use Event chain with .Ctx\(ctx\)`
}

func goodLoggerPointerPickedStoredThroughThenPutInField(ctx context.Context, base zerolog.Logger, other bool) {
	a := base
	b := base
	p := &a
	if other {
		p = &b
	}
	*p = base.With().Ctx(ctx).Logger()
	h := phiLoggerPtrBox{p}
	h.p.Print("picked, stored through, put in a field") // OK - the store through p has ctx
}

func goodLoggerPointerPickedInFieldDeferredStoredAfter(ctx context.Context, base zerolog.Logger, other bool) {
	a := base
	b := base
	p := &a
	if other {
		p = &b
	}
	defer (&phiLoggerPtrBox{p}).p.Print("picked, put in a field, deferred, stored through after") // OK - the store after the defer has ctx
	*p = base.With().Ctx(ctx).Logger()
}

func badLoggerPointerPickedInFieldDeferredStoredWithoutCtxAfter(ctx context.Context, base zerolog.Logger, other bool) {
	a := base.With().Ctx(ctx).Logger()
	b := base.With().Ctx(ctx).Logger()
	p := &a
	if other {
		p = &b
	}
	defer (&phiLoggerPtrBox{p}).p.Print("picked, put in a field, deferred, stored through without ctx after") // want `zerolog direct logging bypasses context; use Event chain with .Ctx\(ctx\)`
	*p = base
}

// A field that the pointer is copied into from another field holds the Phi
// alone too, when that one does where it is loaded (#379).
type (
	phiLoggerPtrOuter struct{ v *zerolog.Logger }
	phiLoggerPtrThird struct{ w *zerolog.Logger }
)

func goodLoggerPointerPickedInNestedFieldDeferredStoredAfter(ctx context.Context, base zerolog.Logger, other bool) {
	a := base
	b := base
	p := &a
	if other {
		p = &b
	}
	defer (&phiLoggerPtrOuter{(&phiLoggerPtrBox{p}).p}).v.Print("picked, in a nested field, deferred, stored through after") // OK - the store after the defer has ctx
	*p = base.With().Ctx(ctx).Logger()
}

func badLoggerPointerPickedInNestedFieldDeferredStoredWithoutCtxAfter(ctx context.Context, base zerolog.Logger, other bool) {
	a := base.With().Ctx(ctx).Logger()
	b := base.With().Ctx(ctx).Logger()
	p := &a
	if other {
		p = &b
	}
	defer (&phiLoggerPtrOuter{(&phiLoggerPtrBox{p}).p}).v.Print("picked, in a nested field, deferred, stored through without ctx after") // want `zerolog direct logging bypasses context; use Event chain with .Ctx\(ctx\)`
	*p = base
}

func goodLoggerPointerPickedInNestedFieldStoredThrough(ctx context.Context, base zerolog.Logger, other bool) {
	a := base
	b := base
	p := &a
	if other {
		p = &b
	}
	o := &phiLoggerPtrOuter{(&phiLoggerPtrBox{p}).p}
	*p = base.With().Ctx(ctx).Logger()
	o.v.Print("picked, in a nested field, stored through") // OK - the store through p has ctx
}

func badLoggerPointerPickedInNestedFieldStoredThroughWithoutCtx(ctx context.Context, base zerolog.Logger, other bool) {
	a := base.With().Ctx(ctx).Logger()
	b := base.With().Ctx(ctx).Logger()
	p := &a
	if other {
		p = &b
	}
	o := &phiLoggerPtrOuter{(&phiLoggerPtrBox{p}).p}
	*p = base
	o.v.Print("picked, in a nested field, stored through without ctx") // want `zerolog direct logging bypasses context; use Event chain with .Ctx\(ctx\)`
}

func goodLoggerPointerPickedInThreeFieldsDeferredStoredAfter(ctx context.Context, base zerolog.Logger, other bool) {
	a := base
	b := base
	p := &a
	if other {
		p = &b
	}
	defer (&phiLoggerPtrThird{(&phiLoggerPtrOuter{(&phiLoggerPtrBox{p}).p}).v}).w.Print("picked, in three fields, deferred, stored through after") // OK - the store after the defer has ctx
	*p = base.With().Ctx(ctx).Logger()
}

func goodLoggerPointerPickedInNestedLocalFieldsStoredThrough(ctx context.Context, base zerolog.Logger, other bool) {
	a := base
	b := base
	p := &a
	if other {
		p = &b
	}
	h := phiLoggerPtrBox{p}
	o := phiLoggerPtrOuter{h.p}
	*p = base.With().Ctx(ctx).Logger()
	o.v.Info().Msg("picked, in nested local fields, stored through") // OK - the store through p has ctx
}

func goodLoggerPointerPickedInNestedFieldOfCopyStoredThrough(ctx context.Context, base zerolog.Logger, other bool) {
	a := base
	b := base
	p := &a
	if other {
		p = &b
	}
	h := phiLoggerPtrBox{p}
	g := h
	o := phiLoggerPtrOuter{g.p}
	*p = base.With().Ctx(ctx).Logger()
	o.v.Info().Msg("picked, copied, in a nested field, stored through") // OK - the store through p has ctx
}

func goodLoggerPointerPickedInNestedFieldReadInClosure(ctx context.Context, base zerolog.Logger, other bool) {
	a := base
	b := base
	p := &a
	if other {
		p = &b
	}
	o := phiLoggerPtrOuter{(&phiLoggerPtrBox{p}).p}
	*p = base.With().Ctx(ctx).Logger()
	func() {
		o.v.Info().Msg("picked, in a nested field, read in a closure") // OK - the store through p has ctx
	}()
}

// The inner field is replaced after the outer one is loaded from it, so the
// outer field still holds p alone.
func goodLoggerPointerPickedInNestedFieldInnerReplacedAfter(ctx context.Context, base zerolog.Logger, other bool) {
	a := base
	b := base
	c := base
	p := &a
	if other {
		p = &b
	}
	h := phiLoggerPtrBox{p}
	o := phiLoggerPtrOuter{h.p}
	h.p = &c
	*p = base.With().Ctx(ctx).Logger()
	o.v.Info().Msg("picked, in a nested field, the inner one replaced after") // OK - o.v holds p
}

// The inner field holds another pointer when the outer one is loaded from
// it, so the store through p is not through the pointer read back.
func badLoggerPointerPickedInNestedFieldInnerReplacedBefore(ctx context.Context, base zerolog.Logger, other bool) {
	a := base
	b := base
	c := base
	p := &a
	if other {
		p = &b
	}
	h := phiLoggerPtrBox{p}
	h.p = &c
	o := phiLoggerPtrOuter{h.p}
	*p = base.With().Ctx(ctx).Logger()
	o.v.Info().Msg("picked, in a nested field, the inner one replaced before") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodLoggerPointerPickedInFieldOfCopyStoredThrough(ctx context.Context, base zerolog.Logger, other bool) {
	a := base
	b := base
	p := &a
	if other {
		p = &b
	}
	h := phiLoggerPtrBox{p}
	g := h
	*p = base.With().Ctx(ctx).Logger()
	g.p.Print("picked, put in a field, copied, stored through") // OK - the store through p has ctx
}

func badLoggerPointerPickedInFieldStoredThroughOnBranch(ctx context.Context, base zerolog.Logger, other, cond bool) {
	a := base
	b := base
	p := &a
	if other {
		p = &b
	}
	h := phiLoggerPtrBox{p}
	if cond {
		*p = base.With().Ctx(ctx).Logger()
	}
	h.p.Print("picked, put in a field, stored through on a branch") // want `zerolog direct logging bypasses context; use Event chain with .Ctx\(ctx\)`
}

// The field holds another pointer by the store, so the store through p is
// not through the pointer read back.
func badLoggerPointerPickedInFieldReplacedStoredThrough(ctx context.Context, base zerolog.Logger, other bool) {
	a := base
	b := base
	c := base
	p := &a
	if other {
		p = &b
	}
	h := phiLoggerPtrBox{p}
	h.p = &c
	*p = base.With().Ctx(ctx).Logger()
	h.p.Print("picked, put in a field, the field replaced, stored through") // want `zerolog direct logging bypasses context; use Event chain with .Ctx\(ctx\)`
}

// LIMITATION (false positive): the field may hold another pointer by the
// store, after a store on a branch, so it does not hold p alone there. The
// store through p is then not seen, even on the path where the field holds p.
func limitationLoggerPointerPickedInFieldMaybeReplacedStoredThrough(ctx context.Context, base zerolog.Logger, other, cond bool) {
	a := base
	b := base
	c := base.With().Ctx(ctx).Logger()
	p := &a
	if other {
		p = &b
	}
	h := phiLoggerPtrBox{p}
	if cond {
		h.p = &c
	}
	*p = base.With().Ctx(ctx).Logger()
	h.p.Print("picked, put in a field, the field maybe replaced, stored through") // want `zerolog direct logging bypasses context; use Event chain with .Ctx\(ctx\)`
}

func badLoggerPointerPickedInFieldDeferredReplacedStoredAfter(ctx context.Context, base zerolog.Logger, other bool) {
	a := base
	b := base
	c := base
	p := &a
	if other {
		p = &b
	}
	h := &phiLoggerPtrBox{p}
	h.p = &c
	defer h.p.Print("picked, put in a field, the field replaced, deferred, stored through after") // want `zerolog direct logging bypasses context; use Event chain with .Ctx\(ctx\)`
	*p = base.With().Ctx(ctx).Logger()
}

// LIMITATION(#115) (false negative): a store to a variable that a Phi of
// pointers may point to, after a store through the Phi, is a write through
// another reference. The walk for the stores through the pointer does not
// see it.
func limitationLoggerPointerPickedStoredThroughThenVariable(ctx context.Context, base zerolog.Logger, other bool) {
	a := base
	b := base
	p := &a
	if other {
		p = &b
	}
	*p = base.With().Ctx(ctx).Logger()
	a = base
	p.Print("picked, stored through, then the variable replaced") // LIMITATION(#115): should report but doesn't
}

// LIMITATION(#115) (false negative): the same for a deferred call.
func limitationLoggerPointerPickedDeferredStoredThroughThenVariable(ctx context.Context, base zerolog.Logger, other bool) {
	a := base
	b := base
	p := &a
	if other {
		p = &b
	}
	defer p.Print("picked, deferred, stored through, then the variable replaced") // LIMITATION(#115): should report but doesn't
	*p = base.With().Ctx(ctx).Logger()
	a = base
}

// A goroutine that stores through the pointer before it reads it reads that
// store, whichever value the variable holds when it runs.
func goodLoggerPointerStoredInGoroutineSetAfter(ctx context.Context, base zerolog.Logger) {
	p := zerolog.Ctx(ctx)
	m := base
	go func() {
		*p = base.With().Ctx(ctx).Logger()
		p.Info().Msg("stored through in the goroutine, p set after it starts") // OK - the goroutine stores with ctx first
	}()
	p = &m
}

// A Phi that may point to more variables than the walk takes is traced as a
// value.
func goodLoggerPointerToOneOfManyVariables(ctx context.Context, base zerolog.Logger, n int) {
	l0 := base.With().Ctx(ctx).Logger()
	p := &l0
	switch n {
	case 1:
		l := base.With().Ctx(ctx).Logger()
		p = &l
	case 2:
		l := base.With().Ctx(ctx).Logger()
		p = &l
	case 3:
		l := base.With().Ctx(ctx).Logger()
		p = &l
	case 4:
		l := base.With().Ctx(ctx).Logger()
		p = &l
	case 5:
		l := base.With().Ctx(ctx).Logger()
		p = &l
	case 6:
		l := base.With().Ctx(ctx).Logger()
		p = &l
	case 7:
		l := base.With().Ctx(ctx).Logger()
		p = &l
	case 8:
		l := base.With().Ctx(ctx).Logger()
		p = &l
	case 9:
		l := base.With().Ctx(ctx).Logger()
		p = &l
	case 10:
		l := base.With().Ctx(ctx).Logger()
		p = &l
	case 11:
		l := base.With().Ctx(ctx).Logger()
		p = &l
	case 12:
		l := base.With().Ctx(ctx).Logger()
		p = &l
	case 13:
		l := base.With().Ctx(ctx).Logger()
		p = &l
	case 14:
		l := base.With().Ctx(ctx).Logger()
		p = &l
	case 15:
		l := base.With().Ctx(ctx).Logger()
		p = &l
	case 16:
		l := base.With().Ctx(ctx).Logger()
		p = &l
	}
	p.Info().Msg("one of many variables") // OK - every logger has ctx
}

func goodLoggerFieldOfVariableDeclaredInBranch(ctx context.Context, base zerolog.Logger, cond bool) {
	h0 := phiLoggerHolder{l: base.With().Ctx(ctx).Logger()}
	p := &h0.l
	if cond {
		h := phiLoggerHolder{l: base.With().Ctx(ctx).Logger()}
		p = &h.l
	}
	p.Info().Msg("field of a variable declared in a branch") // OK - both loggers have ctx
}

func goodLoggerPointerToVariableDeclaredInLoop(ctx context.Context, base zerolog.Logger, xs []int) {
	p := zerolog.Ctx(ctx)
	for _, x := range xs {
		l := base.With().Ctx(ctx).Int("x", x).Logger()
		p = &l
	}
	p.Info().Msg("pointer to a variable declared in a loop") // OK - every logger has ctx
}

func goodLoggerPointerToVariableSetBeforePick(ctx context.Context, base zerolog.Logger, a, b bool) {
	p := zerolog.Ctx(ctx)
	if a {
		l := base
		if b {
			l = base.With().Ctx(ctx).Logger()
			p = &l
		}
	}
	p.Info().Msg("l lacks ctx only on the edge that does not pick it") // OK - p points to l only after the store with ctx
}

// The logger of ctx made in a loop, after the Phi, has no store through it,
// and is walked along the paths of the Phi. The variable declared in the
// branch holds nothing on the other edges.
func goodLoggerPointerToVariableDeclaredInBranchPickedInLoop(ctx context.Context, base zerolog.Logger, cond bool, n int) {
	p := zerolog.Ctx(ctx)
	if cond {
		l := base.With().Ctx(ctx).Logger()
		p = &l
	}
	for range n {
		p.Info().Msg("pointer to a variable declared in a branch, in a loop") // OK - both loggers have ctx
		p = zerolog.Ctx(ctx)
	}
}

// The logger of ctx is stored through in the loop, after the read. Only the
// path that picks it, from the iteration before, takes that store.
func goodLoggerPointerToVariableDeclaredInBranchPickedStoredInLoop(ctx context.Context, base zerolog.Logger, cond bool, n int) {
	p := zerolog.Ctx(ctx)
	if cond {
		l := base.With().Ctx(ctx).Logger()
		p = &l
	}
	for range n {
		p.Info().Msg("pointer to a variable declared in a branch, in a loop, stored through") // OK - both loggers have ctx
		q := zerolog.Ctx(ctx)
		*q = q.With().Ctx(ctx).Logger()
		p = q
	}
}

func badLoggerPointerToVariableChangedAfterPickInLoop(ctx context.Context, base zerolog.Logger, cond bool, n int) {
	p := zerolog.Ctx(ctx)
	if cond {
		l := base.With().Ctx(ctx).Logger()
		p = &l
		l = base
	}
	for range n {
		p.Info().Msg("the variable changed after the pick, in a loop") // want `zerolog call chain missing .Ctx\(ctx\)`
		p = zerolog.Ctx(ctx)
	}
}

// The logger of ctx is stored through in the loop, after the read. Only the
// path that picks it, from the iteration before, takes that store.
func badLoggerPointerToVariableChangedAfterPickStoredInLoop(ctx context.Context, base zerolog.Logger, cond bool, n int) {
	p := zerolog.Ctx(ctx)
	if cond {
		l := base.With().Ctx(ctx).Logger()
		p = &l
		l = base
	}
	for range n {
		p.Info().Msg("the variable changed after the pick, in a loop, stored through") // want `zerolog call chain missing .Ctx\(ctx\)`
		q := zerolog.Ctx(ctx)
		*q = q.With().Ctx(ctx).Logger()
		p = q
	}
}

func badLoggerPointerToVariableDeclaredInBranch(ctx context.Context, base zerolog.Logger, cond bool) {
	p := zerolog.Ctx(ctx)
	if cond {
		l := base
		p = &l
	}
	p.Info().Msg("the variable declared in the branch lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badLoggerPointerToVariableDeclaredInLoop(ctx context.Context, base zerolog.Logger, xs []int) {
	p := zerolog.Ctx(ctx)
	for _, x := range xs {
		l := base.With().Int("x", x).Logger()
		p = &l
	}
	p.Info().Msg("the variable declared in the loop lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badLoggerPointerToVariableChangedAfterPick(ctx context.Context, base zerolog.Logger, cond bool) {
	p := zerolog.Ctx(ctx)
	if cond {
		l := base.With().Ctx(ctx).Logger()
		p = &l
		l = base
	}
	p.Info().Msg("the variable changed after the pick") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== MANY BRANCHES THAT REASSIGN THE EVENT =====

// Each branch adds a Phi whose edges both lead to the previous one. Tracing
// must stay linear in the number of branches: traced once per path, these
// 64 branches would never finish (#75).
func goodManyBranchesReassignEvent(ctx context.Context, logger zerolog.Logger, c []bool) {
	e := logger.Info().Ctx(ctx)
	if c[0] {
		e = e.Str("k", "v")
	}
	if c[1] {
		e = e.Str("k", "v")
	}
	if c[2] {
		e = e.Str("k", "v")
	}
	if c[3] {
		e = e.Str("k", "v")
	}
	if c[4] {
		e = e.Str("k", "v")
	}
	if c[5] {
		e = e.Str("k", "v")
	}
	if c[6] {
		e = e.Str("k", "v")
	}
	if c[7] {
		e = e.Str("k", "v")
	}
	if c[8] {
		e = e.Str("k", "v")
	}
	if c[9] {
		e = e.Str("k", "v")
	}
	if c[10] {
		e = e.Str("k", "v")
	}
	if c[11] {
		e = e.Str("k", "v")
	}
	if c[12] {
		e = e.Str("k", "v")
	}
	if c[13] {
		e = e.Str("k", "v")
	}
	if c[14] {
		e = e.Str("k", "v")
	}
	if c[15] {
		e = e.Str("k", "v")
	}
	if c[16] {
		e = e.Str("k", "v")
	}
	if c[17] {
		e = e.Str("k", "v")
	}
	if c[18] {
		e = e.Str("k", "v")
	}
	if c[19] {
		e = e.Str("k", "v")
	}
	if c[20] {
		e = e.Str("k", "v")
	}
	if c[21] {
		e = e.Str("k", "v")
	}
	if c[22] {
		e = e.Str("k", "v")
	}
	if c[23] {
		e = e.Str("k", "v")
	}
	if c[24] {
		e = e.Str("k", "v")
	}
	if c[25] {
		e = e.Str("k", "v")
	}
	if c[26] {
		e = e.Str("k", "v")
	}
	if c[27] {
		e = e.Str("k", "v")
	}
	if c[28] {
		e = e.Str("k", "v")
	}
	if c[29] {
		e = e.Str("k", "v")
	}
	if c[30] {
		e = e.Str("k", "v")
	}
	if c[31] {
		e = e.Str("k", "v")
	}
	if c[32] {
		e = e.Str("k", "v")
	}
	if c[33] {
		e = e.Str("k", "v")
	}
	if c[34] {
		e = e.Str("k", "v")
	}
	if c[35] {
		e = e.Str("k", "v")
	}
	if c[36] {
		e = e.Str("k", "v")
	}
	if c[37] {
		e = e.Str("k", "v")
	}
	if c[38] {
		e = e.Str("k", "v")
	}
	if c[39] {
		e = e.Str("k", "v")
	}
	if c[40] {
		e = e.Str("k", "v")
	}
	if c[41] {
		e = e.Str("k", "v")
	}
	if c[42] {
		e = e.Str("k", "v")
	}
	if c[43] {
		e = e.Str("k", "v")
	}
	if c[44] {
		e = e.Str("k", "v")
	}
	if c[45] {
		e = e.Str("k", "v")
	}
	if c[46] {
		e = e.Str("k", "v")
	}
	if c[47] {
		e = e.Str("k", "v")
	}
	if c[48] {
		e = e.Str("k", "v")
	}
	if c[49] {
		e = e.Str("k", "v")
	}
	if c[50] {
		e = e.Str("k", "v")
	}
	if c[51] {
		e = e.Str("k", "v")
	}
	if c[52] {
		e = e.Str("k", "v")
	}
	if c[53] {
		e = e.Str("k", "v")
	}
	if c[54] {
		e = e.Str("k", "v")
	}
	if c[55] {
		e = e.Str("k", "v")
	}
	if c[56] {
		e = e.Str("k", "v")
	}
	if c[57] {
		e = e.Str("k", "v")
	}
	if c[58] {
		e = e.Str("k", "v")
	}
	if c[59] {
		e = e.Str("k", "v")
	}
	if c[60] {
		e = e.Str("k", "v")
	}
	if c[61] {
		e = e.Str("k", "v")
	}
	if c[62] {
		e = e.Str("k", "v")
	}
	if c[63] {
		e = e.Str("k", "v")
	}
	e.Msg("many branches with ctx") // OK - ctx set via .Ctx(ctx)
}

func badManyBranchesReassignEvent(ctx context.Context, logger zerolog.Logger, c []bool) {
	e := logger.Info()
	if c[0] {
		e = e.Str("k", "v")
	}
	if c[1] {
		e = e.Str("k", "v")
	}
	if c[2] {
		e = e.Str("k", "v")
	}
	if c[3] {
		e = e.Str("k", "v")
	}
	if c[4] {
		e = e.Str("k", "v")
	}
	if c[5] {
		e = e.Str("k", "v")
	}
	if c[6] {
		e = e.Str("k", "v")
	}
	if c[7] {
		e = e.Str("k", "v")
	}
	if c[8] {
		e = e.Str("k", "v")
	}
	if c[9] {
		e = e.Str("k", "v")
	}
	if c[10] {
		e = e.Str("k", "v")
	}
	if c[11] {
		e = e.Str("k", "v")
	}
	if c[12] {
		e = e.Str("k", "v")
	}
	if c[13] {
		e = e.Str("k", "v")
	}
	if c[14] {
		e = e.Str("k", "v")
	}
	if c[15] {
		e = e.Str("k", "v")
	}
	if c[16] {
		e = e.Str("k", "v")
	}
	if c[17] {
		e = e.Str("k", "v")
	}
	if c[18] {
		e = e.Str("k", "v")
	}
	if c[19] {
		e = e.Str("k", "v")
	}
	if c[20] {
		e = e.Str("k", "v")
	}
	if c[21] {
		e = e.Str("k", "v")
	}
	if c[22] {
		e = e.Str("k", "v")
	}
	if c[23] {
		e = e.Str("k", "v")
	}
	if c[24] {
		e = e.Str("k", "v")
	}
	if c[25] {
		e = e.Str("k", "v")
	}
	if c[26] {
		e = e.Str("k", "v")
	}
	if c[27] {
		e = e.Str("k", "v")
	}
	if c[28] {
		e = e.Str("k", "v")
	}
	if c[29] {
		e = e.Str("k", "v")
	}
	if c[30] {
		e = e.Str("k", "v")
	}
	if c[31] {
		e = e.Str("k", "v")
	}
	if c[32] {
		e = e.Str("k", "v")
	}
	if c[33] {
		e = e.Str("k", "v")
	}
	if c[34] {
		e = e.Str("k", "v")
	}
	if c[35] {
		e = e.Str("k", "v")
	}
	if c[36] {
		e = e.Str("k", "v")
	}
	if c[37] {
		e = e.Str("k", "v")
	}
	if c[38] {
		e = e.Str("k", "v")
	}
	if c[39] {
		e = e.Str("k", "v")
	}
	if c[40] {
		e = e.Str("k", "v")
	}
	if c[41] {
		e = e.Str("k", "v")
	}
	if c[42] {
		e = e.Str("k", "v")
	}
	if c[43] {
		e = e.Str("k", "v")
	}
	if c[44] {
		e = e.Str("k", "v")
	}
	if c[45] {
		e = e.Str("k", "v")
	}
	if c[46] {
		e = e.Str("k", "v")
	}
	if c[47] {
		e = e.Str("k", "v")
	}
	if c[48] {
		e = e.Str("k", "v")
	}
	if c[49] {
		e = e.Str("k", "v")
	}
	if c[50] {
		e = e.Str("k", "v")
	}
	if c[51] {
		e = e.Str("k", "v")
	}
	if c[52] {
		e = e.Str("k", "v")
	}
	if c[53] {
		e = e.Str("k", "v")
	}
	if c[54] {
		e = e.Str("k", "v")
	}
	if c[55] {
		e = e.Str("k", "v")
	}
	if c[56] {
		e = e.Str("k", "v")
	}
	if c[57] {
		e = e.Str("k", "v")
	}
	if c[58] {
		e = e.Str("k", "v")
	}
	if c[59] {
		e = e.Str("k", "v")
	}
	if c[60] {
		e = e.Str("k", "v")
	}
	if c[61] {
		e = e.Str("k", "v")
	}
	if c[62] {
		e = e.Str("k", "v")
	}
	if c[63] {
		e = e.Str("k", "v")
	}
	e.Msg("many branches") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badManyBranchesOneWithoutCtx(ctx context.Context, logger zerolog.Logger, c []bool) {
	e := logger.Info().Ctx(ctx)
	if c[0] {
		e = e.Str("k", "v")
	}
	if c[1] {
		e = e.Str("k", "v")
	}
	if c[2] {
		e = e.Str("k", "v")
	}
	if c[3] {
		e = e.Str("k", "v")
	}
	if c[4] {
		e = e.Str("k", "v")
	}
	if c[5] {
		e = e.Str("k", "v")
	}
	if c[6] {
		e = e.Str("k", "v")
	}
	if c[7] {
		e = e.Str("k", "v")
	}
	if c[8] {
		e = e.Str("k", "v")
	}
	if c[9] {
		e = e.Str("k", "v")
	}
	if c[10] {
		e = e.Str("k", "v")
	}
	if c[11] {
		e = e.Str("k", "v")
	}
	if c[12] {
		e = e.Str("k", "v")
	}
	if c[13] {
		e = e.Str("k", "v")
	}
	if c[14] {
		e = e.Str("k", "v")
	}
	if c[15] {
		e = e.Str("k", "v")
	}
	if c[16] {
		e = e.Str("k", "v")
	}
	if c[17] {
		e = e.Str("k", "v")
	}
	if c[18] {
		e = e.Str("k", "v")
	}
	if c[19] {
		e = e.Str("k", "v")
	}
	if c[20] {
		e = e.Str("k", "v")
	}
	if c[21] {
		e = e.Str("k", "v")
	}
	if c[22] {
		e = e.Str("k", "v")
	}
	if c[23] {
		e = e.Str("k", "v")
	}
	if c[24] {
		e = e.Str("k", "v")
	}
	if c[25] {
		e = e.Str("k", "v")
	}
	if c[26] {
		e = e.Str("k", "v")
	}
	if c[27] {
		e = e.Str("k", "v")
	}
	if c[28] {
		e = e.Str("k", "v")
	}
	if c[29] {
		e = e.Str("k", "v")
	}
	if c[30] {
		e = e.Str("k", "v")
	}
	if c[31] {
		e = e.Str("k", "v")
	}
	if c[32] {
		e = e.Str("k", "v")
	}
	if c[33] {
		e = e.Str("k", "v")
	}
	if c[34] {
		e = e.Str("k", "v")
	}
	if c[35] {
		e = e.Str("k", "v")
	}
	if c[36] {
		e = e.Str("k", "v")
	}
	if c[37] {
		e = e.Str("k", "v")
	}
	if c[38] {
		e = e.Str("k", "v")
	}
	if c[39] {
		e = e.Str("k", "v")
	}
	if c[40] {
		e = e.Str("k", "v")
	}
	if c[41] {
		e = e.Str("k", "v")
	}
	if c[42] {
		e = e.Str("k", "v")
	}
	if c[43] {
		e = e.Str("k", "v")
	}
	if c[44] {
		e = e.Str("k", "v")
	}
	if c[45] {
		e = e.Str("k", "v")
	}
	if c[46] {
		e = e.Str("k", "v")
	}
	if c[47] {
		e = e.Str("k", "v")
	}
	if c[48] {
		e = e.Str("k", "v")
	}
	if c[49] {
		e = e.Str("k", "v")
	}
	if c[50] {
		e = e.Str("k", "v")
	}
	if c[51] {
		e = e.Str("k", "v")
	}
	if c[52] {
		e = e.Str("k", "v")
	}
	if c[53] {
		e = e.Str("k", "v")
	}
	if c[54] {
		e = e.Str("k", "v")
	}
	if c[55] {
		e = e.Str("k", "v")
	}
	if c[56] {
		e = e.Str("k", "v")
	}
	if c[57] {
		e = e.Str("k", "v")
	}
	if c[58] {
		e = e.Str("k", "v")
	}
	if c[59] {
		e = e.Str("k", "v")
	}
	if c[60] {
		e = e.Str("k", "v")
	}
	if c[61] {
		e = e.Str("k", "v")
	}
	if c[62] {
		e = e.Str("k", "v")
	}
	if c[63] {
		e = e.Str("k", "v")
	}
	if c[64] {
		e = logger.Warn()
	}
	if c[0] {
		e = e.Str("k", "v")
	}
	if c[1] {
		e = e.Str("k", "v")
	}
	if c[2] {
		e = e.Str("k", "v")
	}
	if c[3] {
		e = e.Str("k", "v")
	}
	if c[4] {
		e = e.Str("k", "v")
	}
	if c[5] {
		e = e.Str("k", "v")
	}
	if c[6] {
		e = e.Str("k", "v")
	}
	if c[7] {
		e = e.Str("k", "v")
	}
	if c[8] {
		e = e.Str("k", "v")
	}
	if c[9] {
		e = e.Str("k", "v")
	}
	if c[10] {
		e = e.Str("k", "v")
	}
	if c[11] {
		e = e.Str("k", "v")
	}
	if c[12] {
		e = e.Str("k", "v")
	}
	if c[13] {
		e = e.Str("k", "v")
	}
	if c[14] {
		e = e.Str("k", "v")
	}
	if c[15] {
		e = e.Str("k", "v")
	}
	if c[16] {
		e = e.Str("k", "v")
	}
	if c[17] {
		e = e.Str("k", "v")
	}
	if c[18] {
		e = e.Str("k", "v")
	}
	if c[19] {
		e = e.Str("k", "v")
	}
	if c[20] {
		e = e.Str("k", "v")
	}
	if c[21] {
		e = e.Str("k", "v")
	}
	if c[22] {
		e = e.Str("k", "v")
	}
	if c[23] {
		e = e.Str("k", "v")
	}
	if c[24] {
		e = e.Str("k", "v")
	}
	if c[25] {
		e = e.Str("k", "v")
	}
	if c[26] {
		e = e.Str("k", "v")
	}
	if c[27] {
		e = e.Str("k", "v")
	}
	if c[28] {
		e = e.Str("k", "v")
	}
	if c[29] {
		e = e.Str("k", "v")
	}
	if c[30] {
		e = e.Str("k", "v")
	}
	if c[31] {
		e = e.Str("k", "v")
	}
	if c[32] {
		e = e.Str("k", "v")
	}
	if c[33] {
		e = e.Str("k", "v")
	}
	if c[34] {
		e = e.Str("k", "v")
	}
	if c[35] {
		e = e.Str("k", "v")
	}
	if c[36] {
		e = e.Str("k", "v")
	}
	if c[37] {
		e = e.Str("k", "v")
	}
	if c[38] {
		e = e.Str("k", "v")
	}
	if c[39] {
		e = e.Str("k", "v")
	}
	if c[40] {
		e = e.Str("k", "v")
	}
	if c[41] {
		e = e.Str("k", "v")
	}
	if c[42] {
		e = e.Str("k", "v")
	}
	if c[43] {
		e = e.Str("k", "v")
	}
	if c[44] {
		e = e.Str("k", "v")
	}
	if c[45] {
		e = e.Str("k", "v")
	}
	if c[46] {
		e = e.Str("k", "v")
	}
	if c[47] {
		e = e.Str("k", "v")
	}
	if c[48] {
		e = e.Str("k", "v")
	}
	if c[49] {
		e = e.Str("k", "v")
	}
	if c[50] {
		e = e.Str("k", "v")
	}
	if c[51] {
		e = e.Str("k", "v")
	}
	if c[52] {
		e = e.Str("k", "v")
	}
	if c[53] {
		e = e.Str("k", "v")
	}
	if c[54] {
		e = e.Str("k", "v")
	}
	if c[55] {
		e = e.Str("k", "v")
	}
	if c[56] {
		e = e.Str("k", "v")
	}
	if c[57] {
		e = e.Str("k", "v")
	}
	if c[58] {
		e = e.Str("k", "v")
	}
	if c[59] {
		e = e.Str("k", "v")
	}
	if c[60] {
		e = e.Str("k", "v")
	}
	if c[61] {
		e = e.Str("k", "v")
	}
	if c[62] {
		e = e.Str("k", "v")
	}
	if c[63] {
		e = e.Str("k", "v")
	}
	e.Msg("many branches, one without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== BRANCHES ON CONSTANTS =====

// The edge of a Phi from a branch that never runs is not taken (#266).

const (
	phiDebug   = true
	phiVerbose = false
)

func goodReassignedUnderIfTrue(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	if true {
		e = e.Ctx(ctx)
	}
	e.Msg("reassigned under if true") // OK - the branch always runs
}

func badReassignedUnderIfFalse(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	if false {
		e = e.Ctx(ctx)
	}
	e.Msg("reassigned under if false") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodReassignedUnderTrueConstant(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	if phiDebug {
		e = e.Ctx(ctx)
	}
	e.Msg("reassigned under a true constant") // OK - the branch always runs
}

func badReassignedUnderFalseConstant(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	if phiVerbose {
		e = e.Ctx(ctx)
	}
	e.Msg("reassigned under a false constant") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodReassignedInElseOfFalseConstant(ctx context.Context, logger zerolog.Logger) {
	var e *zerolog.Event
	if phiVerbose {
		e = logger.Debug()
	} else {
		e = logger.Info().Ctx(ctx)
	}
	e.Msg("reassigned in the else of a false constant") // OK - the else always runs
}

func badReassignedInElseOfTrueConstant(ctx context.Context, logger zerolog.Logger) {
	var e *zerolog.Event
	if phiDebug {
		e = logger.Debug()
	} else {
		e = logger.Info().Ctx(ctx)
	}
	e.Msg("reassigned in the else of a true constant") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodLoggerReassignedUnderConstantInLoop(ctx context.Context, logger zerolog.Logger, xs []int) {
	for range xs {
		l := logger
		if phiDebug {
			l = l.With().Ctx(ctx).Logger()
		}
		l.Info().Msg("reassigned under a constant in a loop") // OK - the branch always runs
	}
}

func goodReassignedUnderConstantInClosure(ctx context.Context, logger zerolog.Logger) {
	func() {
		e := logger.Info()
		if !phiVerbose {
			e = e.Ctx(ctx)
		}
		e.Msg("reassigned under a constant in a closure") // OK - the branch always runs
	}()
}

func goodPointerPickedUnderTrueConstant(ctx context.Context, logger zerolog.Logger) {
	a := logger
	b := logger.With().Ctx(ctx).Logger()
	p := &a
	if phiDebug {
		p = &b
	}
	p.Info().Msg("pointer picked under a true constant") // OK - p is &b
}

func badPointerPickedUnderFalseConstant(ctx context.Context, logger zerolog.Logger) {
	a := logger
	b := logger.With().Ctx(ctx).Logger()
	p := &a
	if phiVerbose {
		p = &b
	}
	p.Info().Msg("pointer picked under a false constant") // want `zerolog call chain missing .Ctx\(ctx\)`
}

type phiMode int

const (
	phiModeOff phiMode = iota
	phiModeOn
)

const phiMode0 = phiModeOn

func goodLoggerReassignedInSwitchOnConstantTag(ctx context.Context, logger zerolog.Logger) {
	l := logger
	switch phiMode0 {
	case phiModeOn:
		l = logger.With().Ctx(ctx).Logger()
	default:
	}
	l.Info().Msg("reassigned in a switch on a constant tag") // OK - the empty default never runs
}

func badLoggerReassignedInSwitchOnConstantTagNoMatch(ctx context.Context, logger zerolog.Logger) {
	l := logger
	switch phiMode0 {
	case phiModeOff:
		l = logger.With().Ctx(ctx).Logger()
	default:
	}
	l.Info().Msg("reassigned in a case that never matches") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodReassignedInSwitchTrueOnConstant(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	switch true {
	case phiDebug:
		e = e.Ctx(ctx)
	}
	e.Msg("reassigned in switch true on a constant") // OK - the case always matches
}

func badReassignedUnderCondition(ctx context.Context, logger zerolog.Logger, cond bool) {
	e := logger.Info()
	if cond {
		e = e.Ctx(ctx)
	}
	e.Msg("reassigned under a condition") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== EARLY RETURN PATTERNS =====

func badEarlyReturn(ctx context.Context, logger zerolog.Logger, shouldReturn bool) {
	e := logger.Info()
	if shouldReturn {
		e.Msg("early return") // want `zerolog call chain missing .Ctx\(ctx\)`
		return
	}
	e.Str("continued", "true").Msg("continued") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEarlyReturnWithCtx(ctx context.Context, logger zerolog.Logger, shouldReturn bool) {
	e := logger.Info().Ctx(ctx)
	if shouldReturn {
		e.Msg("early return with ctx") // OK
		return
	}
	e.Str("continued", "true").Msg("continued with ctx") // OK
}

// ===== LABELED STATEMENTS =====

func badLabeledLoop(ctx context.Context, logger zerolog.Logger) {
outer:
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if j == 1 {
				logger.Info().Int("i", i).Int("j", j).Msg("break outer") // want `zerolog call chain missing .Ctx\(ctx\)`
				break outer
			}
		}
	}
}

// ===== A FIELD SELECTED FROM STRUCT VALUES PICKED ON A BRANCH =====

type phiLoggerWrapper struct{ *zerolog.Logger }

type phiEventWrapper struct{ *zerolog.Event }

type phiEventEmbedded struct{ *zerolog.Event }

type phiEventPtrWrapper struct{ *phiEventEmbedded }

type phiArrayHolder struct{ ls [1]*zerolog.Logger }

func goodPhiLoggerWrapper(ctx context.Context, base zerolog.Logger, cond bool) {
	a := base.With().Ctx(ctx).Logger()
	b := zerolog.Ctx(ctx).With().Str("k", "v").Logger()
	w := phiLoggerWrapper{&a}
	if cond {
		w = phiLoggerWrapper{&b}
	}
	w.Info().Msg("wrapper picked on a branch") // OK - both have ctx
}

func goodPhiLoggerWrapperSwitch(ctx context.Context, base zerolog.Logger, n int) {
	a := base.With().Ctx(ctx).Logger()
	b := zerolog.Ctx(ctx).With().Logger()
	var w phiLoggerWrapper
	switch n {
	case 0:
		w = phiLoggerWrapper{&a}
	default:
		w = phiLoggerWrapper{&b}
	}
	w.Info().Msg("wrapper picked in a switch") // OK
}

func goodPhiLoggerWrapperLoop(ctx context.Context, base zerolog.Logger, n int) {
	a := base.With().Ctx(ctx).Logger()
	w := phiLoggerWrapper{&a}
	for range n {
		w = phiLoggerWrapper{&a}
	}
	w.Info().Msg("wrapper updated in a loop") // OK
}

func goodPhiArrayElement(ctx context.Context, base zerolog.Logger, cond bool) {
	a := base.With().Ctx(ctx).Logger()
	b := zerolog.Ctx(ctx).With().Logger()
	h := phiArrayHolder{[1]*zerolog.Logger{&a}}
	if cond {
		h = phiArrayHolder{[1]*zerolog.Logger{&b}}
	}
	h.ls[0].Info().Msg("element picked on a branch") // OK
}

func goodPhiEventWrapper(ctx context.Context, logger zerolog.Logger, cond bool) {
	w := phiEventWrapper{logger.Info().Ctx(ctx)}
	if cond {
		w = phiEventWrapper{logger.Warn().Ctx(ctx)}
	}
	w.Msg("event wrapper picked on a branch") // OK
}

func goodPhiEventWrapperInPlace(ctx context.Context, logger zerolog.Logger, cond bool) {
	w := phiEventWrapper{logger.Info()}
	if cond {
		w = phiEventWrapper{logger.Warn()}
	}
	w.Ctx(ctx)
	w.Msg("event wrapper, Ctx in place") // OK
}

func goodPhiEventPtrWrapper(ctx context.Context, logger zerolog.Logger, cond bool) {
	w := phiEventPtrWrapper{&phiEventEmbedded{logger.Info().Ctx(ctx)}}
	if cond {
		w = phiEventPtrWrapper{&phiEventEmbedded{logger.Warn().Ctx(ctx)}}
	}
	w.Msg("embedded pointer picked on a branch") // OK
}

func goodPhiEventPtrWrapperInPlace(ctx context.Context, logger zerolog.Logger, cond bool) {
	a := &phiEventEmbedded{logger.Info()}
	b := &phiEventEmbedded{logger.Warn()}
	w := phiEventPtrWrapper{a}
	if cond {
		w = phiEventPtrWrapper{b}
	}
	w.Ctx(ctx)
	w.Msg("embedded pointer, Ctx in place") // OK
}

func badPhiLoggerWrapperOneBranch(ctx context.Context, base zerolog.Logger, cond bool) {
	a := base.With().Ctx(ctx).Logger()
	w := phiLoggerWrapper{&a}
	if cond {
		w = phiLoggerWrapper{&base}
	}
	w.Info().Msg("one wrapper lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badPhiArrayElementOneBranch(ctx context.Context, base zerolog.Logger, cond bool) {
	a := base.With().Ctx(ctx).Logger()
	h := phiArrayHolder{[1]*zerolog.Logger{&a}}
	if cond {
		h = phiArrayHolder{[1]*zerolog.Logger{&base}}
	}
	h.ls[0].Info().Msg("one element lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badPhiEventWrapperOneBranch(ctx context.Context, logger zerolog.Logger, cond bool) {
	w := phiEventWrapper{logger.Info().Ctx(ctx)}
	if cond {
		w = phiEventWrapper{logger.Warn()}
	}
	w.Msg("one event wrapper lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badPhiEventWrapperInPlaceOneBranch(ctx context.Context, logger zerolog.Logger, cond bool) {
	w := phiEventWrapper{logger.Info()}
	if cond {
		w = phiEventWrapper{logger.Warn()}
	} else {
		w.Ctx(ctx)
	}
	w.Msg("Ctx in place on one branch only") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badPhiEventPtrWrapperOneBranch(ctx context.Context, logger zerolog.Logger, cond bool) {
	w := phiEventPtrWrapper{&phiEventEmbedded{logger.Info().Ctx(ctx)}}
	if cond {
		w = phiEventPtrWrapper{&phiEventEmbedded{logger.Warn()}}
	}
	w.Msg("one embedded pointer lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badPhiEventPtrWrapperInPlaceOther(ctx context.Context, logger zerolog.Logger, cond bool) {
	a := &phiEventEmbedded{logger.Info()}
	b := &phiEventEmbedded{logger.Warn()}
	w := phiEventPtrWrapper{a}
	if cond {
		w = phiEventPtrWrapper{b}
	}
	a.Ctx(ctx)
	w.Msg("Ctx in place on one of the events only") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func newPhiEventWrapper(logger zerolog.Logger) phiEventWrapper { return phiEventWrapper{logger.Info()} }

func newPhiEventWrapperPtr(logger zerolog.Logger) *phiEventWrapper {
	return &phiEventWrapper{logger.Info()}
}

func newPhiEventPtrWrapperPtr(logger zerolog.Logger) *phiEventPtrWrapper {
	return &phiEventPtrWrapper{&phiEventEmbedded{logger.Info()}}
}

func goodPhiEventWrapperInPlaceInLoop(ctx context.Context, logger zerolog.Logger, cond bool, n int) {
	for range n {
		w := phiEventWrapper{logger.Info()}
		if cond {
			w = phiEventWrapper{logger.Warn()}
		}
		w.Ctx(ctx)
		w.Msg("event wrapper in a loop, Ctx in place") // OK
	}
}

func badPhiEventWrapperInPlaceInLoopEarlier(ctx context.Context, logger zerolog.Logger, cond bool, n int) {
	var last phiEventWrapper
	for i := range n {
		w := phiEventWrapper{logger.Info()}
		if cond {
			w = phiEventWrapper{logger.Warn()}
		}
		if i > 0 {
			last.Ctx(ctx)
			w.Msg("Ctx in place on the event of the last iteration") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		last = w
	}
}

func badPhiEventWrapperFromCall(ctx context.Context, logger zerolog.Logger) {
	w := phiEventWrapper{logger.Info()}
	w.Ctx(ctx)
	newPhiEventWrapper(logger).Msg("another wrapper a call returns") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badPhiEventPtrWrapperOneFromCall(ctx context.Context, logger zerolog.Logger, cond bool) {
	w := *newPhiEventPtrWrapperPtr(logger)
	if cond {
		w = phiEventPtrWrapper{&phiEventEmbedded{logger.Warn().Ctx(ctx)}}
	}
	w.Msg("one wrapper a call returns") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// LIMITATION: a struct value loaded through a pointer a call returns is not
// followed, so each read of the event in it is an event of its own.
func limitationPhiEventWrapperLoadedFromCall(ctx context.Context, logger zerolog.Logger) {
	w := *newPhiEventWrapperPtr(logger)
	w.Ctx(ctx)
	w.Msg("wrapper loaded through a pointer a call returns") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventWrapperInPlaceInLoopEarlier(ctx context.Context, logger zerolog.Logger, n int) {
	var last phiEventWrapper
	for i := range n {
		w := phiEventWrapper{logger.Info()}
		if i > 0 {
			last.Ctx(ctx)
			w.Msg("Ctx in place on the event of the last iteration, no branch") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		last = w
	}
}

func goodEventWrapperInPlaceLoadedBeforeLoop(ctx context.Context, logger zerolog.Logger, n int) {
	w := phiEventWrapper{logger.Info()}
	w.Ctx(ctx)
	for range n {
		w.Str("k", "v")
	}
	w.Msg("Ctx in place before a loop") // OK
}
