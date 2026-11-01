// Package ssaflow finds where the values of a function in SSA form come from:
// what a read from memory can see, through the stores that reach it, the
// variables that closures capture, and the calls of func literals.
//
// It knows nothing of zerolog. A [Model] tells it what a library does with
// memory that the signatures of its types and methods do not tell.
// internal/chain judges each value the flow finds, by what zerologapi tells
// of zerolog.
//
// The entry is [Resolver]. It stands on [Place], the model of an address,
// on the walks of the control flow in walk.go, and on [Closures], the index
// of closures and func literals.
package ssaflow
