// Package zerolog contains test fixtures for the zerolog context propagation checker.
// This file covers SSA-specific edge cases - patterns that stress-test the SSA-based
// variable tracking, including IIFE, Phi nodes, channels, closures, and struct fields.
// See basic.go for simple cases, evil.go for general edge cases,
// and evil_logger.go for Logger transformation patterns.
//
// KNOWN LIMITATIONS (search for "LIMITATION" or "limitation" to find test cases):
//
// False Negatives (should report but doesn't):
//   - IIFE/Helper returns: Cross-function tracking not supported
//   - Method values: `msg := e.Msg; msg("test")`
//   - Deep FreeVar: Triple-nested closures
//
// False Positives (reports when shouldn't):
//   - Channel send/receive: Can't trace through channels
//   - sync.Pool: Can't trace through Get/Put
//   - Embedded struct: `h.Msg()` where h embeds *Event
//   - Closure-modified capture: Closure writes to outer var
//   - Phi node with nil: `var e; if cond { e = ... }; e.Msg()`
package zerolog

import (
	"context"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ===== IIFE (Immediately Invoked Function Expression) =====

func badIIFEReturnsEvent(ctx context.Context, logger zerolog.Logger) {
	// IIFE returns event, then terminates
	func() *zerolog.Event {
		return logger.Info()
	}().Msg("iife") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badIIFEReturnsEventChained(ctx context.Context, logger zerolog.Logger) {
	// IIFE returns event with fields, then terminates
	func() *zerolog.Event {
		return logger.Info().Str("from", "iife")
	}().Str("key", "value").Msg("chained iife") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func limitationIIFEReturnsEventWithCtx(ctx context.Context, logger zerolog.Logger) {
	// LIMITATION: SSA doesn't track .Ctx() through IIFE returns
	// This is a known limitation - cross-function tracking is not supported
	func() *zerolog.Event {
		return logger.Info().Ctx(ctx)
	}().Msg("iife with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badIIFENestedReturnsEvent(ctx context.Context, logger zerolog.Logger) {
	// Nested IIFE
	func() *zerolog.Event {
		return func() *zerolog.Event {
			return logger.Info()
		}()
	}().Msg("nested iife") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== HELPER FUNCTIONS RETURNING EVENT =====

func badHelperReturnsEvent(ctx context.Context, logger zerolog.Logger) {
	e := createEvent(logger)
	e.Msg("from helper") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badHelperChainedImmediately(ctx context.Context, logger zerolog.Logger) {
	createEvent(logger).Str("key", "val").Msg("immediate chain") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func limitationHelperWithCtx(ctx context.Context, logger zerolog.Logger) {
	// LIMITATION: SSA doesn't track .Ctx() through helper function returns
	e := createEventWithCtx(ctx, logger)
	e.Msg("helper with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func createEvent(logger zerolog.Logger) *zerolog.Event {
	return logger.Info().Str("created", "helper")
}

func createEventWithCtx(ctx context.Context, logger zerolog.Logger) *zerolog.Event {
	return logger.Info().Ctx(ctx).Str("created", "helper")
}

// ===== MULTIPLE FUNCTION HOPS =====

func badMultipleHops(ctx context.Context, logger zerolog.Logger) {
	e := hop1(logger)
	e.Msg("multi hop") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func hop1(logger zerolog.Logger) *zerolog.Event {
	return hop2(logger)
}

func hop2(logger zerolog.Logger) *zerolog.Event {
	return hop3(logger)
}

func hop3(logger zerolog.Logger) *zerolog.Event {
	return logger.Info().Str("hop", "3")
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

// ===== CLOSURE CAPTURING AND DEFERRED USE =====

func badClosureCapture(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	fn := func() {
		e.Msg("captured") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	fn()
}

func badClosureCaptureDeep(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	fn := func() {
		inner := func() {
			e.Msg("deep capture") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
		inner()
	}
	fn()
}

func limitationClosureCaptureWithCtx(ctx context.Context, logger zerolog.Logger) {
	// LIMITATION: SSA doesn't track .Ctx() on captured variables used in closures
	e := logger.Info().Ctx(ctx)
	fn := func() {
		e.Msg("captured with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
	fn()
}

// ===== METHOD CHAINING WITH INTERMEDIATE VARIABLES =====

func badPingPongVariables(ctx context.Context, logger zerolog.Logger) {
	a := logger.Info()
	b := a.Str("key1", "val1")
	c := b.Str("key2", "val2")
	d := c.Int("num", 42)
	d.Msg("ping pong") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badPingPongWithReassignment(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	e = e.Str("key1", "val1")
	e = e.Str("key2", "val2")
	e = e.Int("num", 42)
	e.Msg("reassigned chain") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodPingPongWithCtxInMiddle(ctx context.Context, logger zerolog.Logger) {
	a := logger.Info()
	b := a.Str("key1", "val1")
	c := b.Ctx(ctx) // ctx added in middle
	d := c.Int("num", 42)
	d.Msg("ctx in middle") // OK
}

// ===== STRUCT FIELDS =====

type eventHolder struct {
	event *zerolog.Event
}

func badEventInStruct(ctx context.Context, logger zerolog.Logger) {
	// Struct field access - SSA reports since it sees the field read but can't trace back to .Ctx()
	h := eventHolder{event: logger.Info()}
	h.event.Msg("from struct") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventInStructWithCtx(ctx context.Context, logger zerolog.Logger) {
	// Struct field access with ctx - SSA now tracks through Store/Load
	h := eventHolder{event: logger.Info().Ctx(ctx)}
	h.event.Msg("from struct with ctx") // OK - ctx is set
}

// ===== SLICE/MAP ACCESS =====

func badEventFromSlice(ctx context.Context, logger zerolog.Logger) {
	events := []*zerolog.Event{logger.Info(), logger.Warn()}
	events[0].Msg("from slice") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEventFromMap(ctx context.Context, logger zerolog.Logger) {
	events := map[string]*zerolog.Event{
		"info": logger.Info(),
	}
	events["info"].Msg("from map") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func limitationEventFromSliceWithCtx(ctx context.Context, logger zerolog.Logger) {
	// LIMITATION: SSA doesn't track events through slice/array access
	events := []*zerolog.Event{logger.Info().Ctx(ctx)}
	events[0].Msg("from slice with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== POINTER INDIRECTION =====

func badEventPointer(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	ptr := &e
	(*ptr).Msg("via pointer") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badDoublePointer(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	ptr := &e
	ptr2 := &ptr
	(**ptr2).Msg("double pointer") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== GLOBAL LOGGER EDGE CASES =====

func badGlobalIIFE(ctx context.Context) {
	func() *zerolog.Event {
		return log.Info()
	}().Msg("global iife") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badGlobalConditional(ctx context.Context, flag bool) {
	var e *zerolog.Event
	if flag {
		e = log.Info()
	} else {
		e = log.Logger.Warn()
	}
	e.Msg("global conditional") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodGlobalCtxImmediateChain(ctx context.Context) {
	log.Ctx(ctx).Info().Str("key", "val").Msg("global ctx chain") // OK
}

// ===== DEFERRED LOGGING =====

func badDeferredLog(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	defer e.Msg("deferred") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodDeferredLogWithCtx(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info().Ctx(ctx)
	defer e.Msg("deferred with ctx") // OK
}

// ===== LOOP-CREATED EVENTS =====

func badEventInLoop(ctx context.Context, logger zerolog.Logger) {
	for i := 0; i < 3; i++ {
		e := logger.Info().Int("i", i)
		e.Msg("loop iteration") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
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

// ===== TYPE ASSERTION CHAOS =====

func badEventViaInterface(ctx context.Context, logger zerolog.Logger) {
	var i interface{} = logger.Info()
	e := i.(*zerolog.Event)
	e.Msg("via interface") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEventViaInterfaceWithCtx(ctx context.Context, logger zerolog.Logger) {
	var i interface{} = logger.Info().Ctx(ctx)
	e := i.(*zerolog.Event)
	e.Msg("via interface with ctx") // OK - TypeAssert tracking
}

// ===== NAMED RETURN VALUES =====

func badNamedReturn(ctx context.Context, logger zerolog.Logger) {
	e := getNamedEvent(logger)
	e.Msg("named return") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func getNamedEvent(logger zerolog.Logger) (event *zerolog.Event) {
	event = logger.Info()
	return
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

// ===== VARIADIC SHENANIGANS =====

func badVariadicEvent(ctx context.Context, logger zerolog.Logger) {
	events := []*zerolog.Event{logger.Info(), logger.Warn()}
	processEvents(events...)
}

func processEvents(events ...*zerolog.Event) {
	for _, e := range events {
		e.Msg("variadic") // Can't track ctx here - no context in scope
	}
}

// ===== CHANNEL SEND/RECEIVE =====

func badEventFromChannel(ctx context.Context, logger zerolog.Logger) {
	ch := make(chan *zerolog.Event, 1)
	ch <- logger.Info()
	e := <-ch
	e.Msg("from channel") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// LIMITATION (false positive): Channel send/receive breaks value tracking.
// Even though ctx is present, the analyzer cannot trace through channel operations.
func limitationChannelWithCtx(ctx context.Context, logger zerolog.Logger) {
	ch := make(chan *zerolog.Event, 1)
	ch <- logger.Info().Ctx(ctx)
	e := <-ch
	e.Msg("from channel with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badSelectStatementChannel(ctx context.Context, logger zerolog.Logger) {
	ch1 := make(chan *zerolog.Event, 1)
	ch2 := make(chan *zerolog.Event, 1)
	ch1 <- logger.Info()
	ch2 <- logger.Warn()

	var e *zerolog.Event
	select {
	case e = <-ch1:
	case e = <-ch2:
	}
	e.Msg("from select") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== PANIC RECOVERY =====

func badPanicRecoveryEvent(ctx context.Context, logger zerolog.Logger) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error().Interface("panic", r).Msg("recovered") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
	}()
	panic("test")
}

func goodPanicRecoveryWithCtx(ctx context.Context, logger zerolog.Logger) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error().Ctx(ctx).Interface("panic", r).Msg("recovered with ctx") // OK
		}
	}()
	panic("test")
}

func goodPanicRecoveryEvent(ctx context.Context, logger zerolog.Logger) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error().Ctx(ctx).Interface("panic", r).Msg("recovered with ctx") // OK
		}
	}()
	panic("test")
}

// ===== MULTIPLE TERMINATORS IN SAME SCOPE =====

func badMultipleTerminators(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	e.Msg("first") // want `zerolog call chain missing .Ctx\(ctx\)`

	e2 := logger.Warn()
	e2.Msgf("second: %s", "test") // want `zerolog call chain missing .Ctx\(ctx\)`

	e3 := logger.Error()
	e3.Send() // want `zerolog call chain missing .Ctx\(ctx\)`
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

// ===== TRIPLE NESTED CLOSURES =====

func badTripleNestedClosure(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	func() {
		func() {
			func() {
				e.Msg("triple nested") // want `zerolog call chain missing .Ctx\(ctx\)`
			}()
		}()
	}()
}

func limitationTripleNestedClosureWithCtx(ctx context.Context, logger zerolog.Logger) {
	// LIMITATION: Deep FreeVar tracking through multiple closure levels
	e := logger.Info().Ctx(ctx)
	func() {
		func() {
			func() {
				e.Msg("triple nested with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
			}()
		}()
	}()
}

// ===== METHOD VALUE =====

// LIMITATION: Method values extract the function from the receiver, creating a different
// SSA pattern. The analyzer cannot trace through method value extraction.
func limitationMethodValue(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	msg := e.Msg
	msg("method value") // LIMITATION: should report but doesn't due to method value extraction
}

// ===== STRUCT WITH MULTIPLE EVENT FIELDS =====

type multiEventHolder struct {
	info  *zerolog.Event
	error *zerolog.Event
}

func badMultiEventStruct(ctx context.Context, logger zerolog.Logger) {
	h := multiEventHolder{
		info:  logger.Info(),
		error: logger.Error(),
	}
	h.info.Msg("info") // want `zerolog call chain missing .Ctx\(ctx\)`
	h.error.Msg("err") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodMultiEventStructWithCtx(ctx context.Context, logger zerolog.Logger) {
	h := multiEventHolder{
		info:  logger.Info().Ctx(ctx),
		error: logger.Error().Ctx(ctx),
	}
	h.info.Msg("info with ctx")  // OK
	h.error.Msg("err with ctx") // OK
}

// ===== ARRAY (NOT SLICE) =====

func badEventFromArray(ctx context.Context, logger zerolog.Logger) {
	events := [2]*zerolog.Event{logger.Info(), logger.Warn()}
	events[0].Msg("from array") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== RANGE OVER MAP =====

func badRangeOverMap(ctx context.Context, logger zerolog.Logger) {
	events := map[string]*zerolog.Event{
		"a": logger.Info(),
		"b": logger.Warn(),
	}
	for _, e := range events {
		e.Msg("range over map") // want `zerolog call chain missing .Ctx\(ctx\)`
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

// ===== COMPOSITE LITERAL WITH KEYED FIELDS =====

func badCompositeKeyedFields(ctx context.Context, logger zerolog.Logger) {
	type holder struct {
		e *zerolog.Event
	}
	h := holder{
		e: logger.Info(),
	}
	h.e.Msg("keyed field") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== ANONYMOUS STRUCT =====

func badAnonymousStruct(ctx context.Context, logger zerolog.Logger) {
	h := struct {
		e *zerolog.Event
	}{
		e: logger.Info(),
	}
	h.e.Msg("anonymous struct") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== DEFER WITH CLOSURE =====

func badDeferWithClosure(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	defer func() {
		e.Msg("defer closure") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

// ===== MsgFunc TERMINATOR =====

func badMsgFuncEdge(ctx context.Context, logger zerolog.Logger) {
	logger.Info().MsgFunc(func() string { return "msgfunc" }) // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodMsgFuncEdgeWithCtx(ctx context.Context, logger zerolog.Logger) {
	logger.Info().Ctx(ctx).MsgFunc(func() string { return "msgfunc" }) // OK
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

// ===== VARIADIC HELPER (Returns Event) =====

func variadicHelper(events ...*zerolog.Event) *zerolog.Event {
	if len(events) > 0 {
		return events[0]
	}
	return nil
}

// Cross-function tracking: Analyzer sees through function calls where the terminator is outside.
// This works because SSA tracks the Event through Extract instruction.
func badVariadicHelper(ctx context.Context, logger zerolog.Logger) {
	e := variadicHelper(logger.Info())
	if e != nil {
		e.Msg("from variadic") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

// ===== INTERFACE ASSIGNMENT =====

type eventGetter interface {
	GetEvent() *zerolog.Event
}

type eventGetterImpl struct {
	e *zerolog.Event
}

func (h *eventGetterImpl) GetEvent() *zerolog.Event {
	return h.e
}

// Interface method call: Still reports because the terminator is in a function with context.
func badInterfaceMethod(ctx context.Context, logger zerolog.Logger) {
	var h eventGetter = &eventGetterImpl{e: logger.Info()}
	e := h.GetEvent()
	e.Msg("from interface") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== NAMED RETURN =====

func namedReturn(logger zerolog.Logger) (e *zerolog.Event) {
	e = logger.Info()
	return
}

// Named return: Still reports because the terminator is in a function with context.
func badNamedReturnHelper(ctx context.Context, logger zerolog.Logger) {
	e := namedReturn(logger)
	e.Msg("from named return") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== BLANK IDENTIFIER =====

func badBlankIdentifier(ctx context.Context, logger zerolog.Logger) {
	_ = logger.Info().Ctx(ctx)
	logger.Warn().Msg("not the one with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== REASSIGNMENT SHADOWS =====

func badReassignmentShadows(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info().Ctx(ctx)
	_ = e // use e
	e = logger.Warn()
	e.Msg("shadowed event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== MULTI-VALUE RETURN =====

func multiReturn(logger zerolog.Logger) (*zerolog.Event, error) {
	return logger.Info(), nil
}

// Multi-value return: Still reports because the terminator is in a function with context.
func badMultiReturn(ctx context.Context, logger zerolog.Logger) {
	e, _ := multiReturn(logger)
	e.Msg("from multi return") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== EMBEDDED STRUCT =====

type embeddedHolder struct {
	*zerolog.Event
}

func badEmbeddedStruct(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolder{Event: logger.Info()}
	h.Msg("embedded") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// LIMITATION (false positive): Embedded struct field promotion.
// Can't trace through promoted method calls even though ctx is present.
func limitationEmbeddedStructWithCtx(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolder{Event: logger.Info().Ctx(ctx)}
	h.Msg("embedded with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== POINTER RECEIVER ON STRUCT =====

type ptrHolder struct {
	e *zerolog.Event
}

func (h *ptrHolder) log() {
	h.e.Msg("ptr holder")
}

// LIMITATION: Method call on struct requires cross-function tracking
func limitationPtrReceiverMethod(ctx context.Context, logger zerolog.Logger) {
	h := &ptrHolder{e: logger.Info()}
	h.log() // LIMITATION: should report inside h.log() but doesn't
}

// ===== CLOSURE THAT MODIFIES CAPTURED VAR =====

func badClosureModifiesCaptured(ctx context.Context, logger zerolog.Logger) {
	var e *zerolog.Event
	f := func() {
		e = logger.Info()
	}
	f()
	e.Msg("modified by closure") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// LIMITATION (false positive): Closure-modified captured variable.
// Can't trace through closure that writes to captured var even though ctx is present.
func limitationClosureModifiesCapturedWithCtx(ctx context.Context, logger zerolog.Logger) {
	var e *zerolog.Event
	f := func() {
		e = logger.Info().Ctx(ctx)
	}
	f()
	e.Msg("modified by closure with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== SYNC.POOL =====

// LIMITATION (false positive): sync.Pool Get/Put creates opaque value flow.
// Even though ctx is present in the New function, analyzer can't trace through pool.
func limitationSyncPool(ctx context.Context, logger zerolog.Logger) {
	pool := &sync.Pool{
		New: func() interface{} {
			return logger.Info().Ctx(ctx)
		},
	}
	e := pool.Get().(*zerolog.Event)
	e.Msg("from pool") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== CONTEXT FROM STRUCT FIELD =====

type loggerHolder struct {
	ctx    context.Context
	logger zerolog.Logger
}

// Note: This method has no ctx parameter, so analyzer won't report.
// The ctx field in the struct is not tracked as a context source.
func (h *loggerHolder) logBad() {
	h.logger.Info().Msg("struct method") // No ctx param - not reported
}

func (h *loggerHolder) logGood() {
	h.logger.Info().Ctx(h.ctx).Msg("struct method with ctx") // OK
}

func badStructMethodCall(ctx context.Context, logger zerolog.Logger) {
	h := &loggerHolder{ctx: ctx, logger: logger}
	h.logBad() // The error is inside logBad, not here
}

func goodStructMethodCall(ctx context.Context, logger zerolog.Logger) {
	h := &loggerHolder{ctx: ctx, logger: logger}
	h.logGood() // OK - context used inside
}

// ===== DEFER WITH NAMED RETURN =====

func badDeferNamedReturn(ctx context.Context, logger zerolog.Logger) (err error) {
	defer func() {
		if err != nil {
			logger.Error().Err(err).Msg("failed") // want `zerolog call chain missing .Ctx\(ctx\)`
		}
	}()
	return nil
}

func goodDeferNamedReturnWithCtx(ctx context.Context, logger zerolog.Logger) (err error) {
	defer func() {
		if err != nil {
			logger.Error().Ctx(ctx).Err(err).Msg("failed with ctx") // OK
		}
	}()
	return nil
}

// ===== EARLY RETURN PATTERN =====

func badEarlyReturnPartial(ctx context.Context, logger zerolog.Logger, ok bool) {
	if !ok {
		logger.Error().Msg("early return") // want `zerolog call chain missing .Ctx\(ctx\)`
		return
	}
	logger.Info().Ctx(ctx).Msg("normal path") // OK
}

// ===== MULTIPLE EVENTS IN SAME LINE =====

func badMultipleEventsOneLine(ctx context.Context, logger zerolog.Logger, cond bool) {
	if cond {
		logger.Info().Msg("a") // want `zerolog call chain missing .Ctx\(ctx\)`
	} else {
		logger.Warn().Msg("b") // want `zerolog call chain missing .Ctx\(ctx\)`
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

// LIMITATION (false positive): Phi node sees potential nil from else branch.
// Even with `if true`, SSA still models an else path where e is nil (zero value).
func limitationNilCheckWithCtx(ctx context.Context, logger zerolog.Logger) {
	var e *zerolog.Event
	if true {
		e = logger.Info().Ctx(ctx)
	}
	if e != nil {
		e.Msg("nil checked with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

// ===== FUNCTION LITERAL AS ARGUMENT =====

func badFuncLiteralArg(ctx context.Context, logger zerolog.Logger) {
	doSomething(func() {
		logger.Info().Msg("in func literal arg") // want `zerolog call chain missing .Ctx\(ctx\)`
	})
}

func goodFuncLiteralArgWithCtx(ctx context.Context, logger zerolog.Logger) {
	doSomething(func() {
		logger.Info().Ctx(ctx).Msg("in func literal arg with ctx") // OK
	})
}

func doSomething(f func()) {
	f()
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

// ===== DISCARD EVENT =====

func badDiscardEvent(ctx context.Context, logger zerolog.Logger) {
	e := logger.Info()
	_ = e.Str("key", "value") // Discard result
	// But e itself doesn't have the Str call applied (it's immutable-ish but returns new)
	e.Msg("discarded chain") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== LOGGER FROM CONTEXT =====

func goodLoggerFromContext(ctx context.Context) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Msg("from context") // OK - logger came from ctx
}

func badLoggerFromContextThenNew(ctx context.Context, logger zerolog.Logger) {
	_ = zerolog.Ctx(ctx) // Get but don't use
	logger.Info().Msg("ignored ctx logger") // want `zerolog call chain missing .Ctx\(ctx\)`
}
