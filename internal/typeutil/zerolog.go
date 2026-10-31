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
//	ReturnsEvent(sig)   → identifies Logger.Info, Logger.Debug, etc.
//	ReturnsLogger(sig)  → identifies Context.Logger, zerolog.Ctx
//	ReturnsContext(sig) → identifies Logger.With
//	ReturnsVoid(sig)    → identifies Event terminators (Msg, Send, etc.)
//
// This approach is more robust against API changes and works with custom wrappers.
package typeutil

import (
	"go/types"
)

// CtxMethod is the method name for setting context.
const CtxMethod = "Ctx"

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
// Package Checking
// =============================================================================

// IsZerologPkg reports whether pkg is github.com/rs/zerolog or its global
// logger package github.com/rs/zerolog/log.
func IsZerologPkg(pkg *types.Package) bool {
	return pkg != nil && (pkg.Path() == zerologPkgPath || pkg.Path() == zerologLogPath)
}

// IsZerologLogPkg reports whether pkg is the global logger package
// github.com/rs/zerolog/log.
func IsZerologLogPkg(pkg *types.Package) bool {
	return pkg != nil && pkg.Path() == zerologLogPath
}

// =============================================================================
// Signature Classification
// =============================================================================

// ReturnsEvent checks if a signature returns *zerolog.Event.
func ReturnsEvent(sig *types.Signature) bool {
	return returnsSingleType(sig, IsEvent)
}

// ReturnsLogger checks if a signature returns zerolog.Logger.
func ReturnsLogger(sig *types.Signature) bool {
	return returnsSingleType(sig, IsLogger)
}

// ReturnsContext checks if a signature returns zerolog.Context.
func ReturnsContext(sig *types.Signature) bool {
	return returnsSingleType(sig, IsContext)
}

// returnsSingleType checks if a signature returns exactly one value matching the predicate.
func returnsSingleType(sig *types.Signature, predicate func(types.Type) bool) bool {
	results := sig.Results()
	if results.Len() != 1 {
		return false
	}
	return predicate(results.At(0).Type())
}

// ReturnsVoid checks if a signature has no return values.
func ReturnsVoid(sig *types.Signature) bool {
	return sig.Results().Len() == 0
}

// =============================================================================
// Context Type Checking
// =============================================================================

// IsContextType checks if the type is context.Context, or a pointer to it.
// A type parameter counts when its constraint is, or embeds,
// context.Context, as for C in func f[C context.Context](ctx C).
func IsContextType(t types.Type) bool {
	if tp, ok := types.Unalias(unwrapPointer(t)).(*types.TypeParam); ok {
		return constrainsToContext(tp.Constraint())
	}
	return isNamedType(t, contextPkgPath, "Context")
}

// constrainsToContext reports whether the constraint c is context.Context, or
// an interface that embeds it at any depth. A pointer, as in
// func f[C *context.Context](ctx C), does not count: ctx is not a context.
func constrainsToContext(c types.Type) bool {
	if _, isPtr := types.Unalias(c).(*types.Pointer); isPtr {
		return false
	}
	if isNamedType(c, contextPkgPath, "Context") {
		return true
	}
	iface, ok := c.Underlying().(*types.Interface)
	if !ok {
		return false
	}
	for e := range iface.EmbeddedTypes() {
		if constrainsToContext(e) {
			return true
		}
	}
	return false
}

// =============================================================================
// Type Utilities
// =============================================================================

// unwrapPointer returns the element type if t is a pointer, otherwise returns t.
// Aliases are resolved first, so `type E = *zerolog.Event` unwraps to zerolog.Event.
func unwrapPointer(t types.Type) types.Type {
	if ptr, ok := types.Unalias(t).(*types.Pointer); ok {
		return ptr.Elem()
	}
	return t
}

// isNamedType checks if the type matches the given package path and type name.
// Handles pointer types and aliases transparently.
//
// Aliases must be resolved explicitly: go/types always represents an alias
// declaration as a *types.Alias node, so a bare *types.Named assertion would
// silently miss aliased zerolog types.
//
// Example type resolution:
//
//	*zerolog.Event  →  unwrap pointer  →  zerolog.Event
//	                                           │
//	                   check pkg path: "github.com/rs/zerolog"
//	                   check type name: "Event"
//
//	type E = zerolog.Event
//	*E              →  unwrap pointer  →  E  →  unalias  →  zerolog.Event
func isNamedType(t types.Type, pkgPath, typeName string) bool {
	named, ok := types.Unalias(unwrapPointer(t)).(*types.Named)
	if !ok {
		return false
	}

	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}

	return obj.Pkg().Path() == pkgPath && obj.Name() == typeName
}
