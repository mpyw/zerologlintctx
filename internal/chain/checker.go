// Package chain provides SSA-based analysis for zerolog context propagation.
//
// # Architecture
//
// The package uses SSA (Static Single Assignment) form for precise dataflow analysis:
//
//	┌─────────────────────────────────────────────────────────────────────┐
//	│                      SSA Analysis Flow                               │
//	│                                                                      │
//	│  Source Code                SSA Form                  Analysis       │
//	│  ───────────                ────────                  ────────       │
//	│                                                                      │
//	│  e := log.Info()    →    t0 = (*Logger).Info(log)                   │
//	│  e.Str("k", "v")    →    t1 = (*Event).Str(t0, "k", "v")            │
//	│  e.Msg("hello")     →    (*Event).Msg(t1, "hello")   ← terminator   │
//	│                                                            │        │
//	│                                                      trace back     │
//	│                                                      to find .Ctx() │
//	└─────────────────────────────────────────────────────────────────────┘
//
// # Detection Strategy
//
// The checker identifies terminator calls (Msg, Send, etc.) and traces
// backwards through the SSA value chain to verify .Ctx(ctx) was called.
package chain

import (
	"fmt"
	"go/token"

	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/typeutil"
)

// Checker performs SSA-based analysis of zerolog chains.
//
// It scans SSA instructions for:
//   - Terminator calls: Event.Msg(), Event.Send(), etc.
//   - Direct logging: Logger.Print(), log.Print(), etc.
//
// For each terminator, it traces backwards to verify .Ctx(ctx) was called.
type Checker struct {
	ctxName  string                          // Context variable name (for error messages)
	report   func(pos token.Pos, msg string) // Receives each finding
	reported map[token.Pos]bool              // Deduplication: same position reported once
}

// NewChecker creates a new checker for analyzing a function that has ctxName
// in scope. It passes each finding to report, at most once per position.
func NewChecker(ctxName string, report func(pos token.Pos, msg string)) *Checker {
	return &Checker{
		ctxName:  ctxName,
		report:   report,
		reported: make(map[token.Pos]bool),
	}
}

// CheckFunction analyzes all instructions in a function.
func (c *Checker) CheckFunction(fn *ssa.Function) {
	for instr := range instrsIn(fn) {
		switch v := instr.(type) {
		case *ssa.Call:
			c.checkTerminatorCall(&v.Call, v.Pos())
			c.checkDirectLoggingCall(v)
		case *ssa.Defer:
			c.checkTerminatorCall(&v.Call, v.Pos())
		}
	}
}

// checkTerminatorCall checks if a terminator call (Msg, Msgf, MsgFunc, Send),
// called or deferred at pos, has context properly set in the chain.
func (c *Checker) checkTerminatorCall(cc *ssa.CallCommon, pos token.Pos) {
	callee := cc.StaticCallee()
	if callee == nil {
		return
	}

	// Check if this is a bound method call (method value)
	// e.g., msg := e.Msg; msg("text") or defer msg("text")
	if mc, ok := cc.Value.(*ssa.MakeClosure); ok {
		c.checkBoundMethodTerminator(mc, callee, pos)
		return
	}

	// Must be on zerolog.Event and return void (terminators: Msg, Msgf, MsgFunc, Send)
	recv := cc.Signature().Recv()
	if recv == nil || !typeutil.IsEvent(recv.Type()) || !typeutil.ReturnsVoid(callee.Signature) {
		return
	}

	// Trace back to find if context was set
	if len(cc.Args) > 0 && c.eventChainHasCtx(cc.Args[0]) {
		return
	}

	c.reportOnce(pos, "zerolog call chain missing .Ctx(%s)")
}

// checkBoundMethodTerminator checks if a bound method call (method value),
// called or deferred at pos, is a terminator without context.
//
// Bound methods are created when a method is extracted as a value:
//
//	msg := e.Msg    // Creates MakeClosure with receiver in Bindings[0]
//	msg("text")     // Calls the bound method wrapper (*Event).Msg$bound
//
// SSA representation:
//
//	t0 = (*Event).Msg$bound(e)   ← MakeClosure with e in Bindings[0]
//	t0("text")                   ← Call to the closure
//
// We need to trace Bindings[0] (the receiver) to find if .Ctx() was called.
func (c *Checker) checkBoundMethodTerminator(mc *ssa.MakeClosure, callee *ssa.Function, pos token.Pos) {
	// Check if it returns void (terminators return void)
	if !typeutil.ReturnsVoid(callee.Signature) {
		return
	}

	// Check if receiver (in Bindings[0]) is *zerolog.Event
	if len(mc.Bindings) == 0 {
		return
	}
	recvType := mc.Bindings[0].Type()
	if !typeutil.IsEvent(recvType) {
		return
	}

	// Trace the receiver to find if context was set
	if c.eventChainHasCtx(mc.Bindings[0]) {
		return
	}

	c.reportOnce(pos, "zerolog call chain missing .Ctx(%s)")
}

// checkDirectLoggingCall checks for direct logging calls that bypass the Event chain.
func (c *Checker) checkDirectLoggingCall(call *ssa.Call) {
	callee := call.Call.StaticCallee()
	if callee == nil {
		return
	}

	// Logger.Print* or log.Print*
	if isDirectLoggingCallee(callee, call.Call.Signature().Recv()) {
		c.reportOnce(call.Pos(), "zerolog direct logging bypasses context; use Event chain with .Ctx(%s)")
	}
}

// reportOnce passes a finding at pos to the report func, unless one was
// already passed there. format takes the context name.
func (c *Checker) reportOnce(pos token.Pos, format string) {
	if c.reported[pos] {
		return
	}
	c.reported[pos] = true
	c.report(pos, fmt.Sprintf(format, c.ctxName))
}

// eventChainHasCtx traces an Event value to check if .Ctx() was called.
func (c *Checker) eventChainHasCtx(v ssa.Value) bool {
	return c.traceValue(v, tracerEvent, make(map[ssa.Value]bool))
}
