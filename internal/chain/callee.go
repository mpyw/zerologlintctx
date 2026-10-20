// callee.go classifies the function a call goes to by what it is, where the
// signature alone cannot tell: its name and its package.

package chain

import (
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"

	"github.com/mpyw/zerologlintctx/internal/typeutil"
)

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

// isDirectLoggingCallee reports whether fn, called with receiver recv, logs
// without an Event chain: a Print method of Logger, or a Print function of
// zerolog/log. recv is nil for a call that has no receiver.
//
//declscope:shared // checker.go reports such a call
func isDirectLoggingCallee(fn *ssa.Function, recv *types.Var) bool {
	if !strings.HasPrefix(fn.Name(), "Print") || !typeutil.ReturnsVoid(fn.Signature) {
		return false
	}
	if recv != nil && typeutil.IsLogger(recv.Type()) {
		return true
	}
	return fn.Pkg != nil && typeutil.IsZerologLogPkg(fn.Pkg.Pkg)
}
