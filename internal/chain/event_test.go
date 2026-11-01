package chain

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/mpyw/zerologlintctx/internal/ssaflow"
	"github.com/mpyw/zerologlintctx/internal/zerologapi"
)

// eventStubImporter imports the zerolog stub of testdata, type-checked once
// for each test, and the standard library.
type eventStubImporter struct {
	zerolog *types.Package
	std     types.Importer
}

func (im eventStubImporter) Import(path string) (*types.Package, error) {
	if path == "github.com/rs/zerolog" {
		return im.zerolog, nil
	}
	return im.std.Import(path)
}

// eventFunc builds f of src, a package p that imports the zerolog stub, in
// SSA.
func eventFunc(t *testing.T, src string) *ssa.Function {
	t.Helper()
	fset := token.NewFileSet()
	stubSrc, err := os.ReadFile("../../testdata/src/github.com/rs/zerolog/zerolog.go")
	if err != nil {
		t.Fatal(err)
	}
	stub, err := parser.ParseFile(fset, "zerolog.go", stubSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	std := importer.Default()
	zl, err := (&types.Config{Importer: std}).Check("github.com/rs/zerolog", fset, []*ast.File{stub}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &types.Config{Importer: eventStubImporter{zerolog: zl, std: std}}
	pkg, _, err := ssautil.BuildPackage(cfg, fset, types.NewPackage("p", "p"), []*ast.File{f}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return pkg.Func("f")
}

// eventQueryWork asks eventCtxSetInPlace of each terminator of fn, and
// returns how many it accepts, and the work it took.
func eventQueryWork(fn *ssa.Function) (accepted, terminators, work int) {
	ix := newEventIndex(make(ssaflow.Positions), ssaflow.NewClosures())
	noCtx := func(ssa.Value) bool { return false }
	for instr := range ssaflow.InstrsIn(fn) {
		call, ok := instr.(*ssa.Call)
		if !ok {
			continue
		}
		c, ok := calleeOf(&call.Call, ix.closures)
		if !ok || c.api.Kind != zerologapi.KindTerminator {
			continue
		}
		terminators++
		if ix.eventCtxSetInPlace(c, call, false, noCtx) {
			accepted++
		}
	}
	return accepted, terminators, ix.work
}

// eventShapeSrc writes a function f of the shape named, repeated n times.
func eventShapeSrc(shape string, n int) string {
	var b strings.Builder
	b.WriteString(`package p

import (
	"context"

	"github.com/rs/zerolog"
)

type holder struct{ e *zerolog.Event }

var sink bool

func f(ctx context.Context, l zerolog.Logger) {
`)
	switch shape {
	case "fieldphi":
		b.WriteString("\th := &holder{e: l.Info()}\n")
		for range n {
			b.WriteString("\tif sink {\n\t\th.e = l.Warn()\n\t}\n\th.e.Ctx(ctx)\n\th.e.Msg(\"x\")\n")
		}
	case "fieldphiloop":
		// fieldphi in a loop, so that each event is made on each iteration
		b.WriteString("\tfor sink {\n\th := &holder{e: l.Info()}\n")
		for range n {
			b.WriteString("\tif sink {\n\t\th.e = l.Warn()\n\t}\n\th.e.Ctx(ctx)\n\th.e.Msg(\"x\")\n")
		}
		b.WriteString("\t}\n")
	case "onevmany":
		// One Ctx call on another event, so that the function has one
		b.WriteString("\tl.Debug().Ctx(ctx).Msg(\"y\")\n\te := l.Info()\n")
		for range n {
			b.WriteString("\tif sink {\n\t\te = l.Warn()\n\t}\n\tif sink {\n\t\te.Msg(\"x\")\n\t}\n")
		}
	}
	b.WriteString("}\n")
	return b.String()
}

func TestEventQueryWorkIsQuadratic(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		shape string
		// extra is the terminators but the n repeated
		extra int
		// accepts tells how many terminators are accepted
		accepts func(n int) int
	}{
		{"fieldphi", 0, func(n int) int { return n }},
		{"fieldphiloop", 0, func(n int) int { return n }},
		{"onevmany", 1, func(int) int { return 1 }}, // The one with Ctx in its chain
	} {
		t.Run(tt.shape, func(t *testing.T) {
			t.Parallel()
			const small, large = 40, 160
			works := make(map[int]int)
			for _, n := range []int{small, large} {
				accepted, terminators, work := eventQueryWork(eventFunc(t, eventShapeSrc(tt.shape, n)))
				if terminators != n+tt.extra || accepted != tt.accepts(n) {
					t.Fatalf("n = %d: %d of %d terminators accepted, want %d", n, accepted, terminators, tt.accepts(n))
				}
				works[n] = work
			}
			// n four times as large may take up to 16 times the work, and a
			// little more for what does not grow with n
			ratio := float64(works[large]) / float64(works[small])
			t.Logf("work: n = %d: %d, n = %d: %d (%.1fx)", small, works[small], large, works[large], ratio)
			if ratio > 20 {
				t.Errorf("work grew %.1fx for n %dx: %s", ratio, large/small, fmt.Sprint(works))
			}
		})
	}
}

func TestEventSourcesAddsOnce(t *testing.T) {
	t.Parallel()
	a := eventSource{v: ssa.NewConst(constant.MakeInt64(1), types.Typ[types.Int])}
	b := eventSource{v: ssa.NewConst(constant.MakeInt64(2), types.Typ[types.Int])}
	var s eventSources
	for _, src := range []eventSource{a, b, a} {
		s.add(src)
	}
	if len(s.list) != 2 || s.list[0] != a || s.list[1] != b || !s.has(a) || !s.has(b) {
		t.Errorf("sources %v, want [a b]", s.list)
	}
}

func TestEventCtxSetInPlaceOnNil(t *testing.T) {
	t.Parallel()
	// A nil event is no event, so the Ctx call of the function is not on it
	accepted, terminators, _ := eventQueryWork(eventFunc(t, `package p

import (
	"context"

	"github.com/rs/zerolog"
)

func f(ctx context.Context, l zerolog.Logger) {
	l.Info().Ctx(ctx).Msg("y")
	var e *zerolog.Event
	e.Msg("x")
}
`))
	if terminators != 2 || accepted != 1 {
		t.Errorf("%d of %d terminators accepted, want 1 of 2", accepted, terminators)
	}
}

func TestEventRunGraphOutOfFunction(t *testing.T) {
	t.Parallel()
	fn := eventFunc(t, eventShapeSrc("fieldphiloop", 1))
	ix := newEventIndex(make(ssaflow.Positions), ssaflow.NewClosures())
	instr := fn.Blocks[0].Instrs[0]
	// With no function, every instruction is out of it
	g := &eventRunGraph{ix: ix}
	if p := g.pointBefore(instr); p != (ssaflow.WalkPoint{}) {
		t.Errorf("pointBefore = %v, want none", p)
	}
	if p := g.pointAfter(instr); p != nil {
		t.Errorf("pointAfter = %v, want nil", *p)
	}
	if !g.reachesFrom(ssaflow.WalkPoint{}, ssaflow.WalkPointBefore(instr, ix.positions), instr) {
		t.Error("reachesFrom none = false, want true")
	}
}
