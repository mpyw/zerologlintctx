// place.go is the model of an address in memory that store.go, match.go,
// aggregate.go, tracing.go and context.go share: a place is a root, the
// variable or pointer the address is in, and the fields and elements selected
// from it. Whether two selections are the same is decided here only.
//
// It joins the core namespace because most files of internal/chain speak of
// places, as they stand on value.go.
//
//declscope:core
//declscope:shared

package chain

import (
	"fmt"
	"go/constant"
	"go/token"
	"slices"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// sel is one selection in a place: a field of a struct, or an element of an
// array, through an address or of a value.
type sel struct {
	// field is the index of the field, for a field.
	field int
	// index is the index of the element, when it is a constant. It is nil
	// for a field, and for an element whose index is not a constant.
	//
	//declscope:private
	index constant.Value
	// operand is the index of an element as the selection gives it: the
	// constant, or the value it is computed from. It is nil for a field.
	//
	//declscope:private
	operand ssa.Value
}

// same reports whether s and o select the same field, or the same constant
// element, each from its own aggregate. An element whose index is not a
// constant may be any element, so it is the same as no selection, not even
// one with the same index.
func (s sel) same(o sel) bool {
	if s.variable() || o.variable() {
		return false
	}
	return s.field == o.field && s.index == o.index
}

// variable reports whether s is an element whose index is not a constant.
//
//declscope:private
func (s sel) variable() bool {
	return s.operand != nil && s.index == nil
}

// place is an address: root, and the selections from it, outermost first.
//
//	&t0.inner.event  →  root t0, path [.inner, .event]
type place struct {
	root ssa.Value
	path []sel
}

// placeKey is a place in a form that can be compared with ==, or be a map
// key.
type placeKey struct {
	//declscope:private
	root ssa.Value
	//declscope:private
	path string
}

// key returns a key that is the same for two places with the same root and
// the same selections. An element is spelled by its operand: a constant by its
// value and type, and any other index by the value, which only matches
// itself.
//
// The key of a constant element is not what same compares: same ignores the
// type of the constant, and the key does not.
func (p place) key() placeKey {
	var b strings.Builder
	for _, s := range p.path {
		if s.operand == nil {
			fmt.Fprintf(&b, ".%d", s.field)
			continue
		}
		fmt.Fprintf(&b, "[%s]", s.operand.Name())
	}
	return placeKey{root: p.root, path: b.String()}
}

// with returns p with rest selected further, in a path of its own.
func (p place) with(rest ...sel) place {
	return place{root: p.root, path: append(slices.Clone(p.path), rest...)}
}

// prefix returns the place that the first n selections of p select.
func (p place) prefix(n int) place {
	return place{root: p.root, path: p.path[:n]}
}

// prefixOf reports whether p has the root of q, and selects the same as the
// first selections of q do, as many as p has.
func (p place) prefixOf(q place) bool {
	if p.root != q.root || len(p.path) > len(q.path) {
		return false
	}
	for i, s := range p.path {
		if !s.same(q.path[i]) {
			return false
		}
	}
	return true
}

// same reports whether p and q have the same root, and select the same.
func (p place) same(q place) bool {
	return len(p.path) == len(q.path) && p.prefixOf(q)
}

// placeOf returns the place of addr: the value it is in, after every field
// and element of an address it selects. tracked reports whether isTrackedRoot
// holds for the root.
func placeOf(addr ssa.Value) (p place, tracked bool) {
	for {
		from, s, ok := addrSel(addr)
		if !ok {
			break
		}
		p.path = append(p.path, s)
		addr = from
	}
	slices.Reverse(p.path)
	p.root = addr
	return p, isTrackedRoot(addr)
}

// isTrackedRoot reports whether the stores to a place with root v can all be
// seen in the function alone: v is a local variable, a parameter, or a
// variable that a closure captures. A global, or a pointer the function
// loads, can be changed anywhere.
func isTrackedRoot(v ssa.Value) bool {
	switch v.(type) {
	case *ssa.Alloc, *ssa.Parameter, *ssa.FreeVar:
		return true
	}
	return false
}

// addrSel returns the selection v makes, a field or element of an address,
// and the address it selects from. ok is false for any other value.
func addrSel(v ssa.Value) (from ssa.Value, s sel, ok bool) {
	switch v := v.(type) {
	case *ssa.FieldAddr:
		return v.X, sel{field: v.Field}, true
	case *ssa.IndexAddr:
		return v.X, elemSel(v.Index), true
	}
	return nil, sel{}, false
}

// valueSel returns the selection v makes, a field of a struct value or an
// element of an array value, and the value it selects from. ok is false for
// any other value.
func valueSel(v ssa.Value) (from ssa.Value, s sel, ok bool) {
	switch v := v.(type) {
	case *ssa.Field:
		return v.X, sel{field: v.Field}, true
	case *ssa.Index:
		return v.X, elemSel(v.Index), true
	}
	return nil, sel{}, false
}

// elemSel returns the selection of the element at index.
//
//declscope:private
func elemSel(index ssa.Value) sel {
	s := sel{operand: index}
	if c, ok := index.(*ssa.Const); ok {
		s.index = c.Value
	}
	return s
}

// asLoad returns v as a load, `*x`, and reports whether it is one.
func asLoad(v ssa.Value) (*ssa.UnOp, bool) {
	load, ok := v.(*ssa.UnOp)
	return load, ok && load.Op == token.MUL
}
