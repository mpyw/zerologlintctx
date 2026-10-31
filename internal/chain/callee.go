// callee.go resolves the function a call runs, seen through method values and
// method expressions, and what zerologapi tells of it.

package chain

import (
	"go/types"

	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/zerologapi"
)

// callee is the declared function or method a call runs.
//
//declscope:shared // checker.go classifies each call by it
type callee struct {
	// recv is the receiver the method runs on, or nil for a function. For a
	// method expression of a promoted method, it is the struct the receiver
	// is embedded in, or a pointer to it, and embedded is set.
	recv ssa.Value
	// embedded are the indexes of the fields, one per embedded struct, that
	// lead from recv to the receiver, for a method expression of a promoted
	// method:
	//
	//	type myEvent struct{ *zerolog.Event }
	//	myEvent.Msg(me, "x") // (myEvent).Msg$thunk(me, "x"): embedded [0]
	//
	// It is nil for any other call.
	embedded []int
	// api is what the declared function or method does in a log chain. For a
	// method value or a method expression, it is the method, not the wrapper
	// SSA calls.
	api zerologapi.Method
}

// calleeOf resolves the callee of a call, a go statement or a defer
// statement. It reports false when the callee is not known statically, or
// is not a declared function, such as a closure.
//
// A method is called in one of three forms, and each passes its receiver in
// a different place:
//
//	e.Msg("x")       // (*Event).Msg(e, "x")        receiver in Args[0]
//	m := e.Msg       // (*Event).Msg$bound(e)       receiver in Bindings[0]
//	m("x")           // t0("x")
//	f := (*Event).Msg
//	f(e, "x")        // (*Event).Msg$thunk(e, "x")  receiver in Args[0]
//
// A method expression of a method promoted through an embedded field passes
// the struct that embeds the receiver, and embedded leads to it.
//
//declscope:shared // checker.go resolves every call through it
func calleeOf(cc *ssa.CallCommon) (callee, bool) {
	sfn := cc.StaticCallee()
	if sfn == nil {
		return callee{}, false
	}
	fn, ok := sfn.Object().(*types.Func)
	if !ok {
		return callee{}, false
	}
	c := callee{api: zerologapi.Classify(fn)}
	if fn.Signature().Recv() == nil {
		return c, true
	}
	if mc, bound := cc.Value.(*ssa.MakeClosure); bound {
		if len(mc.Bindings) == 0 {
			return callee{}, false
		}
		c.recv = mc.Bindings[0]
		return c, true
	}
	if len(cc.Args) == 0 {
		return callee{}, false
	}
	c.recv = cc.Args[0]
	c.embedded = calleeEmbedded(c.recv.Type(), fn)
	return c, true
}

// calleeEmbedded returns the indexes of the embedded fields that lead from a
// value of type recv to the receiver of fn, when fn is a method promoted to
// recv, or nil when recv is the receiver type of fn itself.
func calleeEmbedded(recv types.Type, fn *types.Func) []int {
	if types.Identical(recv, fn.Signature().Recv().Type()) {
		return nil
	}
	obj, index, _ := types.LookupFieldOrMethod(recv, false, fn.Pkg(), fn.Name())
	if obj != fn || len(index) < 2 {
		return nil
	}
	return index[:len(index)-1]
}

// calleeReceiver returns the value a call runs on, as tracing follows it: the
// receiver of a method, or the first argument of a call whose callee is not
// known statically. It is nil for a call of a function, such as a helper
// that may return a value unrelated to its arguments.
//
//declscope:shared // value.go walks back through the receivers of calls
func calleeReceiver(cc *ssa.CallCommon) ssa.Value {
	if c, ok := calleeOf(cc); ok {
		return c.recv
	}
	if cc.StaticCallee() == nil && len(cc.Args) > 0 {
		return cc.Args[0]
	}
	return nil
}

// returnsSameEvent reports whether c is a method of *zerolog.Event that
// returns the event it runs on. Every one that returns an event does, except
// Event.CreateDict, which returns a new event for a dict.
//
//declscope:shared // tracing.go follows a chain of methods on one event by it
func (c callee) returnsSameEvent() bool {
	return c.api.Recv == zerologapi.TypeEvent && c.api.SameAsRecv
}
