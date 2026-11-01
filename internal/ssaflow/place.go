// place.go is the model of an address in memory that store.go, match.go and
// resolve.go share, and internal/chain reads: a place is a root, the
// variable or pointer the address is in, and the fields and elements selected
// from it. Whether two selections are the same is decided here only.
//
// It joins the core namespace because most files of the package speak of
// places, as they stand on value.go.
//
//declscope:core
//declscope:shared

package ssaflow

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

// Place is an address: root, and the selections from it, outermost first.
//
//	&t0.inner.event  →  root t0, path [.inner, .event]
type Place struct {
	root ssa.Value
	path []sel
}

// PlaceAt returns the place of root itself, with no selections.
func PlaceAt(root ssa.Value) Place {
	return Place{root: root}
}

// PlaceKey is a place in a form that can be compared with ==, or be a map
// key.
type PlaceKey struct {
	//declscope:private
	root ssa.Value
	//declscope:private
	path string
}

// Key returns a Key that is the same for two places with the same root and
// the same selections. An element is spelled by its operand: a constant by its
// value and type, and any other index by the value, which only matches
// itself.
//
// The Key of a constant element is not what same compares: same ignores the
// type of the constant, and the Key does not.
func (p Place) Key() PlaceKey {
	var b strings.Builder
	for _, s := range p.path {
		if s.operand == nil {
			fmt.Fprintf(&b, ".%d", s.field)
			continue
		}
		fmt.Fprintf(&b, "[%s]", s.operand.Name())
	}
	return PlaceKey{root: p.root, path: b.String()}
}

// rooted returns k with root in place of its root, and the same selections.
func (k PlaceKey) rooted(root ssa.Value) PlaceKey {
	return PlaceKey{root: root, path: k.path}
}

// base returns the root of k.
func (k PlaceKey) base() ssa.Value {
	return k.root
}

// Variable reports whether p selects an element whose index is not a
// constant, which may be any element.
func (p Place) Variable() bool {
	return slices.ContainsFunc(p.path, sel.variable)
}

// with returns p with rest selected further, in a path of its own.
func (p Place) with(rest ...sel) Place {
	return Place{root: p.root, path: append(slices.Clone(p.path), rest...)}
}

// WithFields returns p with the fields selected further, one after another,
// in a path of its own.
func (p Place) WithFields(fields []int) Place {
	at := p.with()
	for _, index := range fields {
		at.path = append(at.path, sel{field: index})
	}
	return at
}

// PickedBy returns the root of p when it is a Phi, which picks one of several
// pointers on each path.
func (p Place) PickedBy() (*ssa.Phi, bool) {
	phi, ok := p.root.(*ssa.Phi)
	return phi, ok
}

// PickedAt returns p with the i-th edge of its root, a Phi (see
// Place.PickedBy), in place of the root: the place that a path that takes the
// edge reads.
//
//	t8 = phi [0: t1, 1: q] #h
//	t9 = &t8.e              // the place t8.e: t1.e on edge 0, q.e on edge 1
func (p Place) PickedAt(i int) Place {
	return Place{root: p.root.(*ssa.Phi).Edges[i], path: p.path}
}

// prefixOf reports whether p has the root of q, and selects the same as the
// first selections of q do, as many as p has.
func (p Place) prefixOf(q Place) bool {
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
//   - Two elements at an index that is not a constant, whose operands once
//     takes for one value, everywhere they are compared: they are the same
//     element.
//   - Any other pair of elements, one of them at an index that is not a
//     constant, may be the same element, or not. That makes it coverMay.
func (p Place) covers(q Place, once func(a, b ssa.Value) bool) placeCover {
	if p.root != q.root || len(p.path) > len(q.path) {
		return coverNone
	}
	cover := coverMust
	for i, s := range p.path {
		o := q.path[i]
		switch {
		case s.variable() && o.variable() && once(s.operand, o.operand):
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
func (p Place) overlaps(q Place) bool {
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

// PlaceOf returns the place of addr: the value it is in, after every field
// and element of an address it selects. tracked reports whether isTrackedRoot
// holds for the root.
func PlaceOf(addr ssa.Value) (p Place, tracked bool) {
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
//
//declscope:private
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

// LoadedRead is a read of a struct or array value loaded whole from an
// address: the place selected from that address, and the load. Through are
// the edges of Phis the value is taken through on the way from the load, the
// one closest to the selection first.
type LoadedRead struct {
	At      Place
	Load    *ssa.UnOp
	Through []LoadedEdge
}

// LoadedEdge is the edge at Index of Phi.
type LoadedEdge struct {
	Phi   *ssa.Phi
	Index int
}

// loadedReadsMax bounds the reads LoadedSelection finds through Phis. With
// more, ok is false.
//
//declscope:private
const loadedReadsMax = 64

// LoadedSelection returns where v, a field of a struct value or an element of
// an array value, selected from one loaded whole from an address, is read
// from: the same selections from that address, and the load. ok is false when
// v does not select from a load:
//
//	t8 = *t4                // the whole struct, loaded
//	t9 = t8.Logger          // v, read from t4.Logger
//
// A value selected from a Phi of such values is read from each edge, at the
// same selections, so there is one read for each way through the Phis to a
// load. An edge that is a constant, the zero value, has nothing to read, and
// one that leads back to a Phi on the way adds nothing new:
//
//	t17 = phi [0: t12, 1: t16]  // t12 = *t10 ; t16 = *t14
//	t18 = t17.Logger            // read from t10.Logger and t14.Logger
//
// ok is false when an edge is neither a load, a selection from one, nor
// such a Phi, or when there are more than loadedReadsMax reads.
func LoadedSelection(v ssa.Value) (reads []LoadedRead, ok bool) {
	from, path := selectionsOf(v)
	if len(path) == 0 {
		return nil, false
	}
	if !appendLoadedReads(&reads, from, path, nil) {
		return nil, false
	}
	return reads, true
}

// selectionsOf returns the value v selects fields and elements from, by
// value, and the selections, first to last.
//
//declscope:private
func selectionsOf(v ssa.Value) (from ssa.Value, path []sel) {
	for {
		x, s, isSel := valueSel(v)
		if !isSel {
			break
		}
		path = append(path, s)
		v = x
	}
	slices.Reverse(path)
	return v, path
}

// appendLoadedReads appends to reads where path, selections from the value
// v, is read from, as LoadedSelection does, and reports whether it could
// tell. through are the edges taken to v.
//
//declscope:private
func appendLoadedReads(reads *[]LoadedRead, v ssa.Value, path []sel, through []LoadedEdge) bool {
	if load, isLoad := AsLoad(v); isLoad {
		if len(*reads) == loadedReadsMax {
			return false
		}
		base, _ := PlaceOf(load.X)
		*reads = append(*reads, LoadedRead{At: base.with(path...), Load: load, Through: slices.Clone(through)})
		return true
	}
	phi, isPhi := v.(*ssa.Phi)
	if !isPhi {
		return false
	}
	if slices.ContainsFunc(through, func(e LoadedEdge) bool { return e.Phi == phi }) {
		return true // Back to a Phi on the way
	}
	for i, edge := range phi.Edges {
		if _, isConst := edge.(*ssa.Const); isConst {
			continue // The zero value has nothing to read
		}
		from, more := selectionsOf(edge)
		if !appendLoadedReads(reads, from, append(more, path...), append(slices.Clip(through), LoadedEdge{phi, i})) {
			return false
		}
	}
	return true
}

// AsLoad returns v as a load, `*x`, and reports whether it is one.
func AsLoad(v ssa.Value) (*ssa.UnOp, bool) {
	load, ok := v.(*ssa.UnOp)
	return load, ok && load.Op == token.MUL
}

// withEmbedded returns at, the address of a struct of type typ, with the
// embedded fields selected from it, one after another, up to the first that
// is a pointer to a struct, which is a field but the last. n is how many it
// selected, and field is the type of the last of them.
func (at Place) withEmbedded(typ types.Type, embedded []int) (_ Place, n int, field types.Type) {
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
