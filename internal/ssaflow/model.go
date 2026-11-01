// model.go is what the flow is told of a library: what its types and
// methods do with memory that their signatures do not tell.

package ssaflow

import "go/types"

// Model tells the flow what a library does with memory that the signatures
// of its types and methods do not tell.
type Model interface {
	// IsHandle reports whether a pointer of type ptr is a value of its own,
	// not a variable a read goes through to what is stored there, such as
	// *zerolog.Event.
	IsHandle(ptr types.Type) bool
	// LeavesReceiver reports whether a call of fn, a method, leaves what a
	// read of its receiver sees: it neither reads the receiver nor changes
	// what a read of it is traced for, as Logger.UpdateContext changes the
	// fields of a Logger but not its context.
	LeavesReceiver(fn *types.Func) bool
}
