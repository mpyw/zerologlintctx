// callee.go classifies the function a call runs by what zerologapi tells of
// it.

package chain

import (
	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/ssaflow"
	"github.com/mpyw/zerologlintctx/internal/zerologapi"
)

// callee is the declared function or method a call runs, and what it does in
// a log chain.
//
//declscope:shared // checker.go classifies each call by it
type callee struct {
	ssaflow.Callee
	// api is what the declared function or method does in a log chain. For a
	// method value or a method expression, it is the method, not the wrapper
	// SSA calls.
	api zerologapi.Method
}

// calleeOf resolves the callee of a call, a go statement or a defer
// statement, as Closures.CalleeOf does, and classifies it.
//
//declscope:shared // event.go and tracing.go resolve each call through it
func calleeOf(cc *ssa.CallCommon, closures *ssaflow.Closures) (callee, bool) {
	c, ok := closures.CalleeOf(cc)
	if !ok {
		return callee{}, false
	}
	return callee{Callee: c, api: zerologapi.Classify(c.Func)}, true
}

// readAtCall returns c with its receiver read at call, which runs it, in
// the closure that reads it from a variable it captures: the struct of a
// promoted method as the closure loads it, or else the value a method value
// is bound to.
//
//declscope:shared // tracing.go asks the Ctx calls in place in the closure by it
func (c callee) readAtCall(call ssa.Instruction) callee {
	c.Read = nil
	if c.Embedded == nil {
		return c
	}
	if local, ok := ssaflow.CalleeOf(call.(ssa.CallInstruction).Common()); ok {
		c.Callee = local
	}
	return c
}

// returnsSameEvent reports whether c is a method of *zerolog.Event that
// returns the event it runs on. Every one that returns an event does, except
// Event.CreateDict, which returns a new event for a dict.
//
//declscope:shared // event.go follows a chain of methods on one event back by it
func (c callee) returnsSameEvent() bool {
	return c.api.Recv == zerologapi.TypeEvent && c.api.SameAsRecv
}
