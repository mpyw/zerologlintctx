// verdict.go is the answer a trace gives for a value: it has context, it
// does not, or it adds no source at all, and the two rules that combine the
// answers of several sources.
//
// It joins the core namespace because every file that traces a value answers
// with a verdict.
//
//declscope:core
//declscope:shared

package chain

import (
	"iter"
	"slices"
)

// verdict is what a trace found for a value: whether every source of the
// value has context. A source that adds nothing, such as a nil event, or a
// value already being traced, which a cycle reaches again, is neutral.
type verdict uint8

const (
	// neutral is a value that adds no source: a nil event, which logs
	// nothing, or a value reached again through a cycle, which adds no source
	// that the trace in progress does not already see.
	neutral verdict = iota
	// yes is a value that has context.
	yes
	// no is a value that does not have context, or that is not known.
	no
)

// verdictOf returns yes for true, and no for false.
func verdictOf(found bool) verdict {
	if found {
		return yes
	}
	return no
}

// all combines the verdicts of the sources of one value, each of which the
// value may be: no when one is no, yes when there is at least one yes and
// the rest are neutral, and neutral when every one is. It stops at the first
// no, so a source after it is not traced.
func all(seq iter.Seq[verdict]) verdict {
	result := neutral
	for v := range seq {
		switch v {
		case no:
			return no
		case yes:
			result = yes
		}
	}
	return result
}

// some combines the verdicts of the ways to find one value, any of which will
// do: yes when one is yes, no when there is at least one no and the rest are
// neutral, and neutral when every one is. It stops at the first yes.
func some(seq iter.Seq[verdict]) verdict {
	result := neutral
	for v := range seq {
		switch v {
		case yes:
			return yes
		case no:
			result = no
		}
	}
	return result
}

// or returns yes when v is yes, and otherwise combines v and the verdict
// next gives, another way to find the same value, as some does.
func (v verdict) or(next func() verdict) verdict {
	if v == yes {
		return yes
	}
	return some(slices.Values([]verdict{v, next()}))
}

// verdictsOf yields the verdict of each value, by trace, one at a time, so
// that all or some can stop before tracing the rest.
func verdictsOf[T any](values []T, trace func(T) verdict) iter.Seq[verdict] {
	return func(yield func(verdict) bool) {
		for _, v := range values {
			if !yield(trace(v)) {
				return
			}
		}
	}
}
