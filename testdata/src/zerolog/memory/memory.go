// Package memory covers loggers and events held in memory: struct fields, embedded
// fields, pointers, arrays, slices, maps, channels and sync.Pool.
package memory

import (
	"context"
	"io"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

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

func limitationEventFromMapWithCtx(ctx context.Context, logger zerolog.Logger) {
	// LIMITATION: SSA doesn't track events through map lookups
	events := map[string]*zerolog.Event{"info": logger.Info().Ctx(ctx)}
	events["info"].Msg("from map with ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
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

// ===== STRUCT COPIED IN A LOOP =====

// A struct copied back and forth in a loop leads back to a copy already
// followed (#133).

type walkState struct {
	log   zerolog.Logger
	depth int
}

func goodStructSavedAndRestoredInLoop(ctx context.Context, logger zerolog.Logger, nodes []string) {
	st := walkState{log: logger.With().Ctx(ctx).Logger()}
	for _, n := range nodes {
		saved := st
		st.depth++
		st.log.Debug().Str("node", n).Msg("saved and restored") // OK - every copy has ctx
		st = saved
	}
}

func goodStructCopiedBothWaysInLoop(ctx context.Context, logger zerolog.Logger, n int) {
	a := walkState{log: logger.With().Ctx(ctx).Logger()}
	b := a
	for range n {
		b = a
		a = b
		a.log.Info().Msg("copied both ways") // OK - every copy has ctx
	}
}

func badStructsSwappedInLoop(ctx context.Context, logger zerolog.Logger, n int) {
	a := walkState{log: logger}
	b := walkState{log: logger.With().Ctx(ctx).Logger()}
	for range n {
		a, b = b, a
		a.log.Info().Msg("swapped") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
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
	h.info.Msg("info with ctx") // OK
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

// ===== EMBEDDED STRUCT =====

type embeddedHolder struct {
	*zerolog.Event
}

func badEmbeddedStruct(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolder{Event: logger.Info()}
	h.Msg("embedded") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A promoted method reads the embedded field by value, from the struct loaded
// whole (#138).
func goodEmbeddedStructWithCtx(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolder{Event: logger.Info().Ctx(ctx)}
	h.Msg("embedded with ctx") // OK - the embedded event has ctx
}

// A method expression of a promoted method takes the struct that embeds the
// event, and reads the event from it (#195).
func goodEmbeddedStructMethodExpression(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolder{logger.Info().Ctx(ctx)}
	embeddedHolder.Msg(h, "method expression via embedding") // OK - the embedded event has ctx
}

func badEmbeddedStructMethodExpression(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolder{logger.Info()}
	embeddedHolder.Msg(h, "method expression via embedding") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEmbeddedStructPointerMethodExpression(ctx context.Context, logger zerolog.Logger) {
	h := &embeddedHolder{logger.Info().Ctx(ctx)}
	(*embeddedHolder).Msg(h, "pointer method expression via embedding") // OK - the embedded event has ctx
}

func badEmbeddedStructPointerMethodExpression(ctx context.Context, logger zerolog.Logger) {
	h := &embeddedHolder{logger.Info()}
	(*embeddedHolder).Msg(h, "pointer method expression via embedding") // want `zerolog call chain missing .Ctx\(ctx\)`
}

type embeddedHolderOfHolder struct {
	embeddedHolder
}

func goodNestedEmbeddedStructMethodExpression(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolderOfHolder{embeddedHolder{logger.Info().Ctx(ctx)}}
	embeddedHolderOfHolder.Msg(h, "nested method expression via embedding") // OK - the embedded event has ctx
}

func badNestedEmbeddedStructMethodExpression(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolderOfHolder{embeddedHolder{logger.Info()}}
	embeddedHolderOfHolder.Msg(h, "nested method expression via embedding") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// The embedding may go through a pointer partway. The pointer stored there
// is followed to the struct it points to (#204).

type embeddedHolderPointerOfHolder struct {
	*embeddedHolder
}

func goodNestedEmbeddedPointerMethodExpression(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolderPointerOfHolder{&embeddedHolder{logger.Info().Ctx(ctx)}}
	embeddedHolderPointerOfHolder.Msg(h, "method expression through a pointer partway") // OK - the embedded event has ctx
}

func goodNestedEmbeddedPointerVariableMethodExpression(ctx context.Context, logger zerolog.Logger) {
	inner := embeddedHolder{logger.Info().Ctx(ctx)}
	h := embeddedHolderPointerOfHolder{&inner}
	embeddedHolderPointerOfHolder.Msg(h, "method expression through a pointer to a variable") // OK - the embedded event has ctx
}

func badNestedEmbeddedPointerMethodExpression(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolderPointerOfHolder{&embeddedHolder{logger.Info()}}
	embeddedHolderPointerOfHolder.Msg(h, "method expression through a pointer partway") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badNestedEmbeddedPointerOneLacksCtxMethodExpression(ctx context.Context, logger zerolog.Logger, cond bool) {
	h := embeddedHolderPointerOfHolder{&embeddedHolder{logger.Info().Ctx(ctx)}}
	if cond {
		h = embeddedHolderPointerOfHolder{&embeddedHolder{logger.Info()}}
	}
	embeddedHolderPointerOfHolder.Msg(h, "method expression through a pointer, one lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badNestedEmbeddedPointerParamMethodExpression(ctx context.Context, inner *embeddedHolder) {
	h := embeddedHolderPointerOfHolder{inner}
	embeddedHolderPointerOfHolder.Msg(h, "method expression through a pointer parameter") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badNestedEmbeddedPointerOfPointerParamMethodExpression(ctx context.Context, h *embeddedHolderPointerOfHolder) {
	embeddedHolderPointerOfHolder.Msg(*h, "method expression of a pointer parameter") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodNestedEmbeddedPointerSetToNilMethodExpression(ctx context.Context, logger zerolog.Logger, cond bool) {
	h := embeddedHolderPointerOfHolder{&embeddedHolder{logger.Info().Ctx(ctx)}}
	if cond {
		h.embeddedHolder = nil
	}
	embeddedHolderPointerOfHolder.Msg(h, "method expression through a pointer set to nil") // OK - a nil pointer has no event to log
}

func badNestedEmbeddedNilPointerMethodExpression(ctx context.Context) {
	var h embeddedHolderPointerOfHolder
	embeddedHolderPointerOfHolder.Msg(h, "method expression through a nil pointer") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// Ctx called in place on the embedded event, with its result unused, sets
// the context of the event the method expression reads (#204).
func goodEmbeddedStructCtxInPlaceMethodExpression(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolder{logger.Info()}
	h.Ctx(ctx)
	embeddedHolder.Msg(h, "method expression after Ctx in place") // OK - Ctx set the context in place
}

func goodEmbeddedStructPointerCtxInPlaceMethodExpression(ctx context.Context, logger zerolog.Logger) {
	h := &embeddedHolder{logger.Info()}
	h.Ctx(ctx)
	(*embeddedHolder).Msg(h, "pointer method expression after Ctx in place") // OK - Ctx set the context in place
}

func badEmbeddedStructCtxInPlaceOverwrittenMethodExpression(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolder{logger.Info()}
	h.Ctx(ctx)
	h.Event = logger.Warn()
	embeddedHolder.Msg(h, "method expression after another event is stored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEmbeddedStructCtxInPlaceOnOneBranchMethodExpression(ctx context.Context, logger zerolog.Logger, cond bool) {
	h := embeddedHolder{logger.Info()}
	if cond {
		h.Ctx(ctx)
	}
	embeddedHolder.Msg(h, "method expression after Ctx on one branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEmbeddedStructCtxBackgroundInPlaceMethodExpression(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolder{logger.Info()}
	h.Ctx(context.Background())
	embeddedHolder.Msg(h, "method expression after a fresh Ctx in place") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// LIMITATION(#204) (false positive): Ctx in place is not seen through an embedded
// pointer partway, which the event is read past with a load.
func limitationNestedEmbeddedPointerCtxInPlaceMethodExpression(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolderPointerOfHolder{&embeddedHolder{logger.Info()}}
	h.Ctx(ctx)
	embeddedHolderPointerOfHolder.Msg(h, "method expression through a pointer after Ctx in place") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func limitationNestedEmbeddedPointerCtxInPlacePromoted(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolderPointerOfHolder{&embeddedHolder{logger.Info()}}
	h.Ctx(ctx)
	h.Msg("promoted through a pointer after Ctx in place") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEmbeddedStructCtxInPlaceInClosureMethodExpression(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolder{logger.Info()}
	func() {
		h.Ctx(ctx)
		embeddedHolder.Msg(h, "method expression after Ctx in place, in a closure") // OK - Ctx set the context in place
	}()
}

func badEmbeddedStructOfPointerParamPointerMethodExpression(ctx context.Context, p **embeddedHolder) {
	(*p).Ctx(ctx)
	(*embeddedHolder).Msg(*p, "pointer method expression of a pointer loaded again") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEmbeddedStructOfGlobalMethodExpression(ctx context.Context) {
	embeddedHolder.Msg(globalEmbeddedHolder, "method expression of a global") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEmbeddedLoggerPointerMethodExpression(ctx context.Context, logger zerolog.Logger) {
	l := logger.With().Ctx(ctx).Logger()
	w := embeddedLoggerPointer{&l}
	embeddedLoggerPointer.Info(w).Msg("logger method expression via embedding") // OK - the embedded logger has ctx
}

func badEmbeddedLoggerPointerMethodExpression(ctx context.Context, logger zerolog.Logger) {
	w := embeddedLoggerPointer{&logger}
	embeddedLoggerPointer.Info(w).Msg("logger method expression via embedding") // want `zerolog call chain missing .Ctx\(ctx\)`
}

type embeddedLoggerPointer struct {
	*zerolog.Logger
}

// A method expression of a promoted Output or Ctx follows the same rules as
// a plain call: Output drops the context Context.Ctx set (#193), and a fresh
// context does not count (#197).
func badEmbeddedLoggerPointerOutputMethodExpression(ctx context.Context, logger zerolog.Logger, out io.Writer) {
	l := logger.With().Ctx(ctx).Logger()
	w := embeddedLoggerPointer{&l}
	detached := embeddedLoggerPointer.Output(w, out)
	detached.Info().Msg("Output via embedding") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEmbeddedZerologCtxOutputMethodExpression(ctx context.Context, out io.Writer) {
	w := embeddedLoggerPointer{zerolog.Ctx(ctx)}
	l := embeddedLoggerPointer.Output(w, out)
	l.Info().Msg("logger from ctx, Output via embedding") // OK - the logger is from ctx
}

func badEmbeddedStructCtxBackgroundMethodExpression(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolder{logger.Info()}
	embeddedHolder.Ctx(h, context.Background()).Msg("Background via embedding") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEmbeddedStructCtxMethodExpression(ctx context.Context, logger zerolog.Logger) {
	h := embeddedHolder{logger.Info()}
	embeddedHolder.Ctx(h, ctx).Msg("ctx via embedding") // OK
}

func goodEmbeddedLoggerPointer(ctx context.Context, logger zerolog.Logger) {
	l := logger.With().Ctx(ctx).Logger()
	w := embeddedLoggerPointer{&l}
	w.Info().Msg("embedded logger pointer") // OK - the embedded logger has ctx
	info := w.Info
	info().Msg("embedded logger pointer method value") // OK - the embedded logger has ctx
}

func goodRangeOverArrayOfLoggers(ctx context.Context, logger zerolog.Logger) {
	l := logger.With().Ctx(ctx).Logger()
	arr := [2]zerolog.Logger{l, l}
	first := arr[0]
	first.Info().Msg("element of an array") // OK - the element has ctx
}

func badEmbeddedLoggerPointer(ctx context.Context, logger zerolog.Logger) {
	w := embeddedLoggerPointer{&logger}
	w.Info().Msg("embedded logger pointer") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badElementOfArray(ctx context.Context, logger zerolog.Logger) {
	arr := [2]zerolog.Logger{logger.With().Ctx(ctx).Logger(), logger}
	second := arr[1]
	second.Info().Msg("element of an array") // want `zerolog call chain missing .Ctx\(ctx\)`
}

var globalEmbeddedHolder embeddedHolder

func badEmbeddedFieldOfGlobal(ctx context.Context) {
	globalEmbeddedHolder.Msg("embedded field of a global") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badEmbeddedFieldOfCallResult(ctx context.Context, newHolder func() embeddedHolder) {
	newHolder().Msg("embedded field of a call result") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A range over an array reads each element at a variable index, which may be
// any element, so the store of each one reaches it (#138, #222).
func goodRangeOverArrayWithCtx(ctx context.Context, logger zerolog.Logger) {
	l := logger.With().Ctx(ctx).Logger()
	for _, x := range [2]zerolog.Logger{l, l} {
		x.Info().Msg("range over array") // OK - every element has ctx
	}
}

func badRangeOverArrayOneElementLacksCtx(ctx context.Context, logger zerolog.Logger) {
	l := logger.With().Ctx(ctx).Logger()
	for _, x := range [2]zerolog.Logger{l, logger} {
		x.Info().Msg("range over array") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
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

// ===== FIELD THROUGH SEPARATE LOADS OF A POINTER =====

// A pointer loaded again for each use, as a receiver that a closure captures
// is, or a pointer field read again from a struct value, is the same pointer
// for each load. A field stored through one load and read through another is
// matched (#200). Every store to it in the function must have context.

type reloadedMonitor struct{ log zerolog.Logger }

func (m *reloadedMonitor) goodRunWithCapture(ctx context.Context) {
	m.log = zerolog.Ctx(ctx).With().Str("k", "v").Logger()
	m.log.Info().Msg("captured receiver") // OK - the only store has ctx
	go func() { _ = m }()
}

func (m *reloadedMonitor) badRunWithCapture(ctx context.Context, logger zerolog.Logger) {
	m.log = logger.With().Str("k", "v").Logger()
	m.log.Info().Msg("captured receiver") // want `zerolog call chain missing .Ctx\(ctx\)`
	go func() { _ = m }()
}

func (m *reloadedMonitor) badRunWithCaptureOneStoreLacksCtx(ctx context.Context, logger zerolog.Logger, cond bool) {
	m.log = zerolog.Ctx(ctx).With().Logger()
	if cond {
		m.log = logger
	}
	m.log.Info().Msg("captured receiver, one store lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
	go func() { _ = m }()
}

type reloadedMonitorHolder struct{ m *reloadedMonitor }

func goodRunHolder(ctx context.Context, h reloadedMonitorHolder) {
	h.m.log = zerolog.Nop().With().Ctx(ctx).Logger()
	h.m.log.Info().Msg("pointer field of a struct value") // OK - the only store has ctx
}

func badRunHolder(ctx context.Context, h reloadedMonitorHolder) {
	h.m.log = zerolog.Nop().With().Logger()
	h.m.log.Info().Msg("pointer field of a struct value") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badRunHolderNoStore(ctx context.Context, h reloadedMonitorHolder) {
	h.m.log.Info().Msg("pointer field of a struct value, not stored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// Two loads of the pointer are the same pointer only when the same stores to
// it reach both. A store of another pointer between them makes the read go
// through that one (#205).

func badReassignedCapturedPointer(ctx context.Context, a, b *reloadedMonitor) {
	p := a
	p.log = zerolog.Ctx(ctx).With().Logger()
	p = b
	p.log.Info().Msg("b.log is not stored here") // want `zerolog call chain missing .Ctx\(ctx\)`
	go func() { _ = p }()
}

func badReassignedCapturedPointerOnOneBranch(ctx context.Context, a, b *reloadedMonitor, cond bool) {
	p := a
	p.log = zerolog.Ctx(ctx).With().Logger()
	if cond {
		p = b
	}
	p.log.Info().Msg("b.log is not stored here") // want `zerolog call chain missing .Ctx\(ctx\)`
	go func() { _ = p }()
}

func goodReassignedCapturedPointerStoredAfter(ctx context.Context, a, b *reloadedMonitor) {
	p := a
	p = b
	p.log = zerolog.Ctx(ctx).With().Logger()
	p.log.Info().Msg("stored through b") // OK - the only store through b has ctx
	go func() { _ = p }()
}

func goodPointerLoadedFromParam(ctx context.Context, pp **reloadedMonitor) {
	(*pp).log = zerolog.Ctx(ctx).With().Logger()
	(*pp).log.Info().Msg("pointer loaded again from a parameter") // OK - the only store has ctx
}

func badPointerLoadedFromParamReassigned(ctx context.Context, pp **reloadedMonitor, b *reloadedMonitor) {
	(*pp).log = zerolog.Ctx(ctx).With().Logger()
	*pp = b
	(*pp).log.Info().Msg("b.log is not stored here") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodPointerInArrayElement(ctx context.Context, a *reloadedMonitor) {
	var arr [1]*reloadedMonitor
	arr[0] = a
	arr[0].log = zerolog.Ctx(ctx).With().Logger()
	arr[0].log.Info().Msg("pointer in an array element") // OK - the only store has ctx
	go func() { _ = arr }()
}

func badPointerInArrayReassigned(ctx context.Context, a, b *reloadedMonitor) {
	var arr [1]*reloadedMonitor
	arr[0] = a
	arr[0].log = zerolog.Ctx(ctx).With().Logger()
	arr = [1]*reloadedMonitor{b}
	arr[0].log.Info().Msg("b.log is not stored here") // want `zerolog call chain missing .Ctx\(ctx\)`
	go func() { _ = arr }()
}

func goodCapturedPointerDeclaredInLoop(ctx context.Context, a *reloadedMonitor, n int, cond bool) {
	for range n {
		var p *reloadedMonitor
		if cond {
			p = a
		}
		p.log = zerolog.Ctx(ctx).With().Logger()
		p.log.Info().Msg("declared in a loop") // OK - the store in the same iteration has ctx
		go func() { _ = p }()
	}
}

func goodCapturedPointerInLoop(ctx context.Context, ms []*reloadedMonitor) {
	for _, m := range ms {
		m.log = zerolog.Ctx(ctx).With().Logger()
		m.log.Info().Msg("captured in a loop") // OK - the store in the same iteration has ctx
		go func() { _ = m }()
	}
}

// ===== WRITES THROUGH ANOTHER REFERENCE =====

// LIMITATION(#115) (false negative): A write through another reference to the same
// memory is not seen (#115). Taking every call that is passed the address, or
// every store through a pointer of the same type, as a possible write would
// report common code, such as a method called on a struct before its logger is
// read, so the value stored before is taken as what the read gets.

func limitationAddressPassedToWriter(ctx context.Context, logger zerolog.Logger) {
	l := logger.With().Ctx(ctx).Logger()
	replaceLoggerAt(&l)
	l.Info().Msg("address passed to a writer") // LIMITATION(#115): should report but doesn't
}

func limitationSlicePassedToWriter(ctx context.Context, logger zerolog.Logger, es []*zerolog.Event) {
	es[0] = logger.Info().Ctx(ctx)
	replaceFirstEvent(es)
	es[0].Msg("slice passed to a writer") // LIMITATION(#115): should report but doesn't
}

func limitationSliceSharingArray(ctx context.Context, logger zerolog.Logger) {
	es := make([]*zerolog.Event, 1)
	es[0] = logger.Info().Ctx(ctx)
	alias := es[:]
	alias[0] = logger.Info()
	es[0].Msg("slice sharing an array") // LIMITATION(#115): should report but doesn't
}

func limitationPointerParamsAlias(ctx context.Context, logger zerolog.Logger, p, q *ptrHolder) {
	p.e = logger.Info().Ctx(ctx)
	q.e = logger.Info()
	p.e.Msg("pointer params may alias") // LIMITATION(#115): should report but doesn't
}

func replaceLoggerAt(p *zerolog.Logger) { *p = zerolog.Nop() }

func replaceFirstEvent(es []*zerolog.Event) { es[0] = log.Info() }

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

// ===== EMBEDDED STRUCT =====

type Service struct {
	zerolog.Logger
}

func badEmbeddedLogger(ctx context.Context, svc *Service) {
	svc.Info().Msg("embedded") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEmbeddedLoggerWithCtx(ctx context.Context, svc *Service) {
	svc.Info().Ctx(ctx).Msg("embedded with ctx") // OK
}

func badEmbeddedLoggerDerived(ctx context.Context, svc *Service) {
	l := svc.With().Str("svc", "test").Logger()
	l.Info().Msg("embedded derived") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEmbeddedLoggerDerivedCtx(ctx context.Context, svc *Service) {
	l := svc.With().Ctx(ctx).Logger()
	l.Info().Msg("embedded derived with ctx") // OK
}

// ===== LOGGER IN A FIELD OR ELEMENT =====

// Logger.Info has a pointer receiver, so its receiver is the address of the
// field or element itself, not a load from it (#97).

type fieldLoggerHolder struct {
	log zerolog.Logger
}

type embeddedLoggerHolder struct {
	zerolog.Logger
}

func goodLoggerInFieldLiteral(ctx context.Context, l zerolog.Logger) {
	s := fieldLoggerHolder{log: l.With().Ctx(ctx).Logger()}
	s.log.Info().Msg("field literal") // OK
}

func goodLoggerInPointerFieldLiteral(ctx context.Context, l zerolog.Logger) {
	s := &fieldLoggerHolder{log: l.With().Ctx(ctx).Logger()}
	s.log.Info().Msg("pointer field literal") // OK
}

func goodLoggerAssignedToField(ctx context.Context, l zerolog.Logger) {
	var s fieldLoggerHolder
	s.log = l.With().Ctx(ctx).Logger()
	s.log.Info().Msg("assigned field") // OK
}

func goodLoggerInArrayElement(ctx context.Context, l zerolog.Logger) {
	var arr [1]zerolog.Logger
	arr[0] = l.With().Ctx(ctx).Logger()
	arr[0].Info().Msg("array element") // OK
}

func goodLoggerInEmbeddedField(ctx context.Context, l zerolog.Logger) {
	e := embeddedLoggerHolder{l.With().Ctx(ctx).Logger()}
	e.Info().Msg("embedded field") // OK
}

func badLoggerInFieldLiteral(ctx context.Context, l zerolog.Logger) {
	s := fieldLoggerHolder{log: l}
	s.log.Info().Msg("field literal") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badLoggerAssignedToFieldInBranch(ctx context.Context, l zerolog.Logger, cond bool) {
	s := fieldLoggerHolder{log: l}
	if cond {
		s.log = l.With().Ctx(ctx).Logger()
	}
	s.log.Info().Msg("one branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badLoggerInArrayElement(ctx context.Context, l zerolog.Logger) {
	var arr [2]zerolog.Logger
	arr[0] = l.With().Ctx(ctx).Logger()
	arr[1] = l
	arr[1].Info().Msg("other element") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badLoggerInEmbeddedField(ctx context.Context, l zerolog.Logger) {
	e := embeddedLoggerHolder{l}
	e.Info().Msg("embedded field") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A field takes updates too, as a local variable does (#98).

func (s *fieldLoggerHolder) goodFieldUpdatedWithCtx(ctx context.Context) {
	s.log = s.log.With().Ctx(ctx).Logger()
	s.log.Info().Msg("field updated") // OK
}

func (s *fieldLoggerHolder) badFieldUpdateContextCtxDropped(ctx context.Context) {
	s.log.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Ctx(ctx)
	})
	s.log.Info().Msg("field UpdateContext") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodLocalFieldUpdatedWithCtx(ctx context.Context, l zerolog.Logger) {
	s := fieldLoggerHolder{log: l}
	s.log = s.log.With().Ctx(ctx).Logger()
	s.log.Info().Msg("local field updated") // OK
}

func (s *fieldLoggerHolder) badFieldUpdatedWithoutCtx(ctx context.Context) {
	s.log = s.log.With().Str("k", "v").Logger()
	s.log.Info().Msg("field updated") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func (s *fieldLoggerHolder) badFieldReadBeforeUpdate(ctx context.Context) {
	s.log.Info().Msg("before update") // want `zerolog call chain missing .Ctx\(ctx\)`
	s.log = s.log.With().Ctx(ctx).Logger()
}

func (s *fieldLoggerHolder) badFieldUpdatedInBranch(ctx context.Context, cond bool) {
	if cond {
		s.log = s.log.With().Ctx(ctx).Logger()
	}
	s.log.Info().Msg("one branch") // want `zerolog call chain missing .Ctx\(ctx\)`
}

var globalLoggerHolder fieldLoggerHolder

// The field of a global can change anywhere, so its updates do not count.
func badGlobalFieldUpdated(ctx context.Context) {
	globalLoggerHolder.log = globalLoggerHolder.log.With().Ctx(ctx).Logger()
	globalLoggerHolder.log.Info().Msg("global field") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A closure that stores to a variable it captures, and then reads it, reads
// what it stored, as a load of a captured event does.
func goodClosureStoresCapturedLogger(ctx context.Context, l zerolog.Logger) {
	var captured zerolog.Logger
	func() {
		captured = l.With().Ctx(ctx).Logger()
		captured.Info().Msg("stored in closure") // OK
	}()
	_ = captured
}

// A store in the closure on another path does not reach the read, which
// sees the value the closure was made with.
func goodClosureStoreOnOtherPath(ctx context.Context, l zerolog.Logger, cond bool) {
	captured := l.With().Ctx(ctx).Logger()
	func() {
		if cond {
			captured = l
			return
		}
		captured.Info().Msg("value the closure was made with") // OK
	}()
}

func badClosureStoreWithoutCtx(ctx context.Context, l zerolog.Logger, cond bool) {
	captured := l.With().Ctx(ctx).Logger()
	func() {
		if cond {
			captured = l
		}
		captured.Info().Msg("stored on one path") // want `zerolog call chain missing .Ctx\(ctx\)`
	}()
}

func newFieldLoggerHolderPtr() *fieldLoggerHolder {
	return &fieldLoggerHolder{}
}

// The holder a call returns may be shared, but the stores in this function
// are what it holds when nothing else is known.
func goodFieldOfReturnedHolderStoredWithCtx(ctx context.Context, l zerolog.Logger) {
	s := newFieldLoggerHolderPtr()
	s.log = l.With().Ctx(ctx).Logger()
	s.log.Info().Msg("field of returned holder") // OK
}

func goodReturnedHolderStoredWithCtx(ctx context.Context, l zerolog.Logger) {
	s := newFieldLoggerHolderPtr()
	*s = fieldLoggerHolder{log: l.With().Ctx(ctx).Logger()}
	s.log.Info().Msg("returned holder") // OK
}

func newFieldLoggerHolder(l zerolog.Logger) fieldLoggerHolder {
	return fieldLoggerHolder{log: l}
}

func badFieldOfZeroedHolder(ctx context.Context, l zerolog.Logger) {
	s := fieldLoggerHolder{log: l.With().Ctx(ctx).Logger()}
	s = fieldLoggerHolder{}
	s.log.Info().Msg("zeroed") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFieldOfHolderFromCall(ctx context.Context, l zerolog.Logger) {
	s := fieldLoggerHolder{log: l.With().Ctx(ctx).Logger()}
	s = newFieldLoggerHolder(l)
	s.log.Info().Msg("from call") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badFieldOfHolderCopiedFromGlobal(ctx context.Context) {
	s := globalLoggerHolder
	s.log.Info().Msg("copied from global") // want `zerolog call chain missing .Ctx\(ctx\)`
}

type twoLoggerHolder struct {
	a, b zerolog.Logger
}

// The field a Phi picks is not known, so each field takes every store that
// reaches any read of it.
func goodEitherFieldWithCtx(ctx context.Context, l zerolog.Logger, cond bool) {
	s := twoLoggerHolder{a: l.With().Ctx(ctx).Logger(), b: l.With().Ctx(ctx).Logger()}
	p := &s.a
	if cond {
		p = &s.b
	}
	p.Info().Msg("either field") // OK
}

func badEitherFieldOneWithoutCtx(ctx context.Context, l zerolog.Logger, cond bool) {
	s := twoLoggerHolder{a: l.With().Ctx(ctx).Logger(), b: l}
	p := &s.a
	if cond {
		p = &s.b
	}
	p.Info().Msg("either field") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodEitherPointerParamUpdated(ctx context.Context, l1, l2 *zerolog.Logger, cond bool) {
	*l1 = l1.With().Ctx(ctx).Logger()
	*l2 = l2.With().Ctx(ctx).Logger()
	l1.Info().Msg("after update") // OK
	p := l1
	if cond {
		p = l2
	}
	p.Info().Msg("either pointer") // OK
}

func badFieldPointerCapturedThenReplaced(ctx context.Context, l zerolog.Logger) {
	s := fieldLoggerHolder{log: l.With().Ctx(ctx).Logger()}
	p := &s.log
	f := func() { p.Info().Msg("captured field") } // want `zerolog call chain missing .Ctx\(ctx\)`
	s = newFieldLoggerHolder(l)
	f()
}

var (
	globalEvent        *zerolog.Event
	globalEventHolderA eventHolder
	globalEventHolderB eventHolder
)

func badGlobalEvent(ctx context.Context) {
	globalEvent.Msg("global event") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// Copies between two globals lead back to each other, and end there.
func badGlobalHoldersCopiedBothWays(ctx context.Context) {
	globalEventHolderA = globalEventHolderB
	globalEventHolderB = globalEventHolderA
	globalEventHolderB.event.Msg("b") // want `zerolog call chain missing .Ctx\(ctx\)`
	globalEventHolderA.event.Msg("a") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== ELEMENT AT A VARIABLE INDEX =====

// A store through an element at an index that is not a constant writes the
// element read through the same index (#222). A store to an element at
// another index may write it too, or not: it does not hide a store before it,
// and what it stores must have context as well.

func goodVariableIndexSameAddress(ctx context.Context, base zerolog.Logger, i int) {
	var arr [2]zerolog.Logger
	p := &arr[i]
	*p = base.With().Ctx(ctx).Logger()
	p.Info().Msg("p carries ctx") // OK - the only store through p has ctx
}

func badVariableIndexSameAddress(ctx context.Context, base zerolog.Logger, i int) {
	var arr [2]zerolog.Logger
	p := &arr[i]
	*p = base
	p.Info().Msg("p lacks ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodVariableIndexSameOperand(ctx context.Context, base zerolog.Logger, i int) {
	var arr [2]zerolog.Logger
	arr[i] = base.With().Ctx(ctx).Logger()
	arr[i].Info().Msg("arr[i] carries ctx") // OK - the store to arr[i] has ctx
}

func goodVariableIndexOverwritesConstantIndex(ctx context.Context, base zerolog.Logger, i int) {
	var arr [2]zerolog.Logger
	arr[0] = base
	p := &arr[i]
	*p = base.With().Ctx(ctx).Logger()
	p.Info().Msg("the store through p comes last") // OK - it overwrites arr[0] when i is 0
}

func goodVariableIndexThenConstantIndexWithCtx(ctx context.Context, base zerolog.Logger, i int) {
	var arr [2]zerolog.Logger
	p := &arr[i]
	*p = base.With().Ctx(ctx).Logger()
	arr[0] = base.With().Ctx(ctx).Logger()
	p.Info().Msg("both stores carry ctx") // OK - arr[0] has ctx too
}

func badVariableIndexThenConstantIndexWithoutCtx(ctx context.Context, base zerolog.Logger, i int) {
	var arr [2]zerolog.Logger
	p := &arr[i]
	*p = base.With().Ctx(ctx).Logger()
	arr[0] = base
	p.Info().Msg("arr[0] may be *p") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badVariableIndexConstantIndexOnly(ctx context.Context, base zerolog.Logger, i int) {
	var arr [2]zerolog.Logger
	arr[0] = base.With().Ctx(ctx).Logger()
	arr[1] = base
	arr[i].Info().Msg("arr[1] may be read") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodVariableIndexConstantIndexOnly(ctx context.Context, base zerolog.Logger, i int) {
	arr := [2]zerolog.Logger{base.With().Ctx(ctx).Logger(), base.With().Ctx(ctx).Logger()}
	arr[i].Info().Msg("every element carries ctx") // OK
}

func badVariableIndexOtherVariableIndex(ctx context.Context, base zerolog.Logger, i, j int) {
	var arr [2]zerolog.Logger
	arr[i] = base.With().Ctx(ctx).Logger()
	arr[j] = base
	arr[i].Info().Msg("j may be i") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badConstantIndexAfterVariableIndex(ctx context.Context, base zerolog.Logger, i int) {
	var arr [2]zerolog.Logger
	arr[0] = base.With().Ctx(ctx).Logger()
	arr[i] = base
	arr[0].Info().Msg("i may be 0") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodVariableIndexInLoop(ctx context.Context, base zerolog.Logger) {
	var arr [2]zerolog.Logger
	for i := range arr {
		p := &arr[i]
		*p = base.With().Ctx(ctx).Logger()
		p.Info().Msg("each element carries ctx") // OK
	}
}

func goodVariableIndexFilledInLoop(ctx context.Context, base zerolog.Logger) {
	var arr [2]zerolog.Logger
	for i := range arr {
		arr[i] = base.With().Ctx(ctx).Logger()
	}
	for i := range arr {
		arr[i].Info().Msg("filled with ctx") // OK
	}
}

func badVariableIndexFilledInLoop(ctx context.Context, base zerolog.Logger) {
	var arr [2]zerolog.Logger
	for i := range arr {
		arr[i] = base
	}
	for i := range arr {
		arr[i].Info().Msg("filled without ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

// LIMITATION (false positive): An index computed in a loop has another value
// on each iteration, so a store at the same index is not taken to write the
// same element. Each store at it may reach the read, and each one must have
// context.
func limitationVariableIndexStoredAfterReadInLoop(ctx context.Context, base zerolog.Logger) {
	var arr [2]zerolog.Logger
	for i := range arr {
		arr[i] = base.With().Ctx(ctx).Logger()
		arr[i].Info().Msg("arr[i] carries ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
		arr[i] = base
	}
}

func goodSliceVariableIndex(ctx context.Context, base zerolog.Logger, s []zerolog.Logger, i int) {
	s[i] = base.With().Ctx(ctx).Logger()
	s[i].Info().Msg("s[i] carries ctx") // OK - the store to s[i] has ctx
}

func badSliceVariableIndex(ctx context.Context, base zerolog.Logger, s []zerolog.Logger, i int) {
	s[i] = base.With().Ctx(ctx).Logger()
	s[0] = base
	s[i].Info().Msg("s[0] may be s[i]") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// LIMITATION (false positive): The same in a slice parameter. A store at an
// index computed in a loop may be to another element, so the element the
// caller passed may still be the one read.
func limitationSliceVariableIndexInLoop(ctx context.Context, base zerolog.Logger, s []zerolog.Logger) {
	for i := range s {
		s[i] = base.With().Ctx(ctx).Logger()
		s[i].Info().Msg("each element carries ctx") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

func goodLocalSliceVariableIndex(ctx context.Context, base zerolog.Logger, i int) {
	s := make([]zerolog.Logger, 2)
	s[i] = base.With().Ctx(ctx).Logger()
	s[i].Info().Msg("s[i] carries ctx") // OK
}

func badLocalSliceVariableIndex(ctx context.Context, base zerolog.Logger, i int) {
	s := make([]zerolog.Logger, 2)
	s[i] = base.With().Ctx(ctx).Logger()
	s[1] = base
	s[i].Info().Msg("s[1] may be s[i]") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func goodVariableIndexPickedDeferred(ctx context.Context, base zerolog.Logger, i, j int, cond bool) {
	var arr [2]zerolog.Logger
	arr[i] = base.With().Ctx(ctx).Logger()
	arr[j] = base.With().Ctx(ctx).Logger()
	p := &arr[i]
	if cond {
		p = &arr[j]
	}
	defer p.Print("deferred through a pointer picked on a branch") // OK - both elements have ctx
}

func badVariableIndexPickedDeferred(ctx context.Context, base zerolog.Logger, i, j int, cond bool) {
	var arr [2]zerolog.Logger
	arr[i] = base.With().Ctx(ctx).Logger()
	arr[j] = base
	p := &arr[i]
	if cond {
		p = &arr[j]
	}
	defer p.Print("deferred through a pointer picked on a branch") // want `zerolog direct logging bypasses context`
}
