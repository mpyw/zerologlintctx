package tracer

import (
	"go/types"

	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/typeutil"
)

// ContextTracer traces zerolog.Context values for context.
//
// Context sources:
//   - Context.Ctx(ctx): direct context setting
//   - Logger.With(): inherits from parent Logger
type ContextTracer struct {
	logger *LoggerTracer
}

// CheckContext implements Tracer.
func (t *ContextTracer) CheckContext(
	call *ssa.Call,
	callee *ssa.Function,
	recv *types.Var,
) Result {
	// Context.Ctx(ctx) - direct context setting
	if callee.Name() == typeutil.CtxMethod && recv != nil && typeutil.IsContext(recv.Type()) {
		return Found()
	}

	// Logger methods that return Context (With) - delegate to logger tracer
	if recv != nil && typeutil.IsLogger(recv.Type()) && typeutil.ReturnsContext(callee) {
		if len(call.Call.Args) > 0 {
			return DelegateTo(t.logger, call.Call.Args[0])
		}
	}

	return Continue()
}

// ContinueOnReceiverType implements Tracer.
func (t *ContextTracer) ContinueOnReceiverType(recv *types.Var) bool {
	return recv != nil && typeutil.IsContext(recv.Type())
}
