// edge.go answers whether one edge of a Phi leads back to the Phi itself.

package ssa

import (
	"maps"

	"golang.org/x/tools/go/ssa"
)

// edgeLeadsTo checks if tracing this edge would eventually lead back to target.
//
//declscope:package // tracing.go asks this
func edgeLeadsTo(edge ssa.Value, target *ssa.Phi, visited map[ssa.Value]bool) bool {
	seen := maps.Clone(visited)
	return edgeLeadsToImpl(edge, target, seen)
}
func edgeLeadsToImpl(v ssa.Value, target *ssa.Phi, seen map[ssa.Value]bool) bool {
	if v == target {
		return true
	}
	if seen[v] {
		return false
	}
	seen[v] = true

	switch val := v.(type) {
	case *ssa.Call:
		if len(val.Call.Args) > 0 {
			return edgeLeadsToImpl(val.Call.Args[0], target, seen)
		}
		return false
	case *ssa.Phi:
		for _, edge := range val.Edges {
			if edgeLeadsToImpl(edge, target, seen) {
				return true
			}
		}
		return false
	}

	if inner := unwrapInner(v); inner != nil {
		return edgeLeadsToImpl(inner, target, seen)
	}

	return false
}
