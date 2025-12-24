package tracer

import (
	"go/types"

	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/typeutil"
)

// EventTracer traces *zerolog.Event values for context.
//
// Context sources:
//
//	log.Info().Ctx(ctx).Msg("direct")          ← Event.Ctx()
//	log.With().Ctx(ctx).Logger().Info().Msg()  ← inherited from Context
//	zerolog.Ctx(ctx).Info().Msg("from ctx")   ← Logger from context
//
// Delegation flow:
//
//	Event ← Logger.Info()     → delegate to LoggerTracer
//	Event ← Context.Logger()  → delegate to ContextTracer
type EventTracer struct {
	logger  *LoggerTracer
	context *ContextTracer
}

// CheckContext implements Tracer.
func (t *EventTracer) CheckContext(
	call *ssa.Call,
	callee *ssa.Function,
	recv *types.Var,
) Result {
	// Event.Ctx(ctx) or Context.Ctx(ctx) - direct context setting
	if callee.Name() == typeutil.CtxMethod && recv != nil {
		if typeutil.IsEvent(recv.Type()) || typeutil.IsContext(recv.Type()) {
			return Found()
		}
	}

	// zerolog.Ctx(ctx) - returns Logger with context
	if typeutil.IsCtxFunc(callee) {
		return Found()
	}

	// Logger methods that return Event (Info, Debug, Err, WithLevel, etc.) - delegate to logger tracer
	if recv != nil && typeutil.IsLogger(recv.Type()) && typeutil.ReturnsEvent(callee) {
		if len(call.Call.Args) > 0 {
			return DelegateTo(t.logger, call.Call.Args[0])
		}
	}

	// Context methods that return Logger - delegate to context tracer
	if recv != nil && typeutil.IsContext(recv.Type()) && typeutil.ReturnsLogger(callee) {
		if len(call.Call.Args) > 0 {
			return DelegateTo(t.context, call.Call.Args[0])
		}
	}

	return Continue()
}

// ContinueOnReceiverType implements Tracer.
func (t *EventTracer) ContinueOnReceiverType(recv *types.Var) bool {
	return recv != nil && typeutil.IsEvent(recv.Type())
}
