package internal

import (
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// =============================================================================
// Package Constants
// =============================================================================

// Package paths.
const (
	zerologPkgPath = "github.com/rs/zerolog"
	zerologLogPath = "github.com/rs/zerolog/log"
	contextPkgPath = "context"
)

// Type names.
const (
	eventType   = "Event"
	contextType = "Context"
	loggerType  = "Logger"
)

// Method names.
const (
	ctxMethod = "Ctx"
)

// =============================================================================
// Zerolog Type Checking
// =============================================================================

func isEvent(t types.Type) bool {
	return isZerologType(t, eventType)
}

func isContext(t types.Type) bool {
	return isZerologType(t, contextType)
}

func isLogger(t types.Type) bool {
	return isZerologType(t, loggerType)
}

func isZerologType(t types.Type, typeName string) bool {
	t = unwrapPointer(t)
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	return obj.Pkg().Path() == zerologPkgPath && obj.Name() == typeName
}

// =============================================================================
// Function Checking
// =============================================================================

// isCtxFunc returns true for zerolog.Ctx() or log.Ctx() functions.
func isCtxFunc(fn *ssa.Function) bool {
	if fn.Name() != ctxMethod {
		return false
	}
	pkg := fn.Package()
	if pkg == nil || pkg.Pkg == nil {
		return false
	}
	path := pkg.Pkg.Path()
	return path == zerologPkgPath || path == zerologLogPath
}

// =============================================================================
// Method Classification
// =============================================================================

// returnsEvent checks if a function returns *zerolog.Event.
// This is used to identify Logger methods that create Events (Info, Debug, Err, etc.)
// without hardcoding method names.
func returnsEvent(fn *ssa.Function) bool {
	return returnsSingleType(fn, isEvent)
}

// returnsLogger checks if a function returns zerolog.Logger.
// This is used to identify Context.Logger() without hardcoding method names.
func returnsLogger(fn *ssa.Function) bool {
	return returnsSingleType(fn, isLogger)
}

// returnsContext checks if a function returns zerolog.Context.
// This is used to identify Logger.With() without hardcoding method names.
func returnsContext(fn *ssa.Function) bool {
	return returnsSingleType(fn, isContext)
}

// returnsSingleType checks if a function returns exactly one value matching the predicate.
func returnsSingleType(fn *ssa.Function, predicate func(types.Type) bool) bool {
	results := fn.Signature.Results()
	if results.Len() != 1 {
		return false
	}
	return predicate(results.At(0).Type())
}

// returnsVoid checks if a function has no return values.
// This is used to identify Event terminator methods (Msg, Send, etc.)
func returnsVoid(fn *ssa.Function) bool {
	return fn.Signature.Results().Len() == 0
}

// isDirectLoggingMethod checks if a function is a direct logging method on Logger
// that bypasses the Event chain (Print, Printf, Println).
// Note: Logger.UpdateContext also returns void but is a configuration method, not logging.
// We use the "Print" prefix to distinguish logging methods.
func isDirectLoggingMethod(fn *ssa.Function, recv *types.Var) bool {
	if recv == nil || !isLogger(recv.Type()) {
		return false
	}
	if !returnsVoid(fn) {
		return false
	}
	return strings.HasPrefix(fn.Name(), "Print")
}

// isDirectLoggingFunc checks if a function is a direct logging function from
// zerolog/log package that bypasses the Event chain (log.Print, log.Printf).
func isDirectLoggingFunc(fn *ssa.Function) bool {
	pkg := fn.Package()
	if pkg == nil || pkg.Pkg == nil {
		return false
	}
	if pkg.Pkg.Path() != zerologLogPath {
		return false
	}
	if !returnsVoid(fn) {
		return false
	}
	return strings.HasPrefix(fn.Name(), "Print")
}

// =============================================================================
// Context Type Checking
// =============================================================================

// IsContextType checks if the type is context.Context.
func IsContextType(t types.Type) bool {
	return isNamedTypeFromType(t, contextPkgPath, "Context")
}

// unwrapPointer returns the element type if t is a pointer, otherwise returns t.
func unwrapPointer(t types.Type) types.Type {
	if ptr, ok := t.(*types.Pointer); ok {
		return ptr.Elem()
	}
	return t
}

// isNamedTypeFromType checks if the type matches the given package path and type name.
func isNamedTypeFromType(t types.Type, pkgPath, typeName string) bool {
	t = unwrapPointer(t)

	named, ok := t.(*types.Named)
	if !ok {
		return false
	}

	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}

	return obj.Pkg().Path() == pkgPath && obj.Name() == typeName
}
