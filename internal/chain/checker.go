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
	"cmp"
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/typeutil"
)

// The messages of the two findings. Each takes the context name.
const (
	checkerMissingCtx    = "zerolog call chain missing .Ctx(%s)"
	checkerDirectLogging = "zerolog direct logging bypasses context; use Event chain with .Ctx(%s)"
)

// Checker performs SSA-based analysis of zerolog chains.
//
// It scans SSA instructions for:
//   - Terminator calls: Event.Msg(), Event.Send(), etc.
//   - Direct logging: Logger.Print(), log.Print(), etc.
//
// For each terminator, it traces backwards to verify .Ctx(ctx) was called.
//
// One Checker serves every function of a package. Its tracer keeps what it
// found of each value, so a closure does not trace again the values of the
// function it is declared in, which every closure there would otherwise do.
type Checker struct {
	ctxName  string                          // Context variable name of the function checked (for error messages)
	report   func(pos token.Pos, msg string) // Receives each finding of the function checked
	reported map[token.Pos]bool              // Deduplication: same position reported once
	tracer   *tracer                         // Traces each terminator's receiver
	scope    *types.Scope                    // The scope of the package of the function checked
}

// NewChecker creates a new checker for the functions of one package.
func NewChecker() *Checker {
	return &Checker{reported: make(map[token.Pos]bool), tracer: newTracer()}
}

// CheckFunction analyzes all instructions in fn, a function that has ctxName
// in scope, and passes each finding to report, at most once per position.
// Calls, go statements and defer statements are checked alike, and each is
// reported at the "(" of its call. The position of a go or defer statement
// itself is its keyword, which a trailing ignore on the line of the "(" would
// not reach.
func (c *Checker) CheckFunction(fn *ssa.Function, ctxName string, report func(pos token.Pos, msg string)) {
	c.ctxName, c.report = ctxName, report
	c.scope = fn.Pkg.Pkg.Scope()
	for instr := range instrsIn(fn) {
		if call, ok := instr.(ssa.CallInstruction); ok {
			c.checkCall(call)
		}
	}
}

// checkCall checks one call, a plain call, a go statement or a defer
// statement:
//   - A terminator (Msg, Msgf, MsgFunc, Send) is reported unless its Event
//     chain has context set
//   - Direct logging (Logger.Print*, log.Print*) is always reported
func (c *Checker) checkCall(call ssa.CallInstruction) {
	cc := call.Common()
	pos := cc.Pos()
	callee, ok := calleeOf(cc)
	if !ok {
		return
	}
	switch {
	case callee.endsEventChain():
		if !c.tracer.eventHasCtx(callee.recv, call) {
			c.reportOnce(pos, checkerMissingCtx)
		}
	case callee.logsDirectly():
		c.reportOnce(pos, checkerDirectLogging)
	}
}

// reportOnce passes a finding at pos to the report func, unless one was
// already passed there, or the context cannot be named there. format takes
// the context name.
func (c *Checker) reportOnce(pos token.Pos, format string) {
	if c.reported[pos] || !c.ctxInScopeAt(pos) {
		return
	}
	c.reported[pos] = true
	c.report(pos, fmt.Sprintf(format, c.ctxName))
}

// ctxInScopeAt reports whether the context name refers to a context at pos,
// of the same kind: a context.Context, or a pointer to one for a *name.
// A closure takes the name from the function it is declared in, where a
// parameter or a variable of its own may shadow it:
//
//	func(ctx context.Context) {
//	    func(ctx string) {
//	        log.Info().Msg("x") // ctx is not the context here
//	    }("x")
//	}
func (c *Checker) ctxInScopeAt(pos token.Pos) bool {
	inner := cmp.Or(c.scope.Innermost(pos), c.scope) // A call is always in a file of the package
	name, deref := strings.CutPrefix(c.ctxName, "*")
	_, obj := inner.LookupParent(name, pos)
	v, ok := obj.(*types.Var)
	if !ok || !typeutil.IsContextType(v.Type()) {
		return false
	}
	// A *context.Context is named as *name, and a context.Context as name
	_, isPtr := v.Type().Underlying().(*types.Pointer)
	return isPtr == deref
}
