// callee.go resolves the function a call runs, seen through method values and
// method expressions, and classifies a callee by what it is where the
// signature alone cannot tell: its name and its package.

package chain

import (
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/typeutil"
)

// callee is the declared function or method a call runs.
//
//declscope:shared // checker.go classifies each call by it
type callee struct {
	// recv is the receiver the method runs on, or nil for a function.
	recv ssa.Value
	// fn is the declared function or method. For a method value or a method
	// expression, it is the method, not the wrapper SSA calls.
	//
	//declscope:private
	fn *types.Func
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
	c := callee{fn: fn}
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
	return c, true
}

// endsEventChain reports whether c ends an Event chain: a method of
// *zerolog.Event that returns nothing, such as Msg, Msgf, MsgFunc or Send.
//
//declscope:shared // checker.go traces the chain such a call ends
func (c callee) endsEventChain() bool {
	recv := c.fn.Signature().Recv()
	return recv != nil && typeutil.IsEvent(recv.Type()) && typeutil.ReturnsVoid(c.fn.Signature())
}

// logsDirectly reports whether c logs without an Event chain: a Print method
// of Logger, or a Print function of zerolog/log.
//
//declscope:shared // checker.go reports such a call
func (c callee) logsDirectly() bool {
	sig := c.fn.Signature()
	if !strings.HasPrefix(c.fn.Name(), "Print") || !typeutil.ReturnsVoid(sig) {
		return false
	}
	if recv := sig.Recv(); recv != nil {
		return typeutil.IsLogger(recv.Type())
	}
	return typeutil.IsZerologLogPkg(c.fn.Pkg())
}

// isCtxCallee reports whether fn is zerolog.Ctx or log.Ctx, or a Ctx method
// declared in zerolog.
//
//declscope:shared // tracing.go treats such a call as a context source
func isCtxCallee(fn *ssa.Function) bool {
	if fn.Name() != typeutil.CtxMethod || fn.Pkg == nil {
		return false
	}
	return typeutil.IsZerologPkg(fn.Pkg.Pkg)
}
