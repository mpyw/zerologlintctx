// Package internal provides SSA-based analysis for zerolog context propagation.
//
// This package uses SSA (Static Single Assignment) form to track zerolog Event
// values through variable assignments, conditionals, and closures. It detects
// cases where a context.Context is available but not passed to the log chain
// via .Ctx(ctx).
package internal

import (
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"
)

// =============================================================================
// Entry Point
// =============================================================================

// RunSSA performs SSA-based analysis for zerolog context propagation.
//
// The analysis tracks context through three zerolog types:
//   - Event: The log event being built (e.g., log.Info().Str("k","v"))
//   - Context: The builder for derived loggers (e.g., log.With().Str("k","v"))
//   - Logger: The logger instance
//
// Context can be set via:
//   - Event.Ctx(ctx): Sets context on the current event
//   - Context.Ctx(ctx): Sets default context for the derived logger
//   - zerolog.Ctx(ctx): Returns a logger from context (already has ctx)
func RunSSA(
	pass *analysis.Pass,
	ssaInfo *buildssa.SSA,
	ignoreMaps map[string]IgnoreMap,
	skipFiles map[string]bool,
	isContextType func(types.Type) bool,
) {
	funcCtx := buildFunctionContextMap(ssaInfo, isContextType)

	for fn, info := range funcCtx {
		pos := fn.Pos()
		if !pos.IsValid() {
			continue
		}
		filename := pass.Fset.Position(pos).Filename
		if skipFiles[filename] {
			continue
		}
		ignoreMap := ignoreMaps[filename]

		chk := newChecker(pass, info.name, ignoreMap)
		chk.checkFunction(fn)
	}

	// Report unused ignore directives
	for _, ignoreMap := range ignoreMaps {
		if ignoreMap == nil {
			continue
		}
		for _, pos := range ignoreMap.GetUnusedIgnores() {
			pass.Reportf(pos, "unused zerologlintctx:ignore directive")
		}
	}
}

// =============================================================================
// Function Context Discovery
// =============================================================================

// contextInfo holds context variable information for a function.
type contextInfo struct {
	name string // The context variable name (for error messages)
}

// buildFunctionContextMap builds a map of functions to their context info.
// It handles both direct context parameters and closures that inherit context.
func buildFunctionContextMap(
	ssaInfo *buildssa.SSA,
	isContextType func(types.Type) bool,
) map[*ssa.Function]contextInfo {
	funcCtx := make(map[*ssa.Function]contextInfo)

	// First pass: find direct context parameters
	for _, fn := range ssaInfo.SrcFuncs {
		if info := findContextInParams(fn, isContextType); info != nil {
			funcCtx[fn] = *info
		}
	}

	// Second pass: propagate context to closures (iterate until stable)
	for {
		changed := false
		for _, fn := range ssaInfo.SrcFuncs {
			if _, hasCtx := funcCtx[fn]; hasCtx {
				continue
			}
			if fn.Parent() != nil {
				if parentCtx, ok := funcCtx[fn.Parent()]; ok {
					funcCtx[fn] = parentCtx
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}

	return funcCtx
}

// findContextInParams finds the context.Context parameter in function signature.
func findContextInParams(fn *ssa.Function, isContextType func(types.Type) bool) *contextInfo {
	if fn.Signature == nil {
		return nil
	}
	params := fn.Signature.Params()
	if params == nil {
		return nil
	}
	for param := range params.Variables() {
		if isContextType(param.Type()) {
			return &contextInfo{name: param.Name()}
		}
	}
	return nil
}

// =============================================================================
// SSA Checker
// =============================================================================

// checker performs SSA-based analysis of zerolog chains.
type checker struct {
	pass      *analysis.Pass
	ctxName   string
	ignoreMap IgnoreMap
	reported  map[token.Pos]bool
}

func newChecker(pass *analysis.Pass, ctxName string, ignoreMap IgnoreMap) *checker {
	return &checker{
		pass:      pass,
		ctxName:   ctxName,
		ignoreMap: ignoreMap,
		reported:  make(map[token.Pos]bool),
	}
}

func (c *checker) checkFunction(fn *ssa.Function) {
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
func (c *checker) checkDeferredCall(d *ssa.Defer) {
	callee := d.Call.StaticCallee()
	if callee == nil {
		return
	}

	// Must be on zerolog.Event and return void (terminators: Msg, Msgf, MsgFunc, Send)
	recv := d.Call.Signature().Recv()
	if recv == nil || !isEvent(recv.Type()) || !returnsVoid(callee) {
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
func (c *checker) checkTerminatorCall(call *ssa.Call) {
	callee := call.Call.StaticCallee()
	if callee == nil {
		return
	}

	// Check if this is a bound method call (method value)
	// e.g., msg := e.Msg; msg("text")
	// In this case, callee is the wrapper (*Event).Msg$bound and recv is nil
	if mc, ok := call.Call.Value.(*ssa.MakeClosure); ok {
		c.checkBoundMethodTerminator(call, mc, callee)
		return
	}

	// Must be on zerolog.Event and return void (terminators: Msg, Msgf, MsgFunc, Send)
	recv := call.Call.Signature().Recv()
	if recv == nil || !isEvent(recv.Type()) || !returnsVoid(callee) {
		return
	}

	// Trace back to find if context was set
	if len(call.Call.Args) > 0 && c.eventChainHasCtx(call.Call.Args[0]) {
		return
	}

	c.report(call.Pos())
}

// checkBoundMethodTerminator checks if a bound method call (method value) is a terminator
// without context. Bound methods are created when a method is extracted as a value:
//
//	msg := e.Msg    // Creates MakeClosure with receiver in Bindings[0]
//	msg("text")     // Calls the bound method wrapper (*Event).Msg$bound
func (c *checker) checkBoundMethodTerminator(call *ssa.Call, mc *ssa.MakeClosure, callee *ssa.Function) {
	// Check if it returns void (terminators return void)
	if !returnsVoid(callee) {
		return
	}

	// Check if receiver (in Bindings[0]) is *zerolog.Event
	if len(mc.Bindings) == 0 {
		return
	}
	recvType := mc.Bindings[0].Type()
	if !isEvent(recvType) {
		return
	}

	// Trace the receiver to find if context was set
	if c.eventChainHasCtx(mc.Bindings[0]) {
		return
	}

	c.report(call.Pos())
}

// checkDirectLoggingCall checks for direct logging calls that bypass the Event chain
// (Logger.Print, Logger.Printf, log.Print, log.Printf).
// These calls cannot propagate context and should be reported.
func (c *checker) checkDirectLoggingCall(call *ssa.Call) {
	callee := call.Call.StaticCallee()
	if callee == nil {
		return
	}

	recv := call.Call.Signature().Recv()

	// Check for Logger.Print/Printf (method on Logger that returns void)
	if isDirectLoggingMethod(callee, recv) {
		c.reportDirectLogging(call.Pos())
		return
	}

	// Check for log.Print/log.Printf (package-level function that returns void)
	if isDirectLoggingFunc(callee) {
		c.reportDirectLogging(call.Pos())
		return
	}
}

func (c *checker) reportDirectLogging(pos token.Pos) {
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

func (c *checker) report(pos token.Pos) {
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
func (c *checker) eventChainHasCtx(v ssa.Value) bool {
	tracer := newTracers()
	return c.traceValue(v, tracer, make(map[ssa.Value]bool))
}
