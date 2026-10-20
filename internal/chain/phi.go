// phi.go traces a Phi node, where control flow paths merge, and tells which
// of its edges lead back to the Phi itself.

package chain

import (
	"maps"

	"golang.org/x/tools/go/ssa"
)

// tracePhi handles SSA Phi nodes where multiple control flow paths merge.
//
// All edges must have context set for the Phi node to be considered valid.
// Cyclic edges and nil constants are skipped.
//
//declscope:shared // tracing.go hands each Phi to it
func (tr *tracer) tracePhi(phi *ssa.Phi, t tracerType, visited map[ssa.Value]bool) bool {
	valid, ok := tr.tracePhiEdges(phi, phi, t, visited, make(map[*ssa.Phi]bool))
	return ok && valid
}

// tracePhiEdges traces the edges of phi, which is origin or a Phi that an
// edge of origin leads back through. ok is false when an edge has no context,
// and valid reports that some edge was traced.
//
// An edge that leads back to origin is an update in a loop, and is skipped.
// When it is a Phi itself, only its own edges that lead back are, and the
// others are traced: a branch in the loop merges there.
//
//	e := logger.Info().Ctx(ctx)
//	for i := 0; i < n; i++ {    // origin = Phi(Ctx, P1)
//	    if cond {
//	        e = logger.Warn()    // P1 = Phi(origin, Warn): Warn is traced
//	    }
//	}
func (tr *tracer) tracePhiEdges(
	phi, origin *ssa.Phi,
	t tracerType,
	visited map[ssa.Value]bool,
	seen map[*ssa.Phi]bool,
) (valid, ok bool) {
	for _, edge := range phi.Edges {
		if isNilConst(edge) {
			continue
		}
		if phiEdgeLeadsBack(edge, origin, visited) {
			inner, isPhi := edge.(*ssa.Phi)
			if !isPhi || inner == origin || seen[inner] {
				continue
			}
			seen[inner] = true
			innerValid, innerOK := tr.tracePhiEdges(inner, origin, t, visited, seen)
			if !innerOK {
				return false, false
			}
			valid = valid || innerValid
			continue
		}

		// Clone visited for independent tracing of each branch, unless the
		// edge was found to have context before
		if !tr.foundBefore(edge, t) && !tr.traceValue(edge, t, maps.Clone(visited)) {
			return false, false
		}
		valid = true
	}
	return valid, true
}

// phiEdgeLeadsBack checks if tracing edge would eventually lead back to phi.
// Values in visited are not followed.
func phiEdgeLeadsBack(edge ssa.Value, phi *ssa.Phi, visited map[ssa.Value]bool) bool {
	isPhi := func(v ssa.Value) bool { return v == phi }
	return derivesFrom(edge, isPhi, unwrapInner, maps.Clone(visited))
}
