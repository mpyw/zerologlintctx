// callee.go resolves the declared function a call runs, and the receiver it
// runs on, seen through method values and method expressions.

package ssaflow

import (
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// Callee is the declared function or method a call runs.
type Callee struct {
	// Func is the declared function or method. For a method value or a
	// method expression, it is the method, not the wrapper SSA calls.
	Func *types.Func
	// Recv is the receiver the method runs on, or nil for a function. For a
	// method expression of a promoted method, it is the struct the receiver
	// is embedded in, or a pointer to it, and Embedded is set.
	Recv ssa.Value
	// Embedded are the indexes of the fields, one per embedded struct, that
	// lead from Recv to the receiver, for a method expression of a promoted
	// method:
	//
	//	type myEvent struct{ *zerolog.Event }
	//	myEvent.Msg(me, "x") // (myEvent).Msg$thunk(me, "x"): Embedded [0]
	//
	// It is nil for any other call.
	Embedded []int
}

// CalleeOf resolves the callee of a call, a go statement or a defer
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
// the struct that embeds the receiver, and Embedded leads to it.
func CalleeOf(cc *ssa.CallCommon) (Callee, bool) {
	sfn := cc.StaticCallee()
	if sfn == nil {
		return Callee{}, false
	}
	fn, ok := sfn.Object().(*types.Func)
	if !ok {
		return Callee{}, false
	}
	c := Callee{Func: fn}
	if fn.Signature().Recv() == nil {
		return c, true
	}
	if mc, bound := cc.Value.(*ssa.MakeClosure); bound {
		if len(mc.Bindings) == 0 {
			return Callee{}, false
		}
		c.Recv = mc.Bindings[0]
		return c, true
	}
	if len(cc.Args) == 0 {
		return Callee{}, false
	}
	c.Recv = cc.Args[0]
	c.Embedded = calleeEmbedded(c.Recv.Type(), fn)
	return c, true
}

// CalleeReceiver returns the value a call runs on, as tracing follows it: the
// receiver of a method, or the first argument of a call whose callee is not
// known statically. It is nil for a call of a function, such as a helper
// that may return a value unrelated to its arguments.
func CalleeReceiver(cc *ssa.CallCommon) ssa.Value {
	if c, ok := CalleeOf(cc); ok {
		return c.Recv
	}
	if cc.StaticCallee() == nil && len(cc.Args) > 0 {
		return cc.Args[0]
	}
	return nil
}

// EmbeddedStart returns where the struct c.Recv is, for c, a method
// expression of a promoted method that call runs: the address the struct is
// at, its type, and the instruction that reads it there. ok is false when the
// struct is a value that is not loaded from an address.
func (c Callee) EmbeddedStart(call ssa.Instruction) (addr ssa.Value, typ types.Type, read ssa.Instruction, ok bool) {
	if ptr, isPtr := c.Recv.Type().Underlying().(*types.Pointer); isPtr {
		return c.Recv, ptr.Elem(), call, true
	}
	load, isLoad := AsLoad(c.Recv)
	if !isLoad {
		return nil, nil, nil, false
	}
	return load.X, c.Recv.Type(), load, true
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
