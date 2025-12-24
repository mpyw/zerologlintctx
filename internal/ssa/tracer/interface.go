package tracer

import (
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// Tracer defines the strategy for tracing a specific zerolog type.
// Each implementation knows how to check for context on its type and
// when to delegate to other tracers.
type Tracer interface {
	// CheckContext examines a call and returns the tracing result.
	// Possible outcomes:
	//   - Found(): context is definitely set
	//   - DelegateTo(t, v): continue tracing value v with tracer t
	//   - Continue(): continue with current tracer on receiver
	CheckContext(call *ssa.Call, callee *ssa.Function, recv *types.Var) Result

	// ContinueOnReceiverType returns true if this tracer should continue
	// tracing when the receiver matches its type (for chained method calls).
	ContinueOnReceiverType(recv *types.Var) bool
}
