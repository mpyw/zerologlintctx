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
