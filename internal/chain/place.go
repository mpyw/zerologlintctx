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
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// sel is one selection in a place: a field of a struct, or an element of an
// array, through an address or of a value.
type sel struct {
	// field is the index of the field, for a field.
	//
	//declscope:private
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

// withFields returns p with the fields selected further, one after another,
// in a path of its own.
func (p place) withFields(fields []int) place {
	at := p.with()
	for _, index := range fields {
		at.path = append(at.path, sel{field: index})
	}
	return at
}

// trailingFields returns the fields that the last selections of p select,
// as many as are fields and not elements, outermost first, and start, the
// number of selections before them.
func (p place) trailingFields() (start int, fields []int) {
	start = len(p.path)
	for start > 0 && p.path[start-1].operand == nil {
		start--
	}
	for _, s := range p.path[start:] {
		fields = append(fields, s.field)
	}
	return start, fields
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

// placeCover is how much of a place a store to another place writes.
type placeCover int

const (
	// coverNone is a store that writes none of the place.
	coverNone placeCover = iota
	// coverMay is a store that may write the place, or not: one to an
	// element at an index that is not known to be the same.
	coverMay
	// coverMust is a store that writes the place, every time it runs.
	coverMust
)

// covers tells how much of q a store to p writes. p must have the root of q,
// and select no more than q does. Each selection of p is compared with the
// one of q at the same depth:
//
//   - Two fields, or two constant elements, as sel.same compares them. Two
//     that differ make it coverNone.
//   - Two elements at an index that is not a constant, with the same operand,
//     for which once holds: the operand has one value everywhere it is
//     compared, so they are the same element.
//   - Any other pair of elements, one of them at an index that is not a
//     constant, may be the same element, or not. That makes it coverMay.
func (p place) covers(q place, once func(index ssa.Value) bool) placeCover {
	if p.root != q.root || len(p.path) > len(q.path) {
		return coverNone
	}
	cover := coverMust
	for i, s := range p.path {
		o := q.path[i]
		switch {
		case s.variable() && s.operand == o.operand && once(s.operand):
		case s.variable() || o.variable():
			cover = coverMay
		case !s.same(o):
			return coverNone
		}
	}
	return cover
}

// overlaps reports whether p and q have the same root, and may select memory
// in common: the selections of one start with those of the other. An element
// whose index is not a constant may be any element, so it may be the one the
// other selects.
func (p place) overlaps(q place) bool {
	if p.root != q.root {
		return false
	}
	for i := range min(len(p.path), len(q.path)) {
		s, o := p.path[i], q.path[i]
		if !s.variable() && !o.variable() && !s.same(o) {
			return false
		}
	}
	return true
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

// withEmbedded returns at, the address of a struct of type typ, with the
// embedded fields selected from it, one after another, up to the first that
// is a pointer to a struct, which is a field but the last. n is how many it
// selected, and field is the type of the last of them.
func (at place) withEmbedded(typ types.Type, embedded []int) (_ place, n int, field types.Type) {
	at = at.with()
	for i, index := range embedded {
		// Each embedded field but the last is a struct or a pointer to one
		typ = typ.Underlying().(*types.Struct).Field(index).Type()
		at.path = append(at.path, sel{field: index})
		if _, isPtr := typ.Underlying().(*types.Pointer); isPtr && i < len(embedded)-1 {
			return at, i + 1, typ
		}
	}
	return at, len(embedded), typ
}
