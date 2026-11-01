package ssaflow

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

const calleeSrc = `package p

type T struct{}

func (*T) M(int) {}

type U struct{ *T }

func g() {}

func f(t *T, u U, pu *U, h func(*T, int)) {
	t.M(1)
	m := t.M
	m(2)
	(*T).M(t, 3)
	U.M(u, 4)
	(*U).M(pu, 5)
	g()
	h(t, 6)
	func() {}()
}
`

// calleeCalls builds f of calleeSrc in SSA, and returns it with its calls,
// in order.
func calleeCalls(t *testing.T) (*ssa.Function, []*ssa.Call) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", calleeSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg := types.NewPackage("p", "p")
	ssaPkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset, pkg, []*ast.File{file}, 0)
	if err != nil {
		t.Fatal(err)
	}
	fn := ssaPkg.Func("f")
	var calls []*ssa.Call
	for instr := range InstrsIn(fn) {
		if call, ok := instr.(*ssa.Call); ok {
			calls = append(calls, call)
		}
	}
	return fn, calls
}

func TestCalleeOf(t *testing.T) {
	t.Parallel()
	fn, calls := calleeCalls(t)
	tp, up, pup := fn.Params[0], fn.Params[1], fn.Params[2]
	if len(calls) != 8 {
		t.Fatalf("got %d calls in f, want 8", len(calls))
	}
	mc := calls[1].Call.Value.(*ssa.MakeClosure)
	bound := mc.Bindings[0]
	for _, tt := range []struct {
		name     string
		call     *ssa.CallCommon
		ok       bool
		fn       string
		recv     ssa.Value
		embedded []int
	}{
		{"a method", &calls[0].Call, true, "M", tp, nil},
		{"a method value", &calls[1].Call, true, "M", bound, nil},
		{"a method expression", &calls[2].Call, true, "M", tp, nil},
		{"a promoted method expression", &calls[3].Call, true, "M", up, []int{0}},
		{"a promoted method expression on a pointer", &calls[4].Call, true, "M", pup, []int{0}},
		{"a function", &calls[5].Call, true, "g", nil, nil},
		{"a func value", &calls[6].Call, false, "", nil, nil},
		{"a func literal", &calls[7].Call, false, "", nil, nil},
		{"a method value with no binding", &ssa.CallCommon{Value: &ssa.MakeClosure{Fn: mc.Fn}}, false, "", nil, nil},
		{"a method with no receiver passed", &ssa.CallCommon{Value: calls[0].Call.Value}, false, "", nil, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, ok := CalleeOf(tt.call)
			if ok != tt.ok {
				t.Fatalf("CalleeOf(%v) ok = %v, want %v", tt.call, ok, tt.ok)
			}
			if !ok {
				return
			}
			if c.Func.Name() != tt.fn || c.Recv != tt.recv || !slices.Equal(c.Embedded, tt.embedded) {
				t.Errorf("CalleeOf(%v) = (%s, %v, %v), want (%s, %v, %v)",
					tt.call, c.Func.Name(), c.Recv, c.Embedded, tt.fn, tt.recv, tt.embedded)
			}
		})
	}
}

func TestCalleeEmbeddedStart(t *testing.T) {
	t.Parallel()
	fn, calls := calleeCalls(t)
	byValue, _ := CalleeOf(&calls[3].Call)
	if _, _, _, ok := byValue.EmbeddedStart(calls[3]); ok {
		t.Error("EmbeddedStart of a struct parameter by value: ok, want not, since it is not loaded")
	}
	byPointer, _ := CalleeOf(&calls[4].Call)
	addr, typ, read, ok := byPointer.EmbeddedStart(calls[4])
	if !ok || addr != fn.Params[2] || read != calls[4] || typ.String() != "p.U" {
		t.Errorf("EmbeddedStart of a pointer = (%v, %v, %v, %v), want (pu, p.U, the call, true)", addr, typ, read, ok)
	}
}
