package tracer

import (
	"go/types"

	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/typeutil"
)

// LoggerTracer traces zerolog.Logger values for context.
//
// Context sources:
//   - zerolog.Ctx(ctx): returns Logger from context
//   - Context.Logger(): inherits from Context builder
//   - Logger.With(): inherits from parent Logger (via Context)
type LoggerTracer struct {
	context *ContextTracer
}

// CheckContext implements Tracer.
func (t *LoggerTracer) CheckContext(
	call *ssa.Call,
	callee *ssa.Function,
	recv *types.Var,
) Result {
	// zerolog.Ctx(ctx) - returns Logger with context
	if typeutil.IsCtxFunc(callee) {
		return Found()
	}

	// Context methods that return Logger - delegate to context tracer
	if recv != nil && typeutil.IsContext(recv.Type()) && typeutil.ReturnsLogger(callee) {
		if len(call.Call.Args) > 0 {
			return DelegateTo(t.context, call.Call.Args[0])
		}
	}

	// Logger methods that return Context (With) - trace parent Logger (self-delegation via context)
	if recv != nil && typeutil.IsLogger(recv.Type()) && typeutil.ReturnsContext(callee) {
		if len(call.Call.Args) > 0 {
			return DelegateTo(t, call.Call.Args[0])
		}
	}

	return Continue()
}

// ContinueOnReceiverType implements Tracer.
func (t *LoggerTracer) ContinueOnReceiverType(recv *types.Var) bool {
	return recv != nil && typeutil.IsLogger(recv.Type())
}
