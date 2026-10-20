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
	if len(phi.Edges) == 0 {
		return false
	}

	hasValidEdge := false
	for _, edge := range phi.Edges {
		// Skip edges that would cycle back to this Phi
		if phiEdgeLeadsBack(edge, phi, visited) {
			continue
		}

		// Skip nil constant edges
		if isNilConst(edge) {
			continue
		}

		hasValidEdge = true

		// Clone visited for independent tracing of each branch
		edgeVisited := maps.Clone(visited)
		if !tr.traceValue(edge, t, edgeVisited) {
			return false
		}
	}

	return hasValidEdge
}

// phiEdgeLeadsBack checks if tracing edge would eventually lead back to phi.
// Values in visited are not followed.
func phiEdgeLeadsBack(edge ssa.Value, phi *ssa.Phi, visited map[ssa.Value]bool) bool {
	isPhi := func(v ssa.Value) bool { return v == phi }
	return derivesFrom(edge, isPhi, unwrapInner, maps.Clone(visited))
}
