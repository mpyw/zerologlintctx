// Package tracer provides the Strategy Pattern implementation for tracing
// zerolog values through SSA form.
//
// The tracing system follows SSA values backwards to find if context was set.
// It uses three tracers for zerolog types:
//
//   - EventTracer   - traces *zerolog.Event values
//   - LoggerTracer  - traces zerolog.Logger values
//   - ContextTracer - traces zerolog.Context values (from With())
//
// Each tracer implements the Tracer interface with type-specific context checks.
// Cross-type delegation happens when values flow between types.
package tracer

import "golang.org/x/tools/go/ssa"

// Result represents the outcome of checking a call for context.
// Using a struct instead of multiple return values makes the API clearer
// and prevents invalid state combinations.
type Result struct {
	kind        resultKind
	delegate    Tracer
	delegateVal ssa.Value
}

type resultKind int

const (
	// resultFound indicates context was definitely found.
	resultFound resultKind = iota
	// resultDelegate indicates tracing should continue with another tracer.
	resultDelegate
	// resultContinue indicates tracing should continue with the current tracer.
	resultContinue
)

// Found creates a result indicating context was found.
func Found() Result {
	return Result{kind: resultFound}
}

// DelegateTo creates a result indicating delegation to another tracer.
func DelegateTo(t Tracer, v ssa.Value) Result {
	return Result{kind: resultDelegate, delegate: t, delegateVal: v}
}

// Continue creates a result indicating tracing should continue.
func Continue() Result {
	return Result{kind: resultContinue}
}

// IsFound returns true if context was found.
func (r Result) IsFound() bool {
	return r.kind == resultFound
}

// IsDelegate returns true if delegation is needed.
func (r Result) IsDelegate() bool {
	return r.kind == resultDelegate
}

// Delegate returns the delegate tracer and value.
func (r Result) Delegate() (Tracer, ssa.Value) {
	return r.delegate, r.delegateVal
}
