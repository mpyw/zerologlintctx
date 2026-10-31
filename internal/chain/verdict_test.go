package chain

import (
	"iter"
	"slices"
	"testing"
)

func TestVerdictAll(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []verdict
		want verdict
	}{
		{"none", nil, neutral},
		{"neutral only", []verdict{neutral, neutral}, neutral},
		{"yes", []verdict{yes}, yes},
		{"yes and neutral", []verdict{neutral, yes, neutral}, yes},
		{"no", []verdict{no}, no},
		{"yes then no", []verdict{yes, no}, no},
		{"no then yes", []verdict{no, yes}, no},
		{"neutral and no", []verdict{neutral, no}, no},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := all(slices.Values(tt.in)); got != tt.want {
				t.Errorf("all(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestVerdictSome(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []verdict
		want verdict
	}{
		{"none", nil, neutral},
		{"neutral only", []verdict{neutral, neutral}, neutral},
		{"no", []verdict{no}, no},
		{"no and neutral", []verdict{neutral, no, neutral}, no},
		{"yes", []verdict{yes}, yes},
		{"no then yes", []verdict{no, yes}, yes},
		{"yes then no", []verdict{yes, no}, yes},
		{"neutral and yes", []verdict{neutral, yes}, yes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := some(slices.Values(tt.in)); got != tt.want {
				t.Errorf("some(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// verdictCounted yields in, and counts in *n how many it yielded.
func verdictCounted(in []verdict, n *int) iter.Seq[verdict] {
	return func(yield func(verdict) bool) {
		for _, v := range in {
			*n++
			if !yield(v) {
				return
			}
		}
	}
}

func TestVerdictStopsEarly(t *testing.T) {
	t.Parallel()
	in := []verdict{neutral, yes, no, yes, no}

	var n int
	all(verdictCounted(in, &n))
	if n != 3 {
		t.Errorf("all took %d verdicts, want 3 (up to the first no)", n)
	}
	n = 0
	some(verdictCounted(in, &n))
	if n != 2 {
		t.Errorf("some took %d verdicts, want 2 (up to the first yes)", n)
	}
}

func TestVerdictOr(t *testing.T) {
	t.Parallel()
	tests := []struct {
		first, next, want verdict
	}{
		{yes, no, yes},
		{no, yes, yes},
		{no, neutral, no},
		{neutral, no, no},
		{neutral, yes, yes},
		{neutral, neutral, neutral},
		{no, no, no},
	}
	for _, tt := range tests {
		called := false
		got := tt.first.or(func() verdict {
			called = true
			return tt.next
		})
		if got != tt.want {
			t.Errorf("%v.or(%v) = %v, want %v", tt.first, tt.next, got, tt.want)
		}
		if tt.first == yes && called {
			t.Errorf("yes.or called next")
		}
	}
}

func TestVerdictOf(t *testing.T) {
	t.Parallel()
	if verdictOf(true) != yes || verdictOf(false) != no {
		t.Errorf("verdictOf(true), verdictOf(false) = %v, %v, want yes, no", verdictOf(true), verdictOf(false))
	}
}

func TestVerdictsOf(t *testing.T) {
	t.Parallel()
	var traced []int
	trace := func(i int) verdict {
		traced = append(traced, i)
		if i == 2 {
			return no
		}
		return yes
	}
	if got := all(verdictsOf([]int{1, 2, 3}, trace)); got != no {
		t.Errorf("all = %v, want no", got)
	}
	if !slices.Equal(traced, []int{1, 2}) {
		t.Errorf("traced %v, want [1 2]: the values after the first no are not traced", traced)
	}
}
