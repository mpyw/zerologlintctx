// Package typeutil provides type checking utilities for zerolog and context types.
//
// # Type Hierarchy
//
// The package handles three main zerolog types and their relationships:
//
//	zerolog.Logger
//	    │
//	    ├── .Info(), .Debug(), .Error(), ...  → *zerolog.Event
//	    │
//	    └── .With()                           → zerolog.Context
//	                                                │
//	                                                └── .Logger() → zerolog.Logger
//
// # Method Classification Strategy
//
// Instead of hardcoding method names, we use return type analysis:
//
//	ReturnsEvent(fn)   → identifies Logger.Info, Logger.Debug, etc.
//	ReturnsLogger(fn)  → identifies Context.Logger, zerolog.Ctx
//	ReturnsContext(fn) → identifies Logger.With
//	ReturnsVoid(fn)    → identifies Event terminators (Msg, Send, etc.)
//
// This approach is more robust against API changes and works with custom wrappers.
package typeutil

import (
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"
)

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

// CtxMethod is the method name for setting context.
const CtxMethod = "Ctx"

// =============================================================================
// Zerolog Type Checking
// =============================================================================

// IsEvent checks if the type is *zerolog.Event.
func IsEvent(t types.Type) bool {
	return isNamedType(t, zerologPkgPath, eventType)
}

// IsContext checks if the type is zerolog.Context.
func IsContext(t types.Type) bool {
	return isNamedType(t, zerologPkgPath, contextType)
}

// IsLogger checks if the type is zerolog.Logger.
func IsLogger(t types.Type) bool {
	return isNamedType(t, zerologPkgPath, loggerType)
}

// =============================================================================
// Function Checking
// =============================================================================

// IsCtxFunc returns true for zerolog.Ctx() or log.Ctx() functions.
func IsCtxFunc(fn *ssa.Function) bool {
	if fn.Name() != CtxMethod {
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

// ReturnsEvent checks if a function returns *zerolog.Event.
func ReturnsEvent(fn *ssa.Function) bool {
	return returnsSingleType(fn, IsEvent)
}

// ReturnsLogger checks if a function returns zerolog.Logger.
func ReturnsLogger(fn *ssa.Function) bool {
	return returnsSingleType(fn, IsLogger)
}

// ReturnsContext checks if a function returns zerolog.Context.
func ReturnsContext(fn *ssa.Function) bool {
	return returnsSingleType(fn, IsContext)
}

// returnsSingleType checks if a function returns exactly one value matching the predicate.
func returnsSingleType(fn *ssa.Function, predicate func(types.Type) bool) bool {
	results := fn.Signature.Results()
	if results.Len() != 1 {
		return false
	}
	return predicate(results.At(0).Type())
}

// ReturnsVoid checks if a function has no return values.
func ReturnsVoid(fn *ssa.Function) bool {
	return fn.Signature.Results().Len() == 0
}

// IsDirectLoggingMethod checks if a function is a direct logging method on Logger
// that bypasses the Event chain (Print, Printf, Println).
func IsDirectLoggingMethod(fn *ssa.Function, recv *types.Var) bool {
	if recv == nil || !IsLogger(recv.Type()) {
		return false
	}
	if !ReturnsVoid(fn) {
		return false
	}
	return strings.HasPrefix(fn.Name(), "Print")
}

// IsDirectLoggingFunc checks if a function is a direct logging function from
// zerolog/log package that bypasses the Event chain (log.Print, log.Printf).
func IsDirectLoggingFunc(fn *ssa.Function) bool {
	pkg := fn.Package()
	if pkg == nil || pkg.Pkg == nil {
		return false
	}
	if pkg.Pkg.Path() != zerologLogPath {
		return false
	}
	if !ReturnsVoid(fn) {
		return false
	}
	return strings.HasPrefix(fn.Name(), "Print")
}

// =============================================================================
// Context Type Checking
// =============================================================================

// IsContextType checks if the type is context.Context.
func IsContextType(t types.Type) bool {
	return isNamedType(t, contextPkgPath, "Context")
}

// =============================================================================
// Type Utilities
// =============================================================================

// unwrapPointer returns the element type if t is a pointer, otherwise returns t.
func unwrapPointer(t types.Type) types.Type {
	if ptr, ok := t.(*types.Pointer); ok {
		return ptr.Elem()
	}
	return t
}

// isNamedType checks if the type matches the given package path and type name.
// Handles pointer types transparently.
//
// Example type resolution:
//
//	*zerolog.Event  →  unwrap pointer  →  zerolog.Event
//	                                           │
//	                   check pkg path: "github.com/rs/zerolog"
//	                   check type name: "Event"
func isNamedType(t types.Type, pkgPath, typeName string) bool {
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
