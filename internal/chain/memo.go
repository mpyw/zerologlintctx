// memo.go keeps the verdict of each value traced, and marks the values whose
// trace is in progress, which a cycle reaches again.

package chain

import (
	"math"

	"golang.org/x/tools/go/ssa"
)

// traceMemo keeps the verdict of each value traced, per trace state, and the
// stack of the traces in progress.
//
// A value reached again while its trace is in progress is neutral: the edge
// that leads back to it, as an update in a loop does, adds no source that
// the trace in progress does not see on its own.
//
// A verdict found with such a value taken as neutral holds only while that
// value is in progress, since its own verdict is not known yet. It is kept
// with the frame it depends on, and used again only while that frame is on
// the stack: a loop of n Phis is then traced once, not once per path. Once
// the frame is done, the verdict is traced again the next time it is asked.
// A verdict that depends on no frame below its own holds for good.
//
//declscope:shared // tracing.go keeps one per tracer
type traceMemo struct {
	// done holds the verdict of each key traced, and marks each key in
	// progress.
	//
	//declscope:private
	done map[memoKey]memoEntry
	// frames holds the serial number of the frame at each depth of the
	// stack, which tells a frame apart from a later one at the same depth.
	//
	//declscope:private
	frames []uint64
	// serial is the serial number of the last frame pushed.
	//
	//declscope:private
	serial uint64
	// low is the lowest depth of a frame in progress that the frame on top
	// depends on so far, or math.MaxInt for none.
	//
	//declscope:private
	low int
}

// newTraceMemo creates a memo with nothing traced yet.
//
//declscope:shared // tracing.go creates one per tracer
func newTraceMemo() traceMemo {
	return traceMemo{done: make(map[memoKey]memoEntry), low: math.MaxInt}
}

// trace returns the verdict of key, which trace finds, unless key is in
// progress, which makes it neutral, or a verdict kept for it still holds.
// The verdict is kept only when keep is true.
//
//declscope:shared // tracing.go traces each value through it
func (m *traceMemo) trace(key memoKey, keep bool, trace func() verdict) verdict {
	if e, ok := m.done[key]; ok {
		switch {
		case e.active:
			m.low = min(m.low, e.depth)
			return neutral
		case e.depth < 0:
			return e.verdict
		case m.holds(e):
			m.low = min(m.low, e.depth)
			return e.verdict
		}
	}

	depth := len(m.frames)
	m.serial++
	m.frames = append(m.frames, m.serial)
	m.done[key] = memoEntry{active: true, depth: depth}
	outer := m.low
	m.low = math.MaxInt

	v := trace()

	m.frames = m.frames[:depth]
	low := m.low
	if low >= depth {
		low = math.MaxInt // It depends on itself at most, which is done now
	}
	switch {
	case !keep:
		delete(m.done, key)
	case low == math.MaxInt:
		m.done[key] = memoEntry{verdict: v, depth: -1}
	default:
		m.done[key] = memoEntry{verdict: v, depth: low, serial: m.frames[low]}
	}
	m.low = min(outer, low)
	return v
}

// holds reports whether e, which depends on a frame, still holds: the frame
// is still on the stack.
func (m *traceMemo) holds(e memoEntry) bool {
	return e.depth < len(m.frames) && m.frames[e.depth] == e.serial
}

// memoKey is what a trace is kept for: a value in a trace state, or, when
// param is true, the value of a parameter as the calls of its func literal
// pass it.
//
//declscope:shared // tracing.go keys each trace
type memoKey struct {
	v     ssa.Value
	st    traceState
	param bool
}

// memoEntry is a verdict kept, with the frame it depends on: its depth, or
// -1 for none, and its serial number. For a key in progress, active is true,
// and depth is the depth of its own frame.
type memoEntry struct {
	verdict verdict
	active  bool
	depth   int
	serial  uint64
}
