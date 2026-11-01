package chain

import (
	"go/constant"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// TestStorePickedThrough checks which edges of a Phi of pointers walkBefore
// takes for a picked variable, through edges that are Phis themselves.
func TestStorePickedThrough(t *testing.T) {
	leaf := ssa.NewConst(constant.MakeInt64(1), types.Typ[types.Int])
	other := ssa.NewConst(constant.MakeInt64(2), types.Typ[types.Int])
	inner := &ssa.Phi{Edges: []ssa.Value{other, leaf}}
	elsewhere := &ssa.Phi{Edges: []ssa.Value{other}}
	loop := &ssa.Phi{}
	loop.Edges = []ssa.Value{loop, elsewhere} // Leads back to itself, never to leaf
	outer := &ssa.Phi{Edges: []ssa.Value{inner, loop, leaf}}
	w := &storeWalk{picked: &storePick{phi: outer, leaf: leaf}}

	for _, tt := range []struct {
		name  string
		edge  ssa.Value
		need  *ssa.Phi
		takes bool
	}{
		{"the variable itself", leaf, nil, true},
		{"a Phi that holds it", inner, inner, true},
		{"a Phi that leads only back to itself and elsewhere", loop, nil, false},
		{"another value", other, nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			need, takes := w.pickedThrough(tt.edge)
			if need != tt.need || takes != tt.takes {
				t.Errorf("pickedThrough = (%p, %v), want (%p, %v)", need, takes, tt.need, tt.takes)
			}
		})
	}
}

// TestStorePointerOf checks where storePointerOf finds a pointer made: by
// the load of the struct value a field of it is selected from, through the
// fields of fields too.
func TestStorePointerOf(t *testing.T) {
	from := ssa.NewConst(constant.MakeInt64(1), types.Typ[types.Int])
	load := &ssa.UnOp{Op: token.MUL, X: from}
	field := &ssa.Field{X: &ssa.Field{X: load, Field: 1}}
	p := storePointerOf(field)
	if p.def != load || p.from != from || p.global {
		t.Errorf("storePointerOf = (%v, %v, %v), want (%v, %v, false)", p.def, p.from, p.global, load, from)
	}
}
