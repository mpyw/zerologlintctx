// Package ssa provides SSA-based analysis for zerolog context propagation.
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
package ssa

import (
	"go/token"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/directive"
	"github.com/mpyw/zerologlintctx/internal/ssa/tracer"
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
	pass      *analysis.Pass      // For reporting diagnostics
	ctxName   string              // Context variable name (for error messages)
	ignoreMap directive.IgnoreMap // Line-level ignore directives
	reported  map[token.Pos]bool  // Deduplication: same position reported once
}

// NewChecker creates a new checker for analyzing a function.
func NewChecker(pass *analysis.Pass, ctxName string, ignoreMap directive.IgnoreMap) *Checker {
	return &Checker{
		pass:      pass,
		ctxName:   ctxName,
		ignoreMap: ignoreMap,
		reported:  make(map[token.Pos]bool),
	}
}

// CheckFunction analyzes all instructions in a function.
func (c *Checker) CheckFunction(fn *ssa.Function) {
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			switch v := instr.(type) {
			case *ssa.Call:
				c.checkTerminatorCall(v)
				c.checkDirectLoggingCall(v)
			case *ssa.Defer:
				c.checkDeferredCall(v)
			}
		}
	}
}

// checkDeferredCall checks if a deferred terminator call has context properly set.
func (c *Checker) checkDeferredCall(d *ssa.Defer) {
	callee := d.Call.StaticCallee()
	if callee == nil {
		return
	}

	// Must be on zerolog.Event and return void (terminators: Msg, Msgf, MsgFunc, Send)
	recv := d.Call.Signature().Recv()
	if recv == nil || !typeutil.IsEvent(recv.Type()) || !typeutil.ReturnsVoid(callee) {
		return
	}

	// Trace back to find if context was set
	if len(d.Call.Args) > 0 && c.eventChainHasCtx(d.Call.Args[0]) {
		return
	}

	c.report(d.Pos())
}

// checkTerminatorCall checks if a terminator call (Msg, Msgf, MsgFunc, Send)
// has context properly set in the chain.
func (c *Checker) checkTerminatorCall(call *ssa.Call) {
	callee := call.Call.StaticCallee()
	if callee == nil {
		return
	}

	// Check if this is a bound method call (method value)
	// e.g., msg := e.Msg; msg("text")
	if mc, ok := call.Call.Value.(*ssa.MakeClosure); ok {
		c.checkBoundMethodTerminator(call, mc, callee)
		return
	}

	// Must be on zerolog.Event and return void (terminators: Msg, Msgf, MsgFunc, Send)
	recv := call.Call.Signature().Recv()
	if recv == nil || !typeutil.IsEvent(recv.Type()) || !typeutil.ReturnsVoid(callee) {
		return
	}

	// Trace back to find if context was set
	if len(call.Call.Args) > 0 && c.eventChainHasCtx(call.Call.Args[0]) {
		return
	}

	c.report(call.Pos())
}

// checkBoundMethodTerminator checks if a bound method call (method value) is a terminator
// without context.
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
func (c *Checker) checkBoundMethodTerminator(call *ssa.Call, mc *ssa.MakeClosure, callee *ssa.Function) {
	// Check if it returns void (terminators return void)
	if !typeutil.ReturnsVoid(callee) {
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

	c.report(call.Pos())
}

// checkDirectLoggingCall checks for direct logging calls that bypass the Event chain.
func (c *Checker) checkDirectLoggingCall(call *ssa.Call) {
	callee := call.Call.StaticCallee()
	if callee == nil {
		return
	}

	recv := call.Call.Signature().Recv()

	// Check for Logger.Print/Printf (method on Logger that returns void)
	if typeutil.IsDirectLoggingMethod(callee, recv) {
		c.reportDirectLogging(call.Pos())
		return
	}

	// Check for log.Print/log.Printf (package-level function that returns void)
	if typeutil.IsDirectLoggingFunc(callee) {
		c.reportDirectLogging(call.Pos())
		return
	}
}

func (c *Checker) reportDirectLogging(pos token.Pos) {
	if c.reported[pos] {
		return
	}
	c.reported[pos] = true

	line := c.pass.Fset.Position(pos).Line
	if c.ignoreMap != nil && c.ignoreMap.ShouldIgnore(line) {
		return
	}

	c.pass.Reportf(pos, "zerolog direct logging bypasses context; use Event chain with .Ctx(%s)", c.ctxName)
}

func (c *Checker) report(pos token.Pos) {
	if c.reported[pos] {
		return
	}
	c.reported[pos] = true

	line := c.pass.Fset.Position(pos).Line
	if c.ignoreMap != nil && c.ignoreMap.ShouldIgnore(line) {
		return
	}

	c.pass.Reportf(pos, "zerolog call chain missing .Ctx(%s)", c.ctxName)
}

// eventChainHasCtx traces an Event value to check if .Ctx() was called.
func (c *Checker) eventChainHasCtx(v ssa.Value) bool {
	registry := tracer.NewRegistry()
	return c.traceValue(v, registry.EventTracer(), make(map[ssa.Value]bool))
}
